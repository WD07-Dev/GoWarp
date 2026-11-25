package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"math"
	"testing"
)

type cameraHost struct{ values map[string]any }

func (h *cameraHost) Call(op string, args ...any) (any, error) {
	switch op {
	case "camera.get":
		return h.values[e.String(args[0])], nil
	case "camera.set":
		h.values[e.String(args[0])] = args[1]
		return nil, nil
	case "target.list":
		return []any{map[string]any{"id": "1:0", "name": "sprite", "cloneId": 0.0, "isStage": false}}, nil
	case "target.get":
		if args[0] == "x" {
			return -10.5, nil
		}
		return 20.5, nil
	}
	return nil, fmt.Errorf("unexpected %s", op)
}
func TestMovementAndCoordinateReporters(t *testing.T) {
	h := &cameraHost{map[string]any{"x": 10.0, "y": 20.0, "zoom": 200.0, "direction": 90.0}}
	c := &e.Context{Host: h}
	blocks := map[string]e.Block{}
	for _, b := range New().Blocks {
		blocks[b.Opcode] = b
	}
	run := func(op string, args map[string]any) any {
		t.Helper()
		c.Args = args
		b, ok := blocks["DTcameracontrols_"+op]
		if !ok {
			t.Fatal("missing", op)
		}
		r, err := b.Handler(c)
		if err != nil {
			t.Fatal(err)
		}
		return r.Value
	}
	if got := run("blocktx", map[string]any{"x": 100.0}); got != 60.0 {
		t.Fatal(got)
	}
	if got := run("blockty", map[string]any{"y": -100.0}); got != -30.0 {
		t.Fatal(got)
	}
	run("moveSteps", map[string]any{"val": 5.0})
	if h.values["x"] != 15.0 || h.values["y"] != 20.0 {
		t.Fatal(h.values)
	}
	run("goTo", map[string]any{"sprite": "missing"})
	if h.values["x"] != 15.0 {
		t.Fatal("missing sprite moved camera")
	}
	run("goTo", map[string]any{"sprite": "sprite"})
	if h.values["x"] != -10.0 || h.values["y"] != 21.0 {
		t.Fatal("JS rounding", h.values)
	}
	run("pointTowards", map[string]any{"sprite": "sprite"})
	if math.Abs(e.Number(h.values["direction"])-225) > 1e-10 {
		t.Fatal(h.values)
	}
	for _, op := range []string{"getX", "getY", "getZoom", "getDirection", "getCol", "blocktx", "blockty"} {
		if blocks["DTcameracontrols_"+op].Text == "" {
			t.Error("monitor text", op)
		}
	}
}
