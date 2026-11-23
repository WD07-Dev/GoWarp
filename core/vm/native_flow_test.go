package vm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"gowarp/core/project"
)

func addNativeFlowBlocks(t testing.TB, blocks project.Blocks) {
	t.Helper()
	var extra project.Blocks
	err := json.Unmarshal([]byte(`{
        "flowBody":{"opcode":"control_repeat","next":"flowWait","inputs":{"TIMES":[1,[12,"x","x"]],"SUBSTACK":[2,"flowReset"]}},
        "flowReset":{"opcode":"data_setvariableto","next":"flowUntil","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[1,[4,"0"]]}},
        "flowUntil":{"opcode":"control_repeat_until","inputs":{"CONDITION":[2,"flowEnd"],"SUBSTACK":[2,"flowIncrement"]}},
        "flowEnd":{"opcode":"operator_gt","inputs":{"OPERAND1":[1,[12,"y","y"]],"OPERAND2":[1,[4,"9"]]}},
        "flowIncrement":{"opcode":"data_changevariableby","next":"flowChoice","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[1,[4,"1"]]}},
        "flowChoice":{"opcode":"control_if_else","inputs":{"CONDITION":[2,"flowHalf"],"SUBSTACK":[2,"flowHigh"],"SUBSTACK2":[2,"flowLow"]}},
        "flowHalf":{"opcode":"operator_gt","inputs":{"OPERAND1":[1,[12,"y","y"]],"OPERAND2":[1,[4,"5"]]}},
        "flowHigh":{"opcode":"procedures_call","mutation":{"proccode":"flowStep","argumentids":"[\"nID\"]"},"inputs":{"nID":[1,[4,"0.25"]]}},
        "flowLow":{"opcode":"procedures_call","mutation":{"proccode":"flowStep","argumentids":"[\"nID\"]"},"inputs":{"nID":[1,[4,"0.125"]]}},
        "flowStepBody":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"arg"]}},
        "flowWait":{"opcode":"control_wait","next":"flowDone","inputs":{"DURATION":[1,[4,"0.05"]]}},
        "flowDone":{"opcode":"data_addtolist","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[10,"done"]]}},
        "flowWhileBody":{"opcode":"control_while","next":"flowDone","inputs":{"CONDITION":[2,"flowMore"],"SUBSTACK":[2,"flowIncrement"]}},
        "flowMore":{"opcode":"operator_lt","inputs":{"OPERAND1":[1,[12,"y","y"]],"OPERAND2":[1,[4,"10"]]}},
        "flowEmptyBody":{"opcode":"control_repeat_until","inputs":{"CONDITION":[1,[10,"false"]]}},
        "flowYieldBody":{"opcode":"control_repeat_until","next":"flowDone","inputs":{"CONDITION":[2,"flowEnd"],"SUBSTACK":[2,"flowYieldIncrement"]}},
        "flowYieldIncrement":{"opcode":"data_changevariableby","next":"flowYieldWait","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[1,[4,"1"]]}},
        "flowYieldWait":{"opcode":"control_wait","next":"flowChoice","inputs":{"DURATION":[1,[4,"0"]]}},
        "flowBreakBody":{"opcode":"control_repeat_until","next":"flowDone","inputs":{"CONDITION":[1,[10,"false"]],"SUBSTACK":[2,"flowBreakIncrement"]}},
        "flowBreakIncrement":{"opcode":"data_changevariableby","next":"flowBreakNow","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[1,[4,"1"]]}},
        "flowBreakNow":{"opcode":"control_break"},
        "flowEachBody":{"opcode":"control_repeat_until","next":"flowDone","inputs":{"CONDITION":[2,"flowEnd"],"SUBSTACK":[2,"flowEach"]}},
        "flowEach":{"opcode":"control_for_each","fields":{"VARIABLE":["x","x"]},"inputs":{"VALUE":[1,[4,"3"]],"SUBSTACK":[2,"flowEachIncrement"]}},
        "flowEachIncrement":{"opcode":"data_changevariableby","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[1,[4,"1"]]}},
        "flowCycleBody":{"opcode":"control_repeat_until","inputs":{"CONDITION":[1,[10,"false"]],"SUBSTACK":[2,"flowCycleIncrement"]}},
        "flowCycleIncrement":{"opcode":"data_changevariableby","next":"flowCycleIncrement","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[1,[4,"1"]]}},
        "flowBranchBody":{"opcode":"control_repeat_until","inputs":{"CONDITION":[1,[10,"false"]],"SUBSTACK":[2,"flowBranchIf"]}},
        "flowBranchIf":{"opcode":"control_if","next":"flowBranchAfter","inputs":{"CONDITION":[1,[10,"true"]],"SUBSTACK":[2,"flowBranchInside"]}},
        "flowBranchInside":{"opcode":"data_changevariableby","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[1,[4,"1"]]}},
        "flowBranchAfter":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
        "flowStopBody":{"opcode":"control_repeat_until","inputs":{"CONDITION":[1,[10,"false"]],"SUBSTACK":[2,"flowStopRepeat"]}},
        "flowStopRepeat":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"10"]],"SUBSTACK":[2,"flowStopStamp"]}},
        "flowStopStamp":{"opcode":"pen_stamp","next":"flowStopAfter"},
        "flowStopAfter":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
    }`), &extra)
	if err != nil {
		t.Fatal(err)
	}
	for id, block := range extra {
		blocks[id] = block
	}
	var conditional project.Blocks
	if err := json.Unmarshal([]byte(`{
		"conditionalBody":{"opcode":"control_repeat","inputs":{"TIMES":[1,[12,"x","x"]],"SUBSTACK":[2,"conditionalIncrement"]}},
		"conditionalIncrement":{"opcode":"data_changevariableby","next":"conditionalIf","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[1,[4,"1"]]}},
		"conditionalIf":{"opcode":"control_if","next":"conditionalCalculate","inputs":{"CONDITION":[2,"conditionalAtTen"],"SUBSTACK":[2,"conditionalWait"]}},
		"conditionalAtTen":{"opcode":"operator_equals","inputs":{"OPERAND1":[1,[12,"y","y"]],"OPERAND2":[1,[4,"10"]]}},
		"conditionalWait":{"opcode":"control_wait","inputs":{"DURATION":[1,[12,"waitDuration","waitDuration"]]}},
		"conditionalCalculate":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"conditionalDivide"]}},
		"conditionalDivide":{"opcode":"operator_divide","inputs":{"NUM1":[2,"conditionalAdd"],"NUM2":[1,[4,"3"]]}},
		"conditionalAdd":{"opcode":"operator_add","inputs":{"NUM1":[2,"conditionalMultiply"],"NUM2":[1,[4,"2"]]}},
		"conditionalMultiply":{"opcode":"operator_multiply","inputs":{"NUM1":[1,[12,"y","y"]],"NUM2":[1,[4,"1.25"]]}}
	}`), &conditional); err != nil {
		t.Fatal(err)
	}
	for id, block := range conditional {
		blocks[id] = block
	}
	previous := "conditionalCalculate"
	for i := range 8 {
		id := fmt.Sprintf("conditionalAdjust%d", i)
		blocks[previous].Next = id
		blocks[id] = &project.Block{
			Opcode: "data_changevariableby",
			Fields: map[string][]any{"VARIABLE": {"score", "score"}},
			Inputs: literalInputs(map[string]any{"VALUE": .25}),
		}
		previous = id
	}
	blocks["connectedCallsBody"] = &project.Block{Opcode: "control_repeat", Inputs: map[string][]any{"TIMES": {float64(1), []any{float64(12), "x", "x"}}, "SUBSTACK": {float64(2), "connectedCallsCall"}}}
	blocks["connectedCallsCall"] = &project.Block{Opcode: "procedures_call", Mutation: map[string]json.RawMessage{"proccode": json.RawMessage(`"suspendingStep"`), "argumentids": json.RawMessage(`"[\"nID\"]"`)}, Inputs: literalInputs(map[string]any{"nID": .25})}
	blocks["suspendingStepBody"] = &project.Block{Opcode: "data_changevariableby", Next: "suspendingStepWait", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), "arg"}}}
	blocks["suspendingStepWait"] = &project.Block{Opcode: "control_wait", Next: "suspendingStepAfter", Inputs: map[string][]any{"DURATION": {float64(1), []any{float64(12), "waitDuration", "waitDuration"}}}}
	blocks["suspendingStepAfter"] = &project.Block{Opcode: "data_changevariableby", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), "arg"}}}
}

