// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"encoding/json"
	"fmt"
	e "gowarp/core/extension"
	"math"
	"strconv"
	"strings"
)

type camera struct{ x, y, zoom, direction float64 }

func cameras(c *e.Context) map[string]*camera {
	cams, ok := c.State["cameras"].(map[string]*camera)
	if !ok {
		cams = map[string]*camera{"default": {zoom: 1}}
		c.State["cameras"] = cams
	}
	return cams
}
func cameraName(name string) string {
	if name == "default" {
		return ""
	}
	return name
}
func update(c *e.Context, name string, cam *camera) error {
	_, err := c.Call("camera.transform", cameraName(name), cam.x, cam.y, cam.zoom, cam.direction)
	return err
}
func load(c *e.Context) (e.Result, error) {
	cams := map[string]*camera{"default": {zoom: 1}}
	c.State["cameras"] = cams
	raw, err := c.Call("project.storage", "SPcamera")
	if err != nil {
		return e.Result{}, err
	}
	if e.String(raw) != "" {
		var saved struct {
			Cams []string `json:"cams"`
		}
		if err := json.Unmarshal([]byte(e.String(raw)), &saved); err != nil {
			return e.Result{}, err
		}
		for _, name := range saved.Cams {
			cams[name] = &camera{zoom: 1}
		}
	}
	for _, key := range []string{"interpolation", "fencing"} {
		if _, err := c.Call("options.set", key, false); err != nil {
			return e.Result{}, err
		}
	}
	for name, cam := range cams {
		if err := update(c, name, cam); err != nil {
			return e.Result{}, err
		}
	}
	return e.Result{}, nil
}

// Camera V2 initializes clones from the original sprite's camera, even when
// the immediate source clone was rebound to another camera.
func cloneCreated(c *e.Context) (e.Result, error) {
	source, err := c.Call("target.get", "source")
	if err != nil {
		return e.Result{}, err
	}
	records, err := c.Call("target.list")
	if err != nil {
		return e.Result{}, err
	}
	for _, record := range records.([]any) {
		target := record.(map[string]any)
		if e.Number(target["source"]) == e.Number(source) && e.Number(target["cloneId"]) == 0 && !e.Bool(target["isStage"]) {
			name, err := c.Call("camera.binding", target["id"])
			if err != nil {
				return e.Result{}, err
			}
			_, err = c.Call("camera.bind", e.String(name), "_myself_")
			return e.Result{}, err
		}
	}
	return e.Result{}, nil
}

