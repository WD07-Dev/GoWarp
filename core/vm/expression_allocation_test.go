package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gowarp/core/project"
)

func TestConstantInputDoesNotAllocateContinuation(t *testing.T) {
	r := New(&project.Project{})
	th := &thread{}
	block := &project.Block{Inputs: literalInputs(map[string]any{"VALUE": "12"})}
	allocations := testing.AllocsPerRun(100, func() {
		th.expression = newExpression("set")
		budget := stepsPerFrame
		value, err := r.input(context.Background(), th, block, "VALUE", 0, &budget)
		if err != nil || value != "12" {
			t.Fatalf("constant input = %v, error = %v", value, err)
		}
	})
	if allocations > 1 {
		t.Fatalf("constant input allocated %g objects; only the expression needs storage", allocations)
	}
}

func BenchmarkArithmeticExpression(b *testing.B) {
	r := New(&project.Project{Targets: []*project.Target{{Blocks: map[string]*project.Block{
		"set": {Inputs: map[string][]any{"X": {float64(2), "add"}}},
		"add": {Opcode: "operator_add", Inputs: literalInputs(map[string]any{"NUM1": "5", "NUM2": "7"})},
	}}}})
	th := &thread{target: r.targets[0], expression: newExpression("set")}
	block := th.target.source.Blocks["set"]
	b.ReportAllocs()
	for b.Loop() {
		th.expression.reset("set")
		budget := stepsPerFrame
		value, err := r.input(context.Background(), th, block, "X", 0, &budget)
		if err != nil || value != float64(12) {
			b.Fatalf("arithmetic = %v, error = %v", value, err)
		}
	}
}

func fallbackReporterInput() (*Runtime, *thread, *project.Block) {
	blocks := map[string]*project.Block{
		"set": {Inputs: map[string][]any{"VALUE": {float64(2), "multiply"}}},
		"multiply": {Opcode: "operator_multiply", Inputs: map[string][]any{
			"NUM1": {float64(2), "counter"},
			"NUM2": {float64(1), []any{float64(4), "2"}},
		}},
		"counter": {Opcode: "control_get_counter"},
	}
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
	th := &thread{target: r.stage, expression: newExpression("set")}
	return r, th, blocks["set"]
}

func BenchmarkReporterInputFallback(b *testing.B) {
	r, th, block := fallbackReporterInput()
	r.counter = 120.125
	b.ReportAllocs()
	for b.Loop() {
		th.expression.reset("set")
		budget := stepsPerFrame
		value, err := r.input(context.Background(), th, block, "VALUE", 0, &budget)
		if err != nil || value != float64(240.25) {
			b.Fatalf("value=%v err=%v", value, err)
		}
	}
}

func TestReporterInputFallbackReadsCurrentValue(t *testing.T) {
	r, th, block := fallbackReporterInput()
	for _, counter := range []float64{1, 120.125, -32.5} {
		r.counter = counter
		th.expression.reset("set")
		budget := stepsPerFrame
		value, err := r.input(context.Background(), th, block, "VALUE", 0, &budget)
		if err != nil || value != counter*2 {
			t.Fatalf("value=%v err=%v, want %v", value, err, counter*2)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	th.expression.reset("set")
	budget := stepsPerFrame
	if _, err := r.input(ctx, th, block, "VALUE", 0, &budget); err != context.Canceled {
		t.Fatalf("cached reporter lost cancellation: %v", err)
	}
}

func joinedCostumeInput() (*Runtime, *thread, *project.Block) {
	blocks := map[string]*project.Block{
		"set": {Inputs: map[string][]any{"COSTUME": {float64(2), "join"}}},
		"join": {Opcode: "operator_join", Inputs: map[string][]any{
			"STRING1": {float64(2), "prefix"}, "STRING2": {float64(2), "index"},
		}},
		"prefix": {Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"prefix"}}},
		"index":  {Opcode: "operator_round", Inputs: map[string][]any{"NUM": {float64(1), []any{float64(12), "frame", "frame"}}}},
	}
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
	r.stage.variables["frame"] = &Variable{Value: float64(2.6)}
	th := &thread{target: r.stage, params: procedureParameters{"prefix": {value: "animation/"}}}
	return r, th, blocks["set"]
}

