package desktop

import (
	"context"
	"image"
	"image/color"
	"testing"

	"gowarp/app/config"
	"gowarp/app/ui"
	"gowarp/core/vm"
)

func TestPlayerNativeDPIPreservesLogicalInput(t *testing.T) {
	for _, tc := range []struct {
		dpi            float64
		window         image.Point
		stage          image.Rectangle
		center, button image.Point
	}{
		{1, image.Pt(640, 408), image.Rect(0, 48, 640, 408), image.Pt(320, 228), image.Pt(12, 24)},
		{1.5, image.Pt(960, 612), image.Rect(0, 72, 960, 612), image.Pt(480, 342), image.Pt(18, 36)},
		{2, image.Pt(1280, 816), image.Rect(0, 96, 1280, 816), image.Pt(640, 456), image.Pt(24, 48)},
	} {
		c := cadenceController(t, 30, "{}")
		s := config.Default(nil)
		s.Width, s.Height, s.Player.Controls = 640, 360, true
		c.options.Settings = &s
		if err := c.vm.Configure(30, false, false, 640, 360, "player", "#ffffff"); err != nil {
			t.Fatal(err)
		}
		g := &game{controller: c}
		w, h := g.layoutScale(640, 408, tc.dpi)
		if image.Pt(int(w), int(h)) != tc.window {
			t.Fatalf("DPI %g: canvas=%gx%g, want %v", tc.dpi, w, h, tc.window)
		}
		if got := g.stageBounds(int(w), int(h)); got != tc.stage {
			t.Fatalf("DPI %g: stage=%v, want %v", tc.dpi, got, tc.stage)
		}
		if x, y := g.inputPosition(tc.center.X, tc.center.Y); x != 320 || y != 216 {
			t.Fatalf("DPI %g: center input=%g,%g", tc.dpi, x, y)
		}
		if x, hit := g.toolbarPosition(tc.button.X, tc.button.Y); !hit || x != 12 {
			t.Fatalf("DPI %g: toolbar input=%d,%v", tc.dpi, x, hit)
		}
		if w, h := c.renderer.Size(); w != 640 || h != 360 {
			t.Fatalf("DPI layout changed logical stage to %dx%d", w, h)
		}
	}
}

func TestPlayerNativePaintResizesCacheAndQuestion(t *testing.T) {
	c := cadenceController(t, 30, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"ask"},"ask":{"opcode":"sensing_askandwait","inputs":{"QUESTION":[1,[10,"Name?"]]}}}`)
	s := config.Default(nil)
	c.options.Settings = &s
	if err := c.vm.Run(context.Background(), 1, 30, false); err != nil {
		t.Fatal(err)
	}

	for _, size := range []image.Point{image.Pt(480, 360), image.Pt(720, 540), image.Pt(960, 720), image.Pt(480, 360)} {
		face := ui.Font(14 * float64(size.X) / 480)
		frame := c.paintAt(face, size.X, size.Y)
		if frame.Bounds().Size() != size || c.paintedStage.Bounds().Size() != size {
			t.Fatalf("resize did not redraw stage at %v", size)
		}
		if got := frame.RGBAAt(size.X-4, size.Y-4); got != (color.RGBA{255, 255, 255, 255}) {
			t.Fatalf("question did not overlay physical stage bottom: %v", got)
		}
		cached := c.paintedStage
		c.paintAt(face, size.X, size.Y)
		face.Close()
		if c.paintedStage != cached {
			t.Fatal("unchanged physical stage was rendered again")
		}
		if w, h := c.renderer.Size(); w != 480 || h != 360 {
			t.Fatalf("native rendering changed logical stage to %dx%d", w, h)
		}
	}
}

func TestPlayerNativeLayoutPreservesFitModes(t *testing.T) {
	for _, tc := range []struct {
		fit      string
		controls bool
		bounds   image.Rectangle
		center   image.Point
	}{
		{"preserve", true, image.Rect(0, 296, 1920, 1736), image.Pt(960, 1016)},
		{"stretch", true, image.Rect(0, 72, 1920, 1960), image.Pt(960, 1016)},
		{"preserve", false, image.Rect(0, 260, 1920, 1700), image.Pt(960, 980)},
	} {
		c := cadenceController(t, 30, "{}")
		s := config.Default(nil)
		s.Window.Fit, s.Player.Controls = tc.fit, tc.controls
		c.options.Settings = &s
		g := &game{controller: c}
		w, h := g.layoutScale(1280, 1306.5, 1.5)
		if got := g.stageBounds(int(w), int(h)); got != tc.bounds {
			t.Fatalf("fit=%s controls=%v: %v != %v", tc.fit, tc.controls, got, tc.bounds)
		}
		if x, y := g.inputPosition(tc.center.X, tc.center.Y); x != 240 || y != 216 {
			t.Fatalf("center input=%g,%g", x, y)
		}
	}
	legacy := &game{}
	if w, h := legacy.layoutScale(1280, 960, 2); w != 480 || h != 448 {
		t.Fatalf("CLI layout changed: %gx%g", w, h)
	}
}

func TestNativeMonitorGeometryAndTextDetail(t *testing.T) {
	face := ui.Font(28)
	defer face.Close()
	frame := image.NewRGBA(image.Rect(0, 0, 960, 720))
	painter := newOverlayPainter(frame, face, 480, 360)
	drawMonitorsAt(painter, []vm.MonitorState{{Visible: true, Mode: "large", Value: "123", X: 10, Y: 10}}, 0, 480, 360)
	if frame.RGBAAt(20, 20) != monitorOrange || frame.RGBAAt(19, 20).A != 0 || frame.RGBAAt(20, 64).A != 0 {
		t.Fatal("monitor no longer follows logical stage coordinates")
	}
	/*
		Native font edges contain per-pixel detail within a logical 2x2 cell;
		stretching a completed logical overlay repeats every 2x2 block.
	*/
	for y := 22; y < 60; y += 2 {
		for x := 22; x < 96; x += 2 {
			pixel := frame.RGBAAt(x, y)
			if pixel != frame.RGBAAt(x+1, y) || pixel != frame.RGBAAt(x, y+1) {
				return
			}
		}
	}
	t.Fatal("monitor text is a stretched logical-resolution bitmap")
}
