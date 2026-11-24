package vm

import (
	"context"
	"encoding/json"
	"gowarp/core/extension"
	dt "gowarp/test/extensions/dtbyxeroname"
	"math"
	"testing"

	"gowarp/core/project"
)

func monitorRuntime(t *testing.T) *Runtime {
	t.Helper()
	var p project.Project
	err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage","variables":{"v":["score",1]},"lists":{"l":["items",["a","b"]]}},{"name":"Sprite","variables":{"v":["local",2]},"lists":{"l":["local items",["original"]]}}],"monitors":[{"id":"v","opcode":"data_variable","visible":true,"mode":"slider","sliderMin":-5,"sliderMax":5,"isDiscrete":false},{"id":"v","spriteName":"Sprite","opcode":"data_variable","visible":true},{"id":"l","spriteName":"Sprite","opcode":"data_listcontents","visible":true,"height":100}]}`), &p)
	if err != nil {
		t.Fatal(err)
	}
	return New(&p)
}

func TestMonitorEditsRespectScopeAndCloneLists(t *testing.T) {
	r := monitorRuntime(t)
	r.createClone(r.targets[1])
	if !r.SetMonitorValue(1, "hello") || text(r.targets[1].variables["v"].Value) != "hello" {
		t.Fatal("local variable edit was not applied")
	}
	if text(r.stage.variables["v"].Value) != "1" || text(r.targets[2].variables["v"].Value) != "2" {
		t.Fatal("monitor edit changed another target")
	}
	if !r.EditMonitorList(2, "replace", 0, "changed") || !r.EditMonitorList(2, "add", 0, "new") {
		t.Fatal("list edit rejected")
	}
	if got := r.targets[2].state.Lists["l"].Items; len(got) != 1 || got[0] != "original" {
		t.Fatalf("monitor edit changed clone list: %v", got)
	}
	if !r.EditMonitorList(2, "delete", 0, "") || r.Monitors()[2].Items[0] != "new" {
		t.Fatal("list delete failed")
	}
	if r.EditMonitorList(2, "replace", 99, "bad") || r.SetMonitorValue(2, "bad") || r.SetMonitorValue(-1, "bad") {
		t.Fatal("invalid monitor edit accepted")
	}
}

func TestMonitorSliderUsesSavedRangeAndPrecision(t *testing.T) {
	r := monitorRuntime(t)
	if !r.SetMonitorSlider(0, 1.234) || math.Abs(number(r.stage.variables["v"].Value)-1.23) > 1e-9 {
		t.Fatal("continuous slider must use hundredths")
	}
	r.SetMonitorSlider(0, 100)
	if number(r.stage.variables["v"].Value) != 5 {
		t.Fatal("slider exceeded saved range")
	}
	r.data.monitors[0].source.IsDiscrete = nil
	r.SetMonitorSlider(0, 1.8)
	if number(r.stage.variables["v"].Value) != 2 {
		t.Fatal("slider should default to whole numbers")
	}
}

func TestMonitorWindowDoesNotCopyWholeList(t *testing.T) {
	r := monitorRuntime(t)
	r.data.monitors[2].source.Height = 110
	list := r.targets[1].state.Lists["l"]
	list.Items = make([]any, 100000)
	for i := range list.Items {
		list.Items[i] = "entry"
	}
	list.Items[90000] = "visible"
	r.targets[1].state.Lists["l"] = list
	states := r.MonitorWindow(map[int]int{2: 90000}, 3)
	m := states[2]
	if m.Index != 2 || m.Count != 100000 || m.Offset != 90000 || len(m.Items) != 3 || m.Items[0] != "visible" {
		t.Fatalf("wrong list window: %+v", m)
	}
	m.Items[0] = "detached"
	if list.Items[90000] != "visible" {
		t.Fatal("display snapshot aliases the list")
	}
}