func TestNativeFlowCombinesCalculations(t *testing.T) {
	p := nativeCodegenFixture(t)
	target := &target{source: p.Targets[1], sourceIndex: 1}
	if err := prepareProcedures(target); err != nil {
		t.Fatal(err)
	}
	g := &nativeGenerator{known: make(map[*procedure]*nativeFunction), active: make(map[*procedure]bool)}
	if !g.compileFlow(target, "conditional") {
		t.Fatal("conditional procedure has no compiled flow")
	}
	work := make(map[string]int)
	for _, function := range g.functions {
		work[function.region] = function.work
	}
	// Interior entries must remain available when a time slice ends mid-chain.
	for i := range 8 {
		if work[fmt.Sprintf("conditionalAdjust%d", i)] == 0 {
			t.Fatal("missing calculation resume entry")
		}
	}
	if work["conditionalCalculate"] < 9 {
		t.Fatalf("calculations still have separate dispatch boundaries: work=%d", work["conditionalCalculate"])
	}
}

func testNativeConnectedCalls(t *testing.T) {
	for _, duration := range []float64{0, .02} {
		fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
		slow.native = false
		for _, r := range []*Runtime{fast, slow} {
			target := r.targets[1]
			target.variables["x"].Value = float64(4)
			target.variables["score"].Value = float64(0)
			target.variables["waitDuration"].Value = duration
			r.startHat(target, "connectedCallsHat", false)
		}
		proc := fast.targets[1].procedures["connectedCalls"]
		if proc == nil {
			if err := prepareProcedures(fast.targets[1]); err != nil {
				t.Fatal(err)
			}
			proc = fast.targets[1].procedures["connectedCalls"]
		}
		if proc.regions["connectedCallsBody"] == nil {
			t.Fatal("loop with resumable calls has no compiled flow")
		}
		for tick := range 16 {
			for _, r := range []*Runtime{fast, slow} {
				if err := r.Run(context.Background(), 1, 60, false); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) || fast.Done() != slow.Done() {
				t.Fatalf("duration=%v tick=%d: compiled calls changed scheduling", duration, tick)
			}
		}
		if !fast.Done() || fast.targets[1].variables["score"].Value != float64(2) {
			t.Fatal("calls did not finish with the expected result")
		}
	}
}

