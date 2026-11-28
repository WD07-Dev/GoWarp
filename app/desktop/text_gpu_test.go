package desktop

import (
	"fmt"
	"image"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"
)

func TestGPUTextMatchesCPURaster(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{Costumes: [][]*media.Costume{{{Width: 1, Height: 1}}}})
		var gpu stageGPU
		defer gpu.Close()
		for _, noAA := range []bool{false, true} {
			r.SetView(vm.View{Width: 480, Height: 360, Direction: 90, Zoom: 100, NoAntialias: noAA})
			for _, outline := range []float64{0, 1.25, 2} {
				skin := vm.TextSkin{Text: "AV 한글 e\u0301\n01234 ", Font: "Sans Serif", Width: 100,
					FontSize: 18.25, LineHeight: 23, PaddingY: 2.125, AnchorY: 7,
					OutlineWidth: outline, OutlineColor: "#3185b6", Color: "#eb946c", Align: .5, Visible: true}
				target := vm.DisplayTarget{State: vm.TargetState{Text: &skin, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
				d, ok := r.DisplaySprite(target, 480, 360)
				if !ok {
					t.Fatal("text geometry missing")
				}
				got, temporary := gpu.texture(r, d)
				if got == nil {
					t.Fatal("display text did not produce a GPU texture")
				}
				cpu, _ := r.PenSprite(target)
				want := ebiten.NewImageFromImage(cpu.Costume.Image)
				a, b := make([]byte, got.Bounds().Dx()*got.Bounds().Dy()*4), make([]byte, want.Bounds().Dx()*want.Bounds().Dy()*4)
				got.ReadPixels(a)
				want.ReadPixels(b)
				if len(a) != len(b) {
					t.Fatalf("text dimensions differ: %v vs %v", got.Bounds(), want.Bounds())
				}
				maxDelta := 0
				for i := range a {
					delta := int(a[i]) - int(b[i])
					if delta < 0 {
						delta = -delta
					}
					maxDelta = max(maxDelta, delta)
				}
				// CPU and GPU blend into 8-bit storage with different rounding.
				if maxDelta > 4 {
					t.Fatalf("AA=%v outline=%g: max channel difference %d", !noAA, outline, maxDelta)
				}
				reference := ebiten.NewImageWithOptions(cpu.Costume.Image.Bounds(), &ebiten.NewImageOptions{Unmanaged: true})
				stageUpload(reference, cpu.Costume.Image)
				left := ebiten.NewImageWithOptions(image.Rect(0, 0, 160, 120), &ebiten.NewImageOptions{Unmanaged: true})
				corners := [4][2]float64{{140, 5}, {5, 12}, {10, 111}, {145, 104}}
				for _, effects := range []vm.GraphicEffects{
					{}, {Ghost: 40}, {Color: 40, Brightness: -10}, {Whirl: 30},
					{Fisheye: 30}, {Pixelate: 5}, {Mosaic: 20},
				} {
					left.Clear()
					if err := gpu.initShader(); err != nil {
						t.Fatal(err)
					}
					gpu.quadVisual(left, got, corners, d.Costume.Width, d.Costume.Height, effects, noAA, true, "", nil)

					a, b := make([]byte, 160*120*4), make([]byte, 160*120*4)
					left.ReadPixels(a)
					left.Clear()
					gpu.quadVisual(left, reference, corners, d.Costume.Width, d.Costume.Height, effects, noAA, false, "", nil)
					left.ReadPixels(b)

					totalDelta, maxAlpha, maxColor := 0, 0, 0
					for i := range a {
						delta := int(a[i]) - int(b[i])
						if delta < 0 {
							delta = -delta
						}
						totalDelta += delta
						if i%4 == 3 {
							maxAlpha = max(maxAlpha, delta)
						} else {
							maxColor = max(maxColor, delta)
						}
					}
					// HSV conversion magnifies 8-bit blend rounding near hue boundaries.
					// Check coverage separately from the average color error.
					if maxAlpha > 6 || maxColor > 24 || float64(totalDelta)/float64(len(a)) > .5 {
						t.Fatalf("effect=%+v AA=%v outline=%g alpha=%d color=%d mean=%g", effects, !noAA, outline, maxAlpha, maxColor, float64(totalDelta)/float64(len(a)))
					}
				}
				left.Deallocate()
				reference.Deallocate()
				want.Deallocate()
				if temporary {
					got.Deallocate()
				}
			}
		}
	})
}

