package vm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gowarp/core/extension"
	"gowarp/core/project"
)

var errExtensionYield = errors.New("native extension yielded")

type registeredBlock struct {
	hatPolls map[*target]map[string]*polledHat
	polling  bool
	owner    *extension.Extension
	block    extension.Block
	inputs   map[*project.Block]*extensionInputs
	freeCall *extensionCallStorage
}

type extensionCallStorage struct {
	context extension.Context
	args    map[string]any
	fields  map[string]string
	local   map[string]any
}

func (b *registeredBlock) borrowCall(p *extensionInputs) *extensionCallStorage {
	call := b.freeCall
	b.freeCall = nil
	if call == nil {
		call = &extensionCallStorage{
			args:   make(map[string]any, len(p.arguments)),
			fields: make(map[string]string, len(p.fields)),
			local:  make(map[string]any),
		}
	}
	return call
}

func (b *registeredBlock) releaseCall(call *extensionCallStorage) {
	call.context = extension.Context{}
	if b.freeCall != nil || len(call.args) > 64 || len(call.fields) > 64 || len(call.local) > 64 {
		return
	}
	clear(call.args)
	clear(call.fields)
	clear(call.local)
	b.freeCall = call
}

type extensionInputs struct {
	source      *project.Target
	arguments   []extensionArgument
	fields      []extensionField
	constant    bool
	synchronous bool
	blocks      int
	height      int
}

type extensionField struct {
	name, value string
}

type extensionArgument struct {
	name     string
	value    any
	constant bool
	plan     *numericExpression
}

func (b *registeredBlock) prepare(r *Runtime, th *thread, block *project.Block) *extensionInputs {
	var source *project.Target
	if th.target != nil {
		source = th.target.source
	}
	if prepared := b.inputs[block]; prepared != nil && prepared.source == source {
		return prepared
	}
	prepared := &extensionInputs{
		source:      source,
		synchronous: true,
		arguments:   make([]extensionArgument, len(b.block.Inputs)),
		fields:      make([]extensionField, 0, len(block.Fields)),
	}
	prepared.constant = true
	seen := make(map[string]bool, len(b.block.Inputs))
	for i, name := range b.block.Inputs {
		if seen[name] {
			prepared.synchronous = false
		}
		seen[name] = true
		arg := extensionArgument{name: name}
		if in := block.Inputs[name]; len(in) >= 2 {
			value := in[1]
			if value == nil && len(in) > 2 {
				value = in[2]
			}
			arg.value, arg.constant = constantInput(value)
			if !arg.constant && source != nil {
				arg.plan = r.numericInput(th.target, block, name, 0)
			}
		}
		if !arg.constant {
			prepared.constant = false
			if arg.plan == nil || arg.plan.height > 128 {
				prepared.synchronous = false
			} else {
				prepared.blocks += arg.plan.blocks
				prepared.height = max(prepared.height, arg.plan.height)
			}
		}
		prepared.arguments[i] = arg
	}
	prepared.synchronous = prepared.synchronous && prepared.blocks <= stepsPerFrame
	for name := range block.Fields {
		prepared.fields = append(prepared.fields, extensionField{name: name, value: field(block, name, 0)})
	}
	if b.inputs == nil {
		b.inputs = make(map[*project.Block]*extensionInputs)
	}
	b.inputs[block] = prepared
	return prepared
}

type extensionInvocation struct {
	block      *project.Block
	context    *extension.Context
	finished   bool
	value      any
	reevaluate bool
}

func (p *extensionInputs) readInputs(input func(string) (any, error), args map[string]any) (map[string]any, error) {
	var buffer [16]any
	values := buffer[:]
	if len(p.arguments) > len(buffer) {
		values = make([]any, len(p.arguments))
	}
	for i, arg := range p.arguments {
		value := arg.value
		if !arg.constant {
			var err error
			value, err = input(arg.name)
			if err != nil {
				return nil, err
			}
		}
		values[i] = value
	}
	if args == nil {
		args = make(map[string]any, len(p.arguments))
	}
	for i, arg := range p.arguments {
		args[arg.name] = values[i]
	}
	return args, nil
}

type extensionContext struct {
	r                 *Runtime
	th                *thread
	event             string
	readDeletedTarget bool
}