func BenchmarkNativeConnectedCalls(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	r := New(nativeCodegenFixture(b))
	target := r.targets[1]
	b.ReportAllocs()
	for b.Loop() {
		target.variables["x"].Value = float64(1000)
		target.variables["score"].Value = float64(0)
		r.startHat(target, "connectedCallsHat", false)
		if err := r.Run(context.Background(), 1, 60, false); err != nil {
			b.Fatal(err)
		}
		if !r.Done() || target.variables["score"].Value != float64(500) {
			b.Fatal("connected call workload did not finish")
		}
	}
}

func testNativeConditionalRepeat(t *testing.T) {
	fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
	slow.native = false
	for _, r := range []*Runtime{fast, slow} {
		target := r.targets[1]
		*target.variables["x"] = Variable{Value: float64(100)}
		*target.variables["y"] = Variable{Value: float64(0)}
		*target.variables["score"] = Variable{Value: float64(0)}
		target.variables["waitDuration"].Value = .02
		r.startHat(target, "conditionalHat", false)
	}
	target := fast.targets[1]
	if err := prepareProcedures(target); err != nil {
		t.Fatal(err)
	}
	if target.procedures["conditional"].regions["conditionalIf"] == nil {
		t.Fatal("conditional loop returned to the interpreter for its pure condition")
	}
	for tick := range 4 {
		for _, r := range []*Runtime{fast, slow} {
			if err := r.Run(context.Background(), 1, 60, false); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) || fast.Done() != slow.Done() {
			t.Fatalf("tick %d: conditional loop changed its state or wait timing", tick)
		}
		if tick == 0 && target.variables["y"].Value != float64(10) {
			t.Fatalf("compiled control flow skipped the wait boundary: y=%v", target.variables["y"].Value)
		}
	}
	if !fast.Done() || target.variables["y"].Value != float64(100) {
		t.Fatal("conditional loop did not finish")
	}
}

