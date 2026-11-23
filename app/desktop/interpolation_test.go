package desktop

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"gowarp/app/config"
	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func sceneBufferController(t testing.TB) *controller {
	t.Helper()
	p := &project.Project{Targets: []*project.Target{{IsStage: true, Name: "Stage"}}}
	for range 1000 {
		p.Targets = append(p.Targets, &project.Target{Name: "Sprite", Visible: true, Size: 100, Direction: 90})
	}
	settings := config.Default(p)
	settings.FPS, settings.Interpolation = 60, true
	c := newController(p, &media.Library{}, Options{Settings: &settings, FPS: 60})
	if c.initErr != nil {
		t.Fatal(c.initErr)
	}
	return c
}

func TestDisplaySceneReusesStorage(t *testing.T) {
	c := sceneBufferController(t)
	if err := c.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(10, func() { c.displayScene() })
	if allocations != 0 {
		t.Fatalf("unchanged display allocated %g objects per draw", allocations)
	}
	previous := &c.previous[0]
	c.vm.MoveSprite(1, 0, 16, 0)
	if err := c.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if &c.previous[0] != previous {
		t.Fatal("tick allocated another scene buffer")
	}
	c.vm.MoveSprite(1, 0, 32, 0)
	c.credit = .5
	if got := c.displayScene()[1].State.X; got != 24 {
		t.Fatalf("scene reuse changed interpolation: x=%g, want 24", got)
	}
	if c.previous[1].State.X != 16 || c.vm.Scene()[1].State.X != 32 {
		t.Fatal("drawing overwrote previous or authoritative state")
	}
}

func BenchmarkControllerScenePreparation(b *testing.B) {
	c := sceneBufferController(b)
	ctx := context.Background()
	if err := c.tick(ctx); err != nil {
		b.Fatal(err)
	}
	c.displayScene()
	b.ReportAllocs()
	for b.Loop() {
		if err := c.tick(ctx); err != nil {
			b.Fatal(err)
		}
		c.displayScene()
	}
}

func TestRealtimeInterpolationIsContinuousAcrossUpdates(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage"},{"name":"Sprite","visible":true,"size":100,"direction":90,"blocks":{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
		"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"move"]}},
		"move":{"opcode":"motion_changexby","inputs":{"DX":[1,[4,"16"]]}}
	}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	settings := config.Default(&p)
	settings.FPS, settings.Interpolation = 60, true
	c := newController(&p, &media.Library{}, Options{Settings: &settings, FPS: 60})
	base := time.Unix(100, 0)
	now := base
	c.cadenceNow = func() time.Time { return now }
	for milliseconds := 0; milliseconds <= 96; milliseconds += 4 {
		now = base.Add(time.Duration(milliseconds) * time.Millisecond)
		if milliseconds%8 == 0 {
			if err := c.advance(context.Background(), Input{}); err != nil {
				t.Fatal(err)
			}
		}
		want := 8 + float64(milliseconds)
		if got := c.displayScene()[1].State.X; math.Abs(got-want) > 1e-9 {
			t.Fatalf("at %dms x=%g want %g: display paused or jumped across an update", milliseconds, got, want)
		}
	}
	if c.vm.Frames() != 7 {
		t.Fatalf("interpolation changed VM tick count: %d", c.vm.Frames())
	}
}

func TestRealtimeInterpolationFollowsRoundedCadence(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage"},{"name":"Sprite","visible":true,"x":16,"size":100,"direction":90}]}`), &p); err != nil {
		t.Fatal(err)
	}
	settings := config.Default(&p)
	settings.FPS, settings.Interpolation = 60, true
	c := newController(&p, &media.Library{}, Options{Settings: &settings, FPS: 60})
	if c.initErr != nil {
		t.Fatal(c.initErr)
	}
	c.previous = c.vm.Scene()
	c.previous[1].State.X = 0
	c.previousView = c.vm.View()
	now := time.Unix(100, 0)
	c.cadenceTime = now
	c.cadenceNow = func() time.Time { return now }
	c.cadenceFPS = float64(time.Second) / float64(c.vm.TickInterval())
	c.credit = -.25
	for _, test := range []struct {
		elapsed time.Duration
		x       float64
	}{{0, 4}, {4 * time.Millisecond, 8}, {8 * time.Millisecond, 12}, {12 * time.Millisecond, 16}, {20 * time.Millisecond, 16}} {
		now = c.cadenceTime.Add(test.elapsed)
		got := c.displayScene()[1].State.X
		if math.Abs(got-test.x) > 1e-9 {
			t.Fatalf("elapsed=%v x=%g want %g", test.elapsed, got, test.x)
		}
	}
	if c.credit != -.25 || c.vm.Scene()[1].State.X != 16 || c.vm.Frames() != 0 {
		t.Fatal("drawing advanced authoritative simulation state")
	}
	c.options.Replay = true
	c.credit = .25
	if got := c.displayScene()[1].State.X; got != 4 {
		t.Fatalf("replay interpolation used wall time: %g", got)
	}
}

