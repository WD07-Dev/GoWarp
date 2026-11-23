package vm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"gowarp/core/project"
)

func eventProject(t *testing.T, targets string) *Runtime {
	t.Helper()
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":`+targets+`}`), &p); err != nil {
		t.Fatal(err)
	}
	r := New(&p)
	installTestExtensions(t, r)
	return r
}

const broadcastProject = `[
 {"isStage":true,"name":"Stage","variables":{"score":["score",0]},"broadcasts":{"msg":"go"},"blocks":{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"send"},
 "send":{"opcode":"event_broadcastandwait","next":"after","inputs":{"BROADCAST_INPUT":[1,[11,"GO","msg"]]}},
 "after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"100"]]}}
 }},
 {"name":"Sprite","blocks":{
 "hat":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"before","fields":{"BROADCAST_OPTION":["go","msg"]}},
 "before":{"opcode":"data_changevariableby","next":"wait","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
 "wait":{"opcode":"control_wait","next":"after","inputs":{"DURATION":[1,[4,"0.1"]]}},
 "after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"10"]]}}
 }}]`

func TestBroadcastWaitAndPlainBroadcast(t *testing.T) {
	for _, turbo := range []bool{false, true} {
		r := eventProject(t, broadcastProject)
		run(t, r, 3, turbo)
		if got := number(r.Snapshot()[0].Variables["score"].Value); got != 1 {
			t.Fatalf("wait skipped or receivers delayed: %v", got)
		}
		run(t, r, 3, turbo)
		if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 111 {
			t.Fatal("sender did not resume")
		}
		r = eventProject(t, strings.Replace(broadcastProject, "event_broadcastandwait", "event_broadcast", 1))
		run(t, r, 1, turbo)
		if number(r.Snapshot()[0].Variables["score"].Value) != 101 {
			t.Fatal("plain broadcast waited")
		}
	}
}

func TestUnmatchedBroadcastDoesNotAllocatePerReceiver(t *testing.T) {
	r := eventProject(t, `[
		{"isStage":true},
		{"name":"Sprite","blocks":{
			"a":{"opcode":"event_whenbroadcastreceived","topLevel":true,"fields":{"BROADCAST_OPTION":["패턴 été one",null]}},
			"b":{"opcode":"event_whenbroadcastreceived","topLevel":true,"fields":{"BROADCAST_OPTION":["패턴 été two",null]}}
		}}
	]`)
	allocated := testing.AllocsPerRun(100, func() {
		if threads := r.broadcast("MISSING"); len(threads) != 0 {
			t.Fatal("unmatched broadcast started a receiver")
		}
	})
	if allocated != 0 {
		t.Fatalf("unmatched broadcast allocated %g objects per call", allocated)
	}
}

func TestRunningHatWithoutRestartDoesNotAllocate(t *testing.T) {
	r := eventProject(t, broadcastProject)
	sprite := r.targets[1]
	first := r.startHat(sprite, "hat", false)
	allocations := testing.AllocsPerRun(100, func() {
		if next := r.startHat(sprite, "hat", false); next != nil {
			t.Fatal("an already running hat started again")
		}
	})
	if allocations != 0 {
		t.Fatalf("ignored hat allocated %g objects", allocations)
	}
	if len(r.threads) != 2 || r.threads[1] != first {
		t.Fatal("ignored hat changed the scheduled thread")
	}
}

func TestHatRestartAfterCompactionAndCompletion(t *testing.T) {
	r := eventProject(t, broadcastProject)
	flag := r.startHat(r.stage, "flag", true)
	first := r.startHat(r.targets[1], "hat", false)
	flag.done = true
	r.compactThreads()
	next := r.startHat(r.targets[1], "hat", true)
	if !first.replaced || anyRunning([]*thread{first}) || len(r.threads) != 1 || r.threads[0] != next {
		t.Fatal("restart after compaction changed the slot or retained a waiter")
	}
	next.done = true
	last := r.startHat(r.targets[1], "hat", false)
	r.compactThreads()
	if len(r.threads) != 1 || r.threads[0] != last {
		t.Fatal("completed hat removed its replacement during compaction")
	}
	if r.startHat(r.targets[1], "hat", false) != nil {
		t.Fatal("compaction lost the active hat")
	}
	last.done = true
	r.compactThreads()
	if len(r.targets[1].hatThreads) != 0 {
		t.Fatal("finished hats retain their execution state")
	}
	if fresh := r.startHat(r.targets[1], "hat", false); fresh == nil || fresh == last {
		t.Fatal("finished hat could not be started again")
	}
}