func BenchmarkNativeConditionalRepeat(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	r := New(nativeCodegenFixture(b))
	target := r.targets[1]
	b.ReportAllocs()
	for b.Loop() {
		*target.variables["x"] = Variable{Value: float64(1000)}
		*target.variables["y"] = Variable{Value: float64(0)}
		*target.variables["score"] = Variable{Value: float64(0)}
		r.startHat(target, "conditionalHat", false)
		if err := r.Run(context.Background(), 4, 60, false); err != nil {
			b.Fatal(err)
		}
		if !r.Done() || target.variables["y"].Value != float64(1000) {
			b.Fatal("conditional loop did not finish all iterations")
		}
	}
}

func testNativeFlow(t *testing.T) {
	r := New(nativeCodegenFixture(t))
	target := r.targets[1]
	if err := prepareProcedures(target); err != nil {
		t.Fatal(err)
	}
	region := target.procedures["flow"].regions["flowBody"]
	if region == nil {
		t.Fatal("nested loop header was not compiled")
	}
	runRegion := region.run
	calls := 0
	region.run = func(ctx context.Context, r *Runtime, th *thread) error {
		calls++
		return runRegion(ctx, r, th)
	}
	*target.variables["x"] = Variable{Value: float64(100)}
	r.startHat(target, "flowHat", false)
	err := r.Run(context.Background(), 1, 60, false)
	region.run = runRegion
	if err != nil || calls != 1 || target.variables["score"].Value != float64(187.5) {
		t.Fatalf("connected loop: calls=%d score=%v err=%v", calls, target.variables["score"].Value, err)
	}
	for _, procedure := range []string{"flow", "flowWhile", "flowYield", "flowBreak", "flowEach"} {
		for _, count := range []any{float64(-2), float64(0), float64(3.5), float64(100), "invalid", math.NaN()} {
			fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
			slow.native = false
			for _, r := range []*Runtime{fast, slow} {
				*r.targets[1].variables["x"] = Variable{Value: count}
				r.startHat(r.targets[1], procedure+"Hat", false)
			}
			for tick := 0; tick < 14; tick++ {
				for _, r := range []*Runtime{fast, slow} {
					if err := r.Run(context.Background(), 1, 60, false); err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) || fast.Done() != slow.Done() {
					t.Fatalf("%s count=%v tick=%d: state or wait timing changed", procedure, count, tick)
				}
			}
		}
	}
}

func sameNativeFlowStack(a, b []continuation) bool {
	return slices.EqualFunc(a, b, func(a, b continuation) bool {
		// These fixtures have no suspended extension calls; execution links are caches.
		return a.extension == nil && b.extension == nil &&
			a.forEach == b.forEach && a.index == b.index &&
			a.header == b.header && a.exit == b.exit &&
			a.switchBlock == b.switchBlock && a.caseEnd == b.caseEnd &&
			a.call == b.call && reflect.DeepEqual(parameterValues(a.params), parameterValues(b.params)) &&
			a.warp == b.warp && a.procedure == b.procedure &&
			a.next == b.next && a.body == b.body &&
			a.remaining == b.remaining && a.loop == b.loop
	})
}

