package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"gowarp/core/project"
)

func calculationFixture() *project.Project {
	blocks := project.Blocks{}
	variable := func(id string) []any { return []any{float64(1), []any{float64(12), id, id}} }
	literal := func(value any) []any { return []any{float64(1), []any{float64(4), value}} }
	for i := range 8 {
		prefix := fmt.Sprintf("c%d", i)
		next := ""
		if i < 7 {
			next = fmt.Sprintf("c%d", i+1)
		}
		blocks[prefix] = &project.Block{Opcode: "data_setvariableto", Next: prefix + "y", Fields: map[string][]any{"VARIABLE": {"x", "x"}}, Inputs: map[string][]any{"VALUE": {float64(2), prefix + "sum"}}}
		blocks[prefix+"sum"] = &project.Block{Opcode: "operator_add", Inputs: map[string][]any{"NUM1": {float64(2), prefix + "product"}, "NUM2": variable("y")}}
		blocks[prefix+"product"] = &project.Block{Opcode: "operator_multiply", Inputs: map[string][]any{"NUM1": variable("x"), "NUM2": literal("0.75")}}
		blocks[prefix+"y"] = &project.Block{Opcode: "data_changevariableby", Next: prefix + "write", Fields: map[string][]any{"VARIABLE": {"y", "y"}}, Inputs: map[string][]any{"VALUE": {float64(2), prefix + "delta"}}}
		blocks[prefix+"delta"] = &project.Block{Opcode: "operator_multiply", Inputs: map[string][]any{"NUM1": variable("x"), "NUM2": literal("0.125")}}
		blocks[prefix+"write"] = &project.Block{Opcode: "data_replaceitemoflist", Next: prefix + "read", Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: map[string][]any{"INDEX": literal("1"), "ITEM": variable("x")}}
		blocks[prefix+"read"] = &project.Block{Opcode: "data_setvariableto", Next: next, Fields: map[string][]any{"VARIABLE": {"z", "z"}}, Inputs: map[string][]any{"VALUE": {float64(2), prefix + "item"}}}
		blocks[prefix+"item"] = &project.Block{Opcode: "data_itemoflist", Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: map[string][]any{"INDEX": literal("1")}}
	}
	return &project.Project{Targets: []*project.Target{
		{IsStage: true, Name: "Stage"},
		{Name: "Sprite", Blocks: blocks, Variables: map[string][]any{"x": {"x", 1.25}, "y": {"y", 2.5}, "z": {"z", 0.0}}, Lists: map[string][]any{"items": {"items", []any{0.0}}}},
	}}
}

func TestCalculationChainAllocations(t *testing.T) {
	r := New(calculationFixture())
	th := &thread{target: r.targets[1]}
	allocations := testing.AllocsPerRun(100, func() {
		*th.target.variables["x"] = Variable{Value: 1.25}
		*th.target.variables["y"] = Variable{Value: 2.5}
		th.pc, th.done = "c0", false
		if err := r.step(context.Background(), th); err != nil {
			t.Fatal(err)
		}
	})
	if allocations > 12 {
		t.Fatalf("calculation chain allocated %g objects; intermediate variable writes should stay unboxed", allocations)
	}
}

func TestCalculationChainMatchesInterpreter(t *testing.T) {
	fast, slow := New(calculationFixture()), New(calculationFixture())
	disableCalculations(slow)
	for iteration := range 5 {
		var counts [2]int
		for i, r := range []*Runtime{fast, slow} {
			th := &thread{target: r.targets[1], pc: "c0"}
			if err := r.step(context.Background(), th); err != nil {
				t.Fatal(err)
			}
			if !th.done {
				t.Fatal("calculation chain did not finish")
			}
			counts[i] = th.sliceExecuted
		}
		if counts[0] != counts[1] || !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) {
			t.Fatalf("iteration %d: calculation changed state or statement counts (%v)", iteration, counts)
		}
	}
	if fast.calculations[fast.targets[1].source.Blocks["c0"]] == nil {
		t.Fatal("calculation chain was not prepared")
	}
}

func disableCalculations(r *Runtime) {
	r.calculations = make(map[*project.Block]*calculation)
	for _, target := range r.targets {
		for _, block := range target.source.Blocks {
			r.calculations[block] = nil
		}
	}
}

func TestCalculationErrorCommitsCompletedWrites(t *testing.T) {
	for _, missing := range []string{"x", "y", "z", "items"} {
		t.Run(missing, func(t *testing.T) {
			p := calculationFixture()
			// Let one group finish before the missing binding is accessed.
			if missing == "items" {
				p.Targets[1].Blocks["c1write"].Fields["LIST"][1] = "missing list"
			} else {
				p.Targets[1].Blocks["c1"].Inputs["VALUE"] = []any{float64(1), []any{float64(12), missing, "missing " + missing}}
			}
			fast, slow := New(p), New(p)
			disableCalculations(slow)
			ft, st := &thread{target: fast.targets[1], pc: "c0"}, &thread{target: slow.targets[1], pc: "c0"}
			fe, se := fast.step(context.Background(), ft), slow.step(context.Background(), st)
			if fe == nil || fmt.Sprint(fe) != fmt.Sprint(se) || ft.pc != st.pc ||
				ft.sliceExecuted != st.sliceExecuted || !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) {
				t.Fatalf("error changed completed writes or continuation: fast=%v slow=%v pc=%s/%s", fe, se, ft.pc, st.pc)
			}
		})
	}
}