func TestJoinedCostumeInputAvoidsNestedContinuations(t *testing.T) {
	r, th, block := joinedCostumeInput()
	allocations := testing.AllocsPerRun(100, func() {
		th.expression = newExpression("set")
		budget := stepsPerFrame
		value, err := r.input(context.Background(), th, block, "COSTUME", 0, &budget)
		if err != nil || value != "animation/3" {
			t.Fatalf("costume name=%v, error=%v", value, err)
		}
	})
	// Expression, outer continuation, joined string, and boxed result only.
	if allocations > 4 {
		t.Fatalf("joining a costume name allocated %g objects; limit is 4", allocations)
	}
}

func BenchmarkJoinedCostumeInput(b *testing.B) {
	r, th, block := joinedCostumeInput()
	th.expression = newExpression("set")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		th.expression.reset("set")
		budget := stepsPerFrame
		value, err := r.input(context.Background(), th, block, "COSTUME", 0, &budget)
		if err != nil || value != "animation/3" {
			b.Fatalf("costume name=%v, error=%v", value, err)
		}
	}
}

func TestNewArithmeticExpressionAllocation(t *testing.T) {
	r := motionRuntime(t, `{
		"set":{"opcode":"motion_setx","inputs":{"X":[2,"multiply"]}},
		"multiply":{"opcode":"operator_multiply","inputs":{"NUM1":[2,"add"],"NUM2":[1,[4,"6"]]}},
		"add":{"opcode":"operator_add","inputs":{"NUM1":[1,[4,"5"]],"NUM2":[1,[4,"2"]]}}
	}`)
	th := &thread{target: r.targets[1]}
	ctx := context.Background()
	allocations := testing.AllocsPerRun(100, func() {
		th.expression = newExpression("set")
		budget := stepsPerFrame
		value, err := r.input(ctx, th, th.target.source.Blocks["set"], "X", 0, &budget)
		if err != nil || value != float64(42) {
			t.Fatalf("arithmetic result=%v error=%v", value, err)
		}
	})
	if allocations > 10 {
		t.Fatalf("a five-input arithmetic expression allocated %g objects; limit is 10", allocations)
	}
}

func TestCoordinateExpressionAvoidsIntermediateAllocations(t *testing.T) {
	r := motionRuntime(t, `{
		"set":{"opcode":"motion_setx","inputs":{"X":[2,"result"]}},
		"result":{"opcode":"operator_add","inputs":{"NUM1":[2,"scaled"],"NUM2":[1,[4,"10"]]}},
		"scaled":{"opcode":"operator_multiply","inputs":{"NUM1":[2,"difference"],"NUM2":[1,[4,"0.75"]]}},
		"difference":{"opcode":"operator_subtract","inputs":{"NUM1":[2,"sine"],"NUM2":[2,"cosine"]}},
		"sine":{"opcode":"operator_mathop","fields":{"OPERATOR":["sin",null]},"inputs":{"NUM":[1,[4,"30"]]}},
		"cosine":{"opcode":"operator_mathop","fields":{"OPERATOR":["cos",null]},"inputs":{"NUM":[1,[4,"60"]]}}
	}`)
	th := &thread{target: r.targets[1], expression: newExpression("set")}
	block := th.target.source.Blocks["set"]
	allocations := testing.AllocsPerRun(100, func() {
		th.expression.reset("set")
		budget := stepsPerFrame
		value, err := r.input(context.Background(), th, block, "X", 0, &budget)
		if err != nil || value != float64(10) {
			t.Fatalf("coordinate=%v, error=%v", value, err)
		}
	})
	if allocations > 1 {
		t.Fatalf("coordinate expression allocated %g times; only its final result should be boxed", allocations)
	}
}

