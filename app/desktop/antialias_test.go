package desktop

import (
	"image"
	"image/color"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"
)

func TestGameAntialiasControlsSpritesStampsAndPen(t *testing.T) {
	runGPUFrame(t, func() {
		for _, kind := range []string{"sprite", "stamp", "line", "canvas"} {
			for _, disabled := range []bool{false, true} {
				pixels := image.NewNRGBA(image.Rect(0, 0, 2, 2))
				for y := 0; y < 2; y++ {
					pixels.SetNRGBA(0, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
					pixels.SetNRGBA(1, y, color.NRGBA{A: 255})
				}
				costume := &media.Costume{Image: pixels, Width: 2, Height: 2, CenterX: 1, CenterY: 1}
				r := render.New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
				v := vm.View{Width: 8, Height: 8, Zoom: 100, Direction: 90, Color: "#000000", HighQualityPen: true, NoAntialias: disabled}
				if kind == "canvas" {
					v.Canvas.OffsetX = 6.25
				}
				r.SetView(v)
				var gpu stageGPU
				pen, err := gpu.enablePen(r)
				if err != nil {
					t.Fatal(err)
				}
				target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 150, StretchX: 100, StretchY: 100, Direction: 90, X: .25}}
				var scene []vm.DisplayTarget
				switch kind {
				case "sprite":
					scene = []vm.DisplayTarget{target}
				case "stamp":
					pen.Pen(vm.PenCommand{Kind: "stamp", Target: target})
				case "line", "canvas":
					pen.Pen(vm.PenCommand{Kind: "line", Size: 2, Line: [4]float64{-2, -2, 2, 1}, Color: color.NRGBA{R: 255, G: 255, B: 255, A: 255}})
				}
				dst := ebiten.NewImage(8, 8)
				if err := gpu.Draw(dst, r, scene); err != nil {
					t.Fatal(err)
				}
				got := image.NewRGBA(dst.Bounds())
				dst.ReadPixels(got.Pix)
				dst.Deallocate()
				gpu.Close()
				partial, visible := 0, 0
				for i := 0; i < len(got.Pix); i += 4 {
					if got.Pix[i] != 0 {
						visible++
					}
					if got.Pix[i] != 0 && got.Pix[i] != 255 {
						partial++
					}
				}
				if visible == 0 || (disabled && partial != 0) || (!disabled && partial == 0) {
					t.Fatalf("%s disabled=%t: visible=%d intermediate colors=%d", kind, disabled, visible, partial)
				}
			}
		}
	})
}

func TestGameOverlayFontAntialias(t *testing.T) {
	runGPUFrame(t, func() {
		c := cadenceController(t, 30, "{}")
		g := &game{controller: c}
		defer g.close()
		for _, enabled := range []bool{true, false, true} {
			c.vm.ConfigureAntialias(enabled)
			face := g.scaledFace(1, true)
			dst := ebiten.NewImage(96, 40)
			g.texts.draw(dst, face, 3, 25, 1, 1, "Canvas", color.White)
			got := image.NewRGBA(dst.Bounds())
			dst.ReadPixels(got.Pix)
			dst.Deallocate()
			partial, visible := 0, 0
			for i := 3; i < len(got.Pix); i += 4 {
				if got.Pix[i] > 0 {
					visible++
				}
				if got.Pix[i] > 0 && got.Pix[i] < 255 {
					partial++
				}
			}
			if visible == 0 || (enabled && partial == 0) || (!enabled && partial != 0) {
				t.Fatalf("font antialias=%t: visible=%d partial=%d", enabled, visible, partial)
			}
		}
	})
}
