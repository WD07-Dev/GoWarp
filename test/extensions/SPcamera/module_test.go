package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"math"
	"reflect"
	"testing"
)

type cameraHost struct {
	transforms map[string][]any
	bindings   map[string]string
	options    map[string]any
	storage    string
	penLayer   bool
}

func (h *cameraHost) Call(op string, args ...any) (any, error) {
	switch op {
	case "camera.get":
		if value, ok := h.options["background"]; ok {
			return value, nil
		}
		return "#ffffff", nil
	case "camera.set":
		h.options["background"] = args[1]
		return nil, nil
	case "project.storage":
		return h.storage, nil
	case "options.set":
		h.options[e.String(args[0])] = args[1]
		return nil, nil
	case "camera.transform":
		h.transforms[e.String(args[0])] = append([]any{}, args[1:]...)
		return nil, nil
	case "camera.bind":
		if e.String(args[1]) == "Scratch Stage" && !h.penLayer {
			return nil, fmt.Errorf("default pen drawable does not exist")
		}
		h.bindings[e.String(args[1])] = e.String(args[0])
		return nil, nil
	case "graphics.layer.create":
		if e.String(args[0]) != "Scratch Stage" {
			return nil, fmt.Errorf("unexpected layer %v", args[0])
		}
		h.penLayer = true
		return args[0], nil
	case "graphics.layer.exists":
		return h.penLayer, nil
	case "camera.binding":
		return h.bindings[e.String(args[0])], nil
	case "input.mouse":
		return map[string]any{"x": float64(15), "y": float64(30)}, nil
	case "target.list":
		return []any{map[string]any{"id": "1:0", "name": "actor", "source": float64(1), "cloneId": float64(0), "isStage": false}}, nil
	case "target.get":
		switch args[0] {
		case "source":
			return float64(1), nil
		case "x":
			return float64(50), nil
		case "y":
			return float64(-20), nil
		case "direction":
			return float64(30), nil
		}
	}
	return nil, fmt.Errorf("unexpected %s %v", op, args)
}

