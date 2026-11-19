package vm

import (
	"context"
	"math"
	"strings"

	"gowarp/core/project"
)

type expressionOpcode uint8

const (
	exprLiteral expressionOpcode = iota
	exprVariable
	exprArgument
	exprListItem
	exprListLength
	exprJoin
	exprLetter
	exprStringLength
	exprTimer
	exprXPosition
	exprYPosition
	exprDirection
	exprSize
	exprVolume
	exprAdd
	exprSubtract
	exprMultiply
	exprDivide
	exprMod
	exprRound
	exprMath
	exprEqual
	exprLess
	exprGreater
	exprContains
	exprNot
	exprAnd
	exprOr
)

var expressionOpcodes = map[string]expressionOpcode{
	"data_variable":                   exprVariable,
	"argument_reporter_string_number": exprArgument,
	"data_itemoflist":                 exprListItem,
	"data_lengthoflist":               exprListLength,
	"operator_join":                   exprJoin,
	"operator_letter_of":              exprLetter,
	"operator_length":                 exprStringLength,
	"sensing_timer":                   exprTimer,
	"motion_xposition":                exprXPosition,
	"motion_yposition":                exprYPosition,
	"motion_direction":                exprDirection,
	"looks_size":                      exprSize,
	"sound_volume":                    exprVolume,
	"operator_add":                    exprAdd,
	"operator_subtract":               exprSubtract,
	"operator_multiply":               exprMultiply,
	"operator_divide":                 exprDivide,
	"operator_mod":                    exprMod,
	"operator_round":                  exprRound,
	"operator_mathop":                 exprMath,
	"operator_equals":                 exprEqual,
	"operator_lt":                     exprLess,
	"operator_gt":                     exprGreater,
	"operator_contains":               exprContains,
	"operator_not":                    exprNot,
	"operator_and":                    exprAnd,
	"operator_or":                     exprOr,
}

// Prepare synchronous expressions; the outer continuation retains results across later yields.
type numericExpression struct {
	op          expressionOpcode
	binding     int
	name        string
	value       float64
	literal     any
	left, right *numericExpression
	blocks      int
	height      int
	conditional bool
	numeric     bool
	textOnly    bool
}

type variableWrite struct {
	id      string
	binding int
	add     bool
	value   *numericExpression
}

func (r *Runtime) prepareVariableWrite(t *target, b *project.Block) *variableWrite {
	if write, found := r.variableWrites[b]; found {
		return write
	}
	if r.variableWrites == nil {
		r.variableWrites = make(map[*project.Block]*variableWrite)
	}
	var write *variableWrite
	if value := r.numericInput(t, b, "VALUE", 0); value != nil && value.height <= 128 && value.blocks <= stepsPerFrame {
		id := field(b, "VARIABLE", 1)
		write = &variableWrite{id: id, binding: r.variableBinding(t, id), add: b.Opcode == "data_changevariableby", value: value}
	}
	r.variableWrites[b] = write
	return write
}

func (w *variableWrite) run(ctx context.Context, r *Runtime, th *thread) error {
	var value any
	var delta float64
	var err error
	if w.add && !w.value.conditional {
		delta, err = w.value.evaluate(ctx, r, th)
		if math.IsNaN(delta) {
			delta = 0
		}
	} else {
		budget := stepsPerFrame
		value, err = w.value.readConditional(ctx, r, th, &budget)
		if w.add {
			delta = number(value)
		}
	}
	if err != nil {
		return err
	}
	// Resolve the current owner after evaluating VALUE, matching the interpreter.
	variable := r.boundVariable(th.target, w.binding)
	if variable == nil {
		_, variable, err = r.variable(th.target, w.id)
		if err != nil {
			return err
		}
	}
	if w.add {
		value = numberValue(number(variable.Value) + delta)
	}
	variable.Value = value
	return nil
}

/*
Cache pure control-input plans, never their results. Yielding reporters keep the continuation
path.
*/
func (r *Runtime) controlInput(ctx context.Context, th *thread, instruction *executionBlock, name string, input func(string) (any, error)) (any, error) {
	if instruction.ready&controlPlanReady == 0 {
		b := instruction.source
		plan, found := r.controlInputs[b]
		if !found {
			plan = r.numericInput(th.target, b, name, 0)
			if plan != nil && (plan.height > 128 || plan.blocks > stepsPerFrame) {
				plan = nil
			}
			if r.controlInputs == nil {
				r.controlInputs = make(map[*project.Block]*numericExpression)
			}
			r.controlInputs[b] = plan
		}
		instruction.control = plan
		instruction.ready |= controlPlanReady
	}
	if instruction.control == nil {
		return input(name)
	}
	budget := stepsPerFrame
	return instruction.control.readConditional(ctx, r, th, &budget)
}

