package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"gowarp/core/project"
)

func addNativeCacheBlocks(t testing.TB, blocks project.Blocks) {
	t.Helper()
	var extra project.Blocks
	err := json.Unmarshal([]byte(`{
        "comparisonLoopBody":{"opcode":"control_repeat","inputs":{"TIMES":[1,[12,"x","x"]],"SUBSTACK":[2,"comparisonIncrement"]}},
        "comparisonIncrement":{"opcode":"data_changevariableby","next":"comparisonCheck","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[1,[4,"0.125"]]}},
        "comparisonCheck":{"opcode":"control_if","inputs":{"CONDITION":[2,"comparisonAnd"],"SUBSTACK":[2,"comparisonCount"]}},
        "comparisonAnd":{"opcode":"operator_and","inputs":{"OPERAND1":[2,"comparisonVariable"],"OPERAND2":[2,"comparisonConstant"]}},
        "comparisonVariable":{"opcode":"operator_lt","inputs":{"OPERAND1":[1,[12,"y","y"]],"OPERAND2":[2,"comparisonOffset"]}},
        "comparisonOffset":{"opcode":"operator_add","inputs":{"NUM1":[1,[12,"y","y"]],"NUM2":[1,[4,"0.5"]]}},
        "comparisonConstant":{"opcode":"operator_lt","inputs":{"OPERAND1":[1,[12,"y","y"]],"OPERAND2":[1,[4,"1000.25"]]}},
        "comparisonCount":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
        "observedChange":{"opcode":"data_changevariableby","next":"observedSize","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[1,[4,"2"]]}},
        "observedSize":{"opcode":"looks_setsizeto","next":"observedRead","inputs":{"SIZE":[1,[12,"y","y"]]}},
        "observedRead":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[12,"y","y"]]}},
        "mixedArgSet":{"opcode":"data_setvariableto","next":"mixedArgNumber","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"arg"]}},
        "mixedArgNumber":{"opcode":"data_setvariableto","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[2,"argumentSum"]}},
        "argumentSum":{"opcode":"operator_add","inputs":{"NUM1":[2,"arg"],"NUM2":[1,[4,"0"]]}},
        "pairSet":{"opcode":"data_setvariableto","next":"mixedArgNumber","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"pairLabel"]}},
        "pairLabel":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["label",null]}},
        "pairCallerBody":{"opcode":"procedures_call","mutation":{"proccode":"mixedPair","argumentids":"[\"nID\",\"nID\"]"},"inputs":{"nID":[1,[12,"x","x"]]}},
        "cacheLoopBody":{"opcode":"control_repeat","inputs":{"TIMES":[1,[12,"x","x"]],"SUBSTACK":[2,"cacheIncrement"]}},
        "cacheIncrement":{"opcode":"data_changevariableby","next":"cacheCall","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[2,"cacheDelta"]}},
        "cacheDelta":{"opcode":"operator_multiply","inputs":{"NUM1":[2,"length"],"NUM2":[1,[4,"0.125"]]}},
        "cacheCall":{"opcode":"procedures_call","mutation":{"proccode":"numericPen","argumentids":"[\"nID\"]"},"inputs":{"nID":[2,"cacheArgument"]}},
        "cacheArgument":{"opcode":"operator_multiply","inputs":{"NUM1":[1,[12,"y","y"]],"NUM2":[1,[4,"1.5"]]}},
        "numericPenIf":{"opcode":"control_if_else","inputs":{"CONDITION":[2,"numericPenPositive"],"SUBSTACK":[2,"numericPenUp"],"SUBSTACK2":[2,"numericPenSize"]}},
        "numericPenPositive":{"opcode":"operator_gt","inputs":{"OPERAND1":[2,"numericPenSquare"],"OPERAND2":[1,[4,"0"]]}},
        "numericPenSquare":{"opcode":"operator_multiply","inputs":{"NUM1":[2,"arg"],"NUM2":[2,"arg"]}},
        "numericPenUp":{"opcode":"pen_penUp"},
        "numericPenSize":{"opcode":"pen_setPenSizeTo","inputs":{"SIZE":[1,[4,"1"]]}}
    }`), &extra)
	if err != nil {
		t.Fatal(err)
	}
	for id, block := range extra {
		blocks[id] = block
	}
	for _, pair := range []struct{ caller, callee, next string }{
		{"inlineOuterBody", "inlineMiddle", "inlineStore"},
		{"inlineMiddleBody", "mixedArg", "inlineIncrement"},
		{"inlineIncrement", "inlineStep", ""},
	} {
		blocks[pair.caller] = &project.Block{Opcode: "procedures_call", Next: pair.next,
			Mutation: map[string]json.RawMessage{"proccode": json.RawMessage(strconv.Quote(pair.callee)), "argumentids": json.RawMessage(`"[\"nID\"]"`)},
			Inputs:   map[string][]any{"nID": {float64(2), "arg"}}}
	}
	blocks["inlineStore"] = &project.Block{Opcode: "data_addtolist",
		Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: map[string][]any{"ITEM": {float64(2), "arg"}}}
	blocks["inlineLoopBody"] = &project.Block{Opcode: "control_repeat", Inputs: map[string][]any{
		"TIMES": {float64(1), []any{float64(12), "x", "x"}}, "SUBSTACK": {float64(2), "inlineCall"}}}
	blocks["inlineCall"] = &project.Block{Opcode: "procedures_call", Next: "inlineRead",
		Mutation: map[string]json.RawMessage{"proccode": json.RawMessage(`"inlineStep"`), "argumentids": json.RawMessage(`"[\"nID\"]"`)},
		Inputs:   literalInputs(map[string]any{"nID": .125})}
	blocks["inlineStepBody"] = &project.Block{Opcode: "data_changevariableby",
		Fields: map[string][]any{"VARIABLE": {"y", "y"}}, Inputs: map[string][]any{"VALUE": {float64(2), "arg"}}}
	blocks["inlineRead"] = &project.Block{Opcode: "data_changevariableby",
		Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(1), []any{float64(12), "y", "y"}}}}

	blocks["sharedLoopBody"] = &project.Block{Opcode: "control_repeat", Inputs: map[string][]any{
		"TIMES": {float64(1), []any{float64(12), "x", "x"}}, "SUBSTACK": {float64(2), "sharedBefore"},
	}}
	blocks["sharedBefore"] = &project.Block{Opcode: "data_changevariableby", Next: "sharedCall",
		Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: literalInputs(map[string]any{"VALUE": .125})}
	blocks["sharedCall"] = &project.Block{Opcode: "procedures_call", Next: "inlineRead",
		Mutation: map[string]json.RawMessage{"proccode": json.RawMessage(`"sharedHelper"`)}}
	blocks["sharedOnlyBody"] = &project.Block{Opcode: "procedures_call",
		Mutation: map[string]json.RawMessage{"proccode": json.RawMessage(`"sharedHelper"`)}}
	for i := range 40 {
		id, next := fmt.Sprintf("sharedHelper%d", i), fmt.Sprintf("sharedHelper%d", i+1)
		if i == 39 {
			next = "sharedStamp"
		}
		blocks[id] = &project.Block{Opcode: "data_changevariableby", Next: next,
			Fields: map[string][]any{"VARIABLE": {"y", "y"}}, Inputs: literalInputs(map[string]any{"VALUE": .125})}
	}
	blocks["sharedStamp"] = &project.Block{Opcode: "pen_stamp"}
}

