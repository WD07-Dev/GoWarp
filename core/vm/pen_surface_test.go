package vm

import (
	"math"
	"testing"

	"gowarp/core/extension"
	"gowarp/core/project"
)

type surfaceRecorder struct {
	Graphics
	commands []PenCommand
}

func (s *surfaceRecorder) Pen(command PenCommand) {
	s.commands = append(s.commands, command)
}

func surfaceTriangle() extension.SurfaceTriangle {
	return extension.SurfaceTriangle{Vertices: [3]extension.TriangleVertex{
		{X: -10, Y: 10, R: 1, G: 1, B: 1, A: 1, W: 1},
		{X: 10, Y: -10, U: 1, V: 1, R: 1, G: 1, B: 1, A: 1, W: 1},
		{X: 10, Y: 10, U: 1, R: 1, G: 1, B: 1, A: 1, W: 1},
	}}
}

func TestSurfaceTriangleSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name    string
		texture extension.SurfaceTexture
	}{
		{"solid", extension.SurfaceTexture{}},
		{"surface", extension.SurfaceTexture{Kind: "surface", Surface: "texture"}},
		{"empty surface", extension.SurfaceTexture{Kind: "surface"}},
		{"costume", extension.SurfaceTexture{Kind: "costume", Costume: 1, Target: "_stage_"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := fixture(t, "{}")
			r.stage.source.Costumes = []project.Costume{{Name: "first"}, {Name: "texture"}}
			r.visual.surfaces = map[string][2]int{"texture": {10, 10}, "": {10, 10}}
			recorder := &surfaceRecorder{}
			r.graphics = recorder
			c := &extensionContext{r: r, th: &thread{target: r.stage}}

			triangle := surfaceTriangle()
			triangle.Texture, triangle.Linear, triangle.MultiplyTextureAlpha = tc.texture, true, true
			if _, err := c.Call("graphics.surface.triangle", StageSurface, triangle); err != nil {
				t.Fatal(err)
			}
			if len(recorder.commands) != 1 {
				t.Fatalf("draw count = %d", len(recorder.commands))
			}
			command := recorder.commands[0]
			got := command.Surface
			if command.Kind != "surface-triangle" || got.Name != StageSurface || !got.Linear || !got.MultiplyTextureAlpha {
				t.Fatalf("incorrect draw options: %+v", command)
			}
			if got.Textured != (tc.texture.Kind != "") || got.Texture != tc.texture.Surface || (got.Costume != nil) != (tc.texture.Kind == "costume") {
				t.Fatalf("incorrect texture source: %+v", got)
			}
			triangle.Vertices[0].X = 999
			r.stage.state.CurrentCostume = 0
			if got.Vertices[0].X != -10 || (got.Costume != nil && got.Costume.State.CurrentCostume != 1) {
				t.Fatal("submitted triangle retained mutable inputs")
			}
		})
	}
}

func TestSurfaceTriangleNumbers(t *testing.T) {
	for field := 0; field < 10; field++ {
		for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.Copysign(0, -1), -1, 0, 1} {
			r := fixture(t, "{}")
			recorder := &surfaceRecorder{}
			r.graphics = recorder
			c := &extensionContext{r: r, th: &thread{target: r.stage}}

			triangle := surfaceTriangle()
			v := &triangle.Vertices[0]
			fields := []*float64{&v.X, &v.Y, &v.U, &v.V, &v.R, &v.G, &v.B, &v.A, &v.Z, &v.W}
			*fields[field] = value

			if _, err := c.Call("graphics.surface.triangle", StageSurface, triangle); err != nil {
				t.Fatal(err)
			}
			want := value
			if math.IsNaN(want) {
				want = 0
			}
			if math.IsInf(want, 0) || (field == 9 && want <= 0) {
				if len(recorder.commands) != 0 {
					t.Fatalf("invalid vertex was drawn: field=%d value=%v", field, value)
				}
				continue
			}
			if len(recorder.commands) != 1 {
				t.Fatalf("valid vertex was skipped: field=%d value=%v", field, value)
			}
			got := recorder.commands[0].Surface.Vertices[0]
			values := [...]float64{got.X, got.Y, got.U, got.V, got.R, got.G, got.B, got.A, got.Z, got.W}
			if math.Float64bits(values[field]) != math.Float64bits(want) {
				t.Fatalf("field %d = %v, want %v", field, values[field], want)
			}
		}
	}
}

