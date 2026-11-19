package vm

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"testing"

	"gowarp/core/project"
)

func TestPreparedSpriteReportersReadCurrentTarget(t *testing.T) {
	for _, opcode := range []string{"motion_xposition", "motion_yposition", "motion_direction", "looks_size", "sound_volume"} {
		t.Run(opcode, func(t *testing.T) {
			r := motionRuntime(t, `{"value":{"opcode":"`+opcode+`"}}`)
			original := r.targets[1]
			r.createClone(original)
			clone := r.targets[len(r.targets)-1]
			block := original.source.Blocks["value"]
			plan := r.numericExpression(original, block, 0)
			if plan == nil {
				t.Fatal("sprite reporter cannot use the prepared expression path")
			}
			for i, target := range []*target{original, clone, original} {
				value := float64(i)*30 + 12.3456789012345
				target.state.X, target.state.Y = value, -value
				target.state.Direction, target.state.Size, target.state.Volume = value, value, value
				th := &thread{target: target}
				budget := stepsPerFrame
				got, err := plan.readConditional(context.Background(), r, th, &budget)
				if err != nil || budget != stepsPerFrame-1 {
					t.Fatalf("prepared read: value=%v budget=%d err=%v", got, budget, err)
				}
				// Disable preparation to compare against the interpreter's rounding and value types.
				r.numericExpressions[block] = nil
				budget = stepsPerFrame
				want, err := r.eval(context.Background(), th, "value", 0, &budget)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("target %d: got %v, want %v, err=%v", i, got, want, err)
				}
			}
		})
	}
}

func BenchmarkPositionArithmetic(b *testing.B) {
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: project.Blocks{
		"value": {Opcode: "operator_add", Inputs: map[string][]any{"NUM1": {float64(2), "x"}, "NUM2": {float64(2), "y"}}},
		"x":     {Opcode: "motion_xposition"},
		"y":     {Opcode: "motion_yposition"},
	}}}})
	th := &thread{target: r.stage, expression: newExpression("value")}
	r.stage.state.X, r.stage.state.Y = 12.5, -3.25
	b.ReportAllocs()
	for b.Loop() {
		th.expression.reset("value")
		budget := stepsPerFrame
		value, err := r.eval(context.Background(), th, "value", 0, &budget)
		if err != nil || value != 9.25 || budget != stepsPerFrame-3 {
			b.Fatalf("value=%v budget=%d err=%v", value, budget, err)
		}
	}
}

func preparedNumericComparison(dynamic bool) (*Runtime, *thread, *numericExpression) {
	blocks := map[string]*project.Block{
		"root":   {Opcode: "operator_lt", Inputs: map[string][]any{"OPERAND1": {float64(2), "square"}, "OPERAND2": {float64(1), []any{float64(4), "100.5"}}}},
		"square": {Opcode: "operator_multiply", Inputs: map[string][]any{"NUM1": {float64(2), "x"}, "NUM2": {float64(2), "x"}}},
		"x":      {Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"x"}}},
	}
	if dynamic {
		blocks["root"].Inputs["OPERAND2"] = []any{float64(2), "limit"}
		blocks["limit"] = &project.Block{Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"limit"}}}
	}
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
	th := &thread{target: r.stage, params: procedureParameters{"x": {value: float64(9.5)}, "limit": {value: float64(100.5)}}}
	return r, th, r.numericExpression(r.stage, blocks["root"], 0)
}

func TestPreparedNumericComparisonDoesNotBoxOperands(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		r, th, plan := preparedNumericComparison(dynamic)
		allocations := testing.AllocsPerRun(1000, func() {
			value, err := plan.read(context.Background(), r, th)
			if err != nil || value != true {
				t.Fatalf("comparison = %v, %v", value, err)
			}
		})
		if allocations != 0 {
			t.Fatalf("dynamic=%v: numeric comparison allocated %.0f objects per evaluation", dynamic, allocations)
		}
	}
}

func TestPreparedVariableWritesMatchInterpreter(t *testing.T) {
	values := []any{"007", "", " ", "NaN", "true", true, false, nil, -12.5, math.Copysign(0, -1), math.NaN(), math.Inf(1)}
	for _, opcode := range []string{"data_setvariableto", "data_changevariableby"} {
		for _, conditional := range []bool{false, true} {
			for _, value := range values {
				blocks := `{
					"set":{"opcode":"` + opcode + `","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"arg"]}},
					"arg":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["value",null]}},
					"condition":{"opcode":"operator_or","inputs":{"OPERAND1":[2,"arg"],"OPERAND2":[1,[10,"false"]]}}
				}`
				fast, original := fixture(t, blocks), fixture(t, blocks)
				for _, r := range []*Runtime{fast, original} {
					block := r.stage.source.Blocks["set"]
					if conditional {
						block.Inputs["VALUE"][1] = "condition"
					}
					if r == original {
						r.variableWrites = map[*project.Block]*variableWrite{block: nil}
					}
					th := &thread{target: r.stage, pc: "set", params: procedureParameters{"value": {value: value}}}
					if err := r.step(context.Background(), th); err != nil {
						t.Fatal(err)
					}
				}
				got, want := fast.stage.variables["score"].Value, original.stage.variables["score"].Value
				if n, ok := want.(float64); ok {
					v, isNumber := got.(float64)
					if !isNumber || math.Float64bits(v) != math.Float64bits(n) {
						t.Fatalf("%s conditional=%v input=%v: %v, want %v", opcode, conditional, value, got, want)
					}
				} else if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s conditional=%v input=%v: %v, want %v", opcode, conditional, value, got, want)
				}
			}
		}
	}
}

