package vm

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"gowarp/core/project"
)

func TestNativeNumericListIndexBoundaries(t *testing.T) {
	list := List{Items: []any{"first", "second"}}
	for _, tc := range []struct {
		index float64
		want  any
	}{
		{0, ""}, {-1, ""}, {1, "first"}, {1.9, "first"},
		{2, "second"}, {2.9, "second"}, {3, ""},
		{math.NaN(), ""}, {math.Inf(1), ""}, {math.Inf(-1), ""},
	} {
		if got := nativeNumericItem(&list, tc.index); got != tc.want {
			t.Errorf("index %g = %v, want %v", tc.index, got, tc.want)
		}
	}
}

func TestNativeDynamicRepeatCompilesSmallBody(t *testing.T) {
	p := nativeCodegenFixture(t)
	target := &target{source: p.Targets[1], sourceIndex: 1}
	if err := prepareProcedures(target); err != nil {
		t.Fatal(err)
	}
	g := &nativeGenerator{}
	g.compileRegions(target, "dynamic")
	for _, function := range g.functions {
		if function.region == "dynamicAdd" {
			return
		}
	}
	t.Fatal("dynamic repeat's complete calculation body was not compiled")
}

func TestNativeRepeatCompilesPureCalls(t *testing.T) {
	for _, callee := range []string{"pureStep", "wait", "unsupported", "recursive"} {
		t.Run(callee, func(t *testing.T) {
			p := nativeCodegenFixture(t)
			target := &target{source: p.Targets[1], sourceIndex: 1}
			if err := prepareProcedures(target); err != nil {
				t.Fatal(err)
			}
			target.procedures["pureStep"] = target.procedures[callee]
			g := &nativeGenerator{known: make(map[*procedure]*nativeFunction), active: make(map[*procedure]bool)}
			g.compileRegions(target, "callBatch")
			compiled := false
			for _, function := range g.functions {
				if function.region == "callBatchStep" {
					compiled = true
				}
			}
			if compiled != (callee == "pureStep") {
				t.Fatalf("repeat body calling %s: compiled=%v", callee, compiled)
			}
		})
	}
}

func TestNativeBranchRegionRespectsNestingLimit(t *testing.T) {
	r := fixture(t, `{
		"if":{"opcode":"control_if","inputs":{"CONDITION":[1,[10,"true"]],"SUBSTACK":[2,"set"]}},
		"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
	}`)
	r.native = true
	called := false
	th := &thread{target: r.stage, pc: "if", warp: true, stack: make([]continuation, 1024)}
	th.regions = map[string]*nativeRegion{"if": {depth: 2, run: func(context.Context, *Runtime, *thread) error {
		called = true
		th.done = true
		return nil
	}}}
	err := r.step(context.Background(), th)
	if called || err == nil || !strings.Contains(err.Error(), "control nesting limit") {
		t.Fatalf("compiled branch bypassed nesting guard: called=%v error=%v", called, err)
	}
}

func TestNativeBranchRegionLeavesWaitingBranchesInterpreted(t *testing.T) {
	p := nativeCodegenFixture(t)
	target := &target{source: p.Targets[1], sourceIndex: 1}
	target.source.Blocks["regionYes"] = &project.Block{Opcode: "control_wait", Inputs: literalInputs(map[string]any{"DURATION": "0"})}
	if err := prepareProcedures(target); err != nil {
		t.Fatal(err)
	}
	g := &nativeGenerator{known: make(map[*procedure]*nativeFunction), active: make(map[*procedure]bool)}
	g.compileRegions(target, "regionBranch")
	for _, function := range g.functions {
		if function.region == "regionChoice" {
			t.Fatal("branch containing a wait was compiled")
		}
	}
}

func TestNativeRegionCompilesStandaloneCalculationBranch(t *testing.T) {
	p := nativeCodegenFixture(t)
	target := &target{source: p.Targets[1], sourceIndex: 1}
	target.source.Blocks["regionChoice"].Next = ""
	if err := prepareProcedures(target); err != nil {
		t.Fatal(err)
	}
	g := &nativeGenerator{known: make(map[*procedure]*nativeFunction), active: make(map[*procedure]bool)}
	g.compileRegions(target, "regionBranch")
	for _, function := range g.functions {
		if function.region == "regionChoice" {
			return
		}
	}
	t.Fatal("standalone calculation branch was not compiled")
}

