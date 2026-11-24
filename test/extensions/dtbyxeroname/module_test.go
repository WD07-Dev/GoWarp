package extension

import (
	"fmt"
	"math"
	"testing"

	e "gowarp/core/extension"
)

type clockHost struct{ now, fps float64 }

func (h *clockHost) Call(op string, args ...any) (any, error) {
	switch op {
	case "runtime.monotonicTime":
		return h.now, nil
	case "runtime.fps":
		return h.fps, nil
	default:
		return nil, fmt.Errorf("unexpected host call %s", op)
	}
}

func TestDeltaTimeUsesTickStartAndDoesNotResetOnFlag(t *testing.T) {
	def := New()
	host := &clockHost{fps: 60}
	c := &e.Context{State: def.State, Host: host}
	read := func(op string) float64 {
		t.Helper()
		for _, block := range def.Blocks {
			if block.Opcode == "dtbyxeroname_"+op {
				result, err := block.Handler(c)
				if err != nil {
					t.Fatal(err)
				}
				return e.Number(result.Value)
			}
		}
		t.Fatalf("missing block %s", op)
		return 0
	}
	tick := func() {
		t.Helper()
		if _, err := def.Events["before-tick"](c); err != nil {
			t.Fatal(err)
		}
	}
	if read("dt") != 0 || !math.IsInf(read("fps"), 1) {
		t.Fatal("initial values differ from JS")
	}
	tick()
	if read("dt") != 1.0/60 || read("fps") != 60 {
		t.Fatal("first tick must estimate using configured FPS")
	}
	host.now = .04
	tick()
	if read("dt") != .04 || read("fps") != 25 {
		t.Fatal("elapsed wall time was not used")
	}
	host.now = .057
	tick()
	if math.Abs(read("dt")-.017) > 1e-12 || read("fps") != 58.82 {
		t.Fatal("FPS rounding differs")
	}
	if handler := def.Events["flag"]; handler != nil {
		if _, err := handler(c); err != nil {
			t.Fatal(err)
		}
	}
	if math.Abs(read("dt")-.017) > 1e-12 {
		t.Fatal("green flag reset timing")
	}
	other := New()
	result, err := other.Blocks[0].Handler(&e.Context{State: other.State, Host: host})
	if err != nil || e.Number(result.Value) != 0 {
		t.Fatal("runtimes share timing state")
	}
}