func BenchmarkPreparedNumericComparison(b *testing.B) {
	r, th, plan := preparedNumericComparison(false)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		value, err := plan.read(context.Background(), r, th)
		if err != nil || value != true {
			b.Fatalf("comparison = %v, %v", value, err)
		}
	}
}

func preparedLiteralComparison(opcode, literal string, literalFirst bool) (*Runtime, *thread, *numericExpression) {
	operands := map[string][]any{
		"OPERAND1": {float64(2), "value"},
		"OPERAND2": {float64(1), []any{float64(10), literal}},
	}
	if literalFirst {
		operands["OPERAND1"], operands["OPERAND2"] = operands["OPERAND2"], operands["OPERAND1"]
	}
	blocks := project.Blocks{
		"compare": {Opcode: opcode, Inputs: operands},
		"value":   {Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"value"}}},
	}
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
	th := &thread{target: r.stage, params: procedureParameters{"value": {value: ""}}}
	return r, th, r.numericExpression(r.stage, blocks["compare"], 0)
}

func TestPreparedLiteralComparisonMatchesInterpreter(t *testing.T) {
	values := []any{"", " ", "0", "05", "12x", "일반", "骨", "BONE", "bone", "Σ", "σ", "ς", "İ", "i", "NaN", "Infinity", "\xff", true, false, nil, float64(0), float64(5), math.NaN(), math.Inf(1)}
	for _, opcode := range []string{"operator_equals", "operator_lt", "operator_gt"} {
		for _, literal := range []string{"", " ", "\ufeff", "05", "0x5", "12x", "일반", "骨", "BONE", "Σ", "İ", "NaN", "Infinity", "\xff"} {
			for _, literalFirst := range []bool{false, true} {
				r, th, plan := preparedLiteralComparison(opcode, literal, literalFirst)
				for _, value := range values {
					th.params["value"] = calculationValue{value: value}
					order := compare(value, literal)
					if literalFirst {
						order = compare(literal, value)
					}
					want := order == 0
					if opcode == "operator_lt" {
						want = order < 0
					} else if opcode == "operator_gt" {
						want = order > 0
					}
					budget := stepsPerFrame
					got, err := plan.readConditional(context.Background(), r, th, &budget)
					if err != nil || got != want || budget != stepsPerFrame-plan.blocks {
						t.Fatalf("%s literal=%q first=%v value=%v: got %v, %v, budget=%d; want %v", opcode, literal, literalFirst, value, got, err, budget, want)
					}
				}
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if _, err := plan.read(ctx, r, th); err != context.Canceled {
					t.Fatalf("comparison lost cancellation: %v", err)
				}
			}
		}
	}
}

func BenchmarkPreparedLiteralComparison(b *testing.B) {
	for _, tc := range []struct {
		name, literal string
		values        []any
	}{
		{"text", "BONE", []any{"BONE", "bone", "GB", "일반"}},
		{"numeric", "05", []any{"5", float64(5), float64(6), "GB"}},
		{"empty", "", []any{"", float64(0), "GB", " "}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			r, th, plan := preparedLiteralComparison("operator_equals", tc.literal, false)
			ctx := context.Background()
			i := 0
			b.ReportAllocs()
			for b.Loop() {
				th.params["value"] = calculationValue{value: tc.values[i%len(tc.values)]}
				if _, err := plan.read(ctx, r, th); err != nil {
					b.Fatal(err)
				}
				i++
			}
		})
	}
}