// SetExtensions installs a compiled bundle before execution.
func (r *Runtime) SetExtensions(bundle extension.Bundle) error {
	if r.frame != 0 || len(r.extensions) != 0 {
		return fmt.Errorf("extensions must be installed once before execution")
	}
	instances := make([]*extension.Extension, 0, len(bundle))
	blocks := make(map[string]*registeredBlock)
	ids := make(map[string]bool)
	for _, factory := range bundle {
		if factory == nil {
			return fmt.Errorf("nil extension factory")
		}
		def := factory()
		if def.ID == "" || strings.Contains(def.ID, "_") || extension.IsBuiltin(def.ID) || ids[def.ID] {
			return fmt.Errorf("invalid or duplicate extension ID %q", def.ID)
		}
		ids[def.ID] = true
		// Copy state slots per runtime; factories still own nested values.
		state := make(map[string]any, len(def.State))
		for key, value := range def.State {
			state[key] = value
		}
		def.State = state
		instance := &def
		instances = append(instances, instance)
		for _, block := range def.Blocks {
			if !strings.HasPrefix(block.Opcode, def.ID+"_") {
				return fmt.Errorf("extension %s: invalid opcode %q", def.ID, block.Opcode)
			}
			if _, duplicate := blocks[block.Opcode]; duplicate {
				return fmt.Errorf("duplicate extension opcode %q", block.Opcode)
			}
			switch block.Kind {
			case "command", "reporter", "boolean", "menu", "branch", "terminal":
				if block.Handler == nil {
					return fmt.Errorf("extension %s: missing handler for %s", def.ID, block.Opcode)
				}
			case "hat":
			default:
				return fmt.Errorf("extension %s: invalid block kind %q", def.ID, block.Kind)
			}
			blocks[block.Opcode] = &registeredBlock{owner: instance, block: block}
		}
	}
	if r.project != nil {
		p, err := extension.PrepareProject(r.sourceProject, instances)
		if err != nil {
			return err
		}
		for _, id := range p.Extensions {
			if id != "pen" && !ids[id] {
				return fmt.Errorf("extension %s has no compiled Go module; convert the project and package it again", id)
			}
		}
		for _, target := range p.Targets {
			for _, block := range target.Blocks {
				id, _, _ := strings.Cut(block.Opcode, "_")
				if ids[id] || (p != r.project && !extension.IsBuiltin(id)) {
					if _, ok := blocks[block.Opcode]; !ok {
						return fmt.Errorf("extension %s does not register %s", id, block.Opcode)
					}
				}
			}
		}
		if p != r.project {
			r.project = p
			r.native = nativeProjectKey != "" && nativeKey(p) == nativeProjectKey
			r.threads = nil
			for _, target := range r.targets {
				target.source = p.Targets[target.sourceIndex]
				target.procedures, target.hatThreads = nil, nil
				target.refreshHats()
			}
			r.refreshNativeVariables()
			for i := len(r.executable) - 1; i >= 0; i-- {
				r.startHatsFor(r.executable[i], "event_whenflagclicked", "", true)
			}
		}
	}
	r.extensions, r.extensionBlocks = instances, blocks
	if err := r.extensionEvent("load", r.stage); err != nil {
		return err
	}
	return r.extensionEvent("flag", r.stage)
}

