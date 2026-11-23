package vm

import (
	"context"
	"encoding/json"
	"fmt"

	"gowarp/core/project"
)

type procedureParameters map[string]calculationValue

type procedure struct {
	entry      *executionBlock
	native     *nativeProcedure
	regions    map[string]*nativeRegion
	inputs     map[*project.Block]nativeInput
	body       string
	ids, names []string
	defaults   []any
	warp       bool
}

type procedureArgument struct {
	id       string
	present  bool
	constant bool
	value    any
	plan     *numericExpression
}

type procedureInputs struct {
	source      *project.Target
	procedure   *procedure
	code        string
	arguments   []procedureArgument
	synchronous bool
	native      nativeInput
}

func mutationText(b *project.Block, key string) (string, error) {
	raw := b.Mutation[key]
	if len(raw) == 0 {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("invalid procedure %s: %w", key, err)
	}
	return value, nil
}

func procedureStrings(b *project.Block, key string) ([]string, error) {
	value, err := mutationText(b, key)
	if err != nil {
		return nil, err
	}
	var values []string
	if err := json.Unmarshal([]byte(value), &values); err != nil || values == nil {
		return nil, fmt.Errorf("invalid procedure %s", key)
	}
	return values, nil
}

/*
Project blocks are immutable during execution. Key metadata by the original
block so repeated calls do not hash their JSON strings again.
*/
func (r *Runtime) procedureCode(b *project.Block) (string, error) {
	if code, ok := r.procedureCodeCache[b]; ok {
		return code, nil
	}
	code, err := mutationText(b, "proccode")
	if err != nil {
		return "", err
	}
	if r.procedureCodeCache == nil {
		r.procedureCodeCache = make(map[*project.Block]string)
	}
	r.procedureCodeCache[b] = code
	return code, nil
}

func (r *Runtime) procedureArguments(t *target, b *project.Block, defaults []string) (*procedureInputs, error) {
	if args, ok := r.procedureArgumentCache[b]; ok && args.source == t.source {
		return args, nil
	}
	ids := defaults
	if len(b.Mutation["argumentids"]) > 0 {
		var err error
		ids, err = procedureStrings(b, "argumentids")
		if err != nil {
			return nil, err
		}
	}
	args := make([]procedureArgument, len(ids))
	synchronous, blocks := true, 0
	seen := make(map[string]bool, len(ids))
	for i, id := range ids {
		// Repeated IDs share one input evaluation, including random list reads.
		if seen[id] {
			synchronous = false
		}
		seen[id] = true
		in, present := b.Inputs[id]
		args[i] = procedureArgument{id: id, present: present}
		if len(in) < 2 {
			if present {
				synchronous = false
			}
			continue
		}
		value := in[1]
		if value == nil && len(in) > 2 {
			value = in[2]
		}
		args[i].value, args[i].constant = constantInput(value)
		if !args[i].constant {
			args[i].plan = r.numericInput(t, b, id, 0)
			if args[i].plan == nil {
				synchronous = false
			} else {
				blocks += args[i].plan.blocks
			}
		}
	}
	prepared := &procedureInputs{source: t.source, arguments: args, synchronous: synchronous && blocks <= stepsPerFrame}
	if r.procedureArgumentCache == nil {
		r.procedureArgumentCache = make(map[*project.Block]*procedureInputs)
	}
	r.procedureArgumentCache[b] = prepared
	return prepared, nil
}

func prepareProcedures(t *target) error {
	if t.procedures != nil {
		return nil
	}
	procs := map[string]*procedure{}
	for _, b := range t.source.Blocks {
		if !b.TopLevel || b.Opcode != "procedures_definition" {
			continue
		}
		id, err := substack(b, "custom_block")
		if err != nil {
			return err
		}
		proto := t.source.Blocks[id]
		if proto == nil || proto.Opcode != "procedures_prototype" {
			return fmt.Errorf("missing procedure prototype %q", id)
		}
		code, err := mutationText(proto, "proccode")
		if err != nil {
			return err
		}
		if code == "" {
			return fmt.Errorf("missing procedure code")
		}
		if _, exists := procs[code]; exists {
			return fmt.Errorf("duplicate procedure %q", code)
		}
		p := &procedure{body: b.Next}
		p.ids, err = procedureStrings(proto, "argumentids")
		if err != nil {
			return err
		}
		p.names, err = procedureStrings(proto, "argumentnames")
		if err != nil {
			return err
		}
		defaults, err := mutationText(proto, "argumentdefaults")
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(defaults), &p.defaults); err != nil || p.defaults == nil {
			return fmt.Errorf("invalid defaults for procedure %q", code)
		}
		/*
			Scratch exports can retain defaults for arguments removed from the
			prototype. Only defaults belonging to active inputs are used.
		*/
		if len(p.ids) != len(p.names) || len(p.ids) > len(p.defaults) {
			return fmt.Errorf("argument count mismatch for procedure %q", code)
		}
		for _, value := range p.defaults {
			switch value.(type) {
			case string, float64, bool:
			default:
				return fmt.Errorf("invalid default for procedure %q", code)
			}
		}
		if raw := proto.Mutation["warp"]; len(raw) > 0 {
			if err := json.Unmarshal(raw, &p.warp); err != nil {
				value, err := mutationText(proto, "warp")
				if err != nil || value != "true" && value != "false" {
					return fmt.Errorf("invalid warp for procedure %q", code)
				}
				p.warp = value == "true"
			}
		}
		if t.owner != nil && t.owner.native {
			p.native = nativeProcedures[t.sourceIndex][code]
			p.regions = nativeRegions[t.sourceIndex][code]
		}
		procs[code] = p
	}
	if t.owner != nil && t.owner.native {
		for id, run := range nativeCallInputs[t.sourceIndex] {
			b := t.source.Blocks[id]
			if b == nil {
				continue
			}
			code, err := mutationText(b, "proccode")
			p := procs[code]
			if err != nil || p == nil {
				continue
			}
			if p.inputs == nil {
				p.inputs = make(map[*project.Block]nativeInput)
			}
			p.inputs[b] = run
		}
	}
	t.procedures = procs
	return nil
}