func (r *Runtime) numericExpression(t *target, b *project.Block, depth int) *numericExpression {
	if depth > 128 || b == nil {
		return nil
	}
	if plan, ok := r.numericExpressions[b]; ok {
		return plan
	}
	if r.numericExpressions == nil {
		r.numericExpressions = make(map[*project.Block]*numericExpression)
	}
	// A nil entry also breaks cycles in malformed reporter graphs.
	r.numericExpressions[b] = nil
	op, supported := expressionOpcodes[b.Opcode]
	if !supported {
		return nil
	}
	n := &numericExpression{op: op, blocks: 1, height: 1, conditional: op == exprAnd || op == exprOr}
	n.numeric = op >= exprTimer && op <= exprMath || op == exprListLength || op == exprStringLength
	var left, right string
	switch b.Opcode {
	case "operator_equals", "operator_lt", "operator_gt", "operator_and", "operator_or":
		left, right = "OPERAND1", "OPERAND2"
	case "operator_contains", "operator_join":
		left, right = "STRING1", "STRING2"
	case "operator_letter_of":
		left, right = "LETTER", "STRING"
	case "operator_length":
		left = "STRING"
	case "sensing_timer", "motion_xposition", "motion_yposition", "motion_direction", "looks_size", "sound_volume":
	case "operator_not":
		left = "OPERAND"
	case "operator_add", "operator_subtract", "operator_multiply", "operator_divide", "operator_mod":
		left, right = "NUM1", "NUM2"
	case "operator_round", "operator_mathop":
		left = "NUM"
		n.name = strings.ToLower(field(b, "OPERATOR", 0))
	case "data_variable":
		n.name = field(b, "VARIABLE", 1)
		n.binding = r.variableBinding(t, n.name)
	case "data_lengthoflist":
		n.name = field(b, "LIST", 1)
	case "argument_reporter_string_number":
		n.name = field(b, "VALUE", 0)
		if strings.ToLower(n.name) == "last key pressed" {
			return nil
		}
	case "data_itemoflist":
		n.name = field(b, "LIST", 1)
		left = "INDEX"
	default:
		return nil
	}
	if left != "" {
		n.left = r.numericInput(t, b, left, depth)
		if n.left == nil {
			return nil
		}
		n.blocks += n.left.blocks
		n.height += n.left.height
		n.conditional = n.conditional || n.left.conditional
	}
	if right != "" {
		n.right = r.numericInput(t, b, right, depth)
		if n.right == nil {
			return nil
		}
		n.blocks += n.right.blocks
		n.height = max(n.height, 1+n.right.height)
		n.conditional = n.conditional || n.right.conditional
	}
	/*
		Conditional children keep their own instruction counts. Other operators
		continue through the interpreter, which can still prepare these inputs.
	*/
	if n.conditional && op != exprAnd && op != exprOr && op != exprNot {
		return nil
	}
	if n.blocks > stepsPerFrame || n.height > 128 {
		return nil
	}
	if op >= exprEqual && op <= exprGreater {
		for _, operand := range []*numericExpression{n.left, n.right} {
			if _, text := operand.literal.(string); operand.op == exprLiteral && text && !operand.numeric {
				n.textOnly = true
			}
		}
	}
	/*
		Literal arithmetic is immutable. Keep its original block count and
		height so preparation does not change instruction or recursion limits.
	*/
	if op >= exprAdd && op <= exprMath && n.left.op == exprLiteral && (n.right == nil || n.right.op == exprLiteral) {
		value, err := n.evaluate(context.Background(), r, nil)
		if err == nil {
			n.op = exprLiteral
			n.value, n.literal = value, numberValue(value)
			n.left, n.right = nil, nil
		}
	}
	r.numericExpressions[b] = n
	return n
}

func (r *Runtime) numericInput(t *target, b *project.Block, name string, depth int) *numericExpression {
	in, exists := b.Inputs[name]
	if !exists {
		return &numericExpression{}
	}
	if len(in) < 2 {
		return nil
	}
	value := in[1]
	if value == nil && len(in) > 2 {
		value = in[2]
	}
	if value == nil {
		return &numericExpression{}
	}
	if id, ok := value.(string); ok {
		return r.numericExpression(t, t.source.Blocks[id], depth+1)
	}
	literal, ok := value.([]any)
	if !ok || len(literal) < 2 {
		return nil
	}
	kind, ok := literal[0].(float64)
	if !ok {
		return nil
	}
	if kind == 12 && len(literal) >= 3 {
		id, _ := literal[2].(string)
		return &numericExpression{op: exprVariable, name: id, binding: r.variableBinding(t, id)}
	}
	switch kind {
	case 4, 5, 6, 7, 8, 9, 10, 11:
		switch literal[1].(type) {
		case string, float64, bool:
			name, _ := literal[1].(string)
			isNumeric := !math.IsNaN(numeric(literal[1]))
			if s, ok := literal[1].(string); ok && trim(s) == "" {
				isNumeric = false
			}
			return &numericExpression{value: number(literal[1]), literal: literal[1], name: name, numeric: isNumeric}
		}
	}
	return nil
}