func TestDeletedCloneReleasesHatsWithoutStoppingOriginal(t *testing.T) {
	r := eventProject(t, broadcastProject)
	original := r.startHat(r.targets[1], "hat", false)
	r.createClone(r.targets[1])
	clone := r.targets[len(r.targets)-1]
	first := r.startHat(clone, "hat", false)
	next := r.startHat(clone, "hat", true)
	r.deleteClone(clone)
	if !first.replaced || !next.done || original.done || original.replaced {
		t.Fatal("clone deletion did not isolate its running hats")
	}
	if len(clone.hatThreads) != 0 {
		t.Fatal("deleted clone retains execution state through its hat index")
	}
	r.compactThreads()
	if got := r.startHat(r.targets[1], "hat", false); got != nil {
		t.Fatal("clone deletion lost the original's running hat")
	}
}

func TestBroadcastMatchingKeepsDuplicateOrderAndClones(t *testing.T) {
	r := eventProject(t, `[
		{"isStage":true},
		{"name":"Sprite","blocks":{
			"a":{"opcode":"event_whenbroadcastreceived","topLevel":true,"fields":{"BROADCAST_OPTION":["été",null]}},
			"b":{"opcode":"event_whenbroadcastreceived","topLevel":true,"fields":{"BROADCAST_OPTION":["ÉTÉ",null]}},
			"c":{"opcode":"event_whenbroadcastreceived","topLevel":true,"fields":{"BROADCAST_OPTION":["other",null]}}
		}}
	]`)
	first := r.broadcast("Été")
	if len(first) != 2 || first[0].hat != "a" || first[1].hat != "b" {
		t.Fatal("case-insensitive receivers lost their order")
	}
	r.createClone(r.targets[1])
	next := r.broadcast("été")
	if len(next) != 4 {
		t.Fatalf("got %d receivers, want original and clone pairs", len(next))
	}
	for i, th := range next {
		if th.hat != []string{"a", "b", "a", "b"}[i] {
			t.Fatalf("receiver %d started %s", i, th.hat)
		}
	}
	if !first[0].replaced || !first[1].replaced || next[0].target != r.targets[1] || next[2].target != r.targets[2] {
		t.Fatal("broadcast restart or clone execution order changed")
	}
	r.deleteClone(r.targets[2])
	if got := len(r.broadcast("été")); got != 2 {
		t.Fatalf("deleted clone received broadcast: %d receivers", got)
	}
}

