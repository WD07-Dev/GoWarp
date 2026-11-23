package desktop

import (
	"github.com/hajimehoshi/ebiten/v2"
	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"
	"image"
	"image/color"
	"testing"
)

func TestStageGPUTintSpritesAndStamps(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 80, A: 255})
			}
		}
		library := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 8, Height: 8, CenterX: 4, CenterY: 4, Bitmap: true}}}}
		renderer := render.New(library)
		reference := render.New(library)
		view := vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, Color: "#ffffff"}
		renderer.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		graphics, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
		dst := ebiten.NewImage(64, 48)
		defer dst.Deallocate()
		for _, effects := range []vm.GraphicEffects{{Tint: &[3]float64{0.5, 1, 0.25}}, {Tint: &[3]float64{0.5, 1, 0.25}, Color: 40, Brightness: 20, Ghost: 50}, {}} {
			target.State.Effects = effects
			for _, stamp := range []bool{false, true} {
				graphics.Pen(vm.PenCommand{Kind: "clear"})
				reference.Pen(vm.PenCommand{Kind: "clear"})
				scene := []vm.DisplayTarget{target}
				if stamp {
					graphics.Pen(vm.PenCommand{Kind: "stamp", Target: target})
					reference.Pen(vm.PenCommand{Kind: "stamp", Target: target})
					scene = nil
				}
				if err := gpu.Draw(dst, renderer, scene); err != nil {
					t.Fatal(err)
				}
				expected := reference.DrawAt(scene, 64, 48).RGBAAt(32, 24)
				got := color.RGBAModel.Convert(dst.At(32, 24)).(color.RGBA)
				for _, pair := range [][2]uint8{{got.R, expected.R}, {got.G, expected.G}, {got.B, expected.B}, {got.A, expected.A}} {
					delta := int(pair[0]) - int(pair[1])
					if delta < -2 || delta > 2 {
						t.Fatalf("stamp=%v effects=%+v got=%v want=%v", stamp, effects, got, expected)
					}
				}
			}
		}
	})
}
