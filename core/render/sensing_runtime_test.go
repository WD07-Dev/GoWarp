package render

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"testing"

	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func sensingRuntime(t testing.TB, mode string, decorations int) *vm.Runtime {
	t.Helper()
	var p project.Project
	data := `{"targets":[
		{"isStage":true,"name":"Stage","variables":{"result":["result",false]}},
		{"name":"Probe","visible":true,"blocks":{
			"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
			"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"sense"]}},
			"sense":{"opcode":"data_setvariableto","next":"wait","fields":{"VARIABLE":["result","result"]},"inputs":{"VALUE":[2,"query"]}},
			"query":{"opcode":"sensing_touchingobject","inputs":{"TOUCHINGOBJECTMENU":[1,[10,"Obstacle"]]}},
			"wait":{"opcode":"control_wait","inputs":{"DURATION":[1,[4,"0"]]}}
		}},
		{"name":"Obstacle","visible":true}
	]}`
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatal(err)
	}
	query := p.Targets[1].Blocks["query"]
	switch mode {
	case "edge", "mouse":
		query.Inputs["TOUCHINGOBJECTMENU"] = []any{float64(1), []any{float64(10), "_" + mode + "_"}}
	case "color":
		query.Opcode = "sensing_touchingcolor"
		query.Inputs = map[string][]any{"COLOR": {float64(1), []any{float64(10), "#ff0000"}}}
	}
	pixels := solid(color.NRGBA{R: 255, A: 255})
	lib := &media.Library{Costumes: [][]*media.Costume{nil, {pixels}, {pixels}}}
	for i := 0; i < decorations; i++ {
		p.Targets = append(p.Targets, &project.Target{
			Name: fmt.Sprint("Decoration", i), Visible: true, Size: 100,
			Direction: 90, X: 200, Y: 150,
		})
		lib.Costumes = append(lib.Costumes, []*media.Costume{pixels})
	}
	runtime := vm.New(&p)
	runtime.SetGraphics(New(lib))
	return runtime
}

func TestRuntimeSensingSeesImmediateMovement(t *testing.T) {
	for _, mode := range []string{"object", "color"} {
		t.Run(mode, func(t *testing.T) {
			r := sensingRuntime(t, mode, 30)
			before := r.Scene()
			for _, tc := range []struct {
				x    float64
				want bool
			}{{0, true}, {20, false}, {0, true}} {
				r.MoveSprite(2, 0, tc.x, 0)
				if err := r.Tick(context.Background(), 60, false); err != nil {
					t.Fatal(err)
				}
				if got := r.Snapshot()[0].Variables["result"].Value; got != tc.want {
					t.Fatalf("x=%g touching=%v, want %v", tc.x, got, tc.want)
				}
				if before[2].State.X != 0 {
					t.Fatal("sensing mutated an earlier public scene snapshot")
				}
			}
		})
	}
}

func BenchmarkRuntimeSensing(b *testing.B) {
	for _, mode := range []string{"object", "edge", "color"} {
		b.Run(mode, func(b *testing.B) {
			r := sensingRuntime(b, mode, 1000)
			ctx := context.Background()
			if err := r.Tick(ctx, 60, false); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := r.Tick(ctx, 60, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestColorSensingReusesCandidates(t *testing.T) {
	g := New(&media.Library{Costumes: [][]*media.Costume{{solid(color.NRGBA{R: 255, A: 255})}}})
	probe := vm.DisplayTarget{State: vm.TargetState{
		Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90,
	}}
	other := probe
	other.State.CloneID = 1
	scene := []vm.DisplayTarget{probe, other}
	allocations := testing.AllocsPerRun(100, func() {
		if !g.TouchingColor(scene, probe, color.NRGBA{R: 255, A: 255}, nil) {
			t.Fatal("overlapping red clone was not sensed")
		}
	})
	if allocations != 0 {
		t.Fatalf("color query allocates candidate storage each time: %g", allocations)
	}
}