func TestCalculationBindingsFollowClonesAndGlobals(t *testing.T) {
	p := calculationFixture()
	p.Targets[0].Variables = map[string][]any{"y": {"y", 2.5}}
	delete(p.Targets[1].Variables, "y")
	fast, slow := New(p), New(p)
	disableCalculations(slow)
	for _, r := range []*Runtime{fast, slow} {
		r.targets[1].state.Lists["items"] = List{Items: make([]any, 1024)}
		r.createClone(r.targets[1])
	}
	for _, index := range []int{1, 2, 1, 2} {
		for _, r := range []*Runtime{fast, slow} {
			*r.stage.variables["y"] = Variable{Value: float64(index)}
			th := &thread{target: r.targets[index], pc: "c0"}
			if err := r.step(context.Background(), th); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) {
			t.Fatal("cached slots mixed global, source, or clone values")
		}
	}
}

func TestCalculationQuotaAndCancellation(t *testing.T) {
	for _, warp := range []bool{false, true} {
		fast, slow := New(calculationFixture()), New(calculationFixture())
		disableCalculations(slow)
		var pcs [2]string
		for i, r := range []*Runtime{fast, slow} {
			steps := stepsPerFrame - 2
			th := &thread{target: r.targets[1], pc: "c0", warp: warp, sliceSteps: &steps, sliceLimit: stepsPerFrame, sliceStarted: time.Now().Add(-time.Second)}
			if err := r.stepBody(context.Background(), th); err != nil {
				t.Fatal(err)
			}
			if steps != stepsPerFrame {
				t.Fatalf("quota crossed: %d", steps)
			}
			pcs[i] = th.pc
		}
		if pcs[0] != "c0write" || pcs[0] != pcs[1] || !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) {
			t.Fatalf("warp=%v: quota changed continuation %v", warp, pcs)
		}
	}
	r := New(calculationFixture())
	program := r.prepareCalculation(r.targets[1], r.targets[1].source.Blocks["c0"])
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	steps := 1
	th := &thread{target: r.targets[1], pc: "c0", sliceSteps: &steps}
	before := r.Snapshot()
	if err := program.run(ctx, r, th); err != context.Canceled || th.pc != "c0" || !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("canceled calculation advanced: %v, pc=%s", err, th.pc)
	}
}

func TestCalculationWaitBoundary(t *testing.T) {
	p := calculationFixture()
	p.Targets[1].Blocks["c0read"].Next = "wait"
	p.Targets[1].Blocks["wait"] = &project.Block{Opcode: "control_wait", Next: "c1", Inputs: map[string][]any{"DURATION": {float64(1), []any{float64(4), "0"}}}}
	fast, slow := New(p), New(p)
	disableCalculations(slow)
	threads := []*thread{{target: fast.targets[1], pc: "c0"}, {target: slow.targets[1], pc: "c0"}}
	for i, r := range []*Runtime{fast, slow} {
		if err := r.step(context.Background(), threads[i]); err != nil {
			t.Fatal(err)
		}
		if threads[i].pc != "c1" || !r.redrawRequested {
			t.Fatal("calculation crossed a wait")
		}
		*r.targets[1].variables["x"] = Variable{Value: 99.0}
		r.frame++
		if err := r.step(context.Background(), threads[i]); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) {
		t.Fatal("resuming after wait reused stale values")
	}
}

func TestCalculationSpecialValues(t *testing.T) {
	for _, value := range []any{"007", "", "last", true, false, nil, math.NaN(), math.Inf(1), math.Inf(-1), math.Copysign(0, -1)} {
		p := calculationFixture()
		p.Targets[1].Blocks["c7read"].Inputs["VALUE"] = []any{float64(2), "argument"}
		p.Targets[1].Blocks["argument"] = &project.Block{Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"input"}}}
		fast, slow := New(p), New(p)
		disableCalculations(slow)
		for _, r := range []*Runtime{fast, slow} {
			*r.targets[1].variables["x"] = Variable{Value: value}
			th := &thread{target: r.targets[1], pc: "c0", params: procedureParameters{"input": {value: value}}}
			if err := r.step(context.Background(), th); err != nil {
				t.Fatal(err)
			}
		}
		for id, v := range fast.targets[1].variables {
			got, want := v.Value, slow.targets[1].variables[id].Value
			if n, ok := want.(float64); ok {
				g, isNumber := got.(float64)
				if !isNumber || math.Float64bits(g) != math.Float64bits(n) {
					t.Fatalf("value=%v variable=%s: %v, want %v", value, id, got, want)
				}
			} else if !reflect.DeepEqual(got, want) {
				t.Fatalf("value=%v changed stored type: %v, want %v", value, got, want)
			}
		}
	}
}