type nativeRegisterGraphics struct {
	nativeDrawingRecorder
	independent bool
	count       int
}

func (g *nativeRegisterGraphics) ScriptDataIndependent() bool {
	return g.independent
}

func (g *nativeRegisterGraphics) Pen(command PenCommand) {
	g.count++
	if !g.independent {
		value := command.Target.State.Variables["score"]
		value.Value = number(value.Value) + .5
		command.Target.State.Variables["score"] = value
	}
}

func runNativeSharedRegisters(t testing.TB, r *Runtime, graphics *nativeRegisterGraphics) {
	t.Helper()
	target := r.targets[1]
	target.variables["x"].Value = float64(100)
	target.variables["y"].Value = .25
	target.variables["score"].Value = float64(0)
	graphics.count = 0
	r.startHat(target, "sharedLoopHat", false)
	if err := r.Run(context.Background(), 1, 60, false); err != nil {
		t.Fatal(err)
	}
	wantScore := 25287.5
	if !graphics.independent {
		wantScore = 25337.5
	}
	if !r.Done() || graphics.count != 100 || target.variables["y"].Value != 500.25 || target.variables["score"].Value != wantScore {
		t.Fatalf("shared calls: done=%v stamps=%d y=%v score=%v", r.Done(), graphics.count, target.variables["y"].Value, target.variables["score"].Value)
	}
}