func TestListCoordinateReadAvoidsIntermediateAllocations(t *testing.T) {
	r := motionRuntime(t, `{
		"set":{"opcode":"motion_setx","inputs":{"X":[2,"item"]}},
		"item":{"opcode":"data_itemoflist","fields":{"LIST":["bones","bones"]},"inputs":{"INDEX":[2,"offset"]}},
		"offset":{"opcode":"operator_add","inputs":{"NUM1":[2,"stride"],"NUM2":[1,[4,"2"]]}},
		"stride":{"opcode":"operator_multiply","inputs":{"NUM1":[1,[12,"bone","bone"]],"NUM2":[1,[4,"13"]]}}
	}`)
	r.stage.variables["bone"] = &Variable{Value: float64(30)}
	items := make([]any, 400)
	items[391] = "coordinate"
	r.stage.state.Lists["bones"] = List{Items: items}
	th := &thread{target: r.targets[1], expression: newExpression("set")}
	block := th.target.source.Blocks["set"]
	allocations := testing.AllocsPerRun(100, func() {
		th.expression.reset("set")
		budget := stepsPerFrame
		value, err := r.input(context.Background(), th, block, "X", 0, &budget)
		if err != nil || value != "coordinate" {
			t.Fatalf("list value=%v, error=%v", value, err)
		}
	})
	if allocations != 0 {
		t.Fatalf("reading an existing list value allocated %g objects", allocations)
	}
}

func dynamicListArithmetic() (*Runtime, *thread, *project.Block) {
	blocks := map[string]*project.Block{
		"set": {Inputs: map[string][]any{"X": {float64(2), "result"}}},
		"result": {Opcode: "operator_add", Inputs: map[string][]any{
			"NUM1": {float64(2), "scaled"}, "NUM2": {float64(1), []any{float64(4), "4"}},
		}},
		"scaled": {Opcode: "operator_multiply", Inputs: map[string][]any{
			"NUM1": {float64(2), "item"}, "NUM2": {float64(1), []any{float64(4), "3"}},
		}},
		"item": {Opcode: "data_itemoflist", Fields: map[string][]any{"LIST": {"bones", "bones"}},
			Inputs: map[string][]any{"INDEX": {float64(1), []any{float64(12), "index", "index"}}}},
	}
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
	r.stage.state.Lists["bones"] = List{Items: []any{float64(9), float64(2.5)}}
	r.stage.variables["index"] = &Variable{Value: float64(2)}
	th := &thread{target: r.stage, expression: newExpression("set")}
	return r, th, blocks["set"]
}

func TestDynamicListArithmeticAvoidsIntermediateAllocations(t *testing.T) {
	r, th, block := dynamicListArithmetic()
	allocations := testing.AllocsPerRun(100, func() {
		th.expression.reset("set")
		budget := stepsPerFrame
		got, err := r.input(context.Background(), th, block, "X", 0, &budget)
		if err != nil || got != float64(11.5) {
			t.Fatalf("coordinate=%v, error=%v", got, err)
		}
	})
	if allocations > 1 {
		t.Fatalf("dynamic list arithmetic allocated %g objects; only its final result needs boxing", allocations)
	}
}

func BenchmarkDynamicListArithmetic(b *testing.B) {
	r, th, block := dynamicListArithmetic()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		th.expression.reset("set")
		budget := stepsPerFrame
		got, err := r.input(context.Background(), th, block, "X", 0, &budget)
		if err != nil || got != float64(11.5) {
			b.Fatalf("coordinate=%v, error=%v", got, err)
		}
	}
}