func TestShowVariableCreatesMissingMonitor(t *testing.T) {
	r := monitorRuntime(t)
	r.data.monitors = nil
	r.createClone(r.targets[1])
	block := &project.Block{Opcode: "data_showvariable", Fields: map[string][]any{"VARIABLE": {"local", "v"}}}
	for range 2 {
		if _, err := r.dataStatement(&thread{target: r.targets[2]}, block, dataArguments(nil)); err != nil {
			t.Fatal(err)
		}
	}
	states := r.VisibleMonitors()
	if len(states) != 1 || states[0].Value != "2" || states[0].Label != "Sprite: local" {
		t.Fatalf("show must create one original-target monitor: %+v", states)
	}
}

func TestMonitorModesAndListResize(t *testing.T) {
	r := monitorRuntime(t)
	for _, want := range []string{"large", "slider", "default"} {
		if !r.NextMonitorMode(1) || r.Monitors()[1].Mode != want {
			t.Fatalf("monitor mode = %q; want %q", r.Monitors()[1].Mode, want)
		}
	}
	if r.NextMonitorMode(2) || r.NextMonitorMode(-1) || r.ResizeMonitor(1, 200, 100) {
		t.Fatal("accepted an unsupported monitor operation")
	}
	if !r.ResizeMonitor(2, 240, 180) {
		t.Fatal("list resize rejected")
	}
	if got := r.Monitors()[2]; got.Width != 240 || got.Height != 180 {
		t.Fatalf("list size = %g×%g", got.Width, got.Height)
	}
	r.ResizeMonitor(2, 1, 1)
	if got := r.Monitors()[2]; got.Width != 100 || got.Height != 60 {
		t.Fatalf("list minimum size = %g×%g", got.Width, got.Height)
	}
	if r.ResizeMonitor(2, math.NaN(), 80) {
		t.Fatal("nonfinite list size accepted")
	}
}

func TestExtensionMonitorUpdatesOncePerTick(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage","variables":{"sample_value":["unrelated",42]}}],"monitors":[{"id":"sample_value","opcode":"sample_value","visible":true,"params":{"CHOICE":"clock"}},{"id":"hidden","opcode":"sample_value","visible":false}]}`), &p); err != nil {
		t.Fatal(err)
	}
	r := New(&p)
	calls, ticks := 0, 0
	if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "sample", Blocks: []extension.Block{{Opcode: "sample_value", Kind: "reporter", Text: "value [CHOICE]", Handler: func(c *extension.Context) (extension.Result, error) {
			calls++
			if c.Field("CHOICE") != "clock" {
				t.Fatalf("monitor fields = %v", c.Fields)
			}
			return extension.Result{Value: float64(ticks)}, nil
		}}}, Events: map[string]extension.Handler{"before-tick": func(*extension.Context) (extension.Result, error) {
			ticks++
			return extension.Result{}, nil
		}}}
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.advance(context.Background(), 1, 60, false, true); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		got := r.VisibleMonitors()
		if len(got) != 1 || got[0].Value != "1" || got[0].Label != "value clock" || !got[0].ReadOnly {
			t.Fatalf("monitors = %+v", got)
		}
	}
	if calls != 1 {
		t.Fatalf("monitor evaluated %d times", calls)
	}
	if r.SetMonitorValue(0, "bad") || r.SetMonitorSlider(0, 4) {
		t.Fatal("reporter monitor modified a variable")
	}
	if _, ok := r.MonitorValue(0, -1); ok {
		t.Fatal("reporter is editable")
	}
	for _, want := range []string{"large", "default"} {
		if !r.NextMonitorMode(0) || r.Monitors()[0].Mode != want {
			t.Fatalf("reporter mode = %v", r.Monitors())
		}
	}
	r.MoveMonitor(0, 20, 30)
	if r.Monitors()[0].X != 20 {
		t.Fatal("reporter did not move")
	}
	if err := r.advance(context.Background(), 1, 60, false, true); err != nil {
		t.Fatal(err)
	}
	if got := r.VisibleMonitors()[0].Value; got != "2" {
		t.Fatalf("next value = %s", got)
	}
}

