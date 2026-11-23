package desktop

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"testing"

	"golang.org/x/image/font/basicfont"

	"gowarp/app/config"
	"gowarp/core/media"
	"gowarp/core/project"
)

func TestPlayerAppliesRenderingOptions(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"comments":{"c":{"text":"{\"hq\":true} // _twconfig_"}}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	on, off := true, false
	for _, highQuality := range []*bool{nil, &off, &on} {
		s := config.Default(&p)
		s.HighQualityPen = highQuality
		c := newController(&p, &media.Library{}, Options{FPS: s.FPS, Settings: &s})
		if c.initErr != nil {
			t.Fatal(c.initErr)
		}
		wantHQ := highQuality == nil || *highQuality
		view := c.renderer.DisplayView()
		if view.HighQualityPen != wantHQ || view.DisablePositionRounding {
			t.Fatalf("player did not apply rendering settings: %+v", view)
		}
	}
}

func TestWindowBackgroundDoesNotChangeStage(t *testing.T) {
	s := config.Default(nil)
	s.Window.Background = "#000000"
	p := &project.Project{Targets: []*project.Target{{IsStage: true}}}
	c := newController(p, &media.Library{}, Options{FPS: 30, Settings: &s})
	if c.initErr != nil {
		t.Fatal(c.initErr)
	}
	if c.vm.View().Color != "#ffffff" {
		t.Fatalf("window background changed stage color to %s", c.vm.View().Color)
	}
	if got := c.paint(basicfont.Face7x13).RGBAAt(0, 0); got != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("empty stage pixel=%v, want white", got)
	}
}

func TestPlayerStageSizeAndMouseCoordinates(t *testing.T) {
	for _, tc := range []struct {
		name     string
		controls bool
		fit      string
		window   image.Point
		center   image.Point
		bounds   image.Rectangle
	}{
		{name: "native controls", controls: true, fit: "preserve", window: image.Pt(640, 408), center: image.Pt(320, 228), bounds: image.Rect(0, 48, 640, 408)},
		{name: "scaled controls", controls: true, fit: "preserve", window: image.Pt(1280, 768), center: image.Pt(640, 408), bounds: image.Rect(0, 48, 1280, 768)},
		{name: "letterbox", controls: true, fit: "preserve", window: image.Pt(960, 768), center: image.Pt(480, 408), bounds: image.Rect(0, 138, 960, 678)},
		{name: "no controls", fit: "preserve", window: image.Pt(1280, 720), center: image.Pt(640, 360), bounds: image.Rect(0, 0, 1280, 720)},
		{name: "stretch", controls: true, fit: "stretch", window: image.Pt(1000, 800), center: image.Pt(500, 424), bounds: image.Rect(0, 48, 1000, 800)},
		{name: "integer fullscreen", fit: "integer", window: image.Pt(1920, 1080), center: image.Pt(960, 540), bounds: image.Rect(0, 0, 1920, 1080)},
		{name: "integer borders", fit: "integer", window: image.Pt(1600, 900), center: image.Pt(800, 450), bounds: image.Rect(160, 90, 1440, 810)},
		{name: "integer controls", controls: true, fit: "integer", window: image.Pt(1000, 800), center: image.Pt(500, 424), bounds: image.Rect(180, 244, 820, 604)},
		{name: "integer small window", fit: "integer", window: image.Pt(320, 200), center: image.Pt(160, 100), bounds: image.Rect(0, 10, 320, 190)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cadenceController(t, 30, "{}")
			s := config.Default(nil)
			s.Width, s.Height = 640, 360
			s.Window.Fit = tc.fit
			s.Player.Controls = tc.controls
			c.options.Settings = &s
			if err := c.vm.Configure(30, false, false, 640, 360, "player", "#112233"); err != nil {
				t.Fatal(err)
			}
			g := &game{controller: c}
			w, h := g.Layout(tc.window.X, tc.window.Y)
			g.windowFullscreenRead = true
			g.windowFullscreen = tc.fit == "integer"
			if w != tc.window.X || h != tc.window.Y {
				t.Fatalf("window layout=%dx%d, want %v", w, h, tc.window)
			}
			if bounds := g.stageBounds(w, h); bounds != tc.bounds {
				t.Fatalf("stage bounds=%v, want %v", bounds, tc.bounds)
			}
			buttonX, toolbar := g.toolbarPosition(tc.bounds.Min.X+12, tc.bounds.Min.Y-24)
			if toolbar != tc.controls || (toolbar && buttonX != 12) {
				t.Fatal("control hit area moved with stage scaling")
			}
			x, y := g.inputPosition(tc.center.X, tc.center.Y)
			if x != 320 || y != ToolbarHeight+180 {
				t.Fatalf("stage center maps to %g,%g", x, y)
			}
			frame := c.paint(basicfont.Face7x13)
			if frame.Bounds() != image.Rect(0, 0, 640, 360) {
				t.Fatalf("player frame includes space outside the stage: %v", frame.Bounds())
			}
			if x, y := c.renderer.FromScreen(0, 0); x != -320 || y != 180 {
				t.Fatalf("custom stage coordinates=%g,%g", x, y)
			}
			if c.vm.View().Width != 640 || c.vm.View().Height != 360 {
				t.Fatal("window resize changed stage dimensions")
			}
		})
	}
}