func testNativeSharedRegisters(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		r := New(nativeCodegenFixture(t))
		r.native = compiled
		r.SetGraphics(&nativeRegisterGraphics{independent: true})
		r.targets[1].variables["y"].Value = .25
		r.startHat(r.targets[1], "sharedOnlyHat", false)
		if err := r.Run(context.Background(), 1, 60, false); err != nil {
			t.Fatal(err)
		}
		if value := r.targets[1].variables["y"].Value; value != 5.25 {
			t.Fatalf("native=%v: callee-only write was lost: %v", compiled, value)
		}
	}
	for _, independent := range []bool{false, true} {
		var want []TargetState
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			graphics := &nativeRegisterGraphics{independent: independent}
			r.SetGraphics(graphics)
			runNativeSharedRegisters(t, r, graphics)
			if !compiled {
				want = r.Snapshot()
			} else if !reflect.DeepEqual(want, r.Snapshot()) {
				t.Fatalf("independent=%v: compiled calls changed state", independent)
			}
		}
	}
	r := New(nativeCodegenFixture(t))
	graphics := &nativeRegisterGraphics{independent: true}
	r.SetGraphics(graphics)
	allocations := testing.AllocsPerRun(5, func() { runNativeSharedRegisters(t, r, graphics) })
	if allocations > 20 {
		t.Fatalf("compiled calls boxed intermediate values: %g allocations", allocations)
	}
}

func BenchmarkNativeSharedRegisters(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, independent := range []bool{false, true} {
		b.Run(fmt.Sprintf("independent=%v", independent), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			graphics := &nativeRegisterGraphics{independent: independent}
			r.SetGraphics(graphics)
			b.ReportAllocs()
			for b.Loop() {
				runNativeSharedRegisters(b, r, graphics)
			}
		})
	}
}

func runNativeCacheLoop(t testing.TB, r *Runtime) {
	t.Helper()
	target := r.targets[1]
	*target.variables["x"] = Variable{Value: float64(1000)}
	*target.variables["y"] = Variable{Value: float64(4)}
	target.state.Pen.Down = true
	th := r.startHat(target, "cacheLoopHat", false)
	if err := r.Run(context.Background(), 1, 60, false); err != nil {
		t.Fatal(err)
	}
	if !th.done || target.variables["y"].Value != float64(129) || target.state.Pen.Down {
		t.Fatal("calculation calls changed loop result")
	}
}

func testNativeCallCaches(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		r := New(nativeCodegenFixture(t))
		r.native = compiled
		allocations := testing.AllocsPerRun(5, func() { runNativeCacheLoop(t, r) })
		t.Logf("native=%v allocations=%g", compiled, allocations)
		if compiled && allocations > 20 {
			t.Fatalf("pure numeric calls discarded loop caches or boxed arguments: %g", allocations)
		}
	}
}

func BenchmarkNativeCallCaches(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, compiled := range []bool{false, true} {
		b.Run(fmt.Sprintf("compiled=%v", compiled), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			r.native = compiled
			b.ReportAllocs()
			for b.Loop() {
				runNativeCacheLoop(b, r)
			}
		})
	}
}

func testNativeNumericArguments(t *testing.T) {
	for _, raw := range []any{"007", "", "last", "NaN", "Infinity", float64(0), math.NaN(), math.Copysign(0, -1), false} {
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			target := r.targets[1]
			*target.variables["x"] = Variable{Value: raw}
			target.state.Pen.Down, target.state.Pen.Size = true, 7
			r.startHat(target, "numericPenHat", false)
			if err := r.Run(context.Background(), 1, 60, false); err != nil {
				t.Fatal(err)
			}
			nonzero := raw == "007" || raw == "Infinity"
			if target.state.Pen.Down == nonzero || (nonzero && target.state.Pen.Size != 7) || (!nonzero && target.state.Pen.Size != 1) {
				t.Fatalf("native=%v input=%v changed numeric argument semantics: %+v", compiled, raw, target.state.Pen)
			}
		}
	}
	for _, procedure := range []string{"mixedArg", "pairCaller"} {
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			target := r.targets[1]
			*target.variables["x"] = Variable{Value: "007"}
			r.startHat(target, procedure+"Hat", false)
			if err := r.Run(context.Background(), 1, 60, false); err != nil {
				t.Fatal(err)
			}
			if target.variables["score"].Value != "007" || target.variables["y"].Value != float64(7) {
				t.Fatalf("%s native=%v: numeric conversion changed raw argument", procedure, compiled)
			}
		}
	}
}

type nativeCacheGraphics struct {
	nativeDrawingRecorder
	observed any
}

func (g *nativeCacheGraphics) CostumeSize(target DisplayTarget) (float64, float64) {
	g.observed = target.State.Variables["y"].Value
	target.State.Variables["y"] = Variable{Value: float64(19)}
	return 20, 30
}