func TestNativeGenerationBoundsSharedReporterExpansion(t *testing.T) {
	blocks := map[string]*project.Block{
		"leaf": {Opcode: "operator_add", Inputs: literalInputs(map[string]any{"NUM1": 1.0, "NUM2": 2.0})},
	}
	previous := "leaf"
	for i := 0; i < 14; i++ {
		id := fmt.Sprint(i)
		blocks[id] = &project.Block{Opcode: "operator_add", Inputs: map[string][]any{
			"NUM1": {float64(2), previous}, "NUM2": {float64(2), previous},
		}}
		previous = id
	}
	e := &nativeEmitter{t: &target{source: &project.Target{Blocks: blocks}}, path: make(map[string]bool)}
	_, supported := e.input(&project.Block{Inputs: map[string][]any{"VALUE": {float64(2), previous}}}, "VALUE", 0)
	if supported || e.work > nativeWorkLimit {
		t.Fatalf("shared reporters expanded without a bound: supported=%v work=%d", supported, e.work)
	}
}

func TestNativeGenerationBoundsReporterDepth(t *testing.T) {
	for _, count := range []int{128, 129} {
		blocks := map[string]*project.Block{
			"leaf": {Opcode: "data_variable", Fields: map[string][]any{"VARIABLE": {"score", "score"}}},
		}
		previous := "leaf"
		for i := 1; i < count; i++ {
			id := fmt.Sprint(i)
			blocks[id] = &project.Block{Opcode: "operator_add", Inputs: map[string][]any{
				"NUM1": {float64(2), previous}, "NUM2": {float64(1), []any{float64(4), "0"}},
			}}
			previous = id
		}
		e := &nativeEmitter{t: &target{source: &project.Target{Blocks: blocks}}, path: make(map[string]bool)}
		_, supported := e.input(&project.Block{Inputs: map[string][]any{"VALUE": {float64(2), previous}}}, "VALUE", 0)
		if supported != (count == 128) {
			t.Errorf("%d nested reporters: supported=%v", count, supported)
		}
	}
}

func TestNativeGenerationBoundsNestedRepeats(t *testing.T) {
	blocks := map[string]*project.Block{
		"leaf": {Opcode: "data_changevariableby", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: literalInputs(map[string]any{"VALUE": 1.0})},
	}
	previous := "leaf"
	for i := 0; i < 11; i++ {
		id := fmt.Sprint(i)
		blocks[id] = &project.Block{Opcode: "control_repeat", Inputs: map[string][]any{
			"TIMES": {float64(1), []any{float64(4), "50"}}, "SUBSTACK": {float64(2), previous},
		}}
		previous = id
	}
	e := &nativeEmitter{t: &target{source: &project.Target{Blocks: blocks}}, path: make(map[string]bool)}
	_, supported := e.statements(previous, 1)
	if supported || e.work < 0 {
		t.Fatalf("nested repeats exceeded work bound: supported=%v work=%d", supported, e.work)
	}
}

func TestNativeRegionsRejectCyclesAndTrivialPairs(t *testing.T) {
	for _, cyclic := range []bool{false, true} {
		p := nativeCodegenFixture(t)
		target := &target{source: p.Targets[1], sourceIndex: 1}
		if err := prepareProcedures(target); err != nil {
			t.Fatal(err)
		}
		code := "regionCount"
		if cyclic {
			target.source.Blocks["regionStore"].Next = "regionIncrement"
		} else {
			target.source.Blocks["regionIncrement"].Inputs = literalInputs(map[string]any{"VALUE": 1.0})
			target.source.Blocks["regionCountBody"].Opcode = "control_if"
		}
		g := &nativeGenerator{}
		g.compileRegions(target, code)
		if len(g.functions) != 0 {
			t.Fatalf("cyclic=%v: unsafe or trivial region was compiled", cyclic)
		}
	}
}

func TestNativeBindingsStayLazyAndFlushWrites(t *testing.T) {
	r := fixture(t, "{}")
	th := &thread{target: r.stage}
	steps := 0
	f := nativeFrame{ctx: context.Background(), r: r, t: th.target, steps: &steps}
	var score, missing nativeVariable
	f.set(&score, "score", 12.5)
	if got := f.variable(&score, "score"); got != 12.5 {
		t.Fatalf("staged write = %v", got)
	}
	if got := f.variable(&missing, "missing"); f.err == nil || got != nil {
		t.Fatalf("missing variable = %v, error = %v", got, f.err)
	}
	f.flushVariable(&score)
	if got := r.stage.variables["score"].Value; got != 12.5 {
		t.Fatalf("write before error was lost: %v", got)
	}
}