func (r *Runtime) invokeExtension(ctx context.Context, th *thread, block *project.Block, input func(string) (any, error), reporter bool, depth int, budget *int) (extension.Result, bool, error) {
	if len(r.extensionBlocks) == 0 {
		return extension.Result{}, false, nil
	}
	id, _, _ := strings.Cut(block.Opcode, "_")
	if extension.IsBuiltin(id) {
		return extension.Result{}, false, nil
	}
	registered, ok := r.extensionBlocks[block.Opcode]
	if !ok {
		return extension.Result{}, false, nil
	}
	kind := registered.block.Kind
	if reporter != (kind == "reporter" || kind == "boolean" || kind == "menu") {
		return extension.Result{}, true, fmt.Errorf("extension block %s used in an incompatible position", block.Opcode)
	}
	var slot **extensionInvocation
	if reporter && th.expression != nil {
		slot = &th.expression.current.native
	} else {
		slot = &th.extensionCall
	}
	invocation := *slot
	var call *extension.Context
	if invocation != nil && invocation.block == block {
		if invocation.finished {
			*slot = nil
			return extension.Result{Value: invocation.value}, true, nil
		}
		call = invocation.context
		if invocation.reevaluate {
			args, err := registered.prepare(r, th, block).readInputs(input, call.Args)
			if err != nil {
				return extension.Result{}, true, err
			}
			call.Args = args
			invocation.reevaluate = false
		}
	} else {
		invocation = nil
		prepared := registered.prepare(r, th, block)
		synchronous := prepared.synchronous && th.reporterSteps == nil &&
			prepared.blocks <= *budget && depth+prepared.height <= 128
		/*
			Complete every input before invoking user code. Suspended reporter
			inputs are cached by the existing expression continuation.
		*/
		if synchronous && !prepared.constant {
			if err := ctx.Err(); err != nil {
				return extension.Result{}, true, err
			}
		}
		var args map[string]any
		var storage *extensionCallStorage
		if registered.block.BorrowContext {
			storage = registered.borrowCall(prepared)
			defer registered.releaseCall(storage)
			args = storage.args
		}
		if synchronous {
			if args == nil {
				args = make(map[string]any, len(prepared.arguments))
			}
			for i := range prepared.arguments {
				arg := &prepared.arguments[i]
				value := arg.value
				if !arg.constant {
					if err := ctx.Err(); err != nil {
						return extension.Result{}, true, err
					}
					var err error
					value, err = arg.plan.readConditional(ctx, r, th, budget)
					if err != nil {
						return extension.Result{}, true, err
					}
				}
				args[arg.name] = value
			}
		} else {
			var err error
			args, err = prepared.readInputs(input, args)
			if err != nil {
				return extension.Result{}, true, err
			}
		}
		var fields map[string]string
		var local map[string]any
		if storage != nil {
			call, fields, local = &storage.context, storage.fields, storage.local
		} else {
			call = &extension.Context{}
			fields = make(map[string]string, len(prepared.fields))
			local = make(map[string]any)
		}
		for _, field := range prepared.fields {
			fields[field.name] = field.value
		}
		if th.extensionHost == nil {
			th.extensionHost = &extensionContext{r: r, th: th}
		}
		*call = extension.Context{
			Args:   args,
			Fields: fields,
			State:  registered.owner.State,
			Local:  local,
			Host:   th.extensionHost,
		}
	}
	result, err := registered.block.Handler(call)
	if err != nil {
		*slot = nil
		return extension.Result{}, true, fmt.Errorf("extension %s: %w", block.Opcode, err)
	}
	if registered.block.BorrowContext && (result.Again || result.Yield || result.YieldTick || result.Branch != "") {
		*slot = nil
		return extension.Result{}, true, fmt.Errorf("extension %s: borrowed context handler must finish without yielding or branching", block.Opcode)
	}
	if result.YieldTick {
		th.wakeFrame = r.frame + 1
	}
	resume := result.Again || reporter && (result.Yield || result.YieldTick)
	if invocation == nil && resume {
		invocation = &extensionInvocation{block: block, context: call}
	}
	if result.Again && result.ReevaluateInputs {
		invocation.reevaluate = true
		if th.expression != nil {
			th.expression.recycleInputs(th.expression.current)
		}
	}
	if result.Branch != "" {
		if reporter || kind != "branch" {
			return extension.Result{}, true, fmt.Errorf("only branch blocks may start a substack")
		}
		body, err := substack(block, result.Branch)
		if err != nil {
			return extension.Result{}, true, err
		}
		if len(th.stack) >= 1024 {
			return extension.Result{}, true, fmt.Errorf("extension branch nesting limit exceeded")
		}
		frame := continuation{next: block.Next}
		if result.Again {
			frame.next = th.expression.statement
			frame.extension = invocation
		}
		th.stack = append(th.stack, frame)
		th.pc = body
		*slot = nil
		result.Again = false
		return result, true, nil
	}
	if resume {
		if !result.Again {
			invocation.finished, invocation.value = true, result.Value
		}
		*slot = invocation
		if th.expression != nil {
			th.expression.resume = true
			th.pc = th.expression.statement
		}
		return result, true, errExtensionYield
	}
	*slot = nil
	if kind == "terminal" {
		th.done = true
	}
	return result, true, nil
}

func (r *Runtime) extensionEvent(event string, target *target) error {
	if event == "flag" {
		for i := range r.data.monitors {
			m := &r.data.monitors[i]
			if m.reporter != nil {
				m.thread = &thread{target: m.owner}
			}
		}
		for _, block := range r.extensionBlocks {
			block.hatPolls = nil
		}
	}
	if target == nil {
		target = r.stage
	}
	var failure error
	for _, instance := range r.extensions {
		handler := instance.Events[event]
		if handler == nil {
			continue
		}
		ctx := &extension.Context{
			State: instance.State,
			Local: make(map[string]any),
			Host: &extensionContext{
				r:     r,
				th:    &thread{target: target},
				event: event,
			},
		}
		result, err := handler(ctx)
		if err != nil {
			err = fmt.Errorf("extension %s event %s: %w", instance.ID, event, err)
		} else if result.Again || result.Yield || result.YieldTick {
			err = fmt.Errorf("extension %s: lifecycle event %s cannot yield; start a hat instead", instance.ID, event)
		}
		if err == nil {
			continue
		}
		if event != "dispose" {
			r.extensionErr = err
			return err
		}
		failure = errors.Join(failure, err)
	}
	if failure != nil {
		r.extensionErr = failure
	}
	return failure
}

func (r *Runtime) CloseExtensions() error {
	err := r.extensionEvent("dispose", r.stage)
	r.closeHostAPIs()
	r.extensions = nil
	r.extensionBlocks = nil
	return err
}