func TestSurfaceTriangleSourcesAndTargets(t *testing.T) {
	for _, tc := range []struct {
		name, destination   string
		texture             extension.SurfaceTexture
		current             bool
		wantDraw, wantError bool
	}{
		{"missing destination", "missing", extension.SurfaceTexture{}, true, false, false},
		{"missing source", StageSurface, extension.SurfaceTexture{Kind: "surface", Surface: "missing"}, true, false, false},
		{"self source", "texture", extension.SurfaceTexture{Kind: "surface", Surface: "texture"}, true, false, false},
		{"stage source", "texture", extension.SurfaceTexture{Kind: "surface", Surface: StageSurface}, true, true, false},
		{"costume nan", StageSurface, extension.SurfaceTexture{Kind: "costume", Costume: math.NaN()}, true, true, false},
		{"costume fractional", StageSurface, extension.SurfaceTexture{Kind: "costume", Costume: .5}, true, false, false},
		{"costume negative", StageSurface, extension.SurfaceTexture{Kind: "costume", Costume: -1}, true, false, false},
		{"costume bounds", StageSurface, extension.SurfaceTexture{Kind: "costume", Costume: 1}, true, false, false},
		{"costume infinite", StageSurface, extension.SurfaceTexture{Kind: "costume", Costume: math.Inf(1)}, true, false, false},
		{"unknown target", StageSurface, extension.SurfaceTexture{Kind: "costume", Target: "missing"}, true, false, true},
		{"unknown kind", StageSurface, extension.SurfaceTexture{Kind: "unknown"}, true, false, true},
		{"no current target", StageSurface, extension.SurfaceTexture{}, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := fixture(t, "{}")
			r.stage.source.Costumes = []project.Costume{{Name: "texture"}}
			r.visual.surfaces = map[string][2]int{"texture": {10, 10}}
			recorder := &surfaceRecorder{}
			r.graphics = recorder
			c := &extensionContext{r: r}
			if tc.current {
				c.th = &thread{target: r.stage}
			}
			triangle := surfaceTriangle()
			triangle.Texture = tc.texture
			_, err := c.Call("graphics.surface.triangle", tc.destination, triangle)
			if (err != nil) != tc.wantError || (len(recorder.commands) > 0) != tc.wantDraw {
				t.Fatalf("draws=%d error=%v", len(recorder.commands), err)
			}
		})
	}
}

func TestSurfaceTriangleArguments(t *testing.T) {
	r := fixture(t, "{}")
	recorder := &surfaceRecorder{}
	r.graphics = recorder
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	for _, args := range [][]any{
		{StageSurface},
		{StageSurface, nil},
		{StageSurface, true},
		{StageSurface, map[string]any{}},
		{StageSurface, surfaceTriangle(), true},
	} {
		if _, err := c.Call("graphics.surface.triangle", args...); err == nil {
			t.Fatalf("accepted malformed triangle: %#v", args)
		}
	}
	if len(recorder.commands) != 0 {
		t.Fatal("invalid arguments emitted drawing commands")
	}
}

func TestPenSurfaceRegistration(t *testing.T) {
	r := fixture(t, "{}")
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	if _, err := c.Call("graphics.surface.create", "warning", 480, 360); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"warning", "Scratch Stage"} {
		got, err := c.Call("graphics.surface.exists", name)
		if err != nil || got != true {
			t.Fatalf("surface %q exists = %v, error = %v", name, got, err)
		}
	}
}