func TestRenderedPositionAndSpaceColor(t *testing.T) {
	h, c := cameraFixture(t)
	if got := invoke(t, c, "renderedX", "", map[string]any{"TARGET": "_pen_"}); got != "" || h.penLayer {
		t.Fatal("reporter created missing pen layer", got)
	}
	h.penLayer = true
	invoke(t, c, "bindTarget", "world", map[string]any{"TARGET": "_pen_"})
	h.bindings["1:0"] = "world"
	invoke(t, c, "setXY", "world", map[string]any{"X": 10.0, "Y": 20.0})
	invoke(t, c, "setZoom", "world", map[string]any{"NUM": 200.0})
	invoke(t, c, "setDirectionNew", "world", map[string]any{"NUM": 0.0})
	if got := invoke(t, c, "renderedX", "", map[string]any{"TARGET": "_pen_"}); math.Abs(e.Number(got)-40) > 1e-10 {
		t.Fatal("pen coordinate", got)
	}
	if got := invoke(t, c, "renderedX", "", map[string]any{"TARGET": "actor"}); math.Abs(e.Number(got)-80) > 1e-10 {
		t.Fatal(got)
	}
	if got := invoke(t, c, "renderedY", "", map[string]any{"TARGET": "actor"}); math.Abs(e.Number(got)-80) > 1e-10 {
		t.Fatal(got)
	}
	if got := invoke(t, c, "renderedX", "", map[string]any{"TARGET": "missing"}); got != "" {
		t.Fatal(got)
	}
	if got := invoke(t, c, "spaceColor", "", nil); got != "#ffffff" {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		value any
		want  string
	}{{"#AbC", "#aabbcc"}, {"#11223380", "#11223380"}, {16711680.0, "#ff0000"}, {"#112233ff", "#112233"}} {
		invoke(t, c, "setSpaceColor", "", map[string]any{"COLOR": tc.value})
		if got := invoke(t, c, "spaceColor", "", nil); got != tc.want {
			t.Fatal(tc.value, got, tc.want)
		}
	}
	for _, b := range New().Blocks {
		if (b.Opcode == "SPcamera_spaceColor" || b.Opcode == "SPcamera_renderedX" || b.Opcode == "SPcamera_getX") && b.Text == "" {
			t.Error("monitor text", b.Opcode)
		}
	}
}
func cameraFixture(t *testing.T) (*cameraHost, *e.Context) {
	t.Helper()
	h := &cameraHost{transforms: map[string][]any{}, bindings: map[string]string{}, options: map[string]any{}, storage: `{"cams":["default","HUD","world"]}`}
	c := &e.Context{Host: h, State: map[string]any{}}
	if _, err := New().Events["load"](c); err != nil {
		t.Fatal(err)
	}
	return h, c
}
func invoke(t *testing.T, c *e.Context, op, name string, args map[string]any) any {
	t.Helper()
	c.Args = args
	c.Fields = map[string]string{"CAMERA": name}
	for _, b := range New().Blocks {
		if b.Opcode == "SPcamera_"+op {
			r, err := b.Handler(c)
			if err != nil {
				t.Fatal(err)
			}
			return r.Value
		}
	}
	t.Fatalf("missing %s", op)
	return nil
}
func TestLoadAndIndependentCameraBindings(t *testing.T) {
	h, c := cameraFixture(t)
	if h.options["interpolation"] != false || h.options["fencing"] != false {
		t.Fatal(h.options)
	}
	for _, name := range []string{"", "HUD", "world"} {
		if !reflect.DeepEqual(h.transforms[name], []any{0.0, 0.0, 1.0, 0.0}) {
			t.Fatal("camera not initialized", name, h.transforms)
		}
	}
	invoke(t, c, "bindTarget", "HUD", map[string]any{"TARGET": "_myself_"})
	if h.bindings["_myself_"] != "HUD" {
		t.Fatal(h.bindings)
	}
	invoke(t, c, "setXY", "world", map[string]any{"X": 15.0, "Y": -7.0})
	if !reflect.DeepEqual(h.transforms["world"], []any{15.0, -7.0, 1.0, 0.0}) {
		t.Fatal(h.transforms)
	}
	if got := invoke(t, c, "getX", "world", nil); got != 15.0 {
		t.Fatal(got)
	}
	if got := invoke(t, c, "getY", "HUD", nil); got != 0.0 {
		t.Fatal("camera state leaked", got)
	}
	invoke(t, c, "unbindTarget", "HUD", map[string]any{"TARGET": "_myself_"})
	if h.bindings["_myself_"] != "" {
		t.Fatal("default name was not canonicalized")
	}
	if got := invoke(t, c, "targetCamera", "", map[string]any{"TARGET": "_myself_"}); got != "default" {
		t.Fatal(got)
	}
	invoke(t, c, "bindTarget", "world", map[string]any{"TARGET": "actor"})
	if h.bindings["1:0"] != "world" {
		t.Fatal(h.bindings)
	}
	invoke(t, c, "bindTarget", "HUD", map[string]any{"TARGET": "_pen_"})
	if h.bindings["Scratch Stage"] != "HUD" {
		t.Fatal("pen camera did not bind the default pen layer", h.bindings)
	}
	before := len(h.bindings)
	invoke(t, c, "bindTarget", "missing", map[string]any{"TARGET": "_all_"})
	invoke(t, c, "bindTarget", "HUD", map[string]any{"TARGET": "missing"})
	if len(h.bindings) != before {
		t.Fatal("unknown camera or target bound")
	}
	if got := invoke(t, c, "getX", "missing", nil); got != 0.0 {
		t.Fatal(got)
	}
}
func TestCameraMovementDirectionAndMouseTransform(t *testing.T) {
	h, c := cameraFixture(t)
	invoke(t, c, "setXY", "default", map[string]any{"X": 10.0, "Y": 20.0})
	invoke(t, c, "setDirectionNew", "default", map[string]any{"NUM": 0.0})
	invoke(t, c, "setZoom", "default", map[string]any{"NUM": 200.0})
	if got := invoke(t, c, "fixedMouseX", "default", nil); math.Abs(e.Number(got)+20) > 1e-9 {
		t.Fatal(got)
	}
	if got := invoke(t, c, "fixedMouseY", "default", nil); math.Abs(e.Number(got)-10) > 1e-9 {
		t.Fatal(got)
	}
	invoke(t, c, "moveSteps", "default", map[string]any{"NUM": 5.0})
	if got := invoke(t, c, "getY", "default", nil); got != 25.0 {
		t.Fatal(got)
	}
	if got := invoke(t, c, "getDirectionNew", "default", nil); got != 0.0 {
		t.Fatal(got)
	}
	invoke(t, c, "setDirection", "default", map[string]any{"NUM": 120.0})
	if got := invoke(t, c, "getDirection", "default", nil); got != 120.0 {
		t.Fatal("deprecated direction", got)
	}
	invoke(t, c, "goToObject", "world", map[string]any{"TARGET": "actor"})
	invoke(t, c, "pointCamera", "world", map[string]any{"TARGET": "actor"})
	if !reflect.DeepEqual(h.transforms["world"], []any{50.0, -20.0, 1.0, 60.0}) {
		t.Fatal(h.transforms)
	}
}

func TestCloneUsesOriginalCamera(t *testing.T) {
	h, c := cameraFixture(t)
	h.bindings["1:0"] = "world"
	h.bindings["_myself_"] = "HUD"
	if _, err := New().Events["clone-created"](c); err != nil {
		t.Fatal(err)
	}
	if h.bindings["_myself_"] != "world" {
		t.Fatal("clone inherited rebound source clone instead of original", h.bindings)
	}
}