/*
Keep the list value's original type. Only its index is numeric; callers may
use the returned string or boolean without arithmetic coercion.
*/
func (n *numericExpression) listItem(ctx context.Context, r *Runtime, th *thread) (any, error) {
	_, list, err := r.list(th.target, n.name)
	if err != nil {
		return nil, err
	}
	/*
		A live index may be a number, "last", or "random". Preserve its type
		instead of forcing variable/argument reads through the full interpreter.
		Arithmetic indexes still avoid boxing intermediate numeric results.
	*/
	var index any
	if n.left.numeric {
		index, err = n.left.evaluate(ctx, r, th)
	} else {
		index, err = n.left.read(ctx, r, th)
	}
	if err != nil {
		return nil, err
	}
	i := listIndex(index, len(list.Items))
	if i < 0 {
		return "", nil
	}
	return list.Items[i], nil
}

func (n *numericExpression) readTyped(ctx context.Context, r *Runtime, th *thread, budget *int) (calculationValue, error) {
	if n.op == exprArgument {
		*budget -= n.blocks
		if err := ctx.Err(); err != nil {
			return calculationValue{}, err
		}
		return nativeArgument(th, n.name), nil
	}
	if n.numeric && n.op != exprLiteral {
		*budget -= n.blocks
		value, err := n.evaluate(ctx, r, th)
		return calculationValue{number: value, numeric: true}, err
	}
	value, err := n.readConditional(ctx, r, th, budget)
	return calculationValue{value: value}, err
}

