package vm

import (
	"context"
	"fmt"
	"math"
	"strings"

	"gowarp/core/extension"
	"gowarp/core/project"
)

func (r *Runtime) inputValue(ctx context.Context, th *thread, value any, name string, depth int, budget *int) (any, error) {
	t := th.target
	if id, ok := value.(string); ok {
		return r.eval(ctx, th, id, depth+1, budget)
	}
	literal, ok := value.([]any)
	if !ok || len(literal) < 2 {
		return nil, fmt.Errorf("invalid literal in input %q", name)
	}
	kind, ok := literal[0].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid literal type in input %q", name)
	}
	switch kind {
	case 4, 5, 6, 7, 8, 9, 10, 11:
		switch literal[1].(type) {
		case string, float64, bool:
			return literal[1], nil
		default:
			return nil, fmt.Errorf("invalid literal value in input %q", name)
		}
	case 12:
		if len(literal) < 3 {
			return nil, fmt.Errorf("missing variable ID in input %q", name)
		}
		id, _ := literal[2].(string)
		_, v, err := r.variable(t, id)
		if err != nil {
			return nil, err
		}
		return v.Value, nil
	case 13:
		if len(literal) < 3 {
			return nil, fmt.Errorf("missing list ID in input %q", name)
		}
		id, _ := literal[2].(string)
		_, list, err := r.list(t, id)
		if err != nil {
			return nil, err
		}
		return listContents(ctx, list)
	default:
		return nil, fmt.Errorf("unsupported literal type %v", kind)
	}
}

func (r *Runtime) eval(ctx context.Context, th *thread, id string, depth int, budget *int) (any, error) {
	return r.evalBlock(ctx, th, id, th.target.source.Blocks[id], depth, budget)
}