func TestInterpolationDoesNotChangeVMScene(t *testing.T) {
	var buffer interpolationBuffer
	old := []vm.DisplayTarget{{Source: 1, State: vm.TargetState{
		Visible:   true,
		X:         0,
		Size:      100,
		Direction: 170,
	}}}
	next := []vm.DisplayTarget{{Source: 1, State: vm.TargetState{
		Visible:   true,
		X:         100,
		Size:      200,
		Direction: -170,
	}}}
	got := buffer.interpolate(old, next, .5)
	if got[0].State.X != 50 || got[0].State.Size != 150 || got[0].State.Direction != 180 {
		t.Fatalf("bad midpoint: %+v", got)
	}
	if next[0].State.X != 100 || old[0].State.X != 0 {
		t.Fatal("mutated authoritative state")
	}
	next[0].State.CloneID = 1
	if buffer.interpolate(old, next, .5)[0].State.X != 100 {
		t.Fatal("interpolated a newly created clone")
	}
}

func BenchmarkInterpolationManyClones(b *testing.B) {
	var buffer interpolationBuffer
	old := make([]vm.DisplayTarget, 1000)
	next := make([]vm.DisplayTarget, len(old))
	for i := range old {
		old[i] = vm.DisplayTarget{Source: 1, State: vm.TargetState{
			CloneID: i, Visible: true, Size: 100, Direction: 90, X: float64(i),
		}}
		next[i] = old[i]
		next[i].State.X += 2
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result := buffer.interpolate(old, next, .5)
		if result[999].State.X != 1000 {
			b.Fatal("wrong interpolated position")
		}
	}
}

func TestInterpolationReusesWorkingStorage(t *testing.T) {
	var buffer interpolationBuffer
	old := make([]vm.DisplayTarget, 300)
	next := make([]vm.DisplayTarget, len(old))
	for i := range old {
		old[i] = vm.DisplayTarget{Source: 1, State: vm.TargetState{CloneID: i, Visible: true, Size: 100, Direction: 90}}
		next[len(next)-1-i] = old[i]
		next[len(next)-1-i].State.X = 10
	}
	allocations := testing.AllocsPerRun(20, func() {
		result := buffer.interpolate(old, next, .5)
		if result[0].State.X != 5 {
			t.Fatal("bad reordered interpolation")
		}
	})
	if allocations > 1 {
		t.Fatalf("steady interpolation allocates %.0f objects per draw", allocations)
	}
}

func TestInterpolationTracksLayerChangesAndDeletedClones(t *testing.T) {
	var buffer interpolationBuffer
	old := []vm.DisplayTarget{
		{Source: 1, State: vm.TargetState{CloneID: 1, Visible: true, X: 10, Size: 100, Direction: 90}},
		{Source: 1, State: vm.TargetState{CloneID: 2, Visible: true, X: 30, Size: 100, Direction: 90}},
	}
	next := []vm.DisplayTarget{old[1], old[0]}
	next[0].State.X, next[1].State.X = 50, 20
	got := buffer.interpolate(old, next, .5)
	if got[0].State.X != 40 || got[1].State.X != 15 {
		t.Fatal("layer change mixed clone positions")
	}
	old = []vm.DisplayTarget{next[0]}
	next = []vm.DisplayTarget{next[1]}
	next[0].State.X = 80
	got = buffer.interpolate(old, next, .5)
	if got[0].State.X != 80 {
		t.Fatal("deleted clone's position was reused")
	}
	next[0].State.CloneID = 2
	next[0].State.CurrentCostume = 1
	if buffer.interpolate(old, next, .5)[0].State.X != 80 {
		t.Fatal("interpolated across a costume change")
	}
	if old[0].State.X != 50 || next[0].State.X != 80 {
		t.Fatal("changed input snapshots")
	}
	if len(buffer.interpolate(nil, nil, .5)) != 0 {
		t.Fatal("empty scene retained deleted sprites")
	}
}
