package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"testing"
)

type windowHost struct{}

func (windowHost) Call(op string, args ...any) (any, error) {
	if op == "host.projectFullscreen" {
		return true, nil
	}
	if op == "host.window" {
		value, ok := map[string]any{"screenWidth": 1920.0, "screenHeight": 1080.0, "focused": false, "title": "Project title"}[e.String(args[0])]
		if ok {
			return value, nil
		}
	}
	return nil, fmt.Errorf("unexpected %s %v", op, args)
}
func TestWindowReporters(t *testing.T) {
	wanted := map[string]any{"screenW": 1920.0, "screenH": 1080.0, "isFocused": false, "windowTitle": "Project title", "isFullscreen": true}
	for _, b := range New().Blocks {
		op := b.Opcode[len("cubesterWindowControls_"):]
		want, ok := wanted[op]
		if !ok {
			continue
		}
		r, err := b.Handler(&e.Context{Host: windowHost{}})
		if err != nil || r.Value != want {
			t.Errorf("%s=%v, %v", op, r.Value, err)
		}
		kind := "reporter"
		if op == "isFocused" || op == "isFullscreen" {
			kind = "boolean"
		}
		if b.Kind != kind || b.Text == "" {
			t.Error("metadata", b)
		}
		delete(wanted, op)
	}
	if len(wanted) > 0 {
		t.Fatal("missing reporters", wanted)
	}
}