func TestSurfaceAPIUsesExplicitColorsAndTextures(t *testing.T) {
	r := fixture(t, "{}")
	r.stage.source.Costumes = []project.Costume{{Name: "first"}, {Name: "texture"}}
	r.stage.state.Text = &TextSkin{Visible: true}
	r.stage.state.Pen = PenState{Hue: 0, Saturation: 100, Brightness: 100}
	recorder := &surfaceRecorder{}
	r.graphics = recorder
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	if _, err := c.Call("graphics.surface.triangle", StageSurface, surfaceTriangle()); err != nil {
		t.Fatal(err)
	}
	if got := recorder.commands[0].Surface.Vertices[0]; got.R != 1 || got.G != 1 || got.B != 1 {
		t.Fatalf("generic drawing inherited pen color: %+v", got)
	}
	triangle := surfaceTriangle()
	triangle.Texture = extension.SurfaceTexture{Kind: "costume", Costume: 1}
	if _, err := c.Call("graphics.surface.triangle", StageSurface, triangle); err != nil {
		t.Fatal(err)
	}
	if len(recorder.commands) != 2 || recorder.commands[1].Surface.Costume.State.CurrentCostume != 1 {
		t.Fatal("explicit costume source was not captured")
	}
	if r.stage.state.CurrentCostume != 0 || !r.stage.state.Text.Visible {
		t.Fatal("sampling a costume changed the target")
	}
	if index, err := c.Call("target.costumeIndex", "texture"); err != nil || index != float64(1) {
		t.Fatalf("costume lookup = %v, %v", index, err)
	}
	if index, err := c.Call("target.costumeIndex", "missing"); err != nil || index != float64(-1) {
		t.Fatalf("missing costume lookup = %v, %v", index, err)
	}
	color, err := c.Call("graphics.penColor")
	if err != nil {
		t.Fatal(err)
	}
	if rgba := color.([]float64); rgba[0] != 1 || rgba[1] != 0 || rgba[2] != 0 || rgba[3] != 1 {
		t.Fatalf("pen color = %v", rgba)
	}
	if _, err := c.Call("target.set", "currentCostume", 1); err != nil {
		t.Fatal(err)
	}
	if r.stage.state.CurrentCostume != 1 || r.stage.state.Text.Visible {
		t.Fatal("explicit costume selection did not update the target")
	}
}

func TestPenSurfaceRecreationAndLimits(t *testing.T) {
	r := fixture(t, "{}")
	recorder := &surfaceRecorder{}
	r.graphics = recorder
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	call := func(op string, args ...any) {
		t.Helper()
		if _, err := c.Call("graphics.surface."+op, args...); err != nil {
			t.Fatal(err)
		}
	}
	call("create", "warning", 100, 100)
	call("create", "warning", 20, 30)
	if r.visual.surfacePixels != 600 {
		t.Fatalf("replacement retained old dimensions: %d pixels", r.visual.surfacePixels)
	}
	before := len(recorder.commands)
	for _, size := range []float64{0, -1, 4097, math.Inf(1)} {
		if _, err := c.Call("graphics.surface.create", "warning", size, 30); err == nil {
			t.Fatalf("accepted width %v", size)
		}
	}
	if len(recorder.commands) != before || r.visual.surfacePixels != 600 {
		t.Fatal("invalid size changed a live texture")
	}
	call("clear", "warning")
	if got := recorder.commands[len(recorder.commands)-1].Kind; got != "surface-clear" {
		t.Fatalf("clear command = %q", got)
	}
	call("delete", "warning")
	if value, err := c.Call("graphics.surface.exists", "warning"); err != nil || value != false {
		t.Fatalf("deleted surface still exists: %v, %v", value, err)
	}
	if r.visual.surfacePixels != 0 {
		t.Fatal("deletion did not release the pixel budget")
	}
	call("create", "", 10, 20)
	call("reset")
	if len(r.visual.surfaces) != 0 || r.visual.surfacePixels != 0 {
		t.Fatal("reset retained render textures")
	}
}

func TestSurfaceAPIRejectsUnknownOperation(t *testing.T) {
	r := fixture(t, "{}")
	recorder := &surfaceRecorder{}
	r.graphics = recorder
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	if _, err := c.Call("graphics.surface.missing"); err == nil {
		t.Error("unknown surface operation accepted")
	}
	if len(recorder.commands) != 0 {
		t.Error("unknown surface operation emitted drawing commands")
	}
}
