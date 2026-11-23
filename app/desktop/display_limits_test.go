package desktop

import (
	"context"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"gowarp/app/config"
	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestDisplayLogsInitialSize(t *testing.T) {
	s := config.Default(nil)
	s.Player.Controls = false
	c := cadenceController(t, 60, `{}`)
	c.options.Settings = &s
	var messages []string
	g := &game{controller: c, options: Options{Diagnostic: func(message string) {
		messages = append(messages, message)
	}}}
	g.logDisplaySize(image.Pt(960, 720))
	g.logDisplaySize(image.Pt(960, 720))
	if len(messages) != 1 || !strings.Contains(messages[0], "render_size=960x720") || !strings.Contains(messages[0], "render_fps_limit=0") {
		t.Fatalf("initial display size is missing or logged every frame: %v", messages)
	}
}

func TestDisplayCadenceDoesNotChangeSimulation(t *testing.T) {
	t.Run("limited", func(t *testing.T) { testDisplayCadence(t, 30) })
	t.Run("unlimited", func(t *testing.T) { testDisplayCadence(t, 0) })
}

func testDisplayCadence(t *testing.T, renderFPS int) {
	s := config.Default(nil)
	s.FPS = 60
	s.Player.RenderFPS = renderFPS
	c := cadenceController(t, 60, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
		"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"move"]}},
		"move":{"opcode":"data_changevariableby","next":"draw","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"-1"]]}},
		"draw":{"opcode":"pen_clear"}
	}`)
	c.options.Settings = &s
	g := &game{controller: c, ctx: context.Background()}
	now := time.Unix(1, 0)
	size := image.Pt(960, 720)
	count := 0
	for i := 0; i < 60; i++ {
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
		if g.shouldPresent(now.Add(time.Duration(i)*time.Second/60), size) {
			count++
		}
	}
	wantPresentations := 30
	if renderFPS == 0 {
		wantPresentations = 60
	}
	if count != wantPresentations || c.vm.FPS() != 60 || c.vm.Frames() != 60 || c.vm.Snapshot()[0].Variables["score"].Value != float64(-60) {
		t.Fatalf("presentations=%d logic FPS=%g frames=%d movement=%v", count, c.vm.FPS(), c.vm.Frames(), c.vm.Snapshot()[0].Variables["score"].Value)
	}
	if !g.shouldPresent(now.Add(time.Second), image.Pt(1280, 720)) {
		t.Fatal("resize did not redraw")
	}
	g.focusDraws = 1
	if !g.shouldPresent(now.Add(time.Second+time.Millisecond), size) {
		t.Fatal("focus did not redraw")
	}
	g.focusDraws = 0
	g.waitingStart = true
	if !g.shouldPresent(now.Add(time.Second+2*time.Millisecond), size) {
		t.Fatal("start overlay did not redraw")
	}
	g.limitReached = true
	if !g.shouldPresent(now.Add(time.Second+3*time.Millisecond), size) {
		t.Fatal("capture skipped")
	}
}

func TestCanvasAndHighQualityPenKeepRequestedResolution(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 128, Height: 96, Zoom: 100, Direction: 90, HighQualityPen: true, Canvas: vm.CanvasEffects{RenderWidth: 512, RenderHeight: 384}})
		gpu := stageGPU{}
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.Pen(vm.PenCommand{Kind: "line", Size: 4, Line: [4]float64{-20, 0, 20, 0}, Color: color.NRGBA{R: 255, A: 255}})
		dst := ebiten.NewImage(64, 48)
		defer dst.Deallocate()
		if err := gpu.Draw(dst, r, nil); err != nil {
			t.Fatal(err)
		}
		if pen.layerSize() != image.Pt(512, 384) || pen.image.Bounds().Size() != image.Pt(512, 384) {
			t.Fatalf("high-quality pen lost requested canvas resolution: %v", pen.layerSize())
		}
		if gpu.canvas.frame == nil || gpu.canvas.frame.Bounds().Size() != image.Pt(512, 384) {
			t.Fatal("custom canvas lost requested resolution")
		}
		if w, h := r.Size(); w != 128 || h != 96 {
			t.Fatal("canvas resolution changed logical stage size")
		}
		pixels := make([]byte, 64*48*4)
		dst.ReadPixels(pixels)
		i := (24*64 + 32) * 4
		if pixels[i] < 240 || pixels[i+1] > 10 || pixels[i+2] > 10 {
			t.Fatalf("pen line was lost when scaling: %v", pixels[i:i+4])
		}
	})
}

func TestPlayerRendersAtWindowResolution(t *testing.T) {
	runGPUFrame(t, func() {
		s := config.Default(nil)
		s.Player.Controls = false
		c := cadenceController(t, 60, `{}`)
		c.options.Settings = &s
		g := &game{controller: c}
		defer g.close()
		screen := ebiten.NewImage(960, 720)
		defer screen.Deallocate()
		for i := 0; i < 2; i++ {
			g.drawPlayer(screen)
			if g.drawErr != nil {
				t.Fatal(g.drawErr)
			}
			if got := g.stageCanvas.Bounds().Size(); got != image.Pt(960, 720) {
				t.Fatalf("canvas did not use the window resolution: %v", got)
			}
		}
	})
}

func TestDisplayFPSAppliesWithoutQualitySelector(t *testing.T) {
	s := config.Default(nil)
	s.Player.RenderFPS = 30
	c := cadenceController(t, 60, `{}`)
	c.options.Settings = &s
	g := &game{controller: c}
	now := time.Unix(1, 0)
	if !g.shouldPresent(now, image.Pt(960, 720)) || g.shouldPresent(now.Add(time.Second/60), image.Pt(960, 720)) {
		t.Fatal("display FPS was ignored without a manual quality selector")
	}
}

func TestCaptureIgnoresDisplayFPS(t *testing.T) {
	s := config.Default(nil)
	s.Player.RenderFPS = 30
	c := cadenceController(t, 60, `{}`)
	c.options.Settings = &s
	for _, options := range []Options{{Replay: true}, {Screenshot: "capture.png"}} {
		g := &game{controller: c, options: options}
		now := time.Unix(1, 0)
		for i := 0; i < 60; i++ {
			if !g.shouldPresent(now.Add(time.Duration(i)*time.Second/60), image.Pt(960, 720)) {
				t.Fatal("capture skipped a presentation")
			}
		}
	}
}

func TestSlowPresentationsKeepCanvasResolution(t *testing.T) {
	runGPUFrame(t, func() {
		s := config.Default(nil)
		s.Player.Controls = false
		c := cadenceController(t, 60, `{
   "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
   "loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"draw"]}},
   "draw":{"opcode":"pen_clear"}
  }`)
		c.options.Settings = &s
		g := &game{controller: c, ctx: context.Background()}
		defer g.close()
		screen := ebiten.NewImage(960, 720)
		defer screen.Deallocate()
		/*
			Sustained presentation delays used to lower the resolution even when
			simulation remained cheap. Exercise the actual Draw path throughout.
		*/
		for i := 0; i < 85; i++ {
			if err := g.advanceController(Input{}); err != nil {
				t.Fatal(err)
			}
			g.draw(screen, true)
			if g.drawErr != nil {
				t.Fatal(g.drawErr)
			}
			if got := g.stageCanvas.Bounds().Size(); got != image.Pt(960, 720) {
				t.Fatalf("slow presentation %d changed canvas resolution: %v", i, got)
			}
			time.Sleep(80 * time.Millisecond)
		}
	})
}

func TestPlayerResolutionTracksWindowWithoutChangingCoordinates(t *testing.T) {
	runGPUFrame(t, func() {
		for _, highQuality := range []bool{false, true} {
			for _, fit := range []string{"preserve", "stretch"} {
				s := config.Default(nil)
				s.Player.Controls = false
				s.Window.Fit = fit
				c := cadenceController(t, 60, "{}")
				c.options.Settings = &s
				if err := c.vm.Configure(60, false, false, 100, 100, "player", "#ffffff"); err != nil {
					t.Fatal(err)
				}
				c.vm.ConfigureRendering(highQuality, false)
				g := &game{controller: c}
				defer g.close()
				pen, err := g.gpu.enablePen(c.renderer)
				if err != nil {
					t.Fatal(err)
				}
				pen.Pen(vm.PenCommand{Kind: "line", Size: 30, Line: [4]float64{-20, 0, 20, 0}, Color: color.NRGBA{R: 255, A: 255}})
				for _, display := range []struct {
					width, height, dpi   float64
					preserved, stretched image.Point
					penSize              int
				}{
					{10, 10, 1, image.Pt(10, 10), image.Pt(10, 10), 100},
					{200, 100, 1, image.Pt(100, 100), image.Pt(200, 100), 100},
					{200, 200, 1, image.Pt(200, 200), image.Pt(200, 200), 200},
					{200, 150, 2, image.Pt(300, 300), image.Pt(400, 300), 300},
					{10, 10, 1, image.Pt(10, 10), image.Pt(10, 10), 100},
				} {
					w, h := g.layoutScale(display.width, display.height, display.dpi)
					screen := ebiten.NewImage(int(w), int(h))
					g.drawPlayer(screen)
					if g.drawErr != nil {
						t.Fatal(g.drawErr)
					}
					want := display.preserved
					if fit == "stretch" {
						want = display.stretched
					}
					if got := g.stageCanvas.Bounds().Size(); got != want {
						t.Fatalf("fit=%s dpi=%g canvas=%v, want %v", fit, display.dpi, got, want)
					}
					penSize := 100
					if highQuality {
						penSize = display.penSize
					}
					if got := pen.layerSize(); got != image.Pt(penSize, penSize) {
						t.Fatalf("highQuality=%t pen=%v, want %d squared", highQuality, got, penSize)
					}
					canvas := g.stageCanvas
					g.drawPlayer(screen)
					if g.stageCanvas != canvas {
						t.Fatal("unchanged resolution reallocated the canvas")
					}
					stage := g.stageBounds(int(w), int(h))
					x, y := (stage.Min.X+stage.Max.X)/2, (stage.Min.Y+stage.Max.Y)/2
					sx, sy := g.inputPosition(x, y)
					if sx != 50 || sy != 50+ToolbarHeight {
						t.Fatalf("input no longer maps to logical stage: %g,%g", sx, sy)
					}
					pixels := make([]byte, int(w*h)*4)
					screen.ReadPixels(pixels)
					i := (y*int(w) + x) * 4
					if pixels[i] < 240 || pixels[i+1] > 10 || pixels[i+2] > 10 {
						t.Fatalf("pen line lost after resize: %v", pixels[i:i+4])
					}
					screen.Deallocate()
					if rw, rh := c.renderer.Size(); rw != 100 || rh != 100 {
						t.Fatal("display scaling changed logical stage dimensions")
					}
				}
			}
		}
	})
}

func TestPlayerCanvasShrinkKeepsThinPenLines(t *testing.T) {
	runGPUFrame(t, func() {
		s := config.Default(nil)
		s.Player.Controls = false
		c := cadenceController(t, 60, "{}")
		c.options.Settings = &s
		if err := c.vm.Configure(60, false, false, 8, 8, "player", "#000000"); err != nil {
			t.Fatal(err)
		}
		for x := -4.0; x < 4; x += 2 {
			c.renderer.Pen(vm.PenCommand{Kind: "line", Size: 1, Line: [4]float64{x, -4, x, 4}, Color: color.NRGBA{R: 255, A: 255}})
		}
		g := &game{controller: c}
		defer g.close()
		for _, size := range []int{4, 16} {
			screen := ebiten.NewImage(size, size)
			g.drawPlayer(screen)
			if g.drawErr != nil {
				t.Fatal(g.drawErr)
			}
			got := image.NewRGBA(screen.Bounds())
			screen.ReadPixels(got.Pix)
			screen.Deallocate()
			if size == 4 {
				pixel := got.RGBAAt(1, 2)
				if pixel.R < 126 || pixel.R > 129 || pixel.G != 0 || pixel.B != 0 || pixel.A != 255 {
					t.Fatalf("shrinking discarded thin pen lines: %v", pixel)
				}
			} else if got.RGBAAt(4, 8).R != 255 || got.RGBAAt(6, 8).R != 0 {
				t.Fatal("enlarging blurred pen pixels")
			}
		}
		for _, global := range []bool{false, true} {
			view := c.renderer.DisplayView()
			view.Canvas.Nearest = !global
			view.NoAntialias = global
			c.renderer.SetView(view)
			screen := ebiten.NewImage(4, 4)
			g.drawPlayer(screen)
			got := image.NewRGBA(screen.Bounds())
			screen.ReadPixels(got.Pix)
			screen.Deallocate()
			if pixel := got.RGBAAt(1, 2); pixel != (color.RGBA{A: 255}) {
				t.Fatalf("pixelated rendering (global=%t) was filtered: %v", global, pixel)
			}
		}
	})
}