func targetRef(c *e.Context, name string) (string, bool, error) {
	switch name {
	case "_myself_", "_stage_", "_all_":
		return name, true, nil
	case "_pen_":
		_, err := c.Call("graphics.layer.create", "Scratch Stage")
		return "Scratch Stage", err == nil, err
	case "_video_":
		return "", false, fmt.Errorf("video-layer cameras require video sensing support")
	}
	records, err := c.Call("target.list")
	if err != nil {
		return "", false, err
	}
	for _, record := range records.([]any) {
		t := record.(map[string]any)
		if e.String(t["name"]) == name && e.Number(t["cloneId"]) == 0 && !e.Bool(t["isStage"]) {
			return e.String(t["id"]), true, nil
		}
	}
	return "", false, nil
}
func epsilon(n float64) float64 {
	if math.Abs(n) < 1e-10 {
		return 0
	}
	return n
}
func New() e.Extension {
	x := e.Extension{ID: "SPcamera", State: map[string]any{}, Events: map[string]e.Handler{"load": load, "clone-created": cloneCreated}}
	add := func(op, kind string, inputs ...string) {
		labels := map[string]string{"getX": "[CAMERA] camera x", "getY": "[CAMERA] camera y", "getZoom": "[CAMERA] camera zoom", "getDirection": "[CAMERA] camera direction", "getDirectionNew": "[CAMERA] camera direction", "targetCamera": "camera of [TARGET]", "renderedX": "rendered x position of [TARGET]", "renderedY": "rendered y position of [TARGET]", "spaceColor": "background color", "fixedMouseX": "mouse x in camera [CAMERA]", "fixedMouseY": "mouse y in camera [CAMERA]"}
		x.Blocks = append(x.Blocks, e.Block{Opcode: "SPcamera_" + op, Text: labels[op], Kind: kind, Inputs: inputs, Handler: func(c *e.Context) (e.Result, error) { v, err := run(c, op); return e.Result{Value: v}, err }})
	}
	for _, menu := range []string{"OBJECTS", "EXACT_OBJECTS", "TARGETS", "BINDS"} {
		add("menu_"+menu, "reporter")
	}
	for _, op := range []string{"bindTarget", "unbindTarget", "goToObject", "pointCamera"} {
		add(op, "command", "TARGET")
	}
	add("targetCamera", "reporter", "TARGET")
	add("setXY", "command", "X", "Y")
	for _, op := range []string{"setX", "setY", "changeX", "changeY", "moveSteps", "setZoom", "changeZoom", "setDirection", "setDirectionNew", "turnCamRight", "turnCamLeft"} {
		add(op, "command", "NUM")
	}
	for _, op := range []string{"getX", "getY", "getZoom", "getDirection", "getDirectionNew", "fixedMouseX", "fixedMouseY"} {
		add(op, "reporter")
	}
	add("setSpaceColor", "command", "COLOR")
	add("spaceColor", "reporter")
	add("renderedX", "reporter", "TARGET")
	add("renderedY", "reporter", "TARGET")
	return x
}
func run(c *e.Context, op string) (any, error) {
	if strings.HasPrefix(op, "menu_") {
		return c.Field(strings.TrimPrefix(op, "menu_")), nil
	}

	if op == "setSpaceColor" || op == "spaceColor" {
		if op == "spaceColor" {
			v, err := c.Call("camera.get", "color")
			if err != nil {
				return nil, err
			}
			rgb, alpha := colorParts(v, true)
			suffix := ""
			if alpha < 255 {
				suffix = fmt.Sprintf("%02x", alpha)
			}
			return fmt.Sprintf("#%06x%s", rgb, suffix), nil
		}
		rgb, alpha := colorParts(c.Args["COLOR"], false)
		if alpha == 0 {
			return nil, fmt.Errorf("fully transparent camera backgrounds are not supported by the renderer")
		}
		color := fmt.Sprintf("#%06x", rgb)
		if alpha < 255 {
			color = strconv.FormatUint(uint64(alpha)<<24|uint64(rgb), 10)
		}
		return c.Call("camera.set", "color", color)
	}
	cams := cameras(c)
	if op == "renderedX" || op == "renderedY" {
		pen := c.String("TARGET") == "_pen_"
		if pen {
			exists, err := c.Call("graphics.layer.exists", "Scratch Stage")
			if err != nil {
				return nil, err
			}
			if !e.Bool(exists) {
				return "", nil
			}
		}
		ref, ok, err := targetRef(c, c.String("TARGET"))
		if err != nil {
			return nil, err
		}
		if !ok {
			return "", nil
		}
		var px, py any = float64(0), float64(0)
		if !pen {
			px, err = c.Call("target.get", "x", ref)
			if err != nil {
				return nil, err
			}
			py, err = c.Call("target.get", "y", ref)
			if err != nil {
				return nil, err
			}
		}
		binding, err := c.Call("camera.binding", ref)
		if err != nil {
			return nil, err
		}
		name := e.String(binding)
		if name == "" {
			name = "default"
		}
		cam := cams[name]
		x, y := e.Number(px), e.Number(py)
		if cam != nil {
			x -= cam.x
			y -= cam.y
			sin, cos := math.Sincos(cam.direction * math.Pi / 180)
			x, y = epsilon(cam.zoom*(x*cos-y*sin)), epsilon(cam.zoom*(x*sin+y*cos))
		}
		if op == "renderedX" {
			return x, nil
		}
		return y, nil
	}
	name := c.Field("CAMERA")
	cam := cams[name]
	if op == "bindTarget" || op == "unbindTarget" || op == "targetCamera" {
		if op == "unbindTarget" {
			name = "default"
			cam = cams[name]
		}
		if op != "targetCamera" && cam == nil {
			return nil, nil
		}
		ref, ok, err := targetRef(c, c.String("TARGET"))
		if err != nil {
			return nil, err
		}
		if !ok {
			if op == "targetCamera" {
				return "", nil
			}
			return nil, nil
		}
		if op == "targetCamera" {
			v, err := c.Call("camera.binding", ref)
			if err != nil {
				return nil, err
			}
			if e.String(v) == "" {
				return "default", nil
			}
			return v, nil
		}
		return c.Call("camera.bind", cameraName(name), ref)
	}
	if cam == nil {
		if strings.HasPrefix(op, "get") || strings.HasPrefix(op, "fixed") {
			return float64(0), nil
		}
		return nil, nil
	}
	n := c.Number("NUM")
	next := *cam
	switch op {
	case "getX":
		return cam.x, nil
	case "getY":
		return cam.y, nil
	case "getZoom":
		return cam.zoom * 100, nil
	case "getDirection":
		return cam.direction + 90, nil
	case "getDirectionNew":
		return 90 - cam.direction, nil
	case "fixedMouseX", "fixedMouseY":
		v, err := c.Call("input.mouse")
		if err != nil {
			return nil, err
		}
		mouse := v.(map[string]any)
		x, y := e.Number(mouse["x"])-cam.x, e.Number(mouse["y"])-cam.y
		radians := cam.direction * math.Pi / 180
		sine, cosine := math.Sincos(radians)
		if op == "fixedMouseX" {
			return epsilon(cam.zoom * (x*cosine - y*sine)), nil
		}
		return epsilon(cam.zoom * (x*sine + y*cosine)), nil
	case "setXY":
		next.x, next.y = c.Number("X"), c.Number("Y")
	case "setX":
		next.x = n
	case "setY":
		next.y = n
	case "changeX":
		next.x = epsilon(cam.x + n)
	case "changeY":
		next.y = epsilon(cam.y + n)
	case "moveSteps":
		radians := cam.direction * math.Pi / 180
		next.x = epsilon(cam.x + n*math.Cos(radians))
		next.y = epsilon(cam.y + n*math.Sin(radians))
	case "setZoom":
		next.zoom = n / 100
	case "changeZoom":
		next.zoom += n / 100
	case "setDirection":
		next.direction = n - 90
	case "setDirectionNew":
		next.direction = 90 - n
	case "turnCamRight":
		next.direction -= n
	case "turnCamLeft":
		next.direction += n
	case "goToObject", "pointCamera":
		ref, ok, err := targetRef(c, c.String("TARGET"))
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		if op == "pointCamera" {
			v, err := c.Call("target.get", "direction", ref)
			if err != nil {
				return nil, err
			}
			next.direction = 90 - e.Number(v)
		} else {
			x, err := c.Call("target.get", "x", ref)
			if err != nil {
				return nil, err
			}
			y, err := c.Call("target.get", "y", ref)
			if err != nil {
				return nil, err
			}
			next.x, next.y = e.Number(x), e.Number(y)
		}
	default:
		return nil, fmt.Errorf("unsupported opcode %s", op)
	}
	dirty := func(a, b float64) bool { return a != b && math.Abs(a-b) >= 1e-10 }
	if dirty(next.x, cam.x) || dirty(next.y, cam.y) || dirty(next.zoom, cam.zoom) || dirty(next.direction, cam.direction) {
		if err := update(c, name, &next); err != nil {
			return nil, err
		}
		*cam = next
	}
	return nil, nil
}

// Numeric renderer colors carry ARGB; Scratch color inputs ignore numeric alpha.
func colorParts(value any, renderer bool) (uint32, uint8) {
	if s, ok := value.(string); ok && strings.HasPrefix(s, "#") {
		s = s[1:]
		alpha := uint8(255)
		if len(s) > 6 {
			n, err := strconv.ParseUint(s[6:], 16, 8)
			if err == nil {
				alpha = uint8(n)
			}
			s = s[:6]
		}
		if len(s) == 3 {
			s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
		}
		if len(s) != 6 {
			return 0, alpha
		}
		n, err := strconv.ParseUint(s, 16, 24)
		if err != nil {
			return 0, alpha
		}
		return uint32(n), alpha
	}
	n := e.Number(value)
	bits := uint32(0)
	if !math.IsInf(n, 0) && !math.IsNaN(n) {
		bits = uint32(int64(math.Mod(n, 4294967296)))
	}
	alpha := uint8(255)
	if renderer && bits>>24 != 0 {
		alpha = uint8(bits >> 24)
	}
	return bits & 0xffffff, alpha
}
