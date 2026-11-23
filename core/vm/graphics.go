package vm

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"regexp"
	"sort"
	"strings"

	"gowarp/core/extension"
	"gowarp/core/project"
)

type Bounds struct {
	Left, Right, Bottom, Top float64
}

type View struct {
	NoAntialias                          bool
	MaskTarget                           string
	OffscreenTouching                    bool
	HighQualityPen                       bool
	DisablePositionRounding              bool
	PenNoAntialias                       bool
	Split                                *SplitEffects
	Canvas                               CanvasEffects
	Shader                               *StageShader
	ShaderTime                           float64
	ShaderElapsed                        float64
	Width, Height, X, Y, Zoom, Direction float64
	Color                                string
}

type RuntimeOptions struct {
	View                                               View
	Fencing, MiscLimits, Interpolation, HighQualityPen bool
	CloneLimit                                         int
}

type PenState struct {
	Down                                                   bool
	Size, Hue, Saturation, Brightness, Transparency, Shade float64
}

type PenCommand struct {
	Layer   string
	Kind    string
	Target  DisplayTarget
	Line    [4]float64 // x0, y0, x1, y1; copied with the command.
	Points  []float64  // Triangle coordinates.
	Size    float64
	Color   color.NRGBA
	Surface *PenSurfaceCommand
}

const StageSurface = "Scratch Stage"

type PenVertex struct {
	X, Y, U, V, R, G, B, A, Z, W float64
}

// PenSurfaceCommand is immutable after submission, including its costume snapshot.
type PenSurfaceCommand struct {
	Name                 string
	Width, Height        int
	Vertices             [3]PenVertex
	Textured             bool
	Texture              string
	Costume              *DisplayTarget
	Linear               bool
	MultiplyTextureAlpha bool
}

/*
Graphics executes synchronously so sensing observes preceding pen operations.
Sensing scene slices are borrowed for the duration of the call.
An implementation may provide ScriptDataIndependent() bool. Returning true
promises that it neither accesses script variables/lists nor reenters the VM.
Wrappers which do either must override an inherited opt-in to return false.
*/
type Graphics interface {
	SetView(View)
	Bounds(DisplayTarget, bool) (Bounds, bool)
	CostumeSize(DisplayTarget) (float64, float64)
	Touching([]DisplayTarget, DisplayTarget, string, float64, float64) bool
	TouchingColor([]DisplayTarget, DisplayTarget, color.NRGBA, *color.NRGBA) bool
	Pen(PenCommand)
}

func defaultRuntimeOptions() RuntimeOptions {
	return RuntimeOptions{
		View: View{
			Width:     480,
			Height:    360,
			Zoom:      100,
			Direction: 90,
			Color:     "#ffffff",
		},
		Fencing:    true,
		MiscLimits: true,
		CloneLimit: 300,
	}
}

func (r *Runtime) Options() RuntimeOptions {
	return r.options
}

// StageShaderSources returns registered programs without selecting or running them.
func (r *Runtime) StageShaderSources() []extension.ShaderSource {
	unique := make(map[[2]string]extension.ShaderSource, len(r.visual.shaders))
	for _, shader := range r.visual.shaders {
		source := shader.Source()
		unique[source.Key()] = source
	}
	sources := make([]extension.ShaderSource, 0, len(unique))
	for _, source := range unique {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Code != sources[j].Code {
			return sources[i].Code < sources[j].Code
		}
		return sources[i].Key()[0] < sources[j].Key()[0]
	})
	return sources
}

func (r *Runtime) View() View {
	view := r.options.View
	view.HighQualityPen = r.options.HighQualityPen
	view.OffscreenTouching = !r.options.Fencing
	if view.Shader != nil {
		view.ShaderTime = r.elapsed - r.timerSeconds
		view.ShaderElapsed = r.elapsed
	}
	view.Split = r.splitEffectsView()
	return view
}

func (r *Runtime) FPS() float64 {
	if r.fps > 0 {
		return r.fps
	}
	return 60
}

func (r *Runtime) SetGraphics(g Graphics) {
	r.graphics = g
	r.graphicsDataIndependent = false
	if independent, ok := g.(interface{ ScriptDataIndependent() bool }); ok {
		r.graphicsDataIndependent = independent.ScriptDataIndependent()
	}
	if g != nil {
		if !r.graphicsDataIndependent {
			r.graphics = &observingGraphics{Graphics: g, r: r}
		}
		g.SetView(r.View())
	}
}