func BenchmarkArithmeticControlCondition(b *testing.B) {
	r, th, _ := preparedNumericComparison(false)
	th.target.source.Blocks["if"] = &project.Block{
		Opcode: "control_if",
		Inputs: map[string][]any{"CONDITION": {float64(2), "root"}},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		th.pc, th.done = "if", false
		if err := r.step(context.Background(), th); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPreparedControlInputsPreserveLoopAndWaitTiming(t *testing.T) {
	const blocks = `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"repeat"},
		"repeat":{"opcode":"control_repeat","next":"while","inputs":{"TIMES":[2,"count"],"SUBSTACK":[2,"inc"]}},
		"count":{"opcode":"operator_add","inputs":{"NUM1":[1,[4,"1"]],"NUM2":[1,[4,"2"]]}},
		"inc":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
		"while":{"opcode":"control_while","next":"until","inputs":{"CONDITION":[2,"lt"],"SUBSTACK":[2,"inc"]}},
		"lt":{"opcode":"operator_lt","inputs":{"OPERAND1":[1,[12,"score","score"]],"OPERAND2":[1,[4,"5"]]}},
		"until":{"opcode":"control_repeat_until","next":"wait","inputs":{"CONDITION":[2,"gt"],"SUBSTACK":[2,"inc"]}},
		"gt":{"opcode":"operator_gt","inputs":{"OPERAND1":[1,[12,"score","score"]],"OPERAND2":[1,[4,"6"]]}},
		"wait":{"opcode":"control_wait_until","next":"choose","inputs":{"CONDITION":[2,"ready"]}},
		"ready":{"opcode":"operator_gt","inputs":{"OPERAND1":[1,[12,"score","score"]],"OPERAND2":[1,[4,"7"]]}},
		"choose":{"opcode":"control_if_else","inputs":{"CONDITION":[2,"equal"],"SUBSTACK":[2,"yes"],"SUBSTACK2":[2,"no"]}},
		"equal":{"opcode":"operator_equals","inputs":{"OPERAND1":[1,[12,"score","score"]],"OPERAND2":[1,[4,"8"]]}},
		"yes":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"10"]]}},
		"no":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"-10"]]}}
	}`
	for _, turbo := range []bool{false, true} {
		fast, original := fixture(t, blocks), fixture(t, blocks)
		original.controlInputs = make(map[*project.Block]*numericExpression)
		for _, block := range original.stage.source.Blocks {
			// A cached nil plan selects the existing interpreter fallback.
			original.controlInputs[block] = nil
		}
		for frame := 0; frame < 16; frame++ {
			if frame == 10 {
				*fast.stage.variables["score"] = Variable{Name: "score", Value: float64(8)}
				*original.stage.variables["score"] = Variable{Name: "score", Value: float64(8)}
			}
			for _, runtime := range []*Runtime{fast, original} {
				if err := runtime.Tick(context.Background(), 60, turbo); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(fast.Snapshot(), original.Snapshot()) || fast.Done() != original.Done() || fast.Time() != original.Time() {
				t.Fatalf("turbo=%v frame=%d: prepared control changed execution timing", turbo, frame)
			}
		}
		if !fast.Done() || fast.stage.variables["score"].Value != float64(18) {
			t.Fatalf("turbo=%v: loop/wait result %v", turbo, fast.stage.variables["score"])
		}
	}
}

func TestPreparedNumericComparisonsMatchInterpreter(t *testing.T) {
	literals := []any{"", " ", "NaN", "0x10", " 2 ", "-0", "Infinity", "blue", true, false, float64(2), math.NaN(), math.Inf(-1)}
	for _, op := range []string{"operator_equals", "operator_lt", "operator_gt"} {
		for _, literal := range literals {
			for _, reverse := range []bool{false, true} {
				for _, dynamic := range []bool{false, true} {
					inputs := map[string][]any{
						"OPERAND1": {float64(2), "quotient"},
						"OPERAND2": {float64(1), []any{float64(10), literal}},
					}
					if dynamic {
						inputs["OPERAND2"] = []any{float64(2), "limit"}
					}
					if reverse {
						inputs["OPERAND1"], inputs["OPERAND2"] = inputs["OPERAND2"], inputs["OPERAND1"]
					}
					blocks := map[string]*project.Block{
						"root": {Opcode: op, Inputs: inputs},
						"quotient": {Opcode: "operator_divide", Inputs: map[string][]any{
							"NUM1": {float64(2), "a"}, "NUM2": {float64(2), "b"},
						}},
						"a": {Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"a"}}},
						"b": {Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"b"}}},
					}
					blocks["limit"] = &project.Block{Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"limit"}}}
					r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
					th := &thread{target: r.stage, expression: newExpression("root")}
					for _, pair := range [][2]float64{{9, 2}, {0, 0}, {1, 0}, {-1, 0}, {0, -1}, {math.Inf(1), 1}} {
						th.params = procedureParameters{"a": {value: pair[0]}, "b": {value: pair[1]}, "limit": {value: literal}}
						budget := stepsPerFrame
						th.expression.reset("root")
						got, err := r.eval(context.Background(), th, "root", 1, &budget)
						remaining := budget
						prepared := r.numericExpressions
						r.numericExpressions = make(map[*project.Block]*numericExpression)
						for _, block := range blocks {
							r.numericExpressions[block] = nil
						}
						budget = stepsPerFrame
						th.expression.reset("root")
						want, originalErr := r.eval(context.Background(), th, "root", 1, &budget)
						r.numericExpressions = prepared
						if got != want || fmt.Sprint(err) != fmt.Sprint(originalErr) || remaining != budget {
							t.Fatalf("%s %v / %v vs %v, reverse=%v: got %v/%v/%d, want %v/%v/%d",
								op, pair[0], pair[1], literal, reverse, got, err, remaining, want, originalErr, budget)
						}
					}
				}
			}
		}
	}
}

