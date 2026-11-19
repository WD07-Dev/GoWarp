package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"gowarp/core/extension"
	"gowarp/core/project"
)

func addNativeScriptBlocks(t testing.TB, blocks project.Blocks) {
	t.Helper()
	var extra project.Blocks
	if err := json.Unmarshal([]byte(`{
		"scriptHat":{"opcode":"event_whenkeypressed","topLevel":true,"next":"scriptReset","fields":{"KEY_OPTION":["space",null]}},
		"scriptReset":{"opcode":"data_setvariableto","next":"scriptRepeat","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"0"]]}},
		"scriptRepeat":{"opcode":"control_repeat","next":"scriptZeroWait","inputs":{"TIMES":[1,[4,"3"]],"SUBSTACK":[2,"scriptIncrement"]}},
		"scriptIncrement":{"opcode":"data_changevariableby","next":"scriptIf","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
		"scriptIf":{"opcode":"control_if","inputs":{"CONDITION":[2,"scriptCondition"],"SUBSTACK":[2,"scriptAppend"]}},
		"scriptCondition":{"opcode":"operator_gt","inputs":{"OPERAND1":[1,[12,"score","score"]],"OPERAND2":[1,[4,"1"]]}},
		"scriptAppend":{"opcode":"data_addtolist","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[12,"score","score"]]}},
		"scriptZeroWait":{"opcode":"control_wait","next":"scriptCall","inputs":{"DURATION":[1,[4,"0"]]}},
		"scriptCall":{"opcode":"procedures_call","next":"scriptBroadcast","mutation":{"proccode":"script ordinary","argumentids":"[\"nID\"]"},"inputs":{"nID":[1,[10,"010"]]}},
		"scriptDefinition":{"opcode":"procedures_definition","topLevel":true,"next":"scriptNestedRepeat","inputs":{"custom_block":[1,"scriptPrototype"]}},
		"scriptPrototype":{"opcode":"procedures_prototype","mutation":{"proccode":"script ordinary","argumentids":"[\"nID\"]","argumentnames":"[\"n\"]","argumentdefaults":"[0]","warp":"false"}},
		"scriptNestedRepeat":{"opcode":"control_repeat","next":"scriptWarpCall","inputs":{"TIMES":[1,[4,"2"]],"SUBSTACK":[2,"scriptNestedIncrement"]}},
		"scriptNestedIncrement":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"arg"]}},
		"scriptWarpCall":{"opcode":"procedures_call","next":"scriptWait","mutation":{"proccode":"inner","argumentids":"[]"}},
		"scriptWait":{"opcode":"control_wait","next":"scriptAfterWait","inputs":{"DURATION":[1,[4,"0.05"]]}},
		"scriptAfterWait":{"opcode":"data_changevariableby","next":"scriptSaveArgument","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"scriptArgumentProduct"]}},
		"scriptArgumentProduct":{"opcode":"operator_multiply","inputs":{"NUM1":[2,"arg"],"NUM2":[1,[4,"10"]]}},
		"scriptSaveArgument":{"opcode":"data_addtolist","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[2,"arg"]}},
		"scriptBroadcast":{"opcode":"event_broadcastandwait","next":"scriptFinish","inputs":{"BROADCAST_INPUT":[1,[11,"script message","script message"]]}},
		"scriptFinish":{"opcode":"data_addtolist","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[10,"finished"]]}},
		"scriptReceiver":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"scriptReceiverWait","fields":{"BROADCAST_OPTION":["script message","script message"]}},
		"scriptReceiverWait":{"opcode":"control_wait","next":"scriptReceived","inputs":{"DURATION":[1,[4,"0"]]}},
		"scriptReceived":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1000"]]}},
		"scriptClone":{"opcode":"event_whenkeypressed","topLevel":true,"next":"scriptCloneSet","fields":{"KEY_OPTION":["clone",null]}},
		"scriptCloneSet":{"opcode":"data_setvariableto","next":"scriptCloneWait","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"7"]]}},
		"scriptCloneWait":{"opcode":"control_wait","next":"scriptCloneEnd","inputs":{"DURATION":[1,[4,"0"]]}},
		"scriptCloneEnd":{"opcode":"control_delete_this_clone"},
		"scriptFallbackHat":{"opcode":"event_whenkeypressed","topLevel":true,"next":"scriptFallbackSet","fields":{"KEY_OPTION":["b",null]}},
		"scriptFallbackSet":{"opcode":"data_setvariableto","next":"scriptFallbackCall","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"42"]]}},
		"scriptFallbackCall":{"opcode":"scriptTest_wait","next":"scriptFallbackAfter"},
		"scriptFallbackAfter":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
		"scriptUntilHat":{"opcode":"event_whenkeypressed","topLevel":true,"next":"scriptUntil","fields":{"KEY_OPTION":["c",null]}},
		"scriptUntil":{"opcode":"control_wait_until","next":"scriptIncrement","inputs":{"CONDITION":[2,"scriptCondition"]}},
		"scriptForeverHat":{"opcode":"event_whenkeypressed","topLevel":true,"next":"scriptForever","fields":{"KEY_OPTION":["a",null]}},
		"scriptForever":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"scriptForeverAdd"]}},
		"scriptForeverAdd":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
	}`), &extra); err != nil {
		t.Fatal(err)
	}
	for id, block := range extra {
		blocks[id] = block
	}
}