func testNativeObservedCache(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		r := New(nativeCodegenFixture(t))
		r.native = compiled
		r.options.Fencing = true
		graphics := &nativeCacheGraphics{}
		r.SetGraphics(graphics)
		target := r.targets[1]
		r.startHat(target, "observedHat", false)
		if err := r.Run(context.Background(), 1, 60, false); err != nil {
			t.Fatal(err)
		}
		if graphics.observed != float64(6) || target.variables["score"].Value != float64(19) {
			t.Fatalf("native=%v: graphics saw %v, subsequent block saw %v", compiled, graphics.observed, target.variables["score"].Value)
		}
	}
}

func runNativeComparisonLoop(t testing.TB, r *Runtime) {
	t.Helper()
	target := r.targets[1]
	*target.variables["x"] = Variable{Value: float64(1000)}
	*target.variables["y"] = Variable{Value: 4.25}
	*target.variables["score"] = Variable{Value: float64(0)}
	th := r.startHat(target, "comparisonLoopHat", false)
	if err := r.Run(context.Background(), 1, 60, false); err != nil {
		t.Fatal(err)
	}
	if !th.done || target.variables["y"].Value != 129.25 || target.variables["score"].Value != float64(1000) {
		t.Fatal("comparisons changed loop result")
	}
}

func testNativeComparisonAllocations(t *testing.T) {
	r := New(nativeCodegenFixture(t))
	r.native = true
	allocations := testing.AllocsPerRun(5, func() { runNativeComparisonLoop(t, r) })
	t.Logf("numeric comparison loop: %g allocations", allocations)
	if allocations > 20 {
		t.Fatalf("numeric comparisons allocated temporary values: %g", allocations)
	}
}

func BenchmarkNativeComparisons(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, compiled := range []bool{false, true} {
		b.Run(fmt.Sprintf("compiled=%v", compiled), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			r.native = compiled
			b.ReportAllocs()
			for b.Loop() {
				runNativeComparisonLoop(b, r)
			}
		})
	}
}

func BenchmarkNativeTextComparisonLoop(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, value := range []string{"사각형", "원"} {
		b.Run(value, func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			target := r.targets[1]
			target.variables["x"].Value = float64(1000)
			target.state.Lists["items"] = List{Items: []any{value}}
			want := float64(0)
			if value == "사각형" {
				want = 1000
			}
			b.ReportAllocs()
			for b.Loop() {
				target.variables["score"].Value = float64(0)
				r.startHat(target, "textComparisonLoopHat", false)
				if err := r.Run(context.Background(), 1, 60, false); err != nil {
					b.Fatal(err)
				}
				if !r.Done() || target.variables["score"].Value != want {
					b.Fatal("text comparisons changed the loop result")
				}
			}
		})
	}
}

var nativeComparisonKinds = []string{"variables", "expression", "stored", "numeric text", "blank", "text", "fraction", "reversed", "unicode text", "reversed text", "infinite text"}

func addNativeComparisonBlocks(blocks project.Blocks) {
	variable := func(id string) []any { return []any{float64(1), []any{float64(12), id, id}} }
	literal := func(value string) []any { return []any{float64(1), []any{float64(10), value}} }
	blocks["comparisonDivide"] = &project.Block{Opcode: "operator_divide", Inputs: map[string][]any{"NUM1": variable("x"), "NUM2": variable("y")}}
	blocks["comparisonPrepare"] = &project.Block{Opcode: "data_setvariableto", Next: "comparisonResult0", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), "comparisonDivide"}}}
	blocks["textComparisonLoopBody"] = &project.Block{Opcode: "control_repeat", Inputs: map[string][]any{"TIMES": variable("x"), "SUBSTACK": {float64(2), "textComparisonAdd"}}}
	blocks["textComparisonAdd"] = &project.Block{Opcode: "data_changevariableby", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), "textComparisonEqual"}}}
	blocks["textComparisonEqual"] = &project.Block{Opcode: "operator_equals", Inputs: map[string][]any{"OPERAND1": {float64(2), "textComparisonItem"}, "OPERAND2": literal("사각형")}}
	blocks["textComparisonItem"] = &project.Block{Opcode: "data_itemoflist", Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: literalInputs(map[string]any{"INDEX": float64(1)})}
	for i, kind := range nativeComparisonKinds {
		left, right := variable("x"), variable("y")
		switch kind {
		case "expression":
			left = []any{float64(2), "comparisonDivide"}
		case "stored":
			left = variable("score")
		case "numeric text":
			right = literal("007")
		case "blank":
			right = literal(" ")
		case "text":
			right = literal("Apple")
		case "fraction":
			right = literal("0.0001")
		case "reversed":
			left, right = literal("007"), variable("x")
		case "unicode text":
			right = literal("사각형")
		case "reversed text":
			left, right = literal("Apple"), variable("x")
		case "infinite text":
			right = literal("Infinity")
		}
		for j, opcode := range []string{"operator_equals", "operator_lt", "operator_gt"} {
			n := i*3 + j
			id, value := fmt.Sprintf("comparisonResult%d", n), fmt.Sprintf("comparisonValue%d", n)
			next := ""
			if n+1 < len(nativeComparisonKinds)*3 {
				next = fmt.Sprintf("comparisonResult%d", n+1)
			}
			blocks[id] = &project.Block{Opcode: "data_addtolist", Next: next, Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: map[string][]any{"ITEM": {float64(2), value}}}
			blocks[value] = &project.Block{Opcode: opcode, Inputs: map[string][]any{"OPERAND1": left, "OPERAND2": right}}
		}
	}
}