func TestPlayerQuestionOverlaysStage(t *testing.T) {
	p := &project.Project{Targets: []*project.Target{{
		IsStage: true,
		Blocks: project.Blocks{
			"flag": {Opcode: "event_whenflagclicked", TopLevel: true, Next: "ask"},
			"ask": {
				Opcode: "sensing_askandwait",
				Inputs: map[string][]any{"QUESTION": {float64(1), []any{float64(10), "Name?"}}},
			},
		},
	}}}
	s := config.Default(nil)
	s.Width, s.Height = 640, 360
	c := newController(p, &media.Library{}, Options{FPS: 30, Settings: &s})
	if err := c.vm.Run(context.Background(), 1, 30, false); err != nil {
		t.Fatal(err)
	}
	if c.vm.Question() == nil {
		t.Fatal("project did not ask a question")
	}
	frame := c.paint(basicfont.Face7x13)
	if frame.Bounds() != image.Rect(0, 0, 640, 360) {
		t.Fatalf("question changed stage dimensions: %v", frame.Bounds())
	}
	if pixel := frame.RGBAAt(630, 350); pixel != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("answer field is not inside the stage: %v", pixel)
	}
}

func TestPlayerOverridesProjectLimits(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"comments":{"c":{"text":"{\"runtimeOptions\":{\"fencing\":false,\"miscLimits\":false}} // _twconfig_"}}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"true", "false", "null"} {
		s := config.Default(&p)
		if err := json.Unmarshal([]byte(`{"removeFencing":`+value+`,"removeMiscLimits":`+value+`}`), &s); err != nil {
			t.Fatal(err)
		}
		c := newController(&p, &media.Library{}, Options{FPS: s.FPS, Settings: &s})
		if c.initErr != nil {
			t.Fatal(c.initErr)
		}
		wantLimits := value == "false"
		if options := c.vm.Options(); options.Fencing != wantLimits || options.MiscLimits != wantLimits {
			t.Errorf("override=%s: fencing=%v, misc=%v", value, options.Fencing, options.MiscLimits)
		}
	}
}

func TestScaledPlayerInputPreservesFractionalPosition(t *testing.T) {
	c := cadenceController(t, 30, "{}")
	s := config.Default(nil)
	s.Player.Controls = false
	c.options.Settings = &s
	g := &game{controller: c}
	g.Layout(960, 720)
	x, y := g.inputPosition(481, 361)
	if float64(x) != 240.5 || float64(y) != ToolbarHeight+180.5 {
		t.Fatalf("scaled mouse lost subpixel position: %v,%v", x, y)
	}
}

func TestInitialWindowSizeHonorsFit(t *testing.T) {
	for _, tt := range []struct {
		fit    string
		bounds image.Rectangle
		mouseX int
	}{
		{"preserve", image.Rect(120, 0, 840, 540), 630},
		{"stretch", image.Rect(0, 0, 960, 540), 680},
	} {
		t.Run(tt.fit, func(t *testing.T) {
			s := config.Default(nil)
			s.Window.Width, s.Window.Height = 960, 540
			s.Window.Fit = tt.fit
			s.Player.Controls = false
			p := &project.Project{Targets: []*project.Target{{IsStage: true}}}
			c := newController(p, &media.Library{}, Options{FPS: s.FPS, Settings: &s})
			if c.initErr != nil {
				t.Fatal(c.initErr)
			}
			w, h := playerWindowSize(&s)
			if w != 960 || h != 540 {
				t.Fatalf("initial window=%dx%d, want 960x540", w, h)
			}
			g := &game{controller: c}
			g.Layout(w, h)
			bounds := g.stageBounds(w, h)
			if bounds != tt.bounds {
				t.Fatalf("stage bounds=%v, want %v", bounds, tt.bounds)
			}
			x, y := c.renderer.DisplayPoint(100, 0, bounds.Dx(), bounds.Dy())
			if x+float64(bounds.Min.X) != float64(tt.mouseX) || y+float64(bounds.Min.Y) != 270 {
				t.Fatalf("display point does not follow fit: %g,%g in %v", x, y, bounds)
			}
			x, y = g.inputPosition(tt.mouseX, 270)
			if x != 340 || y != ToolbarHeight+180 {
				t.Fatalf("mouse does not follow fit: %g,%g", x, y)
			}
			if c.vm.View().Width != 480 || c.vm.View().Height != 360 {
				t.Fatal("display fit changed stage coordinates")
			}
		})
	}
}

