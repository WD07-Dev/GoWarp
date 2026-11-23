package vm

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gowarp/core/project"
)

const executionFixture = `{
 "start":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"100"]],"SUBSTACK":[2,"call"]}},
 "call":{"opcode":"procedures_call","next":"count","mutation":{"proccode":"work %s","argumentids":"[\"n\"]"},"inputs":{"n":[1,[4,"3"]]}},
 "count":{"opcode":"data_changevariableby","fields":{"VARIABLE":["calls","calls"]},"inputs":{"VALUE":[1,[4,"1"]]}},
 "def":{"opcode":"procedures_definition","topLevel":true,"next":"repeat","inputs":{"custom_block":[1,"proto"]}},
 "proto":{"opcode":"procedures_prototype","mutation":{"proccode":"work %s","argumentids":"[\"n\"]","argumentnames":"[\"n\"]","argumentdefaults":"[0]","warp":"true"}},
 "repeat":{"opcode":"control_repeat","inputs":{"TIMES":[2,"arg"],"SUBSTACK":[2,"if"]}},
 "arg":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["n",null]}},
 "if":{"opcode":"control_if_else","inputs":{"CONDITION":[2,"compare"],"SUBSTACK":[2,"add"],"SUBSTACK2":[2,"subtract"]}},
 "compare":{"opcode":"operator_lt","inputs":{"OPERAND1":[1,[12,"score","score"]],"OPERAND2":[1,[4,"100"]]}},
 "add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"arg"]}},
 "subtract":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"-1"]]}}
}`

func executionRuntime(tb testing.TB) *Runtime {
	tb.Helper()
	var blocks project.Blocks
	if err := json.Unmarshal([]byte(executionFixture), &blocks); err != nil {
		tb.Fatal(err)
	}
	return New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks,
		Variables: map[string][]any{"score": {"score", float64(0)}, "calls": {"calls", float64(0)}},
	}}})
}

func TestExecutionWholeScript(t *testing.T) {
	r := executionRuntime(t)
	th := &thread{target: r.stage, pc: "start", warp: true}
	if err := r.step(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	if !th.done || number(r.stage.variables["score"].Value) != 100 || number(r.stage.variables["calls"].Value) != 100 {
		t.Fatalf("incomplete or wrong script: done=%v state=%v", th.done, r.stage.variables)
	}
	if th.sliceExecuted != 1702 {
		t.Fatalf("statement count changed: %d", th.sliceExecuted)
	}
}

func BenchmarkExecutionWholeScript(b *testing.B) {
	r := executionRuntime(b)
	th := &thread{target: r.stage, warp: true}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		*r.stage.variables["score"] = Variable{Value: float64(0)}
		r.stage.variables["calls"] = &Variable{Value: float64(0)}
		th.pc, th.done = "start", false
		if err := r.step(context.Background(), th); err != nil {
			b.Fatal(err)
		}
		if !th.done {
			b.Fatal("script did not finish")
		}
	}
}

func TestExecutionLinksShareOnlySource(t *testing.T) {
	r := executionRuntime(t)
	r.stage.state.IsStage = false
	r.createClone(r.stage)
	clone := r.targets[len(r.targets)-1]
	originalCode := r.executionCode(r.stage.source)
	if originalCode != r.executionCode(clone.source) {
		t.Fatal("clone duplicated executable links")
	}
	entry := originalCode.block("call")
	if entry.nextBlock() != originalCode.block("count") {
		t.Fatal("next statement did not reuse its link")
	}
	for _, target := range []*target{r.stage, clone} {
		th := &thread{target: target, pc: "start", warp: true}
		if err := r.step(context.Background(), th); err != nil {
			t.Fatal(err)
		}
		if number(target.variables["score"].Value) != 100 {
			t.Fatal("execution links retained another clone's data")
		}
	}
}

