package vm

import (
	"testing"

	"gowarp/core/project"
)

type extensionTouchGraphics struct {
	Graphics
	object  string
	x, y    float64
	target  string
	inspect func([]DisplayTarget)
}

func (g *extensionTouchGraphics) Touching(scene []DisplayTarget, target DisplayTarget, object string, x, y float64) bool {
	g.object, g.x, g.y, g.target = object, x, y, target.State.Name
	if g.inspect != nil {
		g.inspect(scene)
	}
	return true
}
func TestExtensionTargetTouching(t *testing.T) {
	r := fixture(t, `{}`)
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	if _, err := c.Call("target.touching", "_mouse_"); err == nil {
		t.Fatal("missing geometry must be reported")
	}
	g := &extensionTouchGraphics{}
	r.graphics = g
	r.mouseX, r.mouseY = 15, -21
	v, err := c.Call("target.touching", "_mouse_")
	if err != nil || v != true || g.object != "_mouse_" || g.x != 15 || g.y != -21 || g.target != r.stage.state.Name {
		t.Fatalf("%v %v %+v", v, err, g)
	}
}

func TestExtensionTouchingTargetUsesExactCandidate(t *testing.T) {
	r := eventProject(t, `[{"isStage":true},{"name":"Probe","visible":true},{"name":"Obstacle","visible":true}]`)
	probe, original := r.targets[1], r.targets[2]
	r.createClone(original)
	clone := r.targets[len(r.targets)-1]
	c := &extensionContext{r: r, th: &thread{target: probe}}
	if _, err := c.Call("target.touchingTarget", logicTargetID(clone)); err == nil {
		t.Fatal("missing costume geometry must be reported")
	}
	calls := 0
	r.graphics = &extensionTouchGraphics{inspect: func(scene []DisplayTarget) {
		calls++
		if len(scene) != 1 || scene[0].Source != clone.sourceIndex || scene[0].State.CloneID != clone.state.CloneID {
			t.Fatalf("query included unrelated targets: %+v", scene)
		}
		if scene[0].State.Variables != nil || scene[0].State.Lists != nil {
			t.Fatal("query leaked VM data")
		}
	}}
	if value, err := c.Call("target.touchingTarget", logicTargetID(clone)); err != nil || value != true {
		t.Fatal(value, err)
	}
	r.graphics = &extensionTouchGraphics{}
	if _, err := c.Call("target.touching", "Obstacle"); err != nil {
		t.Fatal(err)
	}
	r.graphics = &extensionTouchGraphics{inspect: func(scene []DisplayTarget) {
		calls++
		if len(scene) != 1 || scene[0].State.CloneID != clone.state.CloneID {
			t.Fatal("name query polluted exact-target query", scene)
		}
	}}
	if _, err := c.Call("target.touchingTarget", logicTargetID(clone)); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{logicTargetID(probe), "_stage_"} {
		if value, err := c.Call("target.touchingTarget", ref); err != nil || value != false {
			t.Fatal(ref, value, err)
		}
	}
	clone.state.Visible = false
	if value, err := c.Call("target.touchingTarget", logicTargetID(clone)); err != nil || value != false {
		t.Fatal(value, err)
	}
	if calls != 2 {
		t.Fatal("invalid candidates reached geometry", calls)
	}
	if _, err := c.Call("target.touchingTarget", "missing"); err == nil {
		t.Fatal("unknown target accepted")
	}
}

func TestExtensionFullscreenCommandUpdatesReportedState(t *testing.T) {
	r := fixture(t, `{}`)
	c := &extensionContext{r: r}
	for _, enabled := range []bool{true, false} {
		value := "false"
		if enabled {
			value = "true"
		}
		if _, err := c.Call("host.command", "fullscreen", value); err != nil {
			t.Fatalf("fullscreen command: %v", err)
		}
		got, err := c.Call("host.projectFullscreen")
		if err != nil || got != enabled {
			t.Fatalf("fullscreen state=%v %v", got, err)
		}
		command := r.host.commands[len(r.host.commands)-1]
		if command.Kind != "fullscreen" || command.Value != value {
			t.Fatalf("queued command=%+v", command)
		}
	}
}