func TestGPUTextReusesGlyphUploads(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{Costumes: [][]*media.Costume{{{Width: 1, Height: 1}}}})
		skin := vm.TextSkin{Text: "0123456789", Width: 480, FontSize: 24, Visible: true}
		target := vm.DisplayTarget{State: vm.TargetState{Text: &skin, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
		gpu := stageGPU{measureUploads: true}
		defer gpu.Close()
		d, _ := r.DisplaySprite(target, 480, 360)
		if im, _ := gpu.texture(r, d); im == nil {
			t.Fatal("missing text")
		}
		before := gpu.uploadBytes
		for i := range 200 {
			skin.Text = fmt.Sprintf("%03d", i)
			d, _ = r.DisplaySprite(target, 480, 360)
			im, temporary := gpu.texture(r, d)
			if im == nil {
				t.Fatal("missing changing text")
			}
			if temporary {
				im.Deallocate()
			}
		}
		// New digit phases may need uploads, but full 960px-wide labels must not.
		if uploaded := gpu.uploadBytes - before; uploaded > 1<<20 {
			t.Fatalf("changing labels uploaded %d bytes", uploaded)
		}
		if gpu.bytes > stageTextureMaxBudget {
			t.Fatal("text exceeded the existing texture budget")
		}
		total := 0
		for _, entry := range gpu.textGlyphs {
			total += entry.bytes
		}
		if total != gpu.textGlyphBytes || total > textGlyphTextureBudget || len(gpu.textGlyphs) > 1024 {
			t.Fatal("glyph cache exceeded its budget")
		}
		gpu.Close()
		if len(gpu.textGlyphs) != 0 || gpu.textGlyphBytes != 0 {
			t.Fatal("glyph textures survived close")
		}
	})
}

func TestSceneTextUsesPremultipliedComposition(t *testing.T) {
	runGPUFrame(t, func() {
		lib := &media.Library{Costumes: [][]*media.Costume{{{Width: 1, Height: 1}}}}
		r := render.New(lib)
		r.SetView(vm.View{Width: 128, Height: 96, Zoom: 100, Direction: 90, Color: "#315f8c"})
		skin := vm.TextSkin{Text: "Text AV", Width: 100, FontSize: 18, Visible: true,
			Color: "#d86c31", OutlineColor: "#186344", OutlineWidth: 1.25, AnchorY: 15}
		target := vm.DisplayTarget{State: vm.TargetState{Text: &skin, Size: 100, Visible: true,
			StretchX: 100, StretchY: 100, Direction: 90, Effects: vm.GraphicEffects{Ghost: 40}}}
		var gpu stageGPU
		defer gpu.Close()
		dst := ebiten.NewImage(128, 96)
		defer dst.Deallocate()
		if err := gpu.drawScene(dst, r, []vm.DisplayTarget{target}); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, 128*96*4)
		dst.ReadPixels(got)
		cpu, _ := r.PenSprite(target)
		lib.Costumes[0][0] = cpu.Costume
		target.State.Text = nil
		if err := gpu.drawScene(dst, r, []vm.DisplayTarget{target}); err != nil {
			t.Fatal(err)
		}
		want := make([]byte, len(got))
		dst.ReadPixels(want)
		for i := range got {
			delta := int(got[i]) - int(want[i])
			if delta < 0 {
				delta = -delta
			}
			if delta > 4 {
				t.Fatalf("scene text differs at byte %d by %d", i, delta)
			}
		}
	})
}
