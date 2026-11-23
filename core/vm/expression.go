package vm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gowarp/core/project"
)

var errReporterSuspend = errors.New("reporter expression suspended")

type inputRequest struct {
	block *project.Block
	name  string
	value any
}

type inputKey struct {
	block *project.Block
	name  string
}

type inputEvaluation struct {
	native   *extensionInvocation
	children *inputEvaluation
	sibling  *inputEvaluation
	key      inputKey
	value    any
	complete bool
	nextFree *inputEvaluation
}

type expressionState struct {
	statement string
	root      inputEvaluation
	current   *inputEvaluation
	resume    bool
	free      *inputEvaluation
}

type reporterFrame struct {
	base       int
	expression *expressionState
	input      *inputEvaluation
	warp       bool
	result     any
}

func newExpression(statement string) *expressionState {
	expression := &expressionState{statement: statement}
	expression.current = &expression.root
	return expression
}

func (e *expressionState) reset(statement string) {
	e.recycleInputs(&e.root)
	e.statement = statement
	e.current = &e.root
	e.resume = false
}

/*
Suspended expressions own their storage; completed inputs are recycled. Small linked child lists
avoid per-reporter maps.
*/
func (e *expressionState) recycleInputs(parent *inputEvaluation) {
	for child := parent.children; child != nil; {
		next := child.sibling
		e.recycleInputs(child)
		child.sibling = nil
		child.key = inputKey{}
		child.nextFree = e.free
		e.free = child
		child = next
	}
	parent.children = nil
	parent.value = nil
	parent.native = nil
	parent.complete = false
}

/*
Cache inputs by tree position, not block ID. Shared reporters still run per input; variable and
list primitives remain live.
*/
func constantInput(value any) (any, bool) {
	if value == nil {
		return float64(0), true
	}
	if literal, ok := value.([]any); ok && len(literal) >= 2 {
		if kind, ok := literal[0].(float64); ok {
			switch kind {
			case 4, 5, 6, 7, 8, 9, 10, 11:
				switch literal[1].(type) {
				case string, float64, bool:
					return literal[1], true
				}
			}
		}
	}
	return nil, false
}

/*
Cache the input wiring, never its evaluated value. All clones read their own
variables and arguments through the same immutable expression plan.
*/
type preparedInput struct {
	name     string
	next     *preparedInput
	value    any
	plan     *numericExpression
	reporter *project.Block
	constant bool
}

func (r *Runtime) prepareInput(th *thread, b *project.Block, name string) (*preparedInput, error) {
	for prepared := r.preparedInputs[b]; prepared != nil; prepared = prepared.next {
		if prepared.name == name {
			return prepared, nil
		}
	}
	in, ok := b.Inputs[name]
	if !ok {
		return nil, nil
	}
	if len(in) < 2 {
		return nil, fmt.Errorf("invalid input %q", name)
	}
	value := in[1]
	if value == nil && len(in) > 2 {
		value = in[2]
	}
	prepared := &preparedInput{name: name, value: value, next: r.preparedInputs[b]}
	if literal, ok := constantInput(value); ok {
		prepared.value, prepared.constant = literal, true
	} else if id, ok := value.(string); ok {
		prepared.reporter = th.target.source.Blocks[id]
		prepared.plan = r.numericExpression(th.target, prepared.reporter, 0)
	} else {
		prepared.plan = r.numericInput(th.target, b, name, 0)
	}
	if r.preparedInputs == nil {
		r.preparedInputs = make(map[*project.Block]*preparedInput)
	}
	r.preparedInputs[b] = prepared
	return prepared, nil
}

func (r *Runtime) statementInput(th *thread, instruction *executionBlock) *preparedInput {
	if instruction.ready&inputPlanReady == 0 {
		instruction.ready |= inputPlanReady
		b := instruction.source
		/*
			A single synchronous input cannot be replayed after another input
			suspends. Cache its wiring directly on the instruction, not its value.
		*/
		if len(b.Inputs) == 1 {
			for name := range b.Inputs {
				prepared, err := r.prepareInput(th, b, name)
				if err == nil && prepared != nil && (prepared.constant || prepared.plan != nil) {
					instruction.input = prepared
				}
			}
		}
	}
	return instruction.input
}

