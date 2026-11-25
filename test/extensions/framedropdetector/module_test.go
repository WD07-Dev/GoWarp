package extension_test

import (
	"context"
	"fmt"
	"math"
	"testing"

	e "gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
	detector "gowarp/test/extensions/framedropdetector"
)

type frameHost struct{ milliseconds float64 }

func (h *frameHost) Call(operation string, _ ...any) (any, error) {
	if operation != "host.frameMilliseconds" {
		return nil, fmt.Errorf("unexpected operation %s", operation)
	}
	return h.milliseconds, nil
}

func TestFrameDropThresholdAndHistory(t *testing.T) {
	def := detector.New()
	host := &frameHost{}
	c := &e.Context{Host: host, State: def.State}
	handlers := make(map[string]e.Handler)
	for _, block := range def.Blocks {
		handlers[block.Opcode] = block.Handler
	}
	call := func(opcode string) any {
		t.Helper()
		handler := handlers["framedropdetector_"+opcode]
		if handler == nil {
			t.Fatalf("missing block %s", opcode)
		}
		result, err := handler(c)
		if err != nil {
			t.Fatal(err)
		}
		return result.Value
	}
	if got := call("getFps"); got != 0.0 {
		t.Fatalf("FPS without an elapsed frame = %v", got)
	}
	c.Args = map[string]any{"MS": 50}
	call("setThreshold")
	if got := call("getThreshold"); got != 50.0 {
		t.Fatalf("threshold = %v; want 50", got)
	}
	for _, tc := range []struct {
		dt, last float64
		dropped  bool
	}{
		{50, 0, false},
		{62.36, 62.4, true},
		{16, 62.4, false},
	} {
		host.milliseconds = tc.dt
		if _, err := def.Events["before-tick"](c); err != nil {
			t.Fatal(err)
		}
		if got := call("isDropped"); got != tc.dropped {
			t.Fatalf("dropped at %g ms = %v; want %v", tc.dt, got, tc.dropped)
		}
		if got := call("getLastDt"); got != tc.last {
			t.Fatalf("last delay at %g ms = %v; want %g", tc.dt, got, tc.last)
		}
	}
}

func TestCurrentFPSMonitorWithoutScripts(t *testing.T) {
	r := vm.New(&project.Project{
		Targets:  []*project.Target{{IsStage: true, Name: "Stage"}},
		Monitors: []project.Monitor{{ID: "fps", Opcode: "framedropdetector_getFps", Visible: true}},
	})
	if err := r.SetExtensions(e.Bundle{detector.New}); err != nil {
		t.Fatal(err)
	}
	for _, milliseconds := range []float64{1000.0 / 60, 40, 20} {
		r.SetHostFrame(false, milliseconds)
		if err := r.Tick(context.Background(), 60, false); err != nil {
			t.Fatal(err)
		}
		monitors := r.VisibleMonitors()
		if len(monitors) != 1 || e.Number(monitors[0].Value) != math.Round(1000/milliseconds) {
			t.Fatalf("FPS at %g ms: %+v", milliseconds, monitors)
		}
	}
}

func TestFrameDropMonitors(t *testing.T) {
	opcodes := []string{
		"framedropdetector_getFps",
		"framedropdetector_getLastDt",
		"framedropdetector_getThreshold",
	}
	bundle := e.Bundle{detector.New}
	if err := e.Validate(bundle, []string{"framedropdetector"}, opcodes); err != nil {
		t.Fatal(err)
	}
	p := &project.Project{Targets: []*project.Target{{IsStage: true, Name: "Stage"}}}
	for _, opcode := range opcodes {
		p.Monitors = append(p.Monitors, project.Monitor{ID: opcode, Opcode: opcode, Visible: true})
	}
	r := vm.New(p)
	if err := r.SetExtensions(bundle); err != nil {
		t.Fatal(err)
	}
	labels := []string{"현재 FPS", "마지막 프레임 지연 시간 (ms)", "현재 감지 기준 (ms)"}
	for _, tc := range []struct {
		milliseconds float64
		values       [3]string
	}{
		{1000.0 / 60, [3]string{"60", "0", "33.33"}},
		{42.34, [3]string{"24", "42.3", "33.33"}},
		{20, [3]string{"50", "42.3", "33.33"}},
	} {
		r.SetHostFrame(false, tc.milliseconds)
		if err := r.Tick(context.Background(), 60, false); err != nil {
			t.Fatal(err)
		}
		monitors := r.VisibleMonitors()
		if len(monitors) != len(opcodes) {
			t.Fatalf("visible monitors: %d; want %d", len(monitors), len(opcodes))
		}
		for i, monitor := range monitors {
			if monitor.Label != labels[i] || monitor.Value != tc.values[i] {
				t.Fatalf("%s at %g ms: label=%q value=%q; want %q %q", opcodes[i], tc.milliseconds, monitor.Label, monitor.Value, labels[i], tc.values[i])
			}
		}
	}
}