func TestCalculationPreparationPreservesProject(t *testing.T) {
	p := calculationFixture()
	before, _ := json.Marshal(p)
	r := New(p)
	r.prepareCalculation(r.targets[1], p.Targets[1].Blocks["c0"])
	after, _ := json.Marshal(p)
	if string(before) != string(after) {
		t.Fatal("preparation rewrote project blocks")
	}
}

func TestCalculationListObserverUsesLiveState(t *testing.T) {
	p := calculationFixture()
	for i := range 8 {
		p.Targets[1].Blocks[fmt.Sprintf("c%dwrite", i)].Opcode = "data_addtolist"
	}
	fast, slow := New(p), New(p)
	// Observers may be installed after the instructions have been prepared.
	fast.prepareCalculation(fast.targets[1], p.Targets[1].Blocks["c0"])
	disableCalculations(slow)
	var snapshots [2][][]TargetState
	for i, r := range []*Runtime{fast, slow} {
		r.listObserver = func(ListAppend) {
			snapshots[i] = append(snapshots[i], r.Snapshot())
		}
		th := &thread{target: r.targets[1], pc: "c0"}
		if err := r.step(context.Background(), th); err != nil {
			t.Fatal(err)
		}
	}
	if len(snapshots[0]) != 8 || !reflect.DeepEqual(snapshots[0], snapshots[1]) {
		t.Fatal("observer missed an append or saw deferred variable writes")
	}
}

func TestCalculationPreservesCombinedInputBudget(t *testing.T) {
	p := calculationFixture()
	blocks := p.Targets[1].Blocks
	blocks["leaf"] = &project.Block{Opcode: "operator_add", Inputs: map[string][]any{"NUM1": {float64(1), []any{float64(4), "1"}}, "NUM2": {float64(1), []any{float64(4), "0"}}}}
	previous := "leaf"
	for i := range 12 {
		id := fmt.Sprintf("shared%d", i)
		blocks[id] = &project.Block{Opcode: "operator_add", Inputs: map[string][]any{"NUM1": {float64(2), previous}, "NUM2": {float64(2), previous}}}
		previous = id
	}
	blocks["c1write"].Inputs = map[string][]any{"INDEX": {float64(2), previous}, "ITEM": {float64(2), previous}}
	fast, slow := New(p), New(p)
	disableCalculations(slow)
	ft, st := &thread{target: fast.targets[1], pc: "c0"}, &thread{target: slow.targets[1], pc: "c0"}
	fe, se := fast.step(context.Background(), ft), slow.step(context.Background(), st)
	if se == nil || fmt.Sprint(fe) != fmt.Sprint(se) || ft.pc != st.pc || !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) {
		t.Fatalf("input budget bypassed: fast=%v slow=%v", fe, se)
	}
}

func BenchmarkCalculationChain(b *testing.B) {
	for _, compiled := range []bool{false, true} {
		b.Run(fmt.Sprintf("commands=%v", compiled), func(b *testing.B) {
			benchmarkCalculationChain(b, compiled)
		})
	}
}

func BenchmarkCalculationSimpleChain(b *testing.B) {
	for _, opcode := range []string{"data_setvariableto", "data_replaceitemoflist"} {
		for _, compiled := range []bool{false, true} {
			b.Run(fmt.Sprintf("%s/commands=%v", opcode, compiled), func(b *testing.B) {
				p := calculationFixture()
				blocks := p.Targets[1].Blocks
				for i := range 8 {
					id := fmt.Sprintf("c%d", i)
					next := ""
					if i < 7 {
						next = fmt.Sprintf("c%d", i+1)
					}
					blocks[id] = &project.Block{
						Opcode: opcode, Next: next,
						Fields: map[string][]any{"VARIABLE": {"x", "x"}, "LIST": {"items", "items"}},
						Inputs: map[string][]any{
							"VALUE": {float64(1), []any{float64(4), "1"}},
							"INDEX": {float64(1), []any{float64(4), "1"}},
							"ITEM":  {float64(1), []any{float64(4), "1"}},
						},
					}
				}
				r := New(p)
				if !compiled {
					disableCalculations(r)
				}
				th := &thread{target: r.targets[1]}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					th.pc, th.done = "c0", false
					if err := r.step(context.Background(), th); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func benchmarkCalculationChain(b *testing.B, compiled bool) {
	r := New(calculationFixture())
	if !compiled {
		disableCalculations(r)
	}
	target := r.targets[1]
	th := &thread{target: target}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		*target.variables["x"] = Variable{Value: 1.25}
		*target.variables["y"] = Variable{Value: 2.5}
		th.pc, th.done = "c0", false
		if err := r.step(context.Background(), th); err != nil {
			b.Fatal(err)
		}
		if !th.done || th.sliceExecuted != 33 {
			b.Fatalf("incomplete chain: done=%v steps=%d", th.done, th.sliceExecuted)
		}
	}
}