func testNativeComparisons(t *testing.T) {
	values := []any{nil, false, true, float64(0), math.Copysign(0, -1), 0.125, float64(7), math.NaN(), math.Inf(1), math.Inf(-1), "", " ", "\ufeff", "0", "007", "0.0001", "0x10", "NaN", "Infinity", "Apple", "apple", "1_2", "-0", "01x", "사각형", "원", "\u00a0", "\u0085"}
	for _, compiled := range []bool{false, true} {
		r := New(nativeCodegenFixture(t))
		r.native = compiled
		target := r.targets[1]
		for _, x := range values {
			for _, y := range values {
				*target.variables["x"] = Variable{Value: x}
				*target.variables["y"] = Variable{Value: y}
				target.state.Lists["items"] = List{}
				r.startHat(target, "comparisonCasesHat", false)
				if err := r.Run(context.Background(), 1, 60, false); err != nil {
					t.Fatal(err)
				}
				got := target.state.Lists["items"].Items
				if len(got) != len(nativeComparisonKinds)*3 {
					t.Fatalf("native=%v incomplete comparisons: %d", compiled, len(got))
				}
				for i, kind := range nativeComparisonKinds {
					a, b := x, y
					switch kind {
					case "expression", "stored":
						a = number(x) / number(y)
					case "numeric text":
						b = "007"
					case "blank":
						b = " "
					case "text":
						b = "Apple"
					case "fraction":
						b = "0.0001"
					case "reversed":
						a, b = "007", x
					case "unicode text":
						b = "사각형"
					case "reversed text":
						a, b = "Apple", x
					case "infinite text":
						b = "Infinity"
					}
					c := compare(a, b)
					for j, want := range []bool{c == 0, c < 0, c > 0} {
						if got[i*3+j] != want {
							t.Fatalf("native=%v %s (%#v,%#v) comparison %d: got %v, want %v", compiled, kind, x, y, j, got[i*3+j], want)
						}
					}
				}
			}
		}
	}
}

func testNativeEntryArguments(t *testing.T) {
	r := New(nativeCodegenFixture(t))
	target := r.targets[1]
	*target.variables["x"] = Variable{Value: "007"}
	r.startHat(target, "mixedArgHat", false)
	if err := r.Run(context.Background(), 1, 60, false); err != nil {
		t.Fatal(err)
	}
	if target.variables["score"].Value != "007" || target.variables["y"].Value != float64(7) {
		t.Fatal("native entry changed its arguments")
	}
	for _, params := range r.freeParams {
		if params != nil {
			t.Fatal("native entry allocated an interpreter argument table")
		}
	}
}

func testNativeEntryNumericValues(t *testing.T) {
	r := New(nativeCodegenFixture(t))
	target := r.targets[1]
	block := *target.source.Blocks["mixedPairEntry"]
	block.Mutation = map[string]json.RawMessage{
		"proccode":    block.Mutation["proccode"],
		"argumentids": json.RawMessage(`"[\"nID\",\"labelID\"]"`),
	}
	block.Inputs = map[string][]any{
		"nID":     {float64(2), "comparisonDivide"},
		"labelID": {float64(2), "comparisonDivide"},
	}
	steps := 0
	th := &thread{target: target, sliceSteps: &steps, sliceLimit: 1 << 30}
	for _, values := range [][2]float64{{0, 0}, {1, 0}, {math.Copysign(0, -1), 1}, {1.25, 2}} {
		target.variables["x"].Value = values[0]
		target.variables["y"].Value = values[1]
		steps = 0
		if _, err := r.call(context.Background(), th, &block, nil, false); err != nil {
			t.Fatal(err)
		}
		want := values[0] / values[1]
		got, ok := target.variables["score"].Value.(float64)
		if !ok || (math.IsNaN(want) && !math.IsNaN(got)) ||
			(!math.IsNaN(want) && math.Float64bits(got) != math.Float64bits(want)) {
			t.Fatalf("raw argument changed: got %v, want %v", got, want)
		}
		if got := target.variables["y"].Value; got != nativeNumber(want)+0 {
			t.Fatalf("numeric argument changed: got %v, want %v", got, nativeNumber(want)+0)
		}
	}
}