func testNativeFlowResume(t *testing.T) {
	for _, procedure := range []string{"flow", "conditional", "connectedCalls"} {
		for _, allowance := range []int{1, 2, 3, 4, 7, 11, 29, 64, 127} {
			fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
			slow.native = false
			var threads []*thread
			for _, r := range []*Runtime{fast, slow} {
				target := r.targets[1]
				if err := prepareProcedures(target); err != nil {
					t.Fatal(err)
				}
				if procedure == "conditional" {
					*target.variables["x"] = Variable{Value: float64(20)}
					*target.variables["y"] = Variable{Value: float64(0)}
					target.variables["waitDuration"].Value = .02
				}
				steps := stepsPerFrame - allowance
				th := &thread{target: target, pc: target.procedures[procedure].body, procedure: procedure, warp: true,
					regions: target.procedures[procedure].regions, sliceSteps: &steps,
					sliceLimit: stepsPerFrame, sliceStarted: time.Now().Add(-time.Second)}
				threads = append(threads, th)
				if err := r.stepBody(context.Background(), th); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) || threads[0].pc != threads[1].pc ||
				*threads[0].sliceSteps != *threads[1].sliceSteps || !sameNativeFlowStack(threads[0].stack, threads[1].stack) {
				t.Fatalf("allowance=%d: suspended state differs (pc=%q/%q steps=%d/%d)", allowance,
					threads[0].pc, threads[1].pc, *threads[0].sliceSteps, *threads[1].sliceSteps)
			}
			for tick := 0; tick < 8; tick++ {
				for i, r := range []*Runtime{fast, slow} {
					r.elapsed += 1.0 / 60
					if err := r.step(context.Background(), threads[i]); err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) || threads[0].done != threads[1].done {
					t.Fatalf("allowance=%d tick=%d: resuming repeated or skipped work", allowance, tick)
				}
			}
			if !threads[0].done {
				t.Fatal("resumed loop did not finish")
			}
		}
	}
}

func testNativeFlowCalculationResume(t *testing.T) {
	entries := []string{"conditionalCalculate"}
	for i := range 8 {
		entries = append(entries, fmt.Sprintf("conditionalAdjust%d", i))
	}
	for _, entry := range entries {
		for _, allowance := range []int{1, 4, 7, 32} {
			fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
			slow.native = false
			var threads []*thread
			for _, r := range []*Runtime{fast, slow} {
				target := r.targets[1]
				if err := prepareProcedures(target); err != nil {
					t.Fatal(err)
				}
				steps := stepsPerFrame - allowance
				th := &thread{
					target: target, pc: entry, procedure: "conditional", warp: true,
					regions:    target.procedures["conditional"].regions,
					sliceSteps: &steps, sliceLimit: stepsPerFrame,
					sliceStarted: time.Now().Add(-time.Second),
				}
				threads = append(threads, th)
				if err := r.stepBody(context.Background(), th); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) ||
				threads[0].pc != threads[1].pc || *threads[0].sliceSteps != *threads[1].sliceSteps {
				t.Fatalf("entry=%s allowance=%d: calculation resume changed state or block count", entry, allowance)
			}
			for i, r := range []*Runtime{fast, slow} {
				if err := r.step(context.Background(), threads[i]); err != nil {
					t.Fatal(err)
				}
			}
			if !threads[0].done || !threads[1].done || !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) {
				t.Fatalf("entry=%s allowance=%d: resumed calculation did not finish correctly", entry, allowance)
			}
		}
	}
}

func testNativeFlowCancellation(t *testing.T) {
	for _, procedure := range []string{"flowEmpty", "flow", "flowCycle", "connectedCalls"} {
		var wantPC string
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			target := r.targets[1]
			if err := prepareProcedures(target); err != nil {
				t.Fatal(err)
			}
			*target.variables["x"] = Variable{Value: math.Inf(1)}
			steps := 0
			th := &thread{target: target, pc: target.procedures[procedure].body, warp: true,
				regions: target.procedures[procedure].regions, sliceSteps: &steps, sliceLimit: stepsPerFrame}
			ctx, cancel := context.WithCancel(context.Background())
			err := r.stepBody(nativeCancelContext{ctx, cancel, &steps}, th)
			cancel()
			if !errors.Is(err, context.Canceled) || steps != 1001 {
				t.Fatalf("%s native=%v: cancellation err=%v steps=%d", procedure, compiled, err, steps)
			}
			if !compiled {
				wantPC = th.pc
			} else if th.pc != wantPC {
				t.Fatalf("%s: cancellation moved pc from %q to %q", procedure, wantPC, th.pc)
			}
		}
	}
}