func TestInitialWindowSizeScalesGameWithoutChangingStage(t *testing.T) {
	s := config.Default(nil)
	s.Window.Width, s.Window.Height = 960, 720
	s.Player.Controls = false
	p := &project.Project{Targets: []*project.Target{{IsStage: true}}}
	c := newController(p, &media.Library{}, Options{FPS: s.FPS, Settings: &s})
	if c.initErr != nil {
		t.Fatal(c.initErr)
	}
	w, h := playerWindowSize(&s)
	if w != 960 || h != 720 {
		t.Fatalf("initial window=%dx%d, want 960x720", w, h)
	}
	g := &game{controller: c}
	g.Layout(w, h)
	bounds := g.stageBounds(w, h)
	if bounds != image.Rect(0, 0, 960, 720) {
		t.Fatalf("display does not fill window: %v", bounds)
	}
	// A 100-unit displacement must occupy 200 display pixels.
	x, y := c.renderer.DisplayPoint(100, 0, bounds.Dx(), bounds.Dy())
	if x != 680 || y != 360 {
		t.Fatalf("game was not scaled: %g,%g", x, y)
	}
	x, y = g.inputPosition(680, 360)
	if x != 340 || y != ToolbarHeight+180 {
		t.Fatalf("scaled mouse mapping changed: %g,%g", x, y)
	}
	if c.vm.View().Width != 480 || c.vm.View().Height != 360 {
		t.Fatal("display enlargement changed game coordinates")
	}
	s.Player.Controls = true
	if w, h := playerWindowSize(&s); w != 960 || h != 768 {
		t.Fatalf("controls reduced requested display size: %dx%d", w, h)
	}
}

func TestPlayerAntialiasIsIndependentOfMotionAndPenQuality(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		s := config.Default(nil)
		s.Canvas.Antialias = enabled
		s.Interpolation = true
		*s.HighQualityPen = true
		p := &project.Project{Targets: []*project.Target{{IsStage: true}}}
		c := newController(p, &media.Library{}, Options{FPS: s.FPS, Settings: &s})
		if c.initErr != nil {
			t.Fatal(c.initErr)
		}
		if v := c.renderer.DisplayView(); v.NoAntialias == enabled || !v.HighQualityPen || !c.vm.Options().Interpolation {
			t.Fatalf("independent render options were not applied: %+v", v)
		}
	}
}

func TestIntegerFitUsesPhysicalPixels(t *testing.T) {
	c := cadenceController(t, 30, "{}")
	s := config.Default(nil)
	s.Player.Controls = false
	s.Window.Fit = "integer"
	c.options.Settings = &s
	if err := c.vm.Configure(30, false, false, 640, 480, "player", "#000000"); err != nil {
		t.Fatal(err)
	}
	g := &game{controller: c, windowFullscreenRead: true, windowFullscreen: true}
	for _, tc := range []struct {
		dpi        float64
		bounds     image.Rectangle
		multiplier int
	}{
		{1.25, image.Rect(180, 135, 820, 615), 1},
		{2, image.Rect(160, 120, 1440, 1080), 2},
	} {
		w, h := g.layoutScale(800, 600, tc.dpi)
		if bounds := g.stageBounds(int(w), int(h)); bounds != tc.bounds {
			t.Fatalf("dpi=%g: stage=%v, want %v", tc.dpi, bounds, tc.bounds)
		}
		x, y := g.inputPosition(tc.bounds.Min.X+400*tc.multiplier, tc.bounds.Min.Y+300*tc.multiplier)
		if x != 400 || y != 300+ToolbarHeight {
			t.Fatalf("dpi=%g: mouse=%g,%g", tc.dpi, x, y)
		}
	}
}

func TestIntegerFitOnlyRoundsFullscreen(t *testing.T) {
	c := cadenceController(t, 30, "{}")
	s := config.Default(nil)
	s.Player.Controls = false
	s.Window.Fit = "integer"
	c.options.Settings = &s
	if err := c.vm.Configure(30, false, false, 640, 480, "player", "#000000"); err != nil {
		t.Fatal(err)
	}
	g := &game{controller: c}
	w, h := g.layoutScale(800, 600, 1)
	for _, fullscreen := range []bool{false, true, false} {
		g.windowFullscreenRead = true
		g.windowFullscreen = fullscreen
		want := image.Rect(0, 0, 800, 600)
		if fullscreen {
			want = image.Rect(80, 60, 720, 540)
		}
		got := g.stageBounds(int(w), int(h))
		if got != want {
			t.Fatalf("fullscreen=%t: stage=%v, want %v", fullscreen, got, want)
		}
		x, y := g.inputPosition(want.Min.X+want.Dx()*3/4, want.Min.Y+want.Dy()*3/4)
		if x != 480 || y != 360+ToolbarHeight {
			t.Fatalf("fullscreen=%t: mouse=%g,%g", fullscreen, x, y)
		}
		if c.vm.View().Width != 640 || c.vm.View().Height != 480 {
			t.Fatal("display mode changed logical coordinates")
		}
	}
}