func testNativeEntryDuplicates(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		r := New(nativeCodegenFixture(t))
		r.native = compiled
		target := r.targets[1]
		for _, raw := range []string{"007", " 9 ", "-0"} {
			*target.variables["x"] = Variable{Value: "first"}
			*target.variables["y"] = Variable{Value: raw}
			r.startHat(target, "duplicateNamesHat", false)
			if err := r.Run(context.Background(), 1, 60, false); err != nil {
				t.Fatal(err)
			}
			if target.variables["score"].Value != raw || target.variables["y"].Value != number(raw) {
				t.Fatalf("native=%v duplicate name lost final argument %q", compiled, raw)
			}
		}
		c := &extensionContext{r: r, th: &thread{target: target}}
		if _, err := c.Call("variable.delete", "x"); err != nil {
			t.Fatal(err)
		}
		*target.variables["score"] = Variable{Value: "unchanged"}
		r.startHat(target, "duplicateNamesHat", false)
		if err := r.Run(context.Background(), 1, 60, false); err == nil {
			t.Fatalf("native=%v skipped overwritten argument error", compiled)
		}
		if target.variables["score"].Value != "unchanged" {
			t.Fatal("callee ran after argument error")
		}
		for _, v := range r.nativeArguments[:cap(r.nativeArguments)] {
			if v != (calculationValue{}) {
				t.Fatal("failed call retained an argument")
			}
		}
	}
}

func BenchmarkNativeEntry(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	r := New(nativeCodegenFixture(b))
	target := r.targets[1]
	th := &thread{target: target, sliceLimit: 1 << 30}
	block := *target.source.Blocks["mixedPairEntry"]
	ids, _ := json.Marshal(`["nID","labelID"]`)
	block.Mutation = map[string]json.RawMessage{"proccode": block.Mutation["proccode"], "argumentids": ids}
	block.Inputs = literalInputs(map[string]any{"nID": "3.125", "labelID": "007"})
	steps := 0
	th.sliceSteps = &steps
	input := func(name string) (any, error) { b.Fatalf("unexpected fallback input %s", name); return nil, nil }
	b.ReportAllocs()
	for b.Loop() {
		steps = 0
		if _, err := r.call(context.Background(), th, &block, input, false); err != nil {
			b.Fatal(err)
		}
	}
	if target.variables["score"].Value != "007" || target.variables["y"].Value != 3.125 {
		b.Fatal("entry argument result changed")
	}
}