func testNativeFlowStackLimit(t *testing.T) {
	for _, depth := range []int{1023, 1024} {
		var want string
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			target := r.targets[1]
			if err := prepareProcedures(target); err != nil {
				t.Fatal(err)
			}
			th := &thread{target: target, pc: "flowBody", warp: true,
				regions: target.procedures["flow"].regions, stack: make([]continuation, depth)}
			err := r.step(context.Background(), th)
			if err == nil {
				t.Fatalf("native=%v: missing stack limit error at depth %d", compiled, depth)
			}
			if !compiled {
				want = err.Error()
			} else if err.Error() != want {
				t.Fatalf("stack error %q, want %q", err, want)
			}
		}
	}
}

func testNativeFlowBranchCancellation(t *testing.T) {
	for _, offset := range []int{995, 996, 997, 998, 999, 1000} {
		var wantPC string
		var wantStack []continuation
		var wantState []TargetState
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			target := r.targets[1]
			if err := prepareProcedures(target); err != nil {
				t.Fatal(err)
			}
			steps := offset
			th := &thread{target: target, pc: "flowBranchBody", warp: true,
				regions: target.procedures["flowBranch"].regions, sliceSteps: &steps, sliceLimit: stepsPerFrame}
			ctx, cancel := context.WithCancel(context.Background())
			err := r.stepBody(nativeCancelContext{ctx, cancel, &steps}, th)
			cancel()
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if !compiled {
				wantPC, wantStack, wantState = th.pc, th.stack, r.Snapshot()
			} else if th.pc != wantPC || !sameNativeFlowStack(th.stack, wantStack) || !reflect.DeepEqual(r.Snapshot(), wantState) {
				t.Fatalf("offset=%d: branch cancellation changed PC, stack or variables (%q/%q)", offset, th.pc, wantPC)
			}
		}
	}
}

type nativeFlowStopGraphics struct {
	nativeDrawingRecorder
	r      *Runtime
	stamps int
}

func (g *nativeFlowStopGraphics) Pen(PenCommand) {
	g.stamps++
	g.r.Stop()
}

func testNativeFlowStop(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		r := New(nativeCodegenFixture(t))
		r.native = compiled
		graphics := &nativeFlowStopGraphics{r: r}
		r.SetGraphics(graphics)
		r.startHat(r.targets[1], "flowStopHat", false)
		if err := r.Run(context.Background(), 1, 60, false); err != nil {
			t.Fatal(err)
		}
		if graphics.stamps != 1 || number(r.targets[1].variables["score"].Value) != 0 {
			t.Fatalf("native=%v: executed after stop (stamps=%d)", compiled, graphics.stamps)
		}
	}
}

func BenchmarkNativeFlow(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	r := New(nativeCodegenFixture(b))
	target := r.targets[1]
	b.ReportAllocs()
	for b.Loop() {
		*target.variables["x"] = Variable{Value: float64(100)}
		*target.variables["score"] = Variable{Value: float64(0)}
		r.startHat(target, "flowHat", false)
		if err := r.Run(context.Background(), 8, 60, false); err != nil {
			b.Fatal(err)
		}
		if !r.Done() || target.variables["score"].Value != float64(187.5) {
			b.Fatal("loop did not complete all iterations")
		}
		list := target.state.Lists["items"]
		list.clear()
		target.state.Lists["items"] = list
	}
}

func BenchmarkNativeNestedRepeat(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	r := New(nativeCodegenFixture(b))
	target := r.targets[1]
	b.ReportAllocs()
	for b.Loop() {
		*target.variables["x"] = Variable{Value: float64(100)}
		*target.variables["y"] = Variable{Value: float64(100)}
		*target.variables["score"] = Variable{Value: float64(0)}
		r.startHat(target, "dynamicNestedHat", false)
		if err := r.Run(context.Background(), 1, 60, false); err != nil {
			b.Fatal(err)
		}
		if !r.Done() || target.variables["score"].Value != float64(10000) {
			b.Fatal("nested repeats did not finish")
		}
	}
}