func display(t *target) DisplayTarget {
	s := t.state
	if t.owner != nil {
		if camera, ok := t.owner.visual.cameras[s.Camera]; ok {
			s.Transform = camera
		}
	}
	return DisplayTarget{Source: t.sourceIndex, State: s}
}

func (r *Runtime) prepareSensingScene(name string, all bool) []DisplayTarget {
	scene := r.sensingScene[:0]
	r.sensingScene = nil
	for _, t := range r.drawList {
		if !all && (t.state.Name != name || !t.state.Visible || t.state.IsStage) {
			continue
		}
		d := display(t)
		d.State.Variables, d.State.Lists = nil, nil
		scene = append(scene, d)
	}
	return scene
}

func (r *Runtime) releaseSensingScene(scene []DisplayTarget) {
	clear(scene)
	// Do not retain a large clone burst, or references to costumes/text state.
	if cap(scene) <= 4096 {
		r.sensingScene = scene[:0]
	}
}

func (r *Runtime) touchingObject(t *target, name string) bool {
	x, y := r.mousePosition()
	if name == "_edge_" || name == "_mouse_" {
		return r.graphics.Touching(nil, display(t), name, x, y)
	}
	scene := r.prepareSensingScene(name, false)
	defer r.releaseSensingScene(scene)
	return r.graphics.Touching(scene, display(t), name, x, y)
}

func finite(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0)
}

func clamp(n, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, n))
}

func validStage(w, h float64) bool {
	return finite(w) && finite(h) && w >= 1 && h >= 1 && w <= 4096 && h <= 4096 && w*h <= 16<<20
}

var runtimeJSONToken = regexp.MustCompile("\"(?:[^\"\\\\]|\\\\.)*\"|-?Infinity|NaN")

func (r *Runtime) configureProject(p *project.Project) {
	for _, t := range p.Targets {
		ids := make([]string, 0, len(t.Comments))
		for id := range t.Comments {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			s := t.Comments[id].Text
			if !strings.Contains(s, "_twconfig_") {
				continue
			}
			start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
			if start < 0 || end < start {
				continue
			}
			var c struct {
				HighQualityPen *bool    `json:"hq"`
				FPS            *float64 `json:"framerate"`
				Width, Height  *float64
				Runtime        struct {
					Fencing    *bool
					MiscLimits *bool
					MaxClones  json.RawMessage
				} `json:"runtimeOptions"`
			}
			data := runtimeJSONToken.ReplaceAllStringFunc(s[start:end+1], func(token string) string {
				if token[0] == '"' {
					return token
				}
				return "null"
			})
			if json.Unmarshal([]byte(data), &c) != nil {
				continue
			}
			if c.FPS != nil && finite(*c.FPS) && *c.FPS <= 1000 {
				r.fps = *c.FPS
				r.fpsOverride = true
			}
			if c.Width != nil && c.Height != nil && validStage(*c.Width, *c.Height) {
				r.options.View.Width, r.options.View.Height = *c.Width, *c.Height
			}
			if c.Runtime.Fencing != nil {
				r.options.Fencing = *c.Runtime.Fencing
			}
			if c.HighQualityPen != nil {
				r.options.HighQualityPen = *c.HighQualityPen
			}
			if c.Runtime.MiscLimits != nil {
				r.options.MiscLimits = *c.Runtime.MiscLimits
			}
			if c.Runtime.MaxClones != nil {
				var limit float64
				if string(c.Runtime.MaxClones) == "null" {
					limit = 1e9
				} else if json.Unmarshal(c.Runtime.MaxClones, &limit) != nil {
					limit = 300
				}
				r.options.CloneLimit = int(clamp(math.Floor(limit), -1, 1e9))
			}
			return
		}
	}
}

