package desktop

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"testing"

	"gowarp/app/config"
	"gowarp/app/ui"
	"gowarp/core/vm"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestGPUTextCacheIsBoundedAndReleasedOnResize(t *testing.T) {
	runGPUFrame(t, func() {
		g := &game{}
		defer g.close()
		dst := ebiten.NewImage(320, 100)
		defer dst.Deallocate()
		for _, scale := range []float64{1, 1.25, 1.5, 2} {
			face := g.scaledFace(scale, true)
			for i := 0; i < 140; i++ {
				g.texts.draw(dst, face, 5, 40, 1, 1, fmt.Sprint(i), color.White)
			}
			if len(g.texts.entries) > 128 || g.texts.bytes > 8<<20 {
				t.Fatal("changing monitor values grew the text cache beyond its budget")
			}
			for key := range g.texts.entries {
				if key.face != face {
					t.Fatal("resizing retained an obsolete font face")
				}
			}
		}
		g.texts.clear()
		if len(g.texts.entries) != 0 || g.texts.bytes != 0 {
			t.Fatal("text textures were not released")
		}
	})
}

func TestGPUOverlayClipsMonitorsAtNativeDPI(t *testing.T) {
	runGPUFrame(t, func() {
		face := ui.Font(28)
		defer face.Close()
		dst := ebiten.NewImage(960, 720)
		defer dst.Deallocate()
		dst.Fill(color.RGBA{20, 40, 60, 255})
		var texts gpuTextCache
		defer texts.clear()
		painter := newGPUOverlayPainter(dst, face, 480, 360, &texts)
		drawMonitorsAt(painter, []vm.MonitorState{
			{Visible: true, Mode: "large", Value: "123", X: 10, Y: 10},
			{Visible: true, Mode: "large", Value: "edge", X: 470, Y: 340},
		}, 0, 480, 360)
		pixels := image.NewRGBA(dst.Bounds())
		dst.ReadPixels(pixels.Pix)
		if pixels.RGBAAt(20, 20) != monitorOrange || pixels.RGBAAt(19, 20) != (color.RGBA{20, 40, 60, 255}) || pixels.RGBAAt(20, 64) != (color.RGBA{20, 40, 60, 255}) {
			t.Fatal("GPU monitor geometry does not follow logical stage coordinates")
		}
		if pixels.RGBAAt(959, 719) != monitorOrange {
			t.Fatal("monitor at stage edge was not clipped to the viewport")
		}
		for y := 22; y < 60; y += 2 {
			for x := 22; x < 96; x += 2 {
				pixel := pixels.RGBAAt(x, y)
				if pixel != pixels.RGBAAt(x+1, y) || pixel != pixels.RGBAAt(x, y+1) {
					return
				}
			}
		}
		t.Fatal("GPU overlay text lost native DPI detail")
	})
}