func (p *preparedInput) read(ctx context.Context, r *Runtime, th *thread, name string, depth int, budget *int) (any, error) {
	if plan := p.plan; plan != nil && plan.blocks <= *budget && depth+plan.height <= 128 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return plan.readConditional(ctx, r, th, budget)
	}
	/*
		Preserve the interpreter's errors and short-circuit budget accounting
		when the whole expression would exceed a limit, or a reporter can yield.
	*/
	if id, ok := p.value.(string); ok {
		return r.evalBlock(ctx, th, id, p.reporter, depth+1, budget)
	}
	return r.inputValue(ctx, th, p.value, name, depth, budget)
}

func (r *Runtime) input(ctx context.Context, th *thread, b *project.Block, name string, depth int, budget *int) (any, error) {
	prepared, err := r.prepareInput(th, b, name)
	if err != nil {
		return nil, err
	}
	if prepared == nil {
		return float64(0), nil
	}
	if prepared.constant {
		return prepared.value, nil
	}

	expression := th.expression
	if expression == nil {
		return prepared.read(ctx, r, th, name, depth, budget)
	}
	parent := expression.current
	key := inputKey{block: b, name: name}
	child := parent.children
	for child != nil && child.key != key {
		child = child.sibling
	}
	if child == nil {
		if expression.free != nil {
			child = expression.free
			expression.free = child.nextFree
			child.nextFree = nil
		} else {
			child = &inputEvaluation{}
		}
		child.key = key
		child.sibling = parent.children
		parent.children = child
	}
	if child.complete {
		return child.value, nil
	}
	expression.current = child
	defer func() {
		expression.current = parent
	}()
	value, err := prepared.read(ctx, r, th, name, depth, budget)
	if err == nil {
		child.value, child.complete = value, true
	}
	return value, err
}

/*
Reporter calls reuse the current tick until a real wait. Resume the suspended statement with
completed inputs.
*/
func (r *Runtime) step(ctx context.Context, th *thread) error {
	if r.Paused() || r.wakePending(th) {
		return nil
	}
	th.sliceExecuted = 0
	th.sliceSteps = &th.sliceExecuted
	th.sliceLimit = stepsPerFrame
	th.sliceStarted = time.Now()
	th.reporterBudget = stepsPerFrame
	if len(th.reporters) > 0 {
		th.reporterSteps = &th.reporterBudget
	}
	for th.canStep() {
		err := r.stepBody(ctx, th)
		if err == errExtensionYield {
			if th.warp && th.reporterSteps == nil && !r.wakePending(th) {
				continue
			}
			return nil
		}
		if err == errReporterSuspend {
			continue
		}
		if err != nil || th.done || r.stopped {
			return err
		}
		if len(th.reporters) == 0 {
			return nil
		}
		frame := th.reporters[len(th.reporters)-1]
		if len(th.stack) > frame.base {
			return nil
		}
		value := th.result
		if value == nil {
			value = ""
		}
		frame.input.value, frame.input.complete = value, true
		th.expression = frame.expression
		th.expression.resume = true
		th.pc = th.expression.statement
		th.warp, th.result = frame.warp, frame.result
		th.reporters = th.reporters[:len(th.reporters)-1]
		th.reporterBoundary = nil
		if len(th.reporters) > 0 {
			th.reporterBoundary = &th.reporters[len(th.reporters)-1].base
		} else {
			th.reporterSteps = nil
		}
	}
	return nil
}

func (th *thread) canStep() bool {
	if *th.sliceSteps < stepsPerFrame {
		return true
	}
	if !th.warp && th.pc == "" && (len(th.stack) == 0 || th.stack[len(th.stack)-1].call) {
		return true
	}
	/*
		Count instructions only for safety checks. Straight-line code does not yield; ordinary loops yield
		at their boundary.
	*/
	if *th.sliceSteps < th.sliceLimit {
		return true
	}
	// Keep the 500 ms safety limit across the entire extended slice.
	if time.Since(th.sliceStarted) >= 500*time.Millisecond {
		return false
	}
	th.sliceLimit = *th.sliceSteps + stepsPerFrame
	return true
}