func TestConstantArithmeticComparisonsMatchInterpreter(t *testing.T) {
	for _, op := range []string{"operator_equals", "operator_lt", "operator_gt"} {
		for _, pair := range [][2]float64{{0, 0}, {1, 0}, {-1, 0}, {0, -1}, {1, 3}} {
			for _, other := range []any{"NaN", "Infinity", "-Infinity", float64(0), 1.0 / 3} {
				blocks := map[string]*project.Block{
					"root": {Opcode: op, Inputs: map[string][]any{
						"OPERAND1": {float64(2), "quotient"}, "OPERAND2": {float64(1), []any{float64(10), other}},
					}},
					"quotient": {Opcode: "operator_divide", Inputs: literalInputs(map[string]any{"NUM1": pair[0], "NUM2": pair[1]})},
				}
				r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
				th := &thread{target: r.stage, expression: newExpression("root")}
				budget := stepsPerFrame
				got, err := r.eval(context.Background(), th, "root", 1, &budget)
				remaining := budget
				for _, block := range blocks {
					r.numericExpressions[block] = nil
				}
				th.expression.reset("root")
				budget = stepsPerFrame
				want, originalErr := r.eval(context.Background(), th, "root", 1, &budget)
				if got != want || fmt.Sprint(err) != fmt.Sprint(originalErr) || remaining != budget {
					t.Fatalf("%s(%v/%v,%v): got %v/%v/%d, want %v/%v/%d", op, pair[0], pair[1], other, got, err, remaining, want, originalErr, budget)
				}
			}
		}
	}
}

func TestNumericExpressionMatchesInterpreter(t *testing.T) {
	values := []any{"", "no", "0x10", " 2 ", "-0", true, float64(-.5), float64(1e20), math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, op := range []string{"operator_add", "operator_subtract", "operator_multiply", "operator_divide", "operator_mod", "operator_round", "operator_mathop"} {
		for _, a := range values {
			for _, b := range values {
				blocks := map[string]*project.Block{
					"root": {Opcode: op, Fields: map[string][]any{"OPERATOR": {"sin"}},
						Inputs: map[string][]any{"NUM1": {float64(2), "child"}, "NUM": {float64(2), "child"}, "NUM2": {float64(1), []any{float64(10), b}}}},
					"child": {Opcode: "operator_multiply", Inputs: literalInputs(map[string]any{"NUM1": a, "NUM2": float64(1)})},
				}
				r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
				th := &thread{target: r.stage, expression: newExpression("root")}
				budget := stepsPerFrame
				got, err := r.eval(context.Background(), th, "root", 1, &budget)
				remaining := budget
				// Disable only preparation, retaining the original interpreter.
				for _, block := range blocks {
					r.numericExpressions[block] = nil
				}
				th.expression.reset("root")
				budget = stepsPerFrame
				want, originalErr := r.eval(context.Background(), th, "root", 1, &budget)
				if fmt.Sprint(err) != fmt.Sprint(originalErr) || budget != remaining {
					t.Fatalf("%s(%v,%v): error/budget differs: %v/%d vs %v/%d", op, a, b, err, remaining, originalErr, budget)
				}
				x, y := got.(float64), want.(float64)
				if !(math.IsNaN(x) && math.IsNaN(y)) && math.Float64bits(x) != math.Float64bits(y) {
					t.Fatalf("%s(%v,%v)=%v, want %v", op, a, b, got, want)
				}
			}
		}
	}
}

func TestPreparedComparisonsMatchInterpreter(t *testing.T) {
	values := []any{"", " ", "0", "02", "false", "하양", "BLUE", "blue", true, false,
		float64(2), float64(-.5), math.NaN(), math.Inf(1), nil}
	for _, op := range []string{"operator_equals", "operator_lt", "operator_gt", "operator_contains", "operator_not", "operator_and", "operator_or"} {
		blocks := map[string]*project.Block{
			"root": {Opcode: op, Inputs: map[string][]any{
				"OPERAND1": {float64(2), "a"}, "OPERAND2": {float64(2), "b"},
				"STRING1": {float64(2), "a"}, "STRING2": {float64(2), "b"}, "OPERAND": {float64(2), "a"},
			}},
			"a": {Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"a"}}},
			"b": {Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"b"}}},
		}
		r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
		th := &thread{target: r.stage, params: procedureParameters{}, expression: newExpression("root")}
		for _, a := range values {
			for _, b := range values {
				th.params["a"], th.params["b"] = calculationValue{value: a}, calculationValue{value: b}
				th.expression.reset("root")
				budget := stepsPerFrame
				got, err := r.eval(context.Background(), th, "root", 1, &budget)
				remaining := budget
				prepared := r.numericExpressions
				r.numericExpressions = make(map[*project.Block]*numericExpression)
				for _, block := range blocks {
					r.numericExpressions[block] = nil
				}
				th.expression.reset("root")
				budget = stepsPerFrame
				want, originalErr := r.eval(context.Background(), th, "root", 1, &budget)
				r.numericExpressions = prepared
				if fmt.Sprint(err) != fmt.Sprint(originalErr) || budget != remaining || got != want {
					t.Fatalf("%s(%v,%v): got %v/%v/%d, want %v/%v/%d", op, a, b, got, err, remaining, want, originalErr, budget)
				}
			}
		}
	}
}