/*
The caller reserves enough budget for either branch, but only visited blocks
consume it. Pure branches use the existing evaluator without continuations.
*/
func (n *numericExpression) readConditional(ctx context.Context, r *Runtime, th *thread, budget *int) (any, error) {
	if !n.conditional {
		*budget -= n.blocks
		return n.read(ctx, r, th)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	*budget--
	a, err := n.left.readConditional(ctx, r, th, budget)
	if err != nil {
		return nil, err
	}
	value := truth(a)
	switch n.op {
	case exprNot:
		return !value, nil
	case exprAnd:
		if !value {
			return false, nil
		}
	case exprOr:
		if value {
			return true, nil
		}
	}
	b, err := n.right.readConditional(ctx, r, th, budget)
	if err != nil {
		return nil, err
	}
	return truth(b), nil
}

func (n *numericExpression) read(ctx context.Context, r *Runtime, th *thread) (any, error) {
	if n.op == exprLiteral {
		if n.literal == nil {
			return float64(0), nil
		}
		return n.literal, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch n.op {
	case exprVariable:
		v := r.boundVariable(th.target, n.binding)
		if v == nil {
			var err error
			_, v, err = r.variable(th.target, n.name)
			if err != nil {
				return nil, err
			}
		}
		return v.Value, nil
	case exprArgument:
		return nativeArgument(th, n.name).boxed(), nil
	case exprListItem:
		return n.listItem(ctx, r, th)
	case exprLetter:
		var index float64
		var err error
		if n.left.numeric {
			index, err = n.left.evaluate(ctx, r, th)
			if math.IsNaN(index) {
				index = 0
			}
		} else {
			var value any
			value, err = n.left.read(ctx, r, th)
			index = number(value)
		}
		if err != nil {
			return nil, err
		}
		value, err := n.right.read(ctx, r, th)
		if err != nil {
			return nil, err
		}
		return utf16Letter(text(value), index-1), nil
	case exprJoin:
		left, err := n.left.read(ctx, r, th)
		if err != nil {
			return nil, err
		}
		right, err := n.right.read(ctx, r, th)
		if err != nil {
			return nil, err
		}
		return text(left) + text(right), nil
	}
	if n.op >= exprEqual {
		return n.compare(ctx, r, th)
	}
	value, err := n.evaluate(ctx, r, th)
	return numberValue(value), err
}

func (n *numericExpression) comparisonValue(ctx context.Context, r *Runtime, th *thread) (float64, any, error) {
	if n.numeric {
		value, err := n.evaluate(ctx, r, th)
		return value, n.literal, err
	}
	value, err := n.read(ctx, r, th)
	if s, ok := value.(string); ok && trim(s) == "" {
		return math.NaN(), value, err
	}
	return numeric(value), value, err
}

func (n *numericExpression) compare(ctx context.Context, r *Runtime, th *thread) (bool, error) {
	/*
		Arithmetic and numeric literals can be compared without boxing a float
		or parsing the same literal again on every execution.
	*/
	if !n.textOnly && n.op >= exprEqual && n.op <= exprGreater && (n.left.numeric || n.right.numeric) {
		a, left, err := n.left.comparisonValue(ctx, r, th)
		if err != nil {
			return false, err
		}
		b, right, err := n.right.comparisonValue(ctx, r, th)
		if err != nil {
			return false, err
		}
		/*
			Scratch falls back to text comparison for NaN, including results
			such as sqrt(-1). Preserve that uncommon case.
		*/
		if math.IsNaN(a) || math.IsNaN(b) {
			if n.left.numeric && n.left.op != exprLiteral {
				left = a
			}
			if n.right.numeric && n.right.op != exprLiteral {
				right = b
			}
			order := compare(left, right)
			switch n.op {
			case exprEqual:
				return order == 0, nil
			case exprLess:
				return order < 0, nil
			default:
				return order > 0, nil
			}
		}
		switch n.op {
		case exprEqual:
			return a == b, nil
		case exprLess:
			return a < b, nil
		default:
			return a > b, nil
		}
	}
	a, err := n.left.read(ctx, r, th)
	if err != nil {
		return false, err
	}
	if n.op == exprNot {
		return !truth(a), nil
	}
	b, err := n.right.read(ctx, r, th)
	if err != nil {
		return false, err
	}
	switch n.op {
	case exprEqual, exprLess, exprGreater:
		var order float64
		if n.textOnly {
			// A nonnumeric literal forces Scratch's text comparison for both operands.
			order = compareText(text(a), text(b))
		} else {
			order = compare(a, b)
		}
		switch n.op {
		case exprEqual:
			return order == 0, nil
		case exprLess:
			return order < 0, nil
		default:
			return order > 0, nil
		}
	case exprContains:
		return strings.Contains(strings.ToLower(text(a)), strings.ToLower(text(b))), nil
	}
	panic("invalid prepared comparison opcode")
}

func (n *numericExpression) evaluate(ctx context.Context, r *Runtime, th *thread) (float64, error) {
	if n.op == exprLiteral {
		return n.value, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if n.op >= exprEqual {
		value, err := n.compare(ctx, r, th)
		if value {
			return 1, err
		}
		return 0, err
	}
	switch n.op {
	case exprTimer:
		return r.elapsed - r.timerSeconds, nil
	case exprXPosition:
		return limitPrecision(th.target.state.X), nil
	case exprYPosition:
		return limitPrecision(th.target.state.Y), nil
	case exprDirection:
		return th.target.state.Direction, nil
	case exprSize:
		return math.Floor(th.target.state.Size + 0.5), nil
	case exprVolume:
		return th.target.state.Volume, nil
	case exprStringLength:
		value, err := n.left.read(ctx, r, th)
		if err != nil {
			return 0, err
		}
		return float64(utf16Length(text(value))), nil
	case exprVariable:
		v := r.boundVariable(th.target, n.binding)
		if v == nil {
			var err error
			_, v, err = r.variable(th.target, n.name)
			if err != nil {
				return 0, err
			}
		}
		return number(v.Value), nil
	case exprListLength:
		_, list, err := r.list(th.target, n.name)
		return float64(len(list.Items)), err
	case exprArgument:
		return nativeArgument(th, n.name).num(), nil
	case exprListItem:
		value, err := n.listItem(ctx, r, th)
		return number(value), err
	case exprJoin, exprLetter:
		value, err := n.read(ctx, r, th)
		return number(value), err
	}
	a, err := n.left.evaluate(ctx, r, th)
	if err != nil {
		return 0, err
	}
	/*
		Arithmetic results such as sqrt(-1) become zero when fed into another
		numeric block, just as number() does in the general evaluator.
	*/
	if math.IsNaN(a) {
		a = 0
	}
	switch n.op {
	case exprRound:
		rounded := math.Floor(a)
		if a-rounded >= 0.5 {
			rounded++
		}
		if rounded == 0 {
			return math.Copysign(0, a), nil
		}
		return rounded, nil
	case exprMath:
		return r.mathOperator(n.name, a), nil
	}
	b, err := n.right.evaluate(ctx, r, th)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(b) {
		b = 0
	}
	switch n.op {
	case exprAdd:
		return a + b, nil
	case exprSubtract:
		return a - b, nil
	case exprMultiply:
		return a * b, nil
	case exprDivide:
		return a / b, nil
	case exprMod:
		m := math.Mod(a, b)
		if m/b < 0 {
			m += b
		}
		return m, nil
	}
	panic("invalid prepared numeric opcode")
}