func (th *thread) takeParams(size int) procedureParameters {
	if size == 0 {
		return nil
	}
	if th.target != nil && th.target.owner != nil {
		pool := &th.target.owner.freeParams
		for i, params := range pool {
			if params != nil {
				pool[i] = nil
				return params
			}
		}
	}
	return make(procedureParameters, size)
}

/*
Keep a small number of argument tables per runtime. Active and suspended calls
own their tables until they return; released values must not stay reachable.
*/
func (th *thread) releaseParams(params procedureParameters) {
	if params == nil || len(params) > 64 {
		return
	}
	clear(params)
	if th.target == nil || th.target.owner == nil {
		return
	}
	pool := &th.target.owner.freeParams
	for i := range pool {
		if pool[i] == nil {
			pool[i] = params
			return
		}
	}
}

func (p *procedure) canRunNative(r *Runtime, th *thread, reporter bool) bool {
	return p.native != nil && !reporter && th.reporterSteps == nil && th.sliceSteps != nil &&
		r.listObserver == nil && (th.warp || p.warp) && len(th.stack)+p.native.depth < 1024 &&
		p.native.work <= th.sliceLimit-*th.sliceSteps
}

// Only synchronous inputs use this entry; suspended calls keep their named parameter tables.
func (r *Runtime) callNativeValues(ctx context.Context, th *thread, prepared *procedureInputs) error {
	p := prepared.procedure
	values := r.nativeArguments
	r.nativeArguments = nil
	if cap(values) < len(p.names) {
		values = make([]calculationValue, len(p.names))
	} else {
		values = values[:len(p.names)]
	}
	defer func() {
		clear(values)
		if cap(values) <= 64 && cap(values) > cap(r.nativeArguments) {
			r.nativeArguments = values[:0]
		}
	}()
	budget := stepsPerFrame
	for i := range p.names {
		value := calculationValue{value: p.defaults[i]}
		if i < len(prepared.arguments) {
			arg := &prepared.arguments[i]
			if arg.present {
				if arg.constant {
					value.value = arg.value
				} else {
					var err error
					value, err = arg.plan.readTyped(ctx, r, th, &budget)
					if err != nil {
						return err
					}
				}
			}
		}
		values[i] = value
	}
	err := p.native.runValues(ctx, r, th, values)
	if err == nil {
		*th.sliceSteps++
		th.result = ""
	}
	return err
}