func TestGPUPlayerDrawPreservesQuestionAfterResize(t *testing.T) {
	runGPUFrame(t, func() {
		c := cadenceController(t, 30, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"ask"},"ask":{"opcode":"sensing_askandwait","inputs":{"QUESTION":[1,[10,"Name?"]]}}}`)
		s := config.Default(nil)
		s.Player.Controls = false
		c.options.Settings = &s
		if err := c.vm.Run(context.Background(), 1, 30, false); err != nil {
			t.Fatal(err)
		}
		g := &game{controller: c, face: ui.Font(14)}
		defer g.face.Close()
		defer g.close()
		for _, size := range []image.Point{image.Pt(480, 360), image.Pt(960, 720), image.Pt(720, 360)} {
			dst := ebiten.NewImage(size.X, size.Y)
			g.Draw(dst)
			if g.drawErr != nil {
				t.Fatal(g.drawErr)
			}
			pixels := image.NewRGBA(dst.Bounds())
			dst.ReadPixels(pixels.Pix)
			dst.Deallocate()
			stage := g.stageBounds(size.X, size.Y)
			if pixels.RGBAAt(stage.Max.X-4, stage.Max.Y-4) != (color.RGBA{255, 255, 255, 255}) {
				t.Fatalf("question panel missing after resizing to %v", size)
			}
			if c.paintedStage != nil {
				t.Fatal("packaged player still rasterizes the full stage on the CPU")
			}
		}
	})
}

func TestPlayerStartOverlayKeepsStageVisible(t *testing.T) {
	runGPUFrame(t, func() {
		c := cadenceController(t, 30, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true}}`)
		s := config.Default(nil)
		s.Player.AutoStart = false
		s.Player.Controls = false
		c.options.Settings = &s
		c.vm.Stop()
		g := &game{controller: c, waitingStart: true, face: ui.Font(14)}
		defer g.face.Close()
		defer g.close()
		for _, fit := range []string{"preserve", "stretch"} {
			s.Window.Fit = fit
			dst := ebiten.NewImage(720, 360)
			g.drawPlayer(dst)
			if g.drawErr != nil {
				t.Fatal(g.drawErr)
			}
			pixels := image.NewRGBA(dst.Bounds())
			dst.ReadPixels(pixels.Pix)
			stage := g.stageBounds(720, 360)
			corner := pixels.RGBAAt(stage.Min.X+5, stage.Min.Y+5)
			if corner.R < 185 || corner.R > 195 || corner.G != corner.R || corner.B != corner.R {
				t.Fatalf("start overlay hid the white stage instead of dimming it: %v", corner)
			}
			center := image.Pt((stage.Min.X+stage.Max.X)/2, (stage.Min.Y+stage.Max.Y)/2)
			green := 0
			for y := center.Y - 24; y < center.Y+24; y++ {
				for x := center.X - 24; x < center.X+24; x++ {
					c := pixels.RGBAAt(x, y)
					if c.G > 120 && int(c.G) > int(c.R)+40 && int(c.G) > int(c.B)+40 {
						green++
					}
				}
			}
			if green < 100 {
				t.Fatal("centered green flag is missing")
			}
			if edge := pixels.RGBAAt(center.X, center.Y-39); edge.R < 230 {
				t.Fatal("circular button outline is missing")
			}
			g.waitingStart = false
			g.drawPlayer(dst)
			dst.ReadPixels(pixels.Pix)
			if pixels.RGBAAt(stage.Min.X+5, stage.Min.Y+5) != (color.RGBA{255, 255, 255, 255}) {
				t.Fatal("start overlay remained after starting")
			}
			g.waitingStart = true
			dst.Deallocate()
		}
	})
}

func TestGPUOverlayTextFollowsCanvasAspect(t *testing.T) {
	runGPUFrame(t, func() {
		face := ui.Font(28)
		defer face.Close()
		var texts gpuTextCache
		defer texts.clear()
		base := ebiten.NewImage(128, 128)
		defer base.Deallocate()
		tall := ebiten.NewImage(128, 256)
		defer tall.Deallocate()
		newGPUOverlayPainter(base, face, 64, 64, &texts).text(4, 20, "Canvas", color.White)
		newGPUOverlayPainter(tall, face, 64, 64, &texts).text(4, 20, "Canvas", color.White)
		a, b := image.NewRGBA(base.Bounds()), image.NewRGBA(tall.Bounds())
		base.ReadPixels(a.Pix)
		tall.ReadPixels(b.Pix)
		visible := 0
		for y := 0; y < 128; y++ {
			for x := 0; x < 128; x++ {
				want := a.RGBAAt(x, y)
				if want.A != 0 {
					visible++
				}
				if b.RGBAAt(x, y*2) != want || b.RGBAAt(x, y*2+1) != want {
					t.Fatalf("text proportions changed at %d,%d", x, y)
				}
			}
		}
		if visible == 0 {
			t.Fatal("text was not drawn")
		}
	})
}
