package extension

import (
	e "gowarp/core/extension"
	"testing"
)

type clockHost struct{ now float64 }

func (h *clockHost) Call(op string, a ...any) (any, error) {
	if op == "runtime.fps" {
		return 60.0, nil
	}
	return h.now, nil
}
func TestRollingFPS(t *testing.T) {
	x := New()
	if x.Events["tick"] == nil || len(x.Blocks) == 0 {
		t.Fatal("missing fps implementation")
	}
	h := &clockHost{}
	c := &e.Context{State: x.State, Host: h}
	var fps e.Handler
	for _, block := range x.Blocks {
		if block.Opcode == "ShovelUtils_getfps" {
			fps = block.Handler
			break
		}
	}
	if fps == nil {
		t.Fatal("missing FPS reporter")
	}
	r, err := fps(c)
	if err != nil || r.Value != 60.0 {
		t.Fatal(r, err)
	}
	for _, now := range []float64{0.1, 0.5, 1.1} {
		h.now = now
		if _, err := x.Events["tick"](c); err != nil {
			t.Fatal(err)
		}
	}
	r, err = fps(c)
	if err != nil || r.Value != 2.0 {
		t.Fatal(r, err)
	}
}