func TestExecutionUntakenBranchDoesNotFail(t *testing.T) {
	r := executionRuntime(t)
	b := r.stage.source.Blocks["if"]
	b.Inputs["SUBSTACK2"] = []any{float64(2), 123}
	th := &thread{target: r.stage, pc: "if", params: procedureParameters{"n": {value: float64(3)}}}
	if err := r.step(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	if number(r.stage.variables["score"].Value) != 3 {
		t.Fatal("true branch did not execute")
	}
	*r.stage.variables["score"] = Variable{Value: float64(100)}
	th.pc, th.done = "if", false
	if err := r.step(context.Background(), th); err == nil || err.Error() != "invalid SUBSTACK2 block reference" {
		t.Fatalf("bad selected branch: %v", err)
	}
}

func TestExecutionWaitResumesCallsAndLoops(t *testing.T) {
	r := executionRuntime(t)
	blocks := r.stage.source.Blocks
	blocks["call"].Next = "wait"
	blocks["wait"] = &project.Block{Opcode: "control_wait", Next: "count", Inputs: map[string][]any{"DURATION": {float64(1), []any{float64(4), "0.001"}}}}
	th := &thread{target: r.stage, pc: "start", warp: true}
	score := float64(0)
	for iteration := 0; iteration < 100; iteration++ {
		for i := 0; i < 3; i++ {
			if score < 100 {
				score += 3
			} else {
				score--
			}
		}
		if err := r.step(context.Background(), th); err != nil {
			t.Fatal(err)
		}
		if th.pc != "count" || th.done || !r.wakePending(th) {
			t.Fatalf("wait skipped at iteration %d: pc=%s done=%v", iteration, th.pc, th.done)
		}
		if number(r.stage.variables["score"].Value) != score || number(r.stage.variables["calls"].Value) != float64(iteration) {
			t.Fatalf("wrong state before wait: %v", r.stage.variables)
		}
		r.frame++
		r.advanceTime()
	}
	if err := r.step(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	if !th.done || number(r.stage.variables["calls"].Value) != 100 {
		t.Fatal("did not return from final wait")
	}
}

func TestExecutionDirectPCChange(t *testing.T) {
	r := executionRuntime(t)
	th := &thread{target: r.stage, pc: "start", warp: true}
	if err := r.step(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	th.pc, th.done = "count", false
	if err := r.step(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	if number(r.stage.variables["calls"].Value) != 101 {
		t.Fatal("external PC change reused stale instruction")
	}
	th.pc, th.done = "missing", false
	if err := r.step(context.Background(), th); err == nil || err.Error() != `missing block "missing"` {
		t.Fatalf("missing block: %v", err)
	}
}

func TestExecutionPCChangeWhileWaiting(t *testing.T) {
	r := executionRuntime(t)
	r.stage.source.Blocks["wait"] = &project.Block{Opcode: "control_wait", Next: "count", Inputs: map[string][]any{"DURATION": {float64(1), []any{float64(4), "0"}}}}
	th := &thread{target: r.stage, pc: "wait"}
	if err := r.step(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	if th.cursor == nil || th.cursor.id != "count" {
		t.Fatal("test did not retain a pending link")
	}
	th.pc = "subtract"
	r.frame++
	if err := r.step(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	if number(r.stage.variables["score"].Value) != -1 || number(r.stage.variables["calls"].Value) != 0 {
		t.Fatal("changed PC followed old pending link")
	}
}

func TestExecutionQuotaKeepsProcedureEntry(t *testing.T) {
	r := executionRuntime(t)
	steps := stepsPerFrame - 2
	th := &thread{target: r.stage, pc: "start", sliceSteps: &steps, sliceLimit: stepsPerFrame, sliceStarted: time.Now().Add(-time.Second)}
	if err := r.stepBody(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	if steps != stepsPerFrame || th.pc != "repeat" || number(r.stage.variables["score"].Value) != 0 {
		t.Fatalf("quota crossed: steps=%d pc=%s", steps, th.pc)
	}
	for !th.done {
		if err := r.step(context.Background(), th); err != nil {
			t.Fatal(err)
		}
	}
	if number(r.stage.variables["score"].Value) != 100 || number(r.stage.variables["calls"].Value) != 100 {
		t.Fatal("resuming quota changed state")
	}
}

func TestExecutionPreparedWritesReadCurrentCloneState(t *testing.T) {
	var blocks project.Blocks
	if err := json.Unmarshal([]byte(`{
		"set":{"opcode":"data_setvariableto","next":"move","fields":{"VARIABLE":["value","value"]},"inputs":{"VALUE":[1,[12,"amount","amount"]]}},
		"move":{"opcode":"motion_gotoxy","next":"write","inputs":{"X":[1,[12,"value","value"]],"Y":[1,[4,"0"]]}},
		"write":{"opcode":"data_replaceitemoflist","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[4,"1"]],"ITEM":[1,[12,"value","value"]]}}
	}`), &blocks); err != nil {
		t.Fatal(err)
	}
	r := New(&project.Project{Targets: []*project.Target{
		{IsStage: true},
		{Name: "Sprite", Blocks: blocks,
			Variables: map[string][]any{"amount": {"amount", 0.0}, "value": {"value", 0.0}},
			Lists:     map[string][]any{"items": {"items", []any{0.0}}},
		},
	}})
	original := r.targets[1]
	r.createClone(original)
	clone := r.targets[len(r.targets)-1]
	for _, test := range []struct {
		target *target
		value  float64
	}{{original, 10}, {clone, 20}, {original, 30}, {clone, 40}} {
		test.target.variables["amount"] = &Variable{Value: test.value}
		th := &thread{target: test.target, pc: "set"}
		if err := r.step(context.Background(), th); err != nil {
			t.Fatal(err)
		}
		if !th.done || test.target.state.X != test.value ||
			number(test.target.variables["value"].Value) != test.value ||
			number(test.target.state.Lists["items"].Items[0]) != test.value {
			t.Fatalf("prepared writes retained old values for clone %d", test.target.state.CloneID)
		}
	}
	delete(clone.variables, "amount")
	if err := r.step(context.Background(), &thread{target: clone, pc: "set"}); err == nil {
		t.Fatal("prepared write retained a deleted variable")
	}
}
