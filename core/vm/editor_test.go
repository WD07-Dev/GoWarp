package vm

import (
	"encoding/json"
	"testing"

	"gowarp/core/project"
)

func TestEditorBlockUpdatePreservesStateAndOtherTargets(t *testing.T) {
	r := eventProject(t, `[
	{"isStage":true,"variables":{"n":["n",7]},"blocks":{"wait":{"opcode":"control_wait","inputs":{"DURATION":[1,[4,"100"]]}}}},
	{"name":"Sprite","blocks":{"add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["n","n"]},"inputs":{"VALUE":[1,[4,"1"]]}}}}
	]`)
	r.Stop()
	r.createClone(r.targets[1])
	r.ToggleScript(0, "wait", false, nil)
	r.ToggleScript(1, "add", false, nil)
	run(t, r, 1, false)
	waiter := r.threads[0]
	var blocks project.Blocks
	if err := json.Unmarshal([]byte(`{"add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["n","n"]},"inputs":{"VALUE":[1,[4,"10"]]}},"new":{"opcode":"motion_setx","inputs":{"X":[1,[4,"42"]]}}}`), &blocks); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateEditorBlocks([]EditorBlocks{{Target: 1, Blocks: blocks}}); err != nil {
		t.Fatal(err)
	}
	if waiter.done || len(r.targets) != 3 || number(r.stage.variables["n"].Value) != 8 {
		t.Fatal("block edit reset unrelated scripts, variables or clones")
	}
	r.ToggleScript(1, "add", false, nil)
	run(t, r, 1, false)
	r.ToggleScript(1, "new", false, nil)
	run(t, r, 1, false)
	if number(r.stage.variables["n"].Value) != 18 || r.targets[1].state.X != 42 {
		t.Fatal("edited and added blocks did not run")
	}
	if err := r.UpdateEditorBlocks([]EditorBlocks{{Target: 1, Blocks: project.Blocks{"bad": {Opcode: "missing_extension"}}}}); err == nil {
		t.Fatal("accepted a block whose extension is not loaded")
	}
	if r.targets[1].source.Blocks["new"] == nil {
		t.Fatal("failed edit changed the live graph")
	}
}

func TestClickedStackKeepsStateAndTogglesOnlyItsThread(t *testing.T) {
	r := eventProject(t, `[
 {"isStage":true,"variables":{"n":["n",7]}},
 {"name":"Sprite","blocks":{
 "hat":{"opcode":"event_whenkeypressed","topLevel":true,"next":"change","fields":{"KEY_OPTION":["space",null]}},
 "change":{"opcode":"data_changevariableby","next":"wait","fields":{"VARIABLE":["n","n"]},"inputs":{"VALUE":[1,[4,"1"]]}},
 "wait":{"opcode":"control_wait","inputs":{"DURATION":[1,[4,"100"]]}},
 "loose":{"opcode":"data_changevariableby","topLevel":true,"fields":{"VARIABLE":["n","n"]},"inputs":{"VALUE":[1,[4,"10"]]}}
 }}]`)
	r.Stop()
	r.createClone(r.targets[1])
	if running, err := r.ToggleScript(1, "hat", false, nil); err != nil || !running {
		t.Fatalf("start: %t, %v", running, err)
	}
	run(t, r, 1, false)
	if number(r.stage.variables["n"].Value) != 8 || len(r.targets) != 3 {
		t.Fatal("click reset project state or ran on clones")
	}
	if running, err := r.ToggleScript(1, "hat", false, nil); err != nil || running {
		t.Fatalf("stop: %t, %v", running, err)
	}
	if _, err := r.ToggleScript(1, "loose", false, nil); err != nil {
		t.Fatal(err)
	}
	run(t, r, 1, false)
	if number(r.stage.variables["n"].Value) != 18 {
		t.Fatal("detached stack skipped its first block")
	}
}

func TestClickedReporterReturnsNativeValue(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"variables":{"n":["n",23]},"blocks":{
 "value":{"opcode":"data_variable","fields":{"VARIABLE":["n","n"]}}
 }}]`)
	r.Stop()
	var value string
	if _, err := r.ToggleScript(0, "value", true, func(v string, err error) {
		if err != nil {
			t.Error(err)
		}
		value = v
	}); err != nil {
		t.Fatal(err)
	}
	run(t, r, 1, false)
	if value != "23" {
		t.Fatalf("report = %q", value)
	}
	if _, err := r.ToggleScript(0, "missing", false, nil); err == nil {
		t.Fatal("missing block accepted")
	}
}