func (r *Runtime) graphicsReporter(th *thread, b *project.Block, input func(string) (any, error)) (any, bool, error) {
	op := b.Opcode
	t := th.target
	switch op {
	case "motion_xscroll", "motion_yscroll", "sensing_userid":
		return undefinedValue{}, true, nil
	case "sensing_touchingobject", "sensing_touchingcolor", "sensing_coloristouchingcolor":
		key := "COLOR"
		if op == "sensing_touchingobject" {
			key = "TOUCHINGOBJECTMENU"
		}
		v, e := input(key)
		if e != nil {
			return nil, true, e
		}
		var mask *color.NRGBA
		if op == "sensing_coloristouchingcolor" {
			c := ParseColor(v)
			mask = &c
			v, e = input("COLOR2")
			if e != nil {
				return nil, true, e
			}
		}
		if r.graphics == nil {
			return nil, true, fmt.Errorf("%s requires costume geometry", op)
		}
		if op == "sensing_touchingobject" {
			return r.touchingObject(t, text(v)), true, nil
		}
		scene := r.prepareSensingScene("", true)
		defer r.releaseSensingScene(scene)
		return r.graphics.TouchingColor(scene, display(t), ParseColor(v), mask), true, nil
	}
	return nil, false, nil
}

func (r *Runtime) enabled(name string) bool {
	switch name {
	case "turbo mode":
		return r.turbo
	case "interpolation":
		return r.options.Interpolation
	case "remove fencing":
		return !r.options.Fencing
	case "remove misc limits":
		return !r.options.MiscLimits
	case "high quality pen":
		return r.options.HighQualityPen
	}
	return false
}

func (r *Runtime) graphicsStatement(th *thread, b *project.Block, input func(string) (any, error)) (bool, error) {
	op := b.Opcode
	t := th.target
	num := func(key string) (float64, error) {
		v, e := input(key)
		return number(v), e
	}
	if strings.HasPrefix(op, "pen_") {
		return r.penStatement(t, b, input)
	}
	switch op {
	case "motion_ifonedgebounce":
		return true, r.bounce(t)
	case "motion_pointtowards_xy", "motion_pointtowards_xyfrom":
		x, e := num("X")
		if e != nil {
			return true, e
		}
		y, e := num("Y")
		if e != nil {
			return true, e
		}
		fromX, fromY := t.state.X, t.state.Y
		if op == "motion_pointtowards_xyfrom" {
			fromX, e = num("FROMX")
			if e != nil {
				return true, e
			}
			fromY, e = num("FROMY")
			if e != nil {
				return true, e
			}
		}
		t.setDirection(90 - math.Atan2(y-fromY, x-fromX)*180/math.Pi)
		return true, nil
	}
	return false, nil
}

func (r *Runtime) bounce(t *target) error {
	if t.state.IsStage {
		return nil
	}
	if r.graphics == nil {
		return fmt.Errorf("edge bounce requires costume geometry")
	}
	box, ok := r.graphics.Bounds(display(t), true)
	if !ok {
		return nil
	}
	w, h := r.View().Width/2, r.View().Height/2
	distances := []float64{
		math.Max(0, w+box.Left),
		math.Max(0, h-box.Top),
		math.Max(0, w-box.Right),
		math.Max(0, h+box.Bottom),
	}
	edge := 0
	for i := 1; i < 4; i++ {
		if distances[i] < distances[edge] {
			edge = i
		}
	}
	if distances[edge] > 0 {
		return nil
	}
	angle := (90 - t.state.Direction) * math.Pi / 180
	dx, dy := math.Cos(angle), -math.Sin(angle)
	switch edge {
	case 0:
		dx = math.Max(.2, math.Abs(dx))
	case 1:
		dy = math.Max(.2, math.Abs(dy))
	case 2:
		dx = -math.Max(.2, math.Abs(dx))
	case 3:
		dy = -math.Max(.2, math.Abs(dy))
	}
	t.setDirection(math.Atan2(dy, dx)*180/math.Pi + 90)
	box, ok = r.graphics.Bounds(display(t), true)
	if !ok {
		return nil
	}
	sx, sy := 0.0, 0.0
	if box.Left < -w {
		sx += -w - box.Left
	}
	if box.Right > w {
		sx += w - box.Right
	}
	if box.Top > h {
		sy += h - box.Top
	}
	if box.Bottom < -h {
		sy += -h - box.Bottom
	}
	t.setXY(t.state.X+sx, t.state.Y+sy)
	return nil
}