func TestPreparedComparisonShortCircuit(t *testing.T) {
	for _, op := range []string{"operator_and", "operator_or"} {
		left := "false"
		if op == "operator_or" {
			left = "true"
		}
		r := motionRuntime(t, `{
			"root":{"opcode":"`+op+`","inputs":{"OPERAND1":[1,[10,"`+left+`"]],"OPERAND2":[2,"bad"]}},
			"bad":{"opcode":"operator_equals","inputs":{"OPERAND1":[2,"missing"],"OPERAND2":[1,[4,"1"]]}}
		}`)
		th := &thread{target: r.targets[1], expression: newExpression("root")}
		budget := stepsPerFrame
		got, err := r.eval(context.Background(), th, "root", 1, &budget)
		if err != nil || got != (op == "operator_or") || budget != stepsPerFrame-1 {
			t.Fatalf("%s evaluated an unused operand: %v, %v, budget=%d", op, got, err, budget)
		}
	}
}

func TestPreparedLogicalBranches(t *testing.T) {
	r := motionRuntime(t, `{
		"root":{"opcode":"operator_not","inputs":{"OPERAND":[2,"and"]}},
		"and":{"opcode":"operator_and","inputs":{"OPERAND1":[2,"left"],"OPERAND2":[2,"or"]}},
		"or":{"opcode":"operator_or","inputs":{"OPERAND1":[2,"middle"],"OPERAND2":[2,"right"]}},
		"left":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["left",null]}},
		"middle":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["middle",null]}},
		"right":{"opcode":"data_variable","fields":{"VARIABLE":["missing","missing"]}}
	}`)
	th := &thread{target: r.targets[1], expression: newExpression("root")}
	blocks := th.target.source.Blocks
	if r.numericExpression(th.target, blocks["root"], 0) == nil {
		t.Fatal("synchronous logical tree was not prepared")
	}
	for _, left := range []bool{false, true} {
		for _, middle := range []bool{false, true} {
			for budgetLimit := 1; budgetLimit <= 8; budgetLimit++ {
				for _, depth := range []int{1, 127} {
					th.params = procedureParameters{"left": {value: left}, "middle": {value: middle}}
					th.expression.reset("root")
					budget := budgetLimit
					got, err := r.eval(context.Background(), th, "root", depth, &budget)
					remaining := budget
					prepared := r.numericExpressions
					r.numericExpressions = make(map[*project.Block]*numericExpression)
					for _, block := range blocks {
						r.numericExpressions[block] = nil
					}
					th.expression.reset("root")
					budget = budgetLimit
					want, originalErr := r.eval(context.Background(), th, "root", depth, &budget)
					r.numericExpressions = prepared
					if got != want || fmt.Sprint(err) != fmt.Sprint(originalErr) || remaining != budget {
						t.Fatalf("left=%v middle=%v limit=%d depth=%d: %v/%v/%d, want %v/%v/%d",
							left, middle, budgetLimit, depth, got, err, remaining, want, originalErr, budget)
					}
				}
			}
		}
	}
}