func testNativeScripts(t *testing.T) {
	for _, hat := range []string{"scriptHat", "scriptForeverHat"} {
		t.Run(hat, func(t *testing.T) {
			fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
			slow.native = false
			fast.startHat(fast.targets[1], hat, false)
			slow.startHat(slow.targets[1], hat, false)
			if len(fast.targets[1].hatThreads[hat].regions) == 0 {
				t.Fatal("event script has no compiled entry points")
			}
			for turn := range 40 {
				for _, r := range []*Runtime{fast, slow} {
					r.elapsed += 1.0 / 60
					for _, th := range r.threads {
						if !th.done {
							if err := r.step(context.Background(), th); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
				compareNativeScripts(t, fast, slow, turn)
			}
			if hat == "scriptHat" && !fast.Done() {
				t.Fatal("script did not complete its waits, calls and broadcast")
			}
			if hat == "scriptForeverHat" && number(fast.targets[1].variables["score"].Value) != 40 {
				t.Fatal("ordinary loop ran more than once per scheduling turn")
			}
		})
	}
}

func compareNativeScripts(t *testing.T, fast, slow *Runtime, turn int) {
	t.Helper()
	if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) || fast.Done() != slow.Done() || len(fast.threads) != len(slow.threads) {
		t.Fatalf("turn %d: script state differs", turn)
	}
	for i, a := range fast.threads {
		b := slow.threads[i]
		if a.pc != b.pc || a.done != b.done || a.warp != b.warp || a.wake != b.wake ||
			!sameNativeFlowStack(a.stack, b.stack) {
			t.Fatalf("turn %d thread %d: resume state differs (%q/%q)", turn, i, a.pc, b.pc)
		}
	}
}

func TestNativeScriptGreenFlagBinding(t *testing.T) {
	p := &project.Project{Targets: []*project.Target{{
		IsStage: true, Name: "Stage", Blocks: project.Blocks{
			"flag": {Opcode: "event_whenflagclicked", TopLevel: true, Next: "body"},
			"body": {Opcode: "control_wait", Inputs: literalInputs(map[string]any{"DURATION": "0"})},
		},
	}}}
	key, scripts := nativeProjectKey, nativeScripts
	defer func() { nativeProjectKey, nativeScripts = key, scripts }()
	nativeProjectKey = nativeKey(p)
	entry := &nativeRegion{resumable: true}
	nativeScripts = map[int]map[string]map[string]*nativeRegion{0: {"flag": {"body": entry}}}
	r := New(p)
	for restart := range 2 {
		if len(r.threads) != 1 || r.threads[0].regions["body"] != entry {
			t.Fatalf("restart %d: green flag did not bind compiled script", restart)
		}
		r.GreenFlag()
	}
	p.Targets[0].Blocks["body"].Inputs = literalInputs(map[string]any{"DURATION": "1"})
	other := New(p)
	if other.native || len(other.threads[0].regions) != 0 {
		t.Fatal("another project used this project's compiled script")
	}
}

func testNativeScriptTicks(t *testing.T) {
	for _, turbo := range []bool{false, true} {
		for _, fps := range []float64{30, 60} {
			fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
			slow.native = false
			for _, r := range []*Runtime{fast, slow} {
				r.startHat(r.targets[1], "scriptHat", false)
			}
			for tick := range 40 {
				for _, r := range []*Runtime{fast, slow} {
					if err := r.Tick(context.Background(), fps, turbo); err != nil {
						t.Fatal(err)
					}
				}
				compareNativeScripts(t, fast, slow, tick)
			}
			if !fast.Done() {
				t.Fatalf("fps=%v turbo=%v: script did not finish", fps, turbo)
			}
		}
	}
}

func testNativeScriptFallback(t *testing.T) {
	fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
	slow.native = false
	for _, r := range []*Runtime{fast, slow} {
		err := r.SetExtensions(extension.Bundle{func() extension.Extension {
			return extension.Extension{ID: "scriptTest", Blocks: []extension.Block{{
				Opcode: "scriptTest_wait", Kind: "command",
				Handler: func(c *extension.Context) (extension.Result, error) {
					v := r.targets[1].variables["score"]
					if c.Local["waiting"] == nil {
						if v.Value != "42" {
							return extension.Result{}, fmt.Errorf("compiled writes were not flushed: %#v", v.Value)
						}
						c.Local["waiting"] = true
						return extension.Result{Again: true, YieldTick: true}, nil
					}
					v.Value = float64(100)
					return extension.Result{}, nil
				},
			}}}
		}})
		if err != nil {
			t.Fatal(err)
		}
		r.startHat(r.targets[1], "scriptFallbackHat", false)
	}
	for tick := range 2 {
		for _, r := range []*Runtime{fast, slow} {
			if err := r.Tick(context.Background(), 60, false); err != nil {
				t.Fatal(err)
			}
		}
		compareNativeScripts(t, fast, slow, tick)
		if tick == 0 && fast.Done() {
			t.Fatal("compiled script skipped extension yield")
		}
	}
	if !fast.Done() || fast.targets[1].variables["score"].Value != float64(101) {
		t.Fatal("compiled code did not see the extension's write after resuming")
	}
	for _, r := range []*Runtime{fast, slow} {
		r.targets[1].variables["score"].Value = float64(0)
		r.startHat(r.targets[1], "scriptUntilHat", false)
	}
	for turn := range 4 {
		for _, r := range []*Runtime{fast, slow} {
			if turn == 2 {
				r.targets[1].variables["score"].Value = float64(2)
			}
			if err := r.step(context.Background(), r.targets[1].hatThreads["scriptUntilHat"]); err != nil {
				t.Fatal(err)
			}
		}
		compareNativeScripts(t, fast, slow, turn)
	}
	if fast.targets[1].variables["score"].Value != float64(3) {
		t.Fatal("wait-until did not reevaluate the condition")
	}
}

func testNativeScriptBudgets(t *testing.T) {
	for _, allowance := range []int{1, 2, 3, 4, 7, 11, 32} {
		fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
		slow.native = false
		for _, r := range []*Runtime{fast, slow} {
			th := r.startHat(r.targets[1], "scriptHat", false)
			steps := stepsPerFrame - allowance
			th.sliceSteps, th.sliceLimit = &steps, stepsPerFrame
			th.sliceStarted = time.Now().Add(-time.Second)
			if err := r.stepBody(context.Background(), th); err != nil {
				t.Fatal(err)
			}
		}
		compareNativeScripts(t, fast, slow, allowance)
		if *fast.threads[0].sliceSteps != *slow.threads[0].sliceSteps {
			t.Fatalf("allowance %d: instruction counts differ", allowance)
		}
	}
}

func testNativeScriptClones(t *testing.T) {
	fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
	slow.native = false
	for _, r := range []*Runtime{fast, slow} {
		r.createClone(r.targets[1])
		r.startHat(r.targets[2], "scriptClone", false)
		r.broadcast("script message")
	}
	if fast.targets[2].hatThreads["scriptClone"].regions["scriptCloneSet"] == nil {
		t.Fatal("clone did not inherit compiled code")
	}
	for turn := range 4 {
		for _, r := range []*Runtime{fast, slow} {
			r.elapsed += 1.0 / 60
			for _, th := range r.threads {
				if !th.done {
					if err := r.step(context.Background(), th); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		compareNativeScripts(t, fast, slow, turn)
	}
}

func BenchmarkNativeEventScript(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, compiled := range []bool{false, true} {
		b.Run(fmt.Sprintf("compiled=%v", compiled), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			r.native = compiled
			th := r.startHat(r.targets[1], "scriptHat", false)
			b.ReportAllocs()
			for b.Loop() {
				th.pc, th.done = "scriptReset", false
				th.stack = th.stack[:0]
				if err := r.step(context.Background(), th); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