func TestNativeNumericBindingsPreserveValueTypes(t *testing.T) {
	r := fixture(t, "{}")
	steps := 0
	f := nativeFrame{ctx: context.Background(), r: r, t: r.stage, steps: &steps}
	var slot nativeVariable
	for _, value := range []float64{12.5, math.NaN(), math.Inf(1), math.Copysign(0, -1)} {
		f.setNumber(&slot, "score", value)
		got, ok := f.variable(&slot, "score").(float64)
		if !ok || math.Float64bits(got) != math.Float64bits(value) {
			t.Fatalf("numeric read changed %v to %v", value, got)
		}
		wantNumber := value
		if math.IsNaN(value) {
			wantNumber = 0
		}
		if got := f.number(&slot, "score"); math.Float64bits(got) != math.Float64bits(wantNumber) {
			t.Fatalf("numeric coercion changed %v to %v", value, got)
		}
		f.flushVariable(&slot)
		if got := r.stage.variables["score"].Value.(float64); math.Float64bits(got) != math.Float64bits(value) {
			t.Fatal("flush changed the numeric value")
		}
	}
	for _, value := range []any{"007", true, "last"} {
		f.set(&slot, "score", value)
		if f.variable(&slot, "score") != value || f.number(&slot, "score") != number(value) {
			t.Fatalf("numeric slot retained stale state after setting %v", value)
		}
		f.flushVariable(&slot)
		if r.stage.variables["score"].Value != value {
			t.Fatal("numeric coercion changed a stored string or boolean")
		}
	}
}

func TestNativeUnchangedNumberDoesNotAllocate(t *testing.T) {
	r := fixture(t, "{}")
	f := nativeFrame{r: r, t: r.stage}
	var slot nativeVariable
	for _, value := range []float64{12.5, 4096, math.Copysign(0, -1), math.NaN(), math.Inf(1)} {
		f.setNumber(&slot, "score", value)
		f.flushVariable(&slot)
		allocs := testing.AllocsPerRun(100, func() {
			f.setNumber(&slot, "score", value)
			f.flushVariable(&slot)
		})
		if allocs != 0 {
			t.Errorf("storing unchanged %v allocated %g times", value, allocs)
		}
	}
}

func TestNativeNumberWritePreservesSnapshots(t *testing.T) {
	r := fixture(t, "{}")
	f := nativeFrame{r: r, t: r.stage}
	var slot nativeVariable
	values := []float64{12.5, 24.5, 0, math.Copysign(0, -1), 0, math.Float64frombits(0x7ff8000000000001), math.Float64frombits(0x7ff8000000000002)}
	var saved []any
	for _, value := range values {
		f.setNumber(&slot, "score", value)
		f.flushVariable(&slot)
		saved = append(saved, r.stage.variables["score"].Value)
	}
	for i, value := range saved {
		if got, ok := value.(float64); !ok || math.Float64bits(got) != math.Float64bits(values[i]) {
			t.Fatalf("saved number %d changed: %v", i, value)
		}
	}
	f.set(&slot, "score", "24.5")
	f.setNumber(&slot, "score", 24.5)
	f.flushVariable(&slot)
	if _, ok := r.stage.variables["score"].Value.(float64); !ok {
		t.Fatal("numeric assignment kept the old string")
	}
}

func BenchmarkNativeNumberWrite(b *testing.B) {
	for _, changed := range []bool{false, true} {
		b.Run(fmt.Sprintf("changed=%t", changed), func(b *testing.B) {
			r := New(&project.Project{Targets: []*project.Target{{IsStage: true}}})
			r.stage.variables["score"] = &Variable{Value: float64(0)}
			f := nativeFrame{r: r, t: r.stage}
			var slot nativeVariable
			value := 12.5
			b.ReportAllocs()
			for b.Loop() {
				if changed {
					value = 37 - value
				}
				f.setNumber(&slot, "score", value)
				f.flushVariable(&slot)
			}
		})
	}
}