func addNativeDataflowBlocks(t testing.TB, blocks project.Blocks) {
	t.Helper()
	var extra project.Blocks
	err := json.Unmarshal([]byte(`{
        "typedSum":{"opcode":"operator_add","inputs":{"NUM1":[1,[4,"1"]],"NUM2":[1,[4,"2"]]}},
        "typedStart":{"opcode":"data_setvariableto","next":"typedY","fields":{"VARIABLE":["x","x"]},"inputs":{"VALUE":[2,"typedSum"]}},
        "typedY":{"opcode":"data_setvariableto","next":"typedScoreY","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[2,"typedPlus"]}},
        "typedPlus":{"opcode":"operator_add","inputs":{"NUM1":[1,[12,"x","x"]],"NUM2":[1,[4,"4"]]}},
        "typedScoreY":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[12,"y","y"]]}},
        "typedScoreX":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[12,"x","x"]]}},
        "typedConditionStart":{"opcode":"data_setvariableto","next":"typedCondition","fields":{"VARIABLE":["x","x"]},"inputs":{"VALUE":[2,"typedSum"]}},
        "typedCondition":{"opcode":"control_if","next":"typedScoreX","inputs":{"CONDITION":[2,"arg"],"SUBSTACK":[2,"typedText"]}},
        "typedBothStart":{"opcode":"data_setvariableto","next":"typedBothCondition","fields":{"VARIABLE":["x","x"]},"inputs":{"VALUE":[2,"typedSum"]}},
        "typedBothCondition":{"opcode":"control_if_else","next":"typedScoreX","inputs":{"CONDITION":[2,"arg"],"SUBSTACK":[2,"typedXNumber"],"SUBSTACK2":[2,"typedXPlus"]}},
        "typedXNumber":{"opcode":"data_setvariableto","fields":{"VARIABLE":["x","x"]},"inputs":{"VALUE":[2,"typedSum"]}},
        "typedXPlus":{"opcode":"data_setvariableto","fields":{"VARIABLE":["x","x"]},"inputs":{"VALUE":[2,"typedPlus"]}},
        "typedText":{"opcode":"data_setvariableto","fields":{"VARIABLE":["x","x"]},"inputs":{"VALUE":[1,[10,"007"]]}},
        "typedLoopStart":{"opcode":"data_setvariableto","next":"typedLoop","fields":{"VARIABLE":["x","x"]},"inputs":{"VALUE":[2,"typedSum"]}},
        "typedLoop":{"opcode":"control_repeat","next":"typedScoreY","inputs":{"TIMES":[1,[4,"2"]],"SUBSTACK":[2,"typedCopy"]}},
        "typedCopy":{"opcode":"data_setvariableto","next":"typedText","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[1,[12,"x","x"]]}},
        "typedEmpty":{"opcode":"control_repeat","next":"typedScoreX","inputs":{"TIMES":[1,[4,"0"]],"SUBSTACK":[2,"typedStart"]}}
    }`), &extra)
	if err != nil {
		t.Fatal(err)
	}
	for id, block := range extra {
		blocks[id] = block
	}
	blocks["copyLoopBody"] = &project.Block{Opcode: "control_repeat", Inputs: map[string][]any{
		"TIMES": {float64(1), []any{float64(12), "x", "x"}}, "SUBSTACK": {float64(2), "copyBranch"},
	}}
	blocks["copyBranch"] = &project.Block{Opcode: "control_if_else", Next: "copyValue", Inputs: map[string][]any{
		"CONDITION": {float64(2), "arg"}, "SUBSTACK": {float64(2), "copyIncrement"}, "SUBSTACK2": {float64(2), "copyText"},
	}}
	blocks["copyIncrement"] = &project.Block{Opcode: "data_changevariableby",
		Fields: map[string][]any{"VARIABLE": {"y", "y"}}, Inputs: literalInputs(map[string]any{"VALUE": .125})}
	blocks["copyText"] = &project.Block{Opcode: "data_setvariableto",
		Fields: map[string][]any{"VARIABLE": {"y", "y"}}, Inputs: literalInputs(map[string]any{"VALUE": "007"})}
	blocks["copyValue"] = &project.Block{Opcode: "data_setvariableto",
		Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(1), []any{float64(12), "y", "y"}}}}
}

func TestNativeNumericFlowCodegen(t *testing.T) {
	p := nativeCodegenFixture(t)
	target := &target{source: p.Targets[1], sourceIndex: 1}
	if err := prepareProcedures(target); err != nil {
		t.Fatal(err)
	}
	g := nativeGenerator{known: make(map[*procedure]*nativeFunction), active: make(map[*procedure]bool)}
	fn := g.compile(target, "typedChain")
	if fn == nil {
		t.Fatal("numeric chain did not compile")
	}
	if strings.Contains(fn.body, `f.number(&v0,"x")`) || !strings.Contains(fn.body, "v0.number") {
		t.Fatal("proven numeric intermediate still requires dynamic lookup")
	}
}

func testNativeDataflow(t *testing.T) {
	for _, code := range []string{"typedChain", "typedCondition", "typedBoth", "typedLoop", "typedEmpty"} {
		for _, arg := range []any{false, true} {
			native, interpreted := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
			interpreted.native = false
			var states [2]TargetState
			for i, r := range []*Runtime{native, interpreted} {
				target := r.targets[1]
				*target.variables["x"] = Variable{Value: "seed"}
				target.source.Blocks[code+"Entry"].Inputs = literalInputs(map[string]any{"nID": arg})
				r.startHat(target, code+"Hat", false)
				if err := r.Run(context.Background(), 1, 60, false); err != nil {
					t.Fatal(err)
				}
				states[i] = target.state
			}
			for _, id := range []string{"x", "y", "score"} {
				if states[0].Variables[id].Value != states[1].Variables[id].Value {
					t.Fatalf("%s arg %v variable %s: got %v want %v", code, arg, id, states[0].Variables[id], states[1].Variables[id])
				}
			}
		}
	}
}