func TestObjectSensingReusesQueryStorage(t *testing.T) {
	r := eventProject(t, `[{"isStage":true},{"name":"Probe","visible":true},{"name":"Obstacle","visible":true}]`)
	r.graphics = &extensionTouchGraphics{}
	th := &thread{target: r.targets[1]}
	block := &project.Block{Opcode: "sensing_touchingobject"}
	for _, name := range []string{"Obstacle", "_edge_", "_mouse_"} {
		t.Run(name, func(t *testing.T) {
			value := any(name)
			input := func(string) (any, error) { return value, nil }
			allocations := testing.AllocsPerRun(100, func() {
				_, handled, err := r.graphicsReporter(th, block, input)
				if err != nil || !handled {
					t.Fatalf("query: handled=%v err=%v", handled, err)
				}
			})
			if allocations != 0 {
				t.Fatalf("query allocates a scene each time: %g allocations", allocations)
			}
		})
	}
}

func TestSensingCandidatesFollowCloneChanges(t *testing.T) {
	r := eventProject(t, `[{"isStage":true},{"name":"Probe","visible":true},{"name":"Obstacle","visible":true},{"name":"Other","visible":true}]`)
	probe, source := r.targets[1], r.targets[2]
	want := []int{0}
	wantX := 0.0
	r.graphics = &extensionTouchGraphics{inspect: func(scene []DisplayTarget) {
		if len(scene) != len(want) {
			t.Fatalf("candidate count=%d, want %d", len(scene), len(want))
		}
		for i, candidate := range scene {
			if candidate.State.Name != "Obstacle" || candidate.State.CloneID != want[i] || candidate.State.X != wantX {
				t.Fatalf("stale candidate: %+v", candidate)
			}
			if candidate.State.Variables != nil || candidate.State.Lists != nil {
				t.Fatal("collision query leaked VM data")
			}
		}
	}}
	query := func() {
		t.Helper()
		c := &extensionContext{r: r, th: &thread{target: probe}}
		if _, err := c.Call("target.touching", "Obstacle"); err != nil {
			t.Fatalf("extension touching: %v", err)
		}
		for _, entry := range r.sensingScene[:cap(r.sensingScene)] {
			if entry.State.Name != "" || entry.State.Text != nil {
				t.Fatal("query retained target references")
			}
		}
	}
	query()
	r.createClone(source)
	clone := r.targets[len(r.targets)-1]
	want = []int{clone.state.CloneID, 0}
	query()
	source.state.Visible = false
	clone.state.X = 42
	clone.state.Effects.Ghost = 100
	want, wantX = []int{clone.state.CloneID}, 42
	query()
	r.deleteClone(clone)
	want = nil
	query()
}

func TestExtensionTouchingTargetsBatchesCandidates(t *testing.T) {
	r := eventProject(t, `[{"isStage":true},{"name":"Probe","visible":true},{"name":"_edge_","visible":true},{"name":"Hidden","visible":false}]`)
	r.createClone(r.targets[2])
	clone := r.targets[len(r.targets)-1]
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	refs := []string{"Probe", "_stage_", "Hidden", logicTargetID(r.targets[2]), logicTargetID(clone)}
	calls := 0
	r.graphics = &extensionTouchGraphics{inspect: func(scene []DisplayTarget) {
		calls++
		if len(scene) != 2 || scene[0].State.CloneID != 0 || scene[1].State.CloneID != clone.state.CloneID {
			t.Fatalf("candidates=%v", scene)
		}
		for _, d := range scene {
			if d.State.Name != "" || d.State.Variables != nil || d.State.Lists != nil {
				t.Fatal("unfiltered candidate data")
			}
		}
	}}
	if got, err := c.Call("target.touchingTargets", refs); err != nil || got != true {
		t.Fatal(got, err)
	}
	if calls != 1 {
		t.Fatalf("geometry calls=%d", calls)
	}
	if got, err := c.Call("target.touchingTargets", []string{"Probe", "Hidden"}); err != nil || got != false {
		t.Fatal(got, err)
	}
	if calls != 1 {
		t.Fatal("empty candidate set reached geometry")
	}
	if _, err := c.Call("target.touchingTargets", []string{"missing"}); err == nil {
		t.Fatal("invalid target accepted")
	}
	if _, err := c.Call("target.touchingTargets", "Probe"); err == nil {
		t.Fatal("invalid list accepted")
	}
}