func TestPreparedLogicSurvivesReporterSuspension(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},
		"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"join"]}},
		"join":{"opcode":"operator_join","inputs":{"STRING1":[2,"and"],"STRING2":[2,"call"]}},
		"and":{"opcode":"operator_and","inputs":{"OPERAND1":[2,"equal"],"OPERAND2":[1,[10,"true"]]}},
		"equal":{"opcode":"operator_equals","inputs":{"OPERAND1":[1,[12,"score","score"]],"OPERAND2":[1,[4,"0"]]}},
		"call":{"opcode":"procedures_call","mutation":{"proccode":"delayed"}},
		"def":{"opcode":"procedures_definition","topLevel":true,"next":"wait","inputs":{"custom_block":[1,"proto"]}},
		"proto":{"opcode":"procedures_prototype","mutation":{"proccode":"delayed","argumentids":"[]","argumentnames":"[]","argumentdefaults":"[]","warp":"false"}},
		"wait":{"opcode":"control_wait","next":"return","inputs":{"DURATION":[1,[4,"0"]]}},
		"return":{"opcode":"procedures_return","inputs":{"VALUE":[1,[4,"7"]]}}
	}`)
	run(t, r, 1, false)
	if r.Done() || r.numericExpressions[r.stage.source.Blocks["and"]] == nil {
		t.Fatal("expected a prepared condition followed by a suspended reporter")
	}
	*r.stage.variables["score"] = Variable{Name: "score", Value: float64(2)}
	run(t, r, 1, false)
	if !r.Done() || r.stage.variables["score"].Value != "true7" {
		t.Fatalf("condition was recomputed after suspension: %+v", r.Snapshot())
	}
}

func TestNumericExpressionReadsLiveData(t *testing.T) {
	r := motionRuntime(t, `{
		"root":{"opcode":"operator_multiply","inputs":{"NUM1":[2,"sum"],"NUM2":[2,"factor"]}},
		"sum":{"opcode":"operator_add","inputs":{"NUM1":[1,[12,"x","x"]],"NUM2":[2,"item"]}},
		"item":{"opcode":"data_itemoflist","fields":{"LIST":["values","values"]},"inputs":{"INDEX":[1,[4,"2"]]}},
		"factor":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["factor",null]}}
	}`)
	target := r.targets[1]
	th := &thread{target: target, params: procedureParameters{"factor": {value: float64(3)}}, expression: newExpression("root")}
	target.variables["x"] = &Variable{Value: "4"}
	r.stage.state.Lists["values"] = List{Items: []any{"2", "7"}}
	check := func(want float64) {
		t.Helper()
		th.expression.reset("root")
		budget := stepsPerFrame
		got, err := r.eval(context.Background(), th, "root", 1, &budget)
		if err != nil || got != want {
			t.Fatalf("live expression=%v, %v; want %v", got, err, want)
		}
	}
	check(33)
	*target.variables["x"] = Variable{Value: "5"}
	r.stage.state.Lists["values"].Items[1] = float64(9)
	th.params["factor"] = calculationValue{value: float64(4)}
	check(56)
	delete(target.variables, "x")
	budget := stepsPerFrame
	if _, err := r.eval(context.Background(), th, "root", 1, &budget); err == nil {
		t.Fatal("prepared expression ignored removed variable")
	}
}

func TestNumericExpressionReadsCurrentListLength(t *testing.T) {
	r := motionRuntime(t, `{
		"result":{"opcode":"operator_multiply","inputs":{"NUM1":[2,"length"],"NUM2":[1,[4,"13"]]}},
		"length":{"opcode":"data_lengthoflist","fields":{"LIST":["values","values"]}},
		"last":{"opcode":"data_itemoflist","fields":{"LIST":["values","values"]},"inputs":{"INDEX":[2,"length"]}}
	}`)
	target := r.targets[1]
	th := &thread{target: target, expression: newExpression("result")}
	check := func(id string, want any) {
		t.Helper()
		th.expression.reset(id)
		budget := stepsPerFrame
		got, err := r.eval(context.Background(), th, id, 1, &budget)
		if err != nil || got != want {
			t.Fatalf("%s=%v, %v; want %v", id, got, err, want)
		}
	}
	r.stage.state.Lists["values"] = List{Items: []any{"first", "last"}}
	check("result", float64(26))
	check("last", "last")
	r.stage.state.Lists["values"] = List{Items: []any{"new"}}
	check("result", float64(13))
	check("last", "new")
	target.state.Lists["values"] = List{Items: []any{"local", "second", "third"}}
	check("result", float64(39))
	check("last", "third")
	delete(target.state.Lists, "values")
	r.stage.state.Lists["values"] = List{}
	check("result", float64(0))
	check("last", "")
	delete(r.stage.state.Lists, "values")
	th.expression.reset("result")
	budget := stepsPerFrame
	if _, err := r.eval(context.Background(), th, "result", 1, &budget); err == nil {
		t.Fatal("removed list did not report an error")
	}
}

func TestNumericExpressionKeepsNamedListIndexes(t *testing.T) {
	for _, input := range []string{`[1,[10,"last"]]`, `[3,null,[10,"last"]]`, `[1,[12,"index","index"]]`, `[2,"param"]`} {
		r := motionRuntime(t, `{
			"root":{"opcode":"operator_add","inputs":{"NUM1":[2,"item"],"NUM2":[1,[4,"1"]]}},
			"item":{"opcode":"data_itemoflist","fields":{"LIST":["values","values"]},"inputs":{"INDEX":`+input+`}},
			"param":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["index",null]}}
		}`)
		r.stage.state.Lists["values"] = List{Items: []any{float64(2), float64(7)}}
		r.stage.variables["index"] = &Variable{Value: "last"}
		th := &thread{target: r.targets[1], params: procedureParameters{"index": {value: "last"}}, expression: newExpression("root")}
		budget := stepsPerFrame
		got, err := r.eval(context.Background(), th, "root", 1, &budget)
		if err != nil || got != float64(8) {
			t.Fatalf("index %s=%v, %v", input, got, err)
		}
	}
}

func TestPreparedDynamicListIndexesReadCurrentValues(t *testing.T) {
	for _, input := range []string{`[1,[12,"index","index"]]`, `[2,"param"]`, `[2,"indirect"]`} {
		r := motionRuntime(t, `{
			"item":{"opcode":"data_itemoflist","fields":{"LIST":["values","values"]},"inputs":{"INDEX":`+input+`}},
			"param":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["index",null]}},
			"indirect":{"opcode":"data_itemoflist","fields":{"LIST":["indexes","indexes"]},"inputs":{"INDEX":[1,[4,"1"]]}}
		}`)
		th := &thread{target: r.targets[1], params: procedureParameters{}, expression: newExpression("item")}
		for _, tc := range []struct {
			index any
			items []any
			want  any
		}{
			{float64(1), []any{"first", "second"}, "first"},
			{float64(2.9), []any{"first", "second"}, "second"},
			{"last", []any{"first", "second"}, "second"},
			{"last", []any{"changed"}, "changed"},
			{"random", []any{"only"}, "only"},
			{"any", []any{true}, true},
			{"random", nil, ""},
			{"any", nil, ""},
			{"last", nil, ""},
			{true, []any{"first"}, "first"},
			{false, []any{"first"}, ""},
			{nil, []any{"first"}, ""},
			{"", []any{"first"}, ""},
			{"missing", []any{"first"}, ""},
			{math.NaN(), []any{"first"}, ""},
			{math.Inf(1), []any{"first"}, ""},
		} {
			r.stage.variables["index"] = &Variable{Value: tc.index}
			th.params["index"] = calculationValue{value: tc.index}
			r.stage.state.Lists["indexes"] = List{Items: []any{tc.index}}
			r.stage.state.Lists["values"] = List{Items: tc.items}
			th.expression.reset("item")
			budget := stepsPerFrame
			got, err := r.eval(context.Background(), th, "item", 1, &budget)
			if err != nil || got != tc.want {
				t.Fatalf("source %s index %v: got %v, want %v: %v", input, tc.index, got, tc.want, err)
			}
		}
	}
}

func TestPreparedJoinMatchesInterpreter(t *testing.T) {
	r := motionRuntime(t, `{
		"join":{"opcode":"operator_join","inputs":{"STRING1":[1,[12,"value","value"]],"STRING2":[2,"suffix"]}},
		"suffix":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["suffix",null]}},
		"number":{"opcode":"operator_add","inputs":{"NUM1":[2,"join"],"NUM2":[1,[4,"1"]]}},
		"equal":{"opcode":"operator_equals","inputs":{"OPERAND1":[2,"join"],"OPERAND2":[1,[4,"7"]]}}
	}`)
	th := &thread{target: r.targets[1], params: procedureParameters{}, expression: newExpression("join")}
	for _, id := range []string{"join", "number", "equal"} {
		if r.numericExpression(th.target, th.target.source.Blocks[id], 0) == nil {
			t.Fatalf("pure join expression %s was not prepared", id)
		}
	}
	for _, value := range []any{nil, true, false, "007", "", "문자🙂", float64(2.5), math.NaN(), math.Inf(1), math.Copysign(0, -1)} {
		for _, suffix := range []any{"", "0", "🙂", float64(3), true} {
			r.stage.variables["value"] = &Variable{Value: value}
			th.params["suffix"] = calculationValue{value: suffix}
			for _, id := range []string{"join", "number", "equal"} {
				th.expression.reset(id)
				budget := stepsPerFrame
				got, err := r.eval(context.Background(), th, id, 1, &budget)
				remaining := budget
				prepared := r.numericExpressions
				r.numericExpressions = make(map[*project.Block]*numericExpression)
				for _, block := range th.target.source.Blocks {
					r.numericExpressions[block] = nil
				}
				th.expression.reset(id)
				budget = stepsPerFrame
				want, originalErr := r.eval(context.Background(), th, id, 1, &budget)
				r.numericExpressions = prepared
				if fmt.Sprint(err) != fmt.Sprint(originalErr) || budget != remaining || !reflect.DeepEqual(got, want) {
					t.Fatalf("%s(%v, %v): got %v/%v/%d, want %v/%v/%d", id, value, suffix, got, err, remaining, want, originalErr, budget)
				}
			}
		}
	}
}

func TestPreparedListReadMatchesInterpreter(t *testing.T) {
	r := motionRuntime(t, `{
		"item":{"opcode":"data_itemoflist","fields":{"LIST":["values","values"]},"inputs":{"INDEX":[2,"offset"]}},
		"offset":{"opcode":"operator_add","inputs":{"NUM1":[1,[12,"index","index"]],"NUM2":[1,[4,"1"]]}}
	}`)
	th := &thread{target: r.targets[1], expression: newExpression("item")}
	blocks := th.target.source.Blocks
	for _, value := range []any{"07", "", true, false, float64(12.5), math.NaN(), nil} {
		for _, index := range []float64{0, 1, 2, -1, math.NaN(), math.Inf(1)} {
			r.stage.state.Lists["values"] = List{Items: []any{"first", value}}
			r.stage.variables["index"] = &Variable{Value: index}
			r.numericExpressions = nil
			th.expression.reset("item")
			budget := stepsPerFrame
			got, err := r.eval(context.Background(), th, "item", 1, &budget)
			remaining := budget
			for _, block := range blocks {
				r.numericExpressions[block] = nil
			}
			th.expression.reset("item")
			budget = stepsPerFrame
			want, originalErr := r.eval(context.Background(), th, "item", 1, &budget)
			if fmt.Sprint(err) != fmt.Sprint(originalErr) || budget != remaining {
				t.Fatalf("list index %v: error/budget differs: %v/%d vs %v/%d", index, err, remaining, originalErr, budget)
			}
			if x, ok := got.(float64); ok && math.IsNaN(x) {
				if y, ok := want.(float64); ok && math.IsNaN(y) {
					continue
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("list index %v: got %v (%T), want %v (%T)", index, got, got, want, want)
			}
		}
	}
}

func TestNumericExpressionTrigChanges(t *testing.T) {
	for _, op := range []string{"sin", "cos", "tan"} {
		r := motionRuntime(t, `{
			"trig":{"opcode":"operator_mathop","fields":{"OPERATOR":["`+op+`",null]},"inputs":{"NUM":[1,[12,"angle","angle"]]}}
		}`)
		th := &thread{target: r.targets[1], expression: newExpression("trig")}
		for _, angle := range []float64{30, 30, 90, 180, 270, 0, math.Copysign(0, -1), math.Inf(1), math.NaN(), -30} {
			r.stage.variables["angle"] = &Variable{Value: angle}
			th.expression.reset("trig")
			budget := stepsPerFrame
			got, err := r.eval(context.Background(), th, "trig", 1, &budget)
			if err != nil {
				t.Fatal(err)
			}
			want := mathOperator(op, number(angle))
			x := got.(float64)
			if !(math.IsNaN(x) && math.IsNaN(want)) && math.Float64bits(x) != math.Float64bits(want) {
				t.Fatalf("%s(%v)=%v, want %v", op, angle, got, want)
			}
		}
	}
}

func BenchmarkNumericExpressionTrig(b *testing.B) {
	for _, op := range []string{"sin", "cos", "tan"} {
		b.Run(op, func(b *testing.B) {
			r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: map[string]*project.Block{
				"trig": {Opcode: "operator_mathop", Fields: map[string][]any{"OPERATOR": {op}},
					Inputs: map[string][]any{"NUM": {float64(1), []any{float64(12), "angle", "angle"}}}},
			}}}})
			r.stage.variables["angle"] = &Variable{Value: float64(30)}
			plan := r.numericExpression(r.stage, r.stage.source.Blocks["trig"], 0)
			th := &thread{target: r.stage}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := plan.evaluate(context.Background(), r, th); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestNumericExpressionPreservesLimits(t *testing.T) {
	r := motionRuntime(t, `{
		"root":{"opcode":"operator_add","inputs":{"NUM1":[2,"child"],"NUM2":[1,[4,"1"]]}},
		"child":{"opcode":"operator_multiply","inputs":{"NUM1":[1,[4,"2"]],"NUM2":[1,[4,"3"]]}}
	}`)
	th := &thread{target: r.targets[1], expression: newExpression("root")}
	budget := stepsPerFrame
	if _, err := r.eval(context.Background(), th, "root", 1, &budget); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ budget, depth int }{{1, 1}, {stepsPerFrame, 128}} {
		th.expression.reset("root")
		budget = tc.budget
		if _, err := r.eval(context.Background(), th, "root", tc.depth, &budget); err == nil {
			t.Fatalf("budget/depth limit ignored: %+v", tc)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget = stepsPerFrame
	if _, err := r.eval(ctx, th, "root", 1, &budget); err != context.Canceled {
		t.Fatalf("cancelled expression=%v", err)
	}
}
