package vm

import (
	"math"
	"reflect"
	"testing"
)

func hostResult(t *testing.T, c *extensionContext, op string, args ...any) any {
	t.Helper()
	v, err := c.Call(op, args...)
	if err != nil {
		t.Fatalf("%s(%v): %v", op, args, err)
	}
	return v
}

func TestSceneLayersFollowReorderingAndClones(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"A"},{"name":"B"}]`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	hostResult(t, c, "graphics.layer.create", "paper")
	hostResult(t, c, "graphics.layer.placement", "paper", "above")
	check := func() {
		t.Helper()
		scene := r.Scene()
		if scene[0].State.Name != "Stage" || scene[len(scene)-1].State.Name != "paper" {
			t.Fatalf("render layer order: %+v", scene)
		}
		for i, entry := range scene {
			if entry.State.LayerOrder != i {
				t.Fatal("layer indices are stale")
			}
		}
	}
	check()
	r.moveLayer(r.targets[1], math.Inf(1), false, false)
	check()
	hostResult(t, c, "target.clone")
	check()
	hostResult(t, c, "graphics.layer.placement", "paper", "below")
	for range 3 {
		scene := r.Scene()
		if scene[1].State.Name != "paper" {
			t.Fatal("placement change was not applied")
		}
	}
}

func BenchmarkSceneWithRenderLayers(b *testing.B) {
	r := &Runtime{}
	r.visual.layers = make(map[string]*renderLayer)
	for i := range 1000 {
		t := &target{state: TargetState{IsStage: i == 0, LayerOrder: i}}
		r.drawList = append(r.drawList, t)
	}
	layer := &target{state: TargetState{Name: "paper", RenderLayer: "paper"}}
	r.drawList = append(r.drawList, layer)
	r.visual.layers["paper"] = &renderLayer{target: layer, placement: "above"}
	r.updateLayers()
	scene := r.Scene()
	b.ReportAllocs()
	for b.Loop() {
		scene = r.SceneInto(scene)
	}
}
func TestLocalDataEnumerationAndAtomicSetXY(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage","variables":{"global":["global name",3]},"lists":{"global-list":["global list",[1]]}},{"name":"A","x":2,"y":4,"variables":{"local":["local name",1]},"lists":{"local-list":["local list",[2]]}}]`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	if got := hostResult(t, c, "variable.list"); !reflect.DeepEqual(got, []any{map[string]any{"id": "local", "name": "local name"}}) {
		t.Fatalf("local variables include inherited stage data: %v", got)
	}
	if got := hostResult(t, c, "list.list"); !reflect.DeepEqual(got, []any{map[string]any{"id": "local-list", "name": "local list"}}) {
		t.Fatalf("local lists include inherited stage data: %v", got)
	}
	if got := hostResult(t, c, "variable.list", "_stage_"); !reflect.DeepEqual(got, []any{map[string]any{"id": "global", "name": "global name"}}) {
		t.Fatal(got)
	}
	id := hostResult(t, c, "target.clone").(string)
	hostResult(t, c, "variable.set", "local", 99.0, id)
	if got := hostResult(t, c, "variable.get", "local"); got != 1.0 {
		t.Fatalf("clone data shared: %v", got)
	}
	if got := hostResult(t, c, "list.list", id); !reflect.DeepEqual(got, []any{map[string]any{"id": "local-list", "name": "local list"}}) {
		t.Fatal(got)
	}
	recorder := &surfaceRecorder{}
	r.graphics = recorder
	r.options.Fencing = false
	r.targets[1].state.Pen.Down = true
	hostResult(t, c, "target.setXY", 8.0, 12.0)
	if len(recorder.commands) != 1 || recorder.commands[0].Line != [4]float64{2, 4, 8, 12} {
		t.Fatalf("setXY emitted intermediate pen segment: %+v", recorder.commands)
	}
}
func TestRenderLayersStayOutOfExecutableTargets(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"A"},{"name":"B"}]`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	for _, name := range []string{StageSurface, "paper"} {
		hostResult(t, c, "graphics.layer.create", name)
	}
	if len(r.targets) != 3 || len(r.executable) != 3 || len(r.drawList) != 5 {
		t.Fatalf("layers became executable targets: %d %d %d", len(r.targets), len(r.executable), len(r.drawList))
	}
	before := hostResult(t, c, "target.list").([]any)
	if len(before) != 3 {
		t.Fatal(before)
	}
	id := hostResult(t, c, "target.clone").(string)
	if len(r.targets) != 4 || len(r.executable) != 4 || len(r.drawList) != 6 {
		t.Fatal("clone list invariants")
	}
	for i, target := range r.drawList {
		if target.state.LayerOrder != i {
			t.Fatalf("layer %d has index %d", i, target.state.LayerOrder)
		}
	}
	hostResult(t, c, "target.delete", id)
	hostResult(t, c, "graphics.layer.delete", "paper")
	for i, target := range r.drawList {
		if target.state.LayerOrder != i {
			t.Fatalf("layer after deletion %d has index %d", i, target.state.LayerOrder)
		}
	}
	if len(r.targets) != 3 || len(r.executable) != 3 || len(r.drawList) != 4 {
		t.Fatal("deletion damaged target lists")
	}
	hostResult(t, c, "graphics.layer.delete", StageSurface)
	if hostResult(t, c, "graphics.layer.exists", StageSurface) != true {
		t.Fatal("default layer removed")
	}
}
func TestLayerPenRoutingAndClearIsolation(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"A"},{"name":"B"}]`)
	recorder := &surfaceRecorder{}
	r.graphics = recorder
	r.options.Fencing = false
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	for _, name := range []string{StageSurface, "one", "two"} {
		hostResult(t, c, "graphics.layer.create", name)
	}
	recorder.commands = nil
	hostResult(t, c, "graphics.layer.bind", "one")
	hostResult(t, c, "graphics.layer.select", "two")
	r.penLine(r.targets[1], 0, 0, 1, 1)
	r.penLine(r.targets[2], 0, 0, 2, 2)
	hostResult(t, c, "graphics.layer.clear", "two")
	hostResult(t, c, "graphics.layer.clear", StageSurface)
	if len(recorder.commands) != 4 {
		t.Fatal(recorder.commands)
	}
	if got := []string{recorder.commands[0].Layer, recorder.commands[1].Layer, recorder.commands[2].Layer, recorder.commands[3].Layer}; !reflect.DeepEqual(got, []string{"one", "one", "two", ""}) {
		t.Fatalf("routing/clear isolation: %v", got)
	}
	if recorder.commands[2].Kind != "clear" || recorder.commands[3].Kind != "clear" {
		t.Fatal("layer clear command")
	}
	hostResult(t, c, "graphics.layer.delete", "one")
	r.penLine(r.targets[1], 0, 0, 3, 3)
	if recorder.commands[len(recorder.commands)-1].Layer != "" {
		t.Fatal("deleted binding swallowed drawing")
	}
}
func TestCameraBindingsKeepLogicalPositionsAndCloneState(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"A","x":40,"y":30}]`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	hostResult(t, c, "camera.transform", "world", 10.0, 20.0, 2.0, 90.0)
	hostResult(t, c, "camera.bind", "world")
	id := hostResult(t, c, "target.clone").(string)
	if hostResult(t, c, "camera.binding", id) != "world" {
		t.Fatal("clone lost camera")
	}
	if hostResult(t, c, "target.get", "x", id) != 40.0 || hostResult(t, c, "target.get", "y", id) != 30.0 {
		t.Fatal("camera changed logical coordinates")
	}
	for _, d := range r.Scene() {
		if d.State.Name == "A" {
			if d.State.Transform == nil || *d.State.Transform != (RenderTransform{10, 20, 2, 90}) {
				t.Fatalf("missing render transform: %+v", d)
			}
		}
	}
	snapshot := r.Scene()
	hostResult(t, c, "camera.transform", "world", 100.0, 200.0, 1.0, 0.0)
	for _, d := range snapshot {
		if d.State.Name == "A" && d.State.Transform.X != 10 {
			t.Fatal("mutable transform leaked into snapshot")
		}
	}
	hostResult(t, c, "graphics.layer.create", StageSurface)
	hostResult(t, c, "camera.bind", "world", StageSurface)
	if hostResult(t, c, "camera.binding", StageSurface) != "world" {
		t.Fatal("layer camera binding reporter")
	}
	if _, err := c.Call("camera.transform", "world", math.Inf(1), 0, 1, 0); err == nil {
		t.Fatal("nonfinite transform accepted")
	}
}
func TestAboveLayersRemainAboveSpritesAfterLayerMovement(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"A"}]`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	for _, name := range []string{"first", "second"} {
		hostResult(t, c, "graphics.layer.create", name)
		hostResult(t, c, "graphics.layer.placement", name, "above")
	}
	r.moveLayer(r.targets[1], math.Inf(1), false, true)
	scene := r.Scene()
	seenLayer := false
	for _, d := range scene {
		if d.State.RenderLayer != "" {
			seenLayer = true
		} else if seenLayer {
			t.Fatalf("sprite above a locked top layer: %+v", scene)
		}
	}
	if len(r.executable) != 2 {
		t.Fatal("scene sorting changed execution order")
	}
}

func TestRenderLayerRecreationHasUniqueDrawableIdentity(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"A"}]`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	hostResult(t, c, "graphics.layer.create", "one")
	hostResult(t, c, "graphics.layer.create", "two")
	old := r.visual.layers["one"].target.sourceIndex
	survivor := r.visual.layers["two"].target.sourceIndex
	hostResult(t, c, "graphics.layer.delete", "one")
	hostResult(t, c, "graphics.layer.create", "three")
	replacement := r.visual.layers["three"].target.sourceIndex
	if replacement == old || replacement == survivor {
		t.Fatalf("reused drawable source identity: %d, %d, %d", old, survivor, replacement)
	}
	if hostResult(t, c, "graphics.layer.exists", "missing") != false {
		t.Fatal("missing layer exists")
	}
	hostResult(t, c, "graphics.layer.clear", "missing")
	hostResult(t, c, "graphics.layer.delete", "missing")
}