func (r *Runtime) evalBlock(ctx context.Context, th *thread, id string, b *project.Block, depth int, budget *int) (any, error) {
	t := th.target
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if *budget <= 0 {
		return nil, fmt.Errorf("reporter evaluation limit exceeded at %q", id)
	}
	*budget--
	if depth > 128 {
		return nil, fmt.Errorf("reporter nesting limit exceeded at %q", id)
	}
	if b == nil {
		return nil, fmt.Errorf("missing reporter %q", id)
	}
	if plan := r.numericExpression(t, b, 0); plan != nil && plan.blocks-1 <= *budget && depth+plan.height-1 <= 128 {
		if plan.conditional {
			// readConditional counts the root and only the branches it visits.
			*budget++
			return plan.readConditional(ctx, r, th, budget)
		}
		*budget -= plan.blocks - 1
		return plan.read(ctx, r, th)
	}
	arg := func(name string) (any, error) {
		return r.input(ctx, th, b, name, depth, budget)
	}
	if result, handled, err := r.invokeExtension(ctx, th, b, arg, true, depth, budget); handled {
		return result.Value, err
	}
	if value, handled, err := r.graphicsReporter(th, b, arg); handled {
		return value, err
	}
	if value, handled, err := r.dataReporter(th, b, arg); handled {
		return value, err
	}
	switch b.Opcode {
	case "argument_reporter_string_number", "argument_reporter_boolean":
		if value, ok := th.params[field(b, "VALUE", 0)]; ok {
			return value.boxed(), nil
		}
		name := strings.ToLower(field(b, "VALUE", 0))
		if b.Opcode == "argument_reporter_boolean" && (name == "is compiled?" || name == "is turbowarp?") {
			return true, nil
		}
		if name == "last key pressed" {
			return r.lastKey, nil
		}
		return float64(0), nil
	case "procedures_call":
		return r.callReporter(ctx, th, b, arg)
	case "control_create_clone_of_menu":
		return field(b, "CLONE_OPTION", 0), nil
	case "event_broadcast_menu":
		return field(b, "BROADCAST_OPTION", 0), nil
	case "math_number", "math_positive_number", "math_whole_number", "math_integer", "math_angle":
		return field(b, "NUM", 0), nil
	case "text":
		return field(b, "TEXT", 0), nil
	case "colour_picker":
		return field(b, "COLOUR", 0), nil
	case "control_get_counter":
		return r.counter, nil
	case "motion_xposition":
		return limitPrecision(t.state.X), nil
	case "motion_yposition":
		return limitPrecision(t.state.Y), nil
	case "motion_direction":
		return t.state.Direction, nil
	case "looks_costumenumbername", "looks_backdropnumbername":
		owner := t
		if b.Opcode == "looks_backdropnumbername" {
			owner = r.stage
		}
		if owner == nil {
			return "", nil
		}
		if field(b, "NUMBER_NAME", 0) == "number" {
			return float64(owner.state.CurrentCostume + 1), nil
		}
		return costumeName(owner), nil
	case "looks_costume", "looks_backdrops", "sensing_keyoptions", "sensing_distancetomenu", "sensing_of_object_menu", "sensing_touchingobjectmenu":
		for _, v := range b.Fields {
			if len(v) > 0 {
				return v[0], nil
			}
		}
		return "", nil
	case "sound_volume":
		return t.state.Volume, nil
	case "sound_sounds_menu":
		return field(b, "SOUND_MENU", 0), nil
	case "looks_size":
		return math.Floor(t.state.Size + 0.5), nil
	case "motion_goto_menu", "motion_glideto_menu":
		return field(b, "TO", 0), nil
	case "motion_pointtowards_menu":
		return field(b, "TOWARDS", 0), nil
	case "sensing_timer":
		return r.elapsed - r.timerSeconds, nil
	case "data_variable":
		_, v, err := r.variable(t, field(b, "VARIABLE", 1))
		if err != nil {
			return nil, err
		}
		return v.Value, nil
	case "data_itemoflist", "data_itemnumoflist", "data_lengthoflist", "data_listcontainsitem", "data_listcontents":
		return r.listReporter(ctx, t, b, arg)
	case "operator_random", "operator_letter_of", "operator_letters_of", "operator_index_of", "operator_change_case", "operator_repeat", "operator_replace", "operator_trim", "operator_pi", "operator_newline", "operator_mathop":
		return extraOperator(b, arg)
	case "sensing_mousex", "sensing_mousey", "sensing_mousedown", "sensing_keypressed", "sensing_answer", "sensing_username", "sensing_loudness", "sensing_loud", "sensing_dayssince2000", "sensing_current", "sensing_distanceto", "sensing_of", "tw_getLastKeyPressed":
		return r.sensingReporter(t, b, arg)
	}
	var left, right string
	switch b.Opcode {
	case "operator_add", "operator_subtract", "operator_multiply", "operator_divide", "operator_mod":
		left, right = "NUM1", "NUM2"
	case "operator_equals", "operator_lt", "operator_gt", "operator_and", "operator_or":
		left, right = "OPERAND1", "OPERAND2"
	case "operator_not":
		left = "OPERAND"
	case "operator_round":
		left = "NUM"
	case "operator_join", "operator_contains":
		left, right = "STRING1", "STRING2"
	case "operator_length":
		left = "STRING"
	default:
		// Extension menus use the same single-field shadow shape as builtin menus.
		if prefix, _, _ := strings.Cut(b.Opcode, "_"); extension.IsBuiltin(prefix) && len(b.Inputs) == 0 && len(b.Fields) == 1 {
			for _, values := range b.Fields {
				if len(values) > 0 {
					return text(values[0]), nil
				}
			}
		}
		return nil, fmt.Errorf("unsupported reporter %q at block %q", b.Opcode, id)
	}
	a, err := arg(left)
	if err != nil {
		return nil, err
	}
	if b.Opcode == "operator_and" && !truth(a) {
		return false, nil
	}
	if b.Opcode == "operator_or" && truth(a) {
		return true, nil
	}
	var c any
	if right != "" {
		c, err = arg(right)
		if err != nil {
			return nil, err
		}
	}
	switch b.Opcode {
	case "operator_add":
		return number(a) + number(c), nil
	case "operator_subtract":
		return number(a) - number(c), nil
	case "operator_multiply":
		return number(a) * number(c), nil
	case "operator_divide":
		return number(a) / number(c), nil
	case "operator_mod":
		n, d := number(a), number(c)
		m := math.Mod(n, d)
		if m/d < 0 {
			m += d
		}
		return m, nil
	case "operator_equals":
		return compare(a, c) == 0, nil
	case "operator_lt":
		return compare(a, c) < 0, nil
	case "operator_gt":
		return compare(a, c) > 0, nil
	case "operator_and":
		return truth(a) && truth(c), nil
	case "operator_or":
		return truth(a) || truth(c), nil
	case "operator_not":
		return !truth(a), nil
	case "operator_round":
		n := number(a)
		rounded := math.Floor(n)
		if n-rounded >= 0.5 {
			rounded++
		}
		if rounded == 0 {
			return math.Copysign(0, n), nil
		}
		return rounded, nil
	case "operator_join":
		return text(a) + text(c), nil
	case "operator_contains":
		return strings.Contains(strings.ToLower(text(a)), strings.ToLower(text(c))), nil
	case "operator_length":
		return float64(utf16Length(text(a))), nil
	}
	return nil, fmt.Errorf("unhandled reporter %q", b.Opcode)
}