func TestBeforeTickRunsBeforeScripts(t *testing.T) {
	r := fixture(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"value"]}},"value":{"opcode":"sample_value"}}`)
	count := 0
	if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "sample", Blocks: []extension.Block{{Opcode: "sample_value", Kind: "reporter", Handler: func(*extension.Context) (extension.Result, error) {
			return extension.Result{Value: float64(count)}, nil
		}}}, Events: map[string]extension.Handler{"before-tick": func(*extension.Context) (extension.Result, error) {
			count++
			return extension.Result{}, nil
		}}}
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background(), 1, 60, false); err != nil {
		t.Fatal(err)
	}
	if got := number(r.stage.variables["score"].Value); got != 1 {
		t.Fatalf("script saw %v; want current tick", got)
	}
}

func TestDeltaTimeMonitorMatchesScriptValue(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage","variables":{"score":["score",0]},"blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"value"]}},"value":{"opcode":"dtbyxeroname_dt"}}}],"extensions":["dtbyxeroname"],"monitors":[{"id":"dt","opcode":"dtbyxeroname_dt","visible":true},{"id":"fps","opcode":"dtbyxeroname_fps","visible":true}]}`), &p); err != nil {
		t.Fatal(err)
	}
	r := New(&p)
	if err := r.SetExtensions(extension.Bundle{dt.New}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := r.Tick(context.Background(), 60, false); err != nil {
			t.Fatal(err)
		}
		m := r.VisibleMonitors()
		if len(m) != 2 || m[0].Label != "ΔT" || m[0].Value != "0.016667" || m[1].Label != "fps" || m[1].Value != "60" {
			t.Fatalf("delta monitors = %+v", m)
		}
	}
	if got := number(r.stage.variables["score"].Value); got != 1.0/60 {
		t.Fatalf("first script delta = %g", got)
	}
}

func TestYieldingMonitorRetainsInvocation(t *testing.T) {
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true}}, Monitors: []project.Monitor{{ID: "m", Opcode: "sample_value", Visible: true}}})
	calls := 0
	handler := func(c *extension.Context) (extension.Result, error) {
		calls++
		if c.Local["pending"] == nil {
			c.Local["pending"] = true
			return extension.Result{Again: true, YieldTick: true}, nil
		}
		return extension.Result{Value: "ready"}, nil
	}
	factory := func() extension.Extension {
		return extension.Extension{ID: "sample", Blocks: []extension.Block{
			{Opcode: "sample_value", Kind: "reporter", Handler: handler},
		}}
	}
	if err := r.SetExtensions(extension.Bundle{factory}); err != nil {
		t.Fatal(err)
	}

	if err := r.Tick(context.Background(), 60, false); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || r.VisibleMonitors()[0].Value == "ready" {
		t.Fatal("yielding reporter completed early")
	}
	if err := r.Tick(context.Background(), 60, false); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || r.VisibleMonitors()[0].Value != "ready" {
		t.Fatal("monitor lost its continuation")
	}
}

func TestRestartCancelsPendingMonitor(t *testing.T) {
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true}}, Monitors: []project.Monitor{{ID: "m", Opcode: "sample_value", Visible: true}}})
	completed := 0
	factory := func() extension.Extension {
		return extension.Extension{ID: "sample", Blocks: []extension.Block{
			{Opcode: "sample_value", Kind: "reporter", Handler: func(c *extension.Context) (extension.Result, error) {
				if c.Local["pending"] != nil {
					completed++
					return extension.Result{Value: completed}, nil
				}
				c.Local["pending"] = true
				return extension.Result{Again: true, YieldTick: true}, nil
			}},
		}}
	}
	if err := r.SetExtensions(extension.Bundle{factory}); err != nil {
		t.Fatal(err)
	}
	if err := r.Tick(context.Background(), 60, false); err != nil {
		t.Fatal(err)
	}
	r.GreenFlag()
	if err := r.Tick(context.Background(), 60, false); err != nil {
		t.Fatal(err)
	}
	if completed != 0 {
		t.Fatal("monitor resumed the invocation from before restart")
	}
}