func TestNativeRawNumberReusesBox(t *testing.T) {
	r := fixture(t, "{}")
	f := nativeFrame{r: r, t: r.stage}
	var slot nativeVariable
	var saved [3]any
	for _, value := range []float64{12.5, math.Copysign(0, -1), math.Float64frombits(0x7ff8000000000001)} {
		f.setNumber(&slot, "score", value)
		f.flushVariable(&slot)
		allocs := testing.AllocsPerRun(100, func() {
			for i := range saved {
				saved[i] = f.variable(&slot, "score")
			}
		})
		if allocs != 0 {
			t.Errorf("reading unchanged %v allocated %g times", value, allocs)
		}
		for _, got := range saved {
			if math.Float64bits(got.(float64)) != math.Float64bits(value) {
				t.Fatalf("raw read changed %v to %v", value, got)
			}
		}
	}
	value := 12.5
	allocs := testing.AllocsPerRun(100, func() {
		value = 37 - value
		f.setNumber(&slot, "score", value)
		for i := range saved {
			saved[i] = f.variable(&slot, "score")
		}
		f.flushVariable(&slot)
	})
	if allocs != 1 {
		t.Errorf("reading and flushing a changed number allocated %g times, want 1", allocs)
	}
}

func TestNativeRawNumberPreservesSnapshotsAndObservers(t *testing.T) {
	r := fixture(t, "{}")
	f := nativeFrame{r: r, t: r.stage}
	registers := nativeRegisters{variables: make([]nativeVariable, 1)}
	slot := &registers.variables[0]
	values := []float64{12.5, 24.5, 0, math.Copysign(0, -1), math.Float64frombits(0x7ff8000000000001), math.Float64frombits(0x7ff8000000000002)}
	saved := make([]any, len(values))
	for i, value := range values {
		f.setNumber(slot, "score", value)
		saved[i] = f.variable(slot, "score")
	}
	registers.sync(&f)
	r.stage.variables["score"].Value = "007"
	if got := f.variable(slot, "score"); got != "007" || f.number(slot, "score") != 7 {
		t.Fatalf("observer change was not reloaded: %v", got)
	}
	f.setNumber(slot, "score", 42.5)
	if got := f.variable(slot, "score"); got != 42.5 {
		t.Fatalf("assignment after observer change = %v", got)
	}
	f.setNumber(slot, "score", 84.5)
	var missing nativeVariable
	if got := f.variable(&missing, "missing"); got != nil || f.err == nil {
		t.Fatalf("missing variable = %v, error = %v", got, f.err)
	}
	f.flushVariable(slot)
	if got := r.stage.variables["score"].Value; got != 84.5 {
		t.Fatalf("write before error was lost: %v", got)
	}
	for i, got := range saved {
		if math.Float64bits(got.(float64)) != math.Float64bits(values[i]) {
			t.Fatalf("saved raw number %d changed: %v", i, got)
		}
	}
}

func BenchmarkNativeRawNumber(b *testing.B) {
	for _, changed := range []bool{false, true} {
		b.Run(fmt.Sprintf("changed=%t", changed), func(b *testing.B) {
			r := New(&project.Project{Targets: []*project.Target{{IsStage: true}}})
			r.stage.variables["score"] = &Variable{Value: float64(12.5)}
			f := nativeFrame{r: r, t: r.stage}
			var slot nativeVariable
			var saved [3]any
			value := 12.5
			b.ReportAllocs()
			for b.Loop() {
				if changed {
					value = 37 - value
				}
				f.setNumber(&slot, "score", value)
				for i := range saved {
					saved[i] = f.variable(&slot, "score")
				}
				f.flushVariable(&slot)
			}
			if saved[0] != value {
				b.Fatal("raw value was not retained")
			}
		})
	}
}

func TestNativeRepeatedCoercionPreservesComparisons(t *testing.T) {
	r := fixture(t, "{}")
	f := nativeFrame{r: r, t: r.stage}
	var slot nativeVariable
	values := []any{"007", "", " \t", "invalid", "NaN", "0x10", "1e309", true, nil, 12.5, math.NaN(), math.Inf(1), math.Copysign(0, -1)}
	for _, value := range values {
		f.set(&slot, "score", value)
		for range 3 {
			if got := f.number(&slot, "score"); math.Float64bits(got) != math.Float64bits(number(value)) {
				t.Fatalf("numeric read changed %v to %v", value, got)
			}
			for _, other := range values {
				got := nativeCompare(f.comparison(&slot, "score"), nativeComparison{raw: other})
				want := compare(value, other)
				if (got < 0) != (want < 0) || (got == 0) != (want == 0) || (got > 0) != (want > 0) {
					t.Fatalf("comparison changed for %v and %v: %g/%g", value, other, got, want)
				}
			}
		}
		f.flushVariable(&slot)
		got := r.stage.variables["score"].Value
		if text(got) != text(value) || fmt.Sprintf("%T", got) != fmt.Sprintf("%T", value) {
			t.Fatalf("conversion changed stored value %v to %v", value, got)
		}
	}
	f.set(&slot, "score", "1e")
	f.number(&slot, "score")
	f.comparison(&slot, "score")
	allocations := testing.AllocsPerRun(100, func() {
		f.number(&slot, "score")
		nativeCompare(f.comparison(&slot, "score"), nativeComparison{number: 1, numeric: true})
	})
	if allocations != 0 {
		t.Fatalf("unchanged variable was parsed again: %g allocations", allocations)
	}
}