func TestListLengthArithmeticAvoidsIntermediateAllocations(t *testing.T) {
	r := motionRuntime(t, `{
		"set":{"opcode":"motion_setx","inputs":{"X":[2,"result"]}},
		"result":{"opcode":"operator_add","inputs":{"NUM1":[2,"scaled"],"NUM2":[1,[4,"0.5"]]}},
		"scaled":{"opcode":"operator_multiply","inputs":{"NUM1":[2,"length"],"NUM2":[1,[4,"13"]]}},
		"length":{"opcode":"data_lengthoflist","fields":{"LIST":["bones","bones"]}}
	}`)
	r.stage.state.Lists["bones"] = List{Items: make([]any, 100)}
	th := &thread{target: r.targets[1], expression: newExpression("set")}
	block := th.target.source.Blocks["set"]
	allocations := testing.AllocsPerRun(100, func() {
		th.expression.reset("set")
		budget := stepsPerFrame
		got, err := r.input(context.Background(), th, block, "X", 0, &budget)
		if err != nil || got != float64(1300.5) {
			t.Fatalf("list length arithmetic=%v, error=%v", got, err)
		}
	})
	if allocations > 1 {
		t.Fatalf("list length arithmetic allocated %g objects; only the final result needs boxing", allocations)
	}
}

func TestConditionExpressionUsesOneInputContinuation(t *testing.T) {
	r := motionRuntime(t, `{
		"if":{"opcode":"control_if","inputs":{"CONDITION":[2,"not"]}},
		"not":{"opcode":"operator_not","inputs":{"OPERAND":[2,"equal"]}},
		"equal":{"opcode":"operator_equals","inputs":{"OPERAND1":[2,"item"],"OPERAND2":[2,"color"]}},
		"item":{"opcode":"data_itemoflist","fields":{"LIST":["bones","bones"]},"inputs":{"INDEX":[1,[4,"1"]]}},
		"color":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["color",null]}}
	}`)
	r.stage.state.Lists["bones"] = List{Items: []any{"하양"}}
	th := &thread{target: r.targets[1], params: procedureParameters{"color": {value: "파랑"}}}
	block := th.target.source.Blocks["if"]
	allocations := testing.AllocsPerRun(100, func() {
		th.expression = newExpression("if")
		budget := stepsPerFrame
		got, err := r.input(context.Background(), th, block, "CONDITION", 0, &budget)
		if err != nil || got != true {
			t.Fatalf("condition=%v, error=%v", got, err)
		}
	})
	if allocations > 2 {
		t.Fatalf("condition allocated %g objects; only the expression and its outer input need storage", allocations)
	}
}