func TestBroadcastMenuDynamicAndUnknownNames(t *testing.T) {
	for _, tc := range []struct {
		replacement, extra string
		want               float64
	}{
		{
			`[1,"menu"]`,
			`,"menu":{"opcode":"event_broadcast_menu","fields":{"BROADCAST_OPTION":["go","msg"]}}`,
			111,
		},
		{
			`[2,"join"]`,
			`,"join":{"opcode":"operator_join","inputs":{"STRING1":[1,[10,"g"]],"STRING2":[1,[10,"o"]]}}`,
			111,
		},
		{`[1,[11,"unknown","msg"]]`, "", 100},
	} {
		data := strings.Replace(broadcastProject, `[1,[11,"GO","msg"]]`, tc.replacement, 1)
		if tc.extra != "" {
			data = strings.Replace(data, `"flag":`, strings.TrimPrefix(tc.extra, ",")+`,"flag":`, 1)
		}
		r := eventProject(t, data)
		run(t, r, 10, false)
		if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != tc.want {
			t.Fatalf("bad broadcast input %s", tc.replacement)
		}
	}
	// No receiver: broadcast-and-wait continues immediately, even with a known name.
	data := strings.Replace(broadcastProject, `"BROADCAST_OPTION":["go","msg"]`, `"BROADCAST_OPTION":["different","other"]`, 1)
	r := eventProject(t, data)
	run(t, r, 1, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 100 {
		t.Fatal("empty broadcast wait stalled")
	}
}

func TestBroadcastRestartsReceiver(t *testing.T) {
	data := strings.Replace(broadcastProject, `"event_broadcastandwait","next":"after"`, `"event_broadcast","next":"pause"`, 1)
	data = strings.Replace(data, `"after":{"opcode":`, `"pause":{"opcode":"control_wait","next":"sendAgain","inputs":{"DURATION":[1,[4,"0"]]}},"sendAgain":{"opcode":"event_broadcastandwait","next":"after","inputs":{"BROADCAST_INPUT":[1,[11,"go","msg"]]}},"after":{"opcode":`, 1)
	r := eventProject(t, data)
	run(t, r, 10, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 112 {
		t.Fatalf("receiver wasn't restarted: %+v", r.Snapshot())
	}
	if len(r.threads) != 0 {
		t.Fatal("completed threads retained")
	}
}

func TestBroadcastReceiverOrder(t *testing.T) {
	r := eventProject(t, `[
 {"isStage":true,"name":"Stage","lists":{"log":["log",[]]},"blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"send"},"send":{"opcode":"event_broadcast","inputs":{"BROADCAST_INPUT":[1,[11,"go","m"]]}}}},
 {"name":"A","blocks":{"hat":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"add","fields":{"BROADCAST_OPTION":["go","m"]}},"add":{"opcode":"data_addtolist","fields":{"LIST":["log","log"]},"inputs":{"ITEM":[1,[10,"A"]]}}}},
 {"name":"B","blocks":{"hat":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"add","fields":{"BROADCAST_OPTION":["go","m"]}},"add":{"opcode":"data_addtolist","fields":{"LIST":["log","log"]},"inputs":{"ITEM":[1,[10,"B"]]}}}}
 ]`)
	run(t, r, 1, false)
	items := r.Snapshot()[0].Lists["log"].Items
	if len(items) != 2 || items[0] != "B" || items[1] != "A" {
		t.Fatalf("wrong order: %v", items)
	}
}

func TestInitialLayerOrderKeepsCollisionCoordinatesTogether(t *testing.T) {
	/*
		Sprite-pane order puts Player before Level. Execution must start with
		the frontmost sprite, so Level sees the newly updated camera position.
	*/
	for _, event := range []string{"flag", "broadcast"} {
		t.Run(event, func(t *testing.T) {
			hat := `"opcode":"event_whenflagclicked"`
			if event == "broadcast" {
				hat = `"opcode":"event_whenbroadcastreceived","fields":{"BROADCAST_OPTION":["centre",null]}`
			}
			r := eventProject(t, `[
 {"isStage":true,"variables":{"camera":["camera",21]}},
 {"name":"Player","layerOrder":9,"blocks":{
   "hat":{`+hat+`,"topLevel":true,"next":"camera"},
   "camera":{"opcode":"data_setvariableto","fields":{"VARIABLE":["camera","camera"]},"inputs":{"VALUE":[1,[4,"-2"]]}}
 }},
 {"name":"Level","layerOrder":3,"blocks":{
   "hat":{`+hat+`,"topLevel":true,"next":"move"},
   "move":{"opcode":"motion_sety","inputs":{"Y":[2,"negativeCamera"]}},
   "negativeCamera":{"opcode":"operator_subtract","inputs":{"NUM1":[1,[4,"0"]],"NUM2":[1,[12,"camera","camera"]]}}
 }}]`)
			if event == "broadcast" {
				r.broadcast("centre")
			}
			run(t, r, 1, false)
			states := r.Snapshot()
			if states[2].Y != 2 {
				t.Fatalf("terrain y = %v, want 2 for camera y = -2", states[2].Y)
			}
			if states[1].Name != "Player" || states[2].Name != "Level" {
				t.Fatal("execution order changed the project target indices")
			}
		})
	}
}

func TestSelfBroadcastIsBoundedAndCancellable(t *testing.T) {
	data := strings.Replace(broadcastProject, `"before":{"opcode":"data_changevariableby","next":"wait"`, `"before":{"opcode":"data_changevariableby","next":"again"`, 1)
	data = strings.Replace(data, `"wait":{"opcode":`, `"again":{"opcode":"event_broadcast","next":"wait","inputs":{"BROADCAST_INPUT":[1,[11,"go","msg"]]}},"wait":{"opcode":`, 1)
	r := eventProject(t, data)
	run(t, r, 100, true)
	if r.Done() || len(r.threads) > 2 {
		t.Fatal("self broadcast leaked or stopped")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Run(ctx, 1, 30, true); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestNestedBroadcastWait(t *testing.T) {
	data := strings.Replace(broadcastProject, `"before":{"opcode":"data_changevariableby","next":"wait"`, `"before":{"opcode":"data_changevariableby","next":"nested"`, 1)
	data = strings.Replace(data, `"wait":{"opcode":`, `"nested":{"opcode":"event_broadcastandwait","next":"after","inputs":{"BROADCAST_INPUT":[1,[11,"inner","innerID"]]}},"innerHat":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"wait","fields":{"BROADCAST_OPTION":["inner","innerID"]}},"wait":{"opcode":`, 1)
	data = strings.Replace(data, `"broadcasts":{"msg":"go"}`, `"broadcasts":{"msg":"go","innerID":"inner"}`, 1)
	// Inner increments 10, outer increments another 10 after it, sender adds 100.
	r := eventProject(t, data)
	run(t, r, 3, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 1 {
		t.Fatal("nested wait skipped")
	}
	run(t, r, 10, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 121 {
		t.Fatal("nested wait lost continuation")
	}
}

func TestWaiterReleasedWhenReceiverReplaced(t *testing.T) {
	data := strings.Replace(broadcastProject, `"hat":`, `"restartFlag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"pause"},"pause":{"opcode":"control_wait","next":"restart","inputs":{"DURATION":[1,[4,"0"]]}},"restart":{"opcode":"event_broadcast","inputs":{"BROADCAST_INPUT":[1,[11,"go","msg"]]}},"hat":`, 1)
	r := eventProject(t, data)
	run(t, r, 3, false)
	// Original waiter observes its receiver was replaced; it does not join the new instance.
	if number(r.Snapshot()[0].Variables["score"].Value) != 102 {
		t.Fatal("waiter followed replacement receiver")
	}
	run(t, r, 10, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 112 || !r.Done() {
		t.Fatal("replacement failed to finish")
	}
}

func TestCyclicBroadcastChainAdvancesFrames(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"blocks":{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"sendA"},
 "sendA":{"opcode":"event_broadcast","inputs":{"BROADCAST_INPUT":[1,[11,"A","a"]]}},
 "a":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"sendB","fields":{"BROADCAST_OPTION":["A","a"]}},
 "sendB":{"opcode":"event_broadcast","inputs":{"BROADCAST_INPUT":[1,[11,"B","b"]]}},
 "b":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"sendA","fields":{"BROADCAST_OPTION":["B","b"]}}
 }}]`)
	run(t, r, 2, true)
	if r.Done() || r.Frames() != 2 || len(r.threads) > 2 {
		t.Fatal("cyclic events were not bounded")
	}
}

type framePen struct {
	Graphics
	stamps int
}

func (p *framePen) Pen(command PenCommand) {
	switch command.Kind {
	case "clear":
		p.stamps = 0
	case "stamp":
		p.stamps++
	}
}

func TestWarpDrawCompletesBeforeNextFrameClear(t *testing.T) {
	/*
		A pen game clears and redraws through broadcasts every frame. A larger
		attack must not suspend its warp drawing script at an instruction quota
		and then get restarted by the next frame's broadcast before stamping.
	*/
	blocks := `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
		"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"clear"]}},
		"clear":{"opcode":"pen_clear","next":"send"},
		"send":{"opcode":"event_broadcast","inputs":{"BROADCAST_INPUT":[1,[11,"draw","draw"]]}},
		"hat":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"call","fields":{"BROADCAST_OPTION":["draw","draw"]}},
		"call":{"opcode":"procedures_call","mutation":{"proccode":"draw attack","argumentids":"[]"}},
		"def":{"opcode":"procedures_definition","topLevel":true,"next":"work","inputs":{"custom_block":[1,"proto"]}},
		"proto":{"opcode":"procedures_prototype","mutation":{"proccode":"draw attack","argumentids":"[]","argumentnames":"[]","argumentdefaults":"[]","warp":"true"}},
		"work":{"opcode":"control_repeat","next":"stamp","inputs":{"TIMES":[1,[6,"6000"]],"SUBSTACK":[2,"count"]}},
		"count":{"opcode":"control_incr_counter"},
		"stamp":{"opcode":"pen_stamp"}
	}`
	for _, placement := range []string{"inside warp", "after warp", "after consecutive warp calls", "at quota boundary"} {
		t.Run(placement, func(t *testing.T) {
			data := blocks
			if placement != "inside warp" {
				data = strings.Replace(data, `"call":{"opcode":"procedures_call",`, `"call":{"opcode":"procedures_call","next":"stamp",`, 1)
				data = strings.Replace(data, `"work":{"opcode":"control_repeat","next":"stamp",`, `"work":{"opcode":"control_repeat",`, 1)
			}
			if placement == "after consecutive warp calls" {
				data = strings.Replace(data, `"call":{"opcode":"procedures_call","next":"stamp",`, `"call":{"opcode":"procedures_call","next":"second",`, 1)
				data = strings.Replace(data, `"stamp":{"opcode":"pen_stamp"}`, `"second":{"opcode":"procedures_call","next":"stamp","mutation":{"proccode":"draw attack","argumentids":"[]"}},"stamp":{"opcode":"pen_stamp"}`, 1)
				data = strings.Replace(data, `[6,"6000"]`, `[6,"3000"]`, 1)
			}
			iterations := 6000
			if placement == "at quota boundary" {
				/*
					Match the project's drawing receiver: if -> warp call -> stamp.
					The procedure return itself consumes the last quota step.
				*/
				data = strings.Replace(data, `"next":"call"`, `"next":"branch"`, 1)
				data = strings.Replace(data, `"stamp":{"opcode":"pen_stamp"}`, `"branch":{"opcode":"control_if","inputs":{"CONDITION":[1,[4,"1"]],"SUBSTACK":[2,"call"]}},"stamp":{"opcode":"pen_stamp"}`, 1)
				data = strings.Replace(data, `[6,"6000"]`, `[6,"4998"]`, 1)
				iterations = 4998
			}
			r := fixture(t, data)
			pen := &framePen{}
			r.graphics = pen
			for frame := 0; frame < 3; frame++ {
				run(t, r, 1, false)
				if pen.stamps != 1 {
					t.Fatalf("frame %d: got %d stamps after clear, want 1 complete attack", frame, pen.stamps)
				}
				if r.counter != float64((frame+1)*iterations) {
					t.Fatalf("frame %d: drawing work was restarted or repeated: %g", frame, r.counter)
				}
				if len(r.threads) != 1 {
					t.Fatal("completed drawing receiver was retained for the next frame")
				}
			}
		})
	}
}

/*
The first receiver deletes clones; the second stops other scripts before
fading. Sorting their IDs reverses the saved order and cancels deletion.
*/
func TestBroadcastCloneDeletionUsesSavedScriptOrder(t *testing.T) {
	r := eventProject(t, `[
 {"isStage":true,"name":"Stage"},
 {"name":"VFX","visible":true,"blocks":{
 "z-delete":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"delete","fields":{"BROADCAST_OPTION":["cleanup",null]}},
 "delete":{"opcode":"control_delete_this_clone","next":"stop-delete"},
 "stop-delete":{"opcode":"control_stop","next":"hide","fields":{"STOP_OPTION":["other scripts in sprite",null]}},
 "hide":{"opcode":"looks_hide"},
 "a-fade":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"stop-fade","fields":{"BROADCAST_OPTION":["cleanup",null]}},
 "stop-fade":{"opcode":"control_stop","next":"fade","fields":{"STOP_OPTION":["other scripts in sprite",null]}},
 "fade":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"50"]],"SUBSTACK":[2,"ghost"]}},
 "ghost":{"opcode":"looks_changeeffectby","fields":{"EFFECT":["GHOST",null]},"inputs":{"CHANGE":[1,[4,"2"]]}}
 }}]`)
	original := r.targets[1]
	r.createClone(original)
	clone := r.targets[len(r.targets)-1]
	for attempt := 0; attempt < 2; attempt++ {
		receivers := r.broadcast("cleanup")
		if len(receivers) != 4 {
			t.Fatalf("original and clone should each receive both hats: got %d", len(receivers))
		}
	}
	run(t, r, 1, false)
	if !clone.dead || r.cloneCount != 0 {
		t.Fatal("clone received cleanup but fading canceled its immediate deletion")
	}
	if original.state.Visible {
		t.Fatal("original did not finish its first cleanup receiver")
	}
}