func runNativeSharedVariableLoop(t testing.TB, r *Runtime, count int) {
	t.Helper()
	target := r.targets[1]
	target.variables["x"].Value = float64(count)
	target.variables["y"].Value = float64(0)
	target.variables["score"].Value = float64(0)
	r.startHat(target, "inlineLoopHat", false)
	if err := r.Run(context.Background(), 1, 60, false); err != nil {
		t.Fatal(err)
	}
	want := float64(count*(count+1)) / 16
	if !r.Done() || target.variables["score"].Value != want || target.variables["y"].Value != float64(count)/8 {
		t.Fatal("caller did not see the callee's variable writes")
	}
}

func testNativeSharedVariableLoop(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		r := New(nativeCodegenFixture(t))
		r.native = compiled
		allocations := testing.AllocsPerRun(5, func() { runNativeSharedVariableLoop(t, r, 1000) })
		if compiled && allocations > 20 {
			t.Fatalf("small calculation calls materialized intermediate values: %g allocations", allocations)
		}
	}
}

func BenchmarkNativeSharedVariableLoop(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, count := range []int{32, 256, 1024} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			b.ReportAllocs()
			for b.Loop() {
				runNativeSharedVariableLoop(b, r, count)
			}
		})
	}
}

func testNativeNestedInlineArguments(t *testing.T) {
	for _, value := range []any{"007", "", "NaN", "Infinity", float64(.5), false} {
		fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
		slow.native = false
		for _, r := range []*Runtime{fast, slow} {
			target := r.targets[1]
			target.variables["x"].Value = value
			r.startHat(target, "inlineOuterHat", false)
			if err := r.Run(context.Background(), 1, 60, false); err != nil {
				t.Fatal(err)
			}
		}
		if !fast.Done() || !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) {
			t.Fatalf("nested calls changed raw/numeric arguments for %v", value)
		}
	}
}

func testNativeEntryNumericAllocations(t *testing.T) {
	r := New(nativeCodegenFixture(t))
	target := r.targets[1]
	target.variables["y"].Value = 1.25
	steps := 0
	th := &thread{target: target, sliceSteps: &steps, sliceLimit: 1 << 30}
	block := target.source.Blocks["cacheCall"]
	allocations := testing.AllocsPerRun(100, func() {
		steps = 0
		if _, err := r.call(context.Background(), th, block, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("numeric arguments allocated %g objects per call", allocations)
	}
}

func BenchmarkNativeNumericEntry(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	r := New(nativeCodegenFixture(b))
	target := r.targets[1]
	target.variables["y"].Value = 1.25
	steps := 0
	th := &thread{target: target, sliceSteps: &steps, sliceLimit: 1 << 30}
	block := target.source.Blocks["cacheCall"]
	b.ReportAllocs()
	for b.Loop() {
		steps = 0
		if _, err := r.call(context.Background(), th, block, nil, false); err != nil {
			b.Fatal(err)
		}
	}
}

func runNativeVariableCopies(t testing.TB, r *Runtime, count int, numeric bool) {
	t.Helper()
	target := r.targets[1]
	target.variables["x"].Value = float64(count)
	target.variables["y"].Value = float64(0)
	target.variables["score"].Value = float64(0)
	target.source.Blocks["copyLoopEntry"].Inputs = literalInputs(map[string]any{"nID": numeric})
	r.startHat(target, "copyLoopHat", false)
	if err := r.Run(context.Background(), 1, 60, false); err != nil {
		t.Fatal(err)
	}
	var want any = "007"
	if numeric {
		want = float64(count) / 8
	}
	if !r.Done() || target.variables["score"].Value != want || target.variables["y"].Value != want {
		t.Fatalf("variable copy changed values: score=%v y=%v want=%v", target.variables["score"].Value, target.variables["y"].Value, want)
	}
}

func testNativeVariableCopies(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		for _, numeric := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			allocations := testing.AllocsPerRun(5, func() { runNativeVariableCopies(t, r, 1000, numeric) })
			t.Logf("compiled=%v numeric=%v allocations=%g", compiled, numeric, allocations)
			if compiled && numeric && allocations > 30 {
				t.Fatalf("intermediate variable copies allocated %g objects", allocations)
			}
		}
	}
}

func BenchmarkNativeVariableCopies(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	r := New(nativeCodegenFixture(b))
	b.ReportAllocs()
	for b.Loop() {
		runNativeVariableCopies(b, r, 1000, true)
	}
}