func TestNativeNumberCacheTracksValueTypes(t *testing.T) {
	r := fixture(t, "{}")
	f := nativeFrame{r: r, t: r.stage}
	var slot nativeVariable
	for _, value := range []any{12.5, "007", true, math.NaN(), " ", "invalid", math.Inf(1), math.Copysign(0, -1)} {
		f.set(&slot, "score", value)
		got := f.number(&slot, "score")
		if math.Float64bits(got) != math.Float64bits(number(value)) {
			t.Fatalf("value %v: number=%v", value, got)
		}
		_, wantNumeric := value.(float64)
		if slot.numeric != wantNumeric {
			t.Fatalf("value %T(%v): numeric cache=%v, want %v", value, value, slot.numeric, wantNumeric)
		}
		f.flushVariable(&slot)
		stored := r.stage.variables["score"].Value
		if n, ok := value.(float64); ok {
			actual, numeric := stored.(float64)
			if !numeric || math.Float64bits(actual) != math.Float64bits(n) {
				t.Fatalf("reading a number changed the stored value: %v", value)
			}
		} else if stored != value {
			t.Fatalf("reading a number changed the stored value: %v", value)
		}
	}
	f.set(&slot, "score", 42.5)
	if got := f.comparison(&slot, "score"); !slot.numeric || got.number != 42.5 {
		t.Fatal("comparison did not cache the numeric value")
	}
	err := fmt.Errorf("stopped calculation")
	f.err = err
	if f.number(&slot, "score") != 0 {
		t.Fatal("cached number bypassed the pending error")
	}
	if got := f.comparison(&slot, "score"); got.numeric || got.raw != nil || f.err != err {
		t.Fatal("cached comparison bypassed the pending error")
	}
}

func BenchmarkNativeRepeatedCoercion(b *testing.B) {
	for _, value := range []any{12.5, "12.5", "not a number"} {
		b.Run(fmt.Sprintf("%T/%v", value, value), func(b *testing.B) {
			f := nativeFrame{}
			slot := nativeVariable{value: &Variable{Value: value}}
			b.ReportAllocs()
			for b.Loop() {
				f.number(&slot, "score")
				nativeCompare(f.comparison(&slot, "score"), nativeComparison{number: 1, numeric: true})
			}
		})
	}
}

func TestNativeListBindingTracksAppendsAndReplacements(t *testing.T) {
	r := listRuntime(t, "{}", "[1,2]")
	steps := 0
	f := nativeFrame{ctx: context.Background(), r: r, t: r.stage, steps: &steps}
	var slot nativeList
	list := f.list(&slot, "items")
	list.append("007")
	list.replace(0, true)
	slot.dirty = true
	if got := nativeItem(f.list(&slot, "items"), "last"); got != "007" {
		t.Fatalf("appended value = %v", got)
	}
	f.flushList(&slot, "items")
	if got := r.stage.state.Lists["items"].Items; len(got) != 3 || got[0] != true {
		t.Fatalf("list changes were lost: %v", got)
	}
}

func TestNativeRegionCompilesSingleHeavyStatement(t *testing.T) {
	for _, simple := range []bool{false, true} {
		p := nativeCodegenFixture(t)
		target := &target{source: p.Targets[1], sourceIndex: 1}
		if simple {
			target.source.Blocks["singleIncrement"].Inputs = literalInputs(map[string]any{"VALUE": 1.0})
		}
		target.source.Blocks["singleBody"].Opcode = "control_if"
		if err := prepareProcedures(target); err != nil {
			t.Fatal(err)
		}
		g := &nativeGenerator{}
		g.compileRegions(target, "singleRegion")
		compiled := false
		for _, fn := range g.functions {
			if fn.region == "singleIncrement" {
				compiled = true
			}
		}
		if compiled == simple {
			t.Fatalf("simple=%v: single calculation compiled=%v", simple, compiled)
		}
	}
}