func TestRepeatedInputsReuseEvaluationStorage(t *testing.T) {
	r := motionRuntime(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
		"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"set"]}},
		"set":{"opcode":"motion_setx","inputs":{"X":[1,[4,"10"]]}}
	}`)
	ctx := context.Background()
	allocations := testing.AllocsPerRun(100, func() {
		if err := r.Run(ctx, 1, 60, false); err != nil {
			t.Fatal(err)
		}
	})
	if allocations > 2 {
		t.Fatalf("one repeated input allocated %g objects per frame", allocations)
	}
	if r.targets[1].state.X != 10 {
		t.Fatal("reusing evaluation storage changed the input value")
	}
}

func TestRepeatedProcedureCallsDoNotReparseMetadata(t *testing.T) {
	r := fixture(t, procedureProject)
	th := &thread{target: r.targets[0]}
	b := th.target.source.Blocks["call"]
	input := func(string) (any, error) {
		return float64(3), nil
	}
	allocations := testing.AllocsPerRun(100, func() {
		if _, err := r.call(context.Background(), th, b, input, false); err != nil {
			t.Fatal(err)
		}
		th.stack = th.stack[:0]
		th.params = nil
		th.procedure = ""
	})
	if allocations > 3 {
		t.Fatalf("procedure call allocated %g objects", allocations)
	}
}

func TestReturnedProcedureArgumentsReuseStorage(t *testing.T) {
	r := fixture(t, procedureProject)
	th := &thread{target: r.targets[0]}
	b := th.target.source.Blocks["call"]
	input := func(string) (any, error) {
		return "3", nil
	}
	allocations := testing.AllocsPerRun(100, func() {
		if _, err := r.call(context.Background(), th, b, input, false); err != nil {
			t.Fatal(err)
		}
		if th.params["n"].boxed() != "3" {
			t.Fatal("reused arguments changed the parameter value")
		}
		th.stopScript()
		if th.params != nil {
			t.Fatal("return did not restore caller arguments")
		}
	})
	if allocations != 0 {
		t.Fatalf("repeated call/return allocated %g objects", allocations)
	}
}

func TestSmallIntegerArithmeticReusesValues(t *testing.T) {
	r := motionRuntime(t, `{
		"set":{"opcode":"motion_setx","inputs":{"X":[2,"add"]}},
		"add":{"opcode":"operator_add","inputs":{"NUM1":[1,[12,"value","value"]],"NUM2":[1,[4,"1"]]}}
	}`)
	th := &thread{target: r.targets[1], expression: newExpression("set")}
	block := th.target.source.Blocks["set"]
	for _, value := range []float64{-1000, -1, 0, 99, 1000} {
		r.stage.variables["value"] = &Variable{Value: value}
		allocations := testing.AllocsPerRun(100, func() {
			th.expression.reset("set")
			budget := stepsPerFrame
			got, err := r.input(context.Background(), th, block, "X", 0, &budget)
			if err != nil || got != value+1 {
				t.Fatalf("sum=%v, error=%v", got, err)
			}
		})
		if allocations != 0 {
			t.Errorf("integer %g allocated %g objects", value+1, allocations)
		}
	}
}

func TestSmallIntegerVariableUpdatesReuseValues(t *testing.T) {
	r := fixture(t, `{
		"repeat":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"100"]],"SUBSTACK":[2,"increment"]}},
		"increment":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
	}`)
	th := &thread{target: r.stage, warp: true}
	allocations := testing.AllocsPerRun(100, func() {
		*r.stage.variables["score"] = Variable{Value: float64(0)}
		th.pc, th.done = "repeat", false
		if err := r.step(context.Background(), th); err != nil {
			t.Fatal(err)
		}
		if !th.done || r.stage.variables["score"].Value != float64(100) {
			t.Fatal("integer reuse changed loop execution")
		}
	})
	if allocations > 2 {
		t.Fatalf("counter loop allocated %g objects; integer results should be reused", allocations)
	}
}

func TestVariableArithmeticAvoidsInputContinuation(t *testing.T) {
	r := fixture(t, `{
		"set":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"add"]}},
		"add":{"opcode":"operator_add","inputs":{"NUM1":[1,[4,"1.5"]],"NUM2":[1,[4,"2.5"]]}}
	}`)
	th := &thread{target: r.stage}
	allocations := testing.AllocsPerRun(100, func() {
		th.pc, th.done, th.expression = "set", false, nil
		*r.stage.variables["score"] = Variable{Value: float64(0)}
		if err := r.step(context.Background(), th); err != nil {
			t.Fatal(err)
		}
		if r.stage.variables["score"].Value != float64(4) {
			t.Fatal("variable update changed arithmetic")
		}
	})
	if allocations > 1 {
		t.Fatalf("variable arithmetic allocated %g objects; only the execution slice needs storage", allocations)
	}
}

func TestConstantArithmeticResultReusesValue(t *testing.T) {
	r := motionRuntime(t, `{
		"set":{"opcode":"motion_setx","inputs":{"X":[2,"divide"]}},
		"divide":{"opcode":"operator_divide","inputs":{"NUM1":[1,[4,"1"]],"NUM2":[1,[4,"3"]]}}
	}`)
	th := &thread{target: r.targets[1], expression: newExpression("set")}
	block := th.target.source.Blocks["set"]
	allocations := testing.AllocsPerRun(100, func() {
		th.expression.reset("set")
		budget := stepsPerFrame
		got, err := r.input(context.Background(), th, block, "X", 0, &budget)
		if err != nil || got != float64(1.0/3.0) {
			t.Fatalf("constant division=%v, error=%v", got, err)
		}
	})
	if allocations != 0 {
		t.Fatalf("constant fraction allocated %g objects", allocations)
	}
}

func coordinateMotionRuntime() (*Runtime, *thread) {
	r := New(&project.Project{Targets: []*project.Target{{Blocks: map[string]*project.Block{
		"move": {Opcode: "motion_gotoxy", Inputs: map[string][]any{
			"X": {float64(2), "sum"}, "Y": {float64(2), "arg"},
		}},
		"sum": {Opcode: "operator_add", Inputs: map[string][]any{
			"NUM1": {float64(2), "arg"}, "NUM2": {float64(1), []any{float64(4), "0.25"}},
		}},
		"arg": {Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"x"}}},
	}}}})
	return r, &thread{target: r.targets[0], params: procedureParameters{"x": {value: float64(12.5)}}}
}

func TestCoordinateMotionAvoidsBoxedInputs(t *testing.T) {
	r, th := coordinateMotionRuntime()
	ctx := context.Background()
	allocations := testing.AllocsPerRun(100, func() {
		th.pc, th.done = "move", false
		if err := r.step(ctx, th); err != nil {
			t.Fatal(err)
		}
	})
	if th.target.state.X != 12.75 || th.target.state.Y != 12.5 {
		t.Fatalf("incorrect coordinate evaluation: %g, %g", th.target.state.X, th.target.state.Y)
	}
	if allocations != 0 {
		t.Fatalf("coordinate motion allocated %g objects per move", allocations)
	}
}

func BenchmarkCoordinateMotion(b *testing.B) {
	r, th := coordinateMotionRuntime()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		th.pc, th.done = "move", false
		if err := r.step(ctx, th); err != nil {
			b.Fatal(err)
		}
	}
}

// Warming an input must not bypass evaluation limits or reuse a previous value.
func TestRepeatedReporterInputMatchesDirectEvaluation(t *testing.T) {
	for _, reporter := range []string{"join", "index", "prefix", "condition"} {
		for _, depth := range []int{0, 126, 128} {
			for _, limit := range []int{0, 1, stepsPerFrame} {
				t.Run(fmt.Sprintf("%s/depth%d/budget%d", reporter, depth, limit), func(t *testing.T) {
					r, th, block := joinedCostumeInput()
					th.target.source.Blocks["condition"] = &project.Block{
						Opcode: "operator_or",
						Inputs: map[string][]any{"OPERAND1": {float64(2), "prefix"}, "OPERAND2": {float64(2), "index"}},
					}
					block.Inputs["COSTUME"][1] = reporter
					th.expression = newExpression("set")
					budget := stepsPerFrame
					if _, err := r.input(context.Background(), th, block, "COSTUME", 0, &budget); err != nil {
						t.Fatal(err)
					}
					th.expression.reset("set")
					r.stage.variables["frame"] = &Variable{Value: float64(8.2)}
					th.params["prefix"] = calculationValue{value: "updated/"}
					budget, directBudget := limit, limit
					got, err := r.input(context.Background(), th, block, "COSTUME", depth, &budget)
					want, directErr := r.eval(context.Background(), th, reporter, depth+1, &directBudget)
					if !reflect.DeepEqual(got, want) || fmt.Sprint(err) != fmt.Sprint(directErr) || budget != directBudget {
						t.Fatalf("input=(%v, %v, %d), direct=(%v, %v, %d)", got, err, budget, want, directErr, directBudget)
					}
					th.expression.reset("set")
					ctx, cancel := context.WithCancel(context.Background())
					cancel()
					budget = stepsPerFrame
					if _, err := r.input(ctx, th, block, "COSTUME", 0, &budget); err != context.Canceled {
						t.Fatalf("cancelled input: %v", err)
					}
				})
			}
		}
	}
}

func BenchmarkLiteralInput(b *testing.B) {
	r := New(&project.Project{})
	th := &thread{expression: newExpression("set")}
	block := &project.Block{Inputs: literalInputs(map[string]any{"VALUE": "12"})}
	b.ReportAllocs()
	for b.Loop() {
		budget := stepsPerFrame
		value, err := r.input(context.Background(), th, block, "VALUE", 0, &budget)
		if err != nil || value != "12" {
			b.Fatalf("literal=%v, error=%v", value, err)
		}
	}
}

func textTimingCondition() (*Runtime, *thread, *project.Block) {
	blocks := project.Blocks{
		"if":     {Opcode: "control_if", Inputs: map[string][]any{"CONDITION": {float64(2), "ready"}}},
		"ready":  {Opcode: "operator_and", Inputs: map[string][]any{"OPERAND1": {float64(2), "time"}, "OPERAND2": {float64(2), "match"}}},
		"time":   {Opcode: "operator_gt", Inputs: map[string][]any{"OPERAND1": {float64(2), "timer"}, "OPERAND2": {float64(1), []any{float64(4), "2"}}}},
		"timer":  {Opcode: "sensing_timer"},
		"match":  {Opcode: "operator_equals", Inputs: map[string][]any{"OPERAND1": {float64(2), "letter"}, "OPERAND2": {float64(1), []any{float64(10), "a"}}}},
		"letter": {Opcode: "operator_letter_of", Inputs: map[string][]any{"LETTER": {float64(2), "length"}, "STRING": {float64(2), "text"}}},
		"length": {Opcode: "operator_length", Inputs: map[string][]any{"STRING": {float64(2), "text"}}},
		"text":   {Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"text"}}},
	}
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
	r.elapsed = 3
	th := &thread{target: r.stage, params: procedureParameters{"text": {value: strings.Repeat("a", 2048)}}}
	return r, th, blocks["if"]
}

func TestTextTimingConditionAvoidsIntermediateStorage(t *testing.T) {
	r, th, block := textTimingCondition()
	allocations := testing.AllocsPerRun(100, func() {
		th.expression = newExpression("if")
		budget := stepsPerFrame
		got, err := r.input(context.Background(), th, block, "CONDITION", 0, &budget)
		if err != nil || got != true {
			t.Fatalf("text condition = %v, %v", got, err)
		}
	})
	if allocations > 4 {
		t.Fatalf("text condition allocates %.0f objects per evaluation; want no string-wide buffers or nested continuations", allocations)
	}
}

func BenchmarkTextTimingCondition(b *testing.B) {
	r, th, block := textTimingCondition()
	th.expression = newExpression("if")
	b.ReportAllocs()
	for b.Loop() {
		th.expression.reset("if")
		budget := stepsPerFrame
		got, err := r.input(context.Background(), th, block, "CONDITION", 0, &budget)
		if err != nil || got != true {
			b.Fatalf("text condition = %v, %v", got, err)
		}
	}
}

func TestTextTimingConditionReadsCurrentValues(t *testing.T) {
	r, th, block := textTimingCondition()
	th.expression = newExpression("if")
	for _, tc := range []struct {
		elapsed, timer float64
		text           string
		want           bool
		steps          int
	}{
		{3, 0, "a", true, 8},
		{3, 0, "b", false, 8},
		{3, 2, "a", false, 3},
		{5, 2, "😀a", true, 8},
		{5, 2, "", false, 8},
	} {
		r.elapsed, r.timerSeconds = tc.elapsed, tc.timer
		th.params["text"] = calculationValue{value: tc.text}
		th.expression.reset("if")
		budget := stepsPerFrame
		got, err := r.input(context.Background(), th, block, "CONDITION", 0, &budget)
		if err != nil || got != tc.want || budget != stepsPerFrame-tc.steps {
			t.Fatalf("%+v: result=%v error=%v steps=%d", tc, got, err, stepsPerFrame-budget)
		}
	}
}

func TestSingleInputEffectAvoidsContinuationAllocation(t *testing.T) {
	r := motionRuntime(t, `{
		"effect":{"opcode":"looks_seteffectto","fields":{"EFFECT":["ghost",null]},"inputs":{"VALUE":[2,"value"]}},
		"value":{"opcode":"operator_add","inputs":{"NUM1":[1,[4,"40"]],"NUM2":[1,[4,"2"]]}}
	}`)
	th := &thread{target: r.targets[1]}
	allocations := testing.AllocsPerRun(100, func() {
		th.pc, th.done, th.expression = "effect", false, nil
		if err := r.step(context.Background(), th); err != nil {
			t.Fatal(err)
		}
	})
	if th.target.state.Effects.Ghost != 42 {
		t.Fatal("prepared input changed the effect value")
	}
	if allocations > 1 {
		t.Fatalf("single synchronous input allocated %g objects; only the statement expression needs storage", allocations)
	}
}

func TestVariableInputAvoidsContinuationAllocation(t *testing.T) {
	r := motionRuntime(t, `{
		"effect":{"opcode":"looks_seteffectto","fields":{"EFFECT":["ghost",null]},"inputs":{"VALUE":[1,[12,"amount","amount"]]}}
	}`)
	original := r.targets[1]
	original.variables["amount"] = &Variable{Value: float64(10)}
	r.createClone(original)
	clone := r.targets[len(r.targets)-1]
	clone.variables["amount"] = &Variable{Value: float64(42)}
	for _, target := range []*target{original, clone, original} {
		th := &thread{target: target}
		allocations := testing.AllocsPerRun(100, func() {
			th.pc, th.done, th.expression = "effect", false, nil
			if err := r.step(context.Background(), th); err != nil {
				t.Fatal(err)
			}
		})
		want := 10.0
		if target == clone {
			want = 42
		}
		if target.state.Effects.Ghost != want {
			t.Fatalf("clone %d: ghost=%v, want %v", target.state.CloneID, target.state.Effects.Ghost, want)
		}
		if allocations > 1 {
			t.Fatalf("variable input allocated %g objects; only the statement expression needs storage", allocations)
		}
	}
}

func BenchmarkSingleInputEffectStatement(b *testing.B) {
	var blocks map[string]*project.Block
	if err := json.Unmarshal([]byte(`{
		"effect":{"opcode":"looks_seteffectto","fields":{"EFFECT":["ghost",null]},"inputs":{"VALUE":[2,"value"]}},
		"value":{"opcode":"operator_add","inputs":{"NUM1":[1,[4,"40"]],"NUM2":[1,[4,"2"]]}}
	}`), &blocks); err != nil {
		b.Fatal(err)
	}
	r := New(&project.Project{Targets: []*project.Target{{Blocks: blocks}}})
	th := &thread{target: r.targets[0]}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		th.pc, th.done = "effect", false
		if err := r.step(ctx, th); err != nil {
			b.Fatal(err)
		}
	}
}

func TestSingleInputStatementReadsCurrentCloneValues(t *testing.T) {
	r := motionRuntime(t, `{
		"effect":{"opcode":"looks_seteffectto","fields":{"EFFECT":["ghost",null]},"inputs":{"VALUE":[2,"value"]}},
		"value":{"opcode":"operator_add","inputs":{"NUM1":[1,[12,"amount","amount"]],"NUM2":[1,[4,"2"]]}}
	}`)
	original := r.targets[1]
	original.variables["amount"] = &Variable{Value: float64(10)}
	r.createClone(original)
	clone := r.targets[len(r.targets)-1]
	for _, tc := range []struct {
		target *target
		amount float64
		want   float64
	}{{original, 10, 12}, {clone, 20, 22}, {original, 30, 32}, {clone, 40, 42}} {
		tc.target.variables["amount"] = &Variable{Value: tc.amount}
		th := &thread{target: tc.target, pc: "effect"}
		if err := r.step(context.Background(), th); err != nil {
			t.Fatal(err)
		}
		if got := tc.target.state.Effects.Ghost; got != tc.want {
			t.Fatalf("clone %d effect=%v, want %v", tc.target.state.CloneID, got, tc.want)
		}
	}
}