func (r *Runtime) call(ctx context.Context, th *thread, b *project.Block, input func(string) (any, error), reporter bool) (bool, error) {
	prepared := r.procedureArgumentCache[b]
	if prepared == nil || prepared.source != th.target.source {
		if err := prepareProcedures(th.target); err != nil {
			return false, err
		}
		code, err := r.procedureCode(b)
		if err != nil {
			return false, err
		}
		p := th.target.procedures[code]
		if p == nil {
			if !reporter {
				if yield, handled, err := r.debugCall(th, b, code, input); handled {
					return yield, err
				}
			}
			/*
				Scratch permits calls copied without their definition. Like TurboWarp,
				skip these calls instead of stopping the entire project.
			*/
			return false, nil
		}
		prepared, err = r.procedureArguments(th.target, b, p.ids)
		if err != nil {
			return false, err
		}
		/*
			Definitions are immutable and shared by clones; only argument values
			are evaluated afresh. Avoid hashing the procedure's name on every call.
		*/
		prepared.procedure, prepared.code = p, code
		if r.native && prepared.synchronous {
			prepared.native = p.inputs[b]
		}
	}
	p, code := prepared.procedure, prepared.code
	var err error
	args := prepared.arguments
	if len(args) > len(p.names) {
		return false, fmt.Errorf("too many arguments for procedure %q", code)
	}
	if prepared.synchronous && p.canRunNative(r, th, reporter) {
		return false, r.callNativeValues(ctx, th, prepared)
	}
	params := th.takeParams(len(p.names))
	if r.native && prepared.native != nil && !reporter && th.reporterSteps == nil && r.listObserver == nil && (th.warp || p.warp) {
		err = prepared.native(ctx, r, th, params)
		if err != nil {
			th.releaseParams(params)
			return false, err
		}
	} else {
		budget := stepsPerFrame
		for i, name := range p.names {
			value := calculationValue{value: p.defaults[i]}
			if i < len(args) && args[i].present {
				if args[i].constant {
					value.value = args[i].value
				} else {
					/*
						Bypass continuations only when every command argument is synchronous. Reporter calls retain shared
						depth/budget accounting.
					*/
					if prepared.synchronous && !reporter {
						value, err = args[i].plan.readTyped(ctx, r, th, &budget)
					} else {
						value.value, err = input(args[i].id)
					}
					if err != nil {
						th.releaseParams(params)
						return false, err
					}
				}
			}
			// Duplicate argument names use the final matching argument.
			params[name] = value
		}
	}
	if len(th.stack) >= 1024 {
		th.releaseParams(params)
		return false, fmt.Errorf("procedure nesting limit exceeded")
	}
	if p.canRunNative(r, th, reporter) {
		previous := th.params
		th.params = params
		err := p.native.run(ctx, r, th)
		th.params = previous
		th.releaseParams(params)
		if err == nil {
			*th.sliceSteps++
			th.result = ""
		}
		return false, err
	}
	recursive := th.procedure == code
	for i, seen := len(th.stack)-1, 0; !recursive && i >= 0 && seen < 6; i, seen = i-1, seen+1 {
		c := th.stack[i]
		if c.call && c.procedure == code {
			recursive = true
		}
	}
	nextLink := th.cursor
	if nextLink != nil && nextLink.id != b.Next {
		nextLink = nil
	}
	th.stack = append(th.stack, continuation{
		nextLink:  nextLink,
		next:      b.Next,
		call:      true,
		params:    th.params,
		warp:      th.warp,
		procedure: th.procedure,
		regions:   th.regions,
	})
	th.params, th.warp, th.procedure = params, th.warp || p.warp, code
	th.regions = p.regions
	if p.entry == nil && p.body != "" {
		p.entry = r.executionCode(th.target.source).block(p.body)
	}
	th.pc, th.cursor = p.body, p.entry
	return recursive && !th.warp, nil
}

/*
Scratch's stop-this-script block returns from the innermost custom block.
Control continuations inside that call are discarded along with its body.
*/
func (th *thread) stopScript() {
	for i := len(th.stack) - 1; i >= 0; i-- {
		c := th.stack[i]
		if c.call {
			th.releaseParams(th.params)
			th.params, th.warp, th.procedure = c.params, c.warp, c.procedure
			th.regions = c.regions
			th.pc, th.cursor = c.next, c.nextLink
			th.stack[i].params = nil
			th.stack = th.stack[:i]
			return
		}
	}
	th.done = true
}

/*
Reporter bodies share the caller's thread and retain completed inputs, preserving wait and stop-
script semantics.
*/
func (r *Runtime) callReporter(ctx context.Context, th *thread, b *project.Block, input func(string) (any, error)) (any, error) {
	if len(th.reporters) >= 128 {
		return nil, fmt.Errorf("reporter procedure nesting limit exceeded")
	}
	expression := th.expression
	if expression == nil || expression.current == nil {
		return nil, fmt.Errorf("reporter procedure requires an expression continuation")
	}
	frame := &reporterFrame{
		base:       len(th.stack),
		expression: expression,
		input:      expression.current,
		warp:       th.warp,
		result:     th.result,
	}
	/*
		Arguments may themselves suspend. Do not change the caller until every
		argument has completed and the procedure frame has actually been pushed.
	*/
	if _, err := r.call(ctx, th, b, input, true); err != nil {
		return nil, err
	}
	if len(th.stack) == frame.base {
		// A missing definition did not push a procedure continuation.
		return "", nil
	}
	th.stack[len(th.stack)-1].next = expression.statement
	th.reporters = append(th.reporters, frame)
	th.reporterBoundary = &frame.base
	if th.reporterSteps == nil {
		th.reporterBudget = stepsPerFrame
		th.reporterSteps = &th.reporterBudget
	}
	th.warp = true
	th.expression = nil
	return nil, errReporterSuspend
}