func TestNativeVariableCopyPreservesTypes(t *testing.T) {
	for _, value := range []any{12.5, math.NaN(), math.Inf(1), math.Copysign(0, -1), "007", "", "NaN", false, nil} {
		t.Run(fmt.Sprintf("%T/%v", value, value), func(t *testing.T) {
			f := nativeFrame{}
			source := nativeVariable{value: &Variable{Value: value}}
			destination := nativeVariable{value: &Variable{Value: "old"}}
			if n, ok := value.(float64); ok {
				source.number, source.numeric, source.dirty = n, true, true
				source.value.Value = "uncommitted"
			} else {
				f.number(&source, "source")
			}
			f.setValue(&destination, "destination", f.variableValue(&source, "source"))
			if source.numeric && source.value.Value != "uncommitted" {
				t.Fatal("copy materialized the source number")
			}
			f.flushVariable(&destination)
			got := destination.value.Value
			if n, ok := value.(float64); ok {
				actual, ok := got.(float64)
				if !ok || math.Float64bits(actual) != math.Float64bits(n) {
					t.Fatalf("copy = %v, want %v", got, value)
				}
			} else if got != value {
				t.Fatalf("copy = %#v, want %#v", got, value)
			}
			f.setValue(&destination, "destination", f.variableValue(&destination, "destination"))
			f.flushVariable(&destination)
			if n, ok := got.(float64); ok {
				if math.Float64bits(destination.value.Value.(float64)) != math.Float64bits(n) {
					t.Fatal("self-copy changed the number")
				}
			} else if destination.value.Value != got {
				t.Fatal("self-copy changed the value")
			}
		})
	}
}

func TestNativeVariableCopyDoesNotKeepValuesAcrossSync(t *testing.T) {
	r := fixture(t, "{}")
	f := nativeFrame{r: r, t: r.stage}
	registers := nativeRegisters{variables: make([]nativeVariable, 1)}
	slot := &registers.variables[0]
	f.setNumber(slot, "score", 12.5)
	saved := f.variableValue(slot, "score")
	registers.sync(&f)
	r.stage.variables["score"].Value = "007"
	got := f.variableValue(slot, "score")
	if got.numeric || got.value != "007" || saved.boxed() != 12.5 {
		t.Fatalf("copy retained stale state: got=%+v saved=%+v", got, saved)
	}
	var missing nativeVariable
	destination := nativeVariable{value: &Variable{Value: "unchanged"}}
	f.setValue(&destination, "destination", f.variableValue(&missing, "missing"))
	if f.err == nil || destination.value.Value != "unchanged" || destination.dirty {
		t.Fatal("missing source changed the destination")
	}
}

func TestNativeCopiedNumbersKeepNumericState(t *testing.T) {
	r := fixture(t, "{}")
	f := nativeFrame{r: r, t: r.stage}
	var slot nativeVariable
	for _, value := range []any{12.5, "007", -3.25, true, math.Copysign(0, -1), "last", math.NaN(), math.Inf(1)} {
		f.set(&slot, "score", value)
		if n, ok := value.(float64); ok {
			if !slot.numeric || math.Float64bits(slot.number) != math.Float64bits(n) {
				t.Fatalf("copied number %v lost its numeric state", value)
			}
		}
		got := f.number(&slot, "score")
		if math.Float64bits(got) != math.Float64bits(number(value)) {
			t.Fatalf("number(%v) = %v", value, got)
		}
		f.flushVariable(&slot)
		raw := f.variable(&slot, "score")
		if n, ok := value.(float64); ok {
			stored, ok := raw.(float64)
			if !ok || math.Float64bits(stored) != math.Float64bits(n) {
				t.Fatalf("copy changed %v to %v", value, raw)
			}
		} else if raw != value {
			t.Fatalf("copy changed %v to %v", value, raw)
		}
	}
}

func BenchmarkNativeCopiedNumber(b *testing.B) {
	for _, value := range []any{12.5, "12.5", true} {
		b.Run(fmt.Sprintf("%T", value), func(b *testing.B) {
			r := New(&project.Project{Targets: []*project.Target{{IsStage: true}}})
			r.stage.variables["score"] = &Variable{}
			f := nativeFrame{r: r, t: r.stage}
			var source, destination nativeVariable
			b.ReportAllocs()
			for b.Loop() {
				f.set(&source, "score", value)
				f.setNumber(&destination, "score", f.number(&source, "score")*2)
				f.flushVariable(&source)
			}
			if f.err != nil {
				b.Fatal(f.err)
			}
		})
	}
}
