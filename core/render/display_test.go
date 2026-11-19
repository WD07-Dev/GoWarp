package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestDisplayResolutionPreservesStageAndCollision(t *testing.T) {
	r, target := optimizationFixture()
	scene := []vm.DisplayTarget{target}
	r.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{-10, 0, 10, 0}, Size: 3, Color: color.NRGBA{G: 255, A: 255}})
	before := append([]byte(nil), r.Draw(scene).Pix...)
	box, _ := r.Bounds(target, true)
	px, py := r.ToScreen(7, 11)
	hit, hitOK := r.Hit(scene, 2, -2)
	touching := r.TouchingColor(scene, target, color.NRGBA{G: 255, A: 255}, nil)
	for _, size := range []image.Point{image.Pt(960, 720), image.Pt(720, 540), image.Pt(800, 400)} {
		output := r.DrawAt(scene, size.X, size.Y)
		if output.Bounds().Size() != size {
			t.Fatalf("display size = %v, want %v", output.Bounds().Size(), size)
		}
		if w, h := r.Size(); w != 480 || h != 360 {
			t.Fatalf("display resize changed logical stage: %dx%d", w, h)
		}
		if x, y := r.ToScreen(7, 11); x != px || y != py {
			t.Fatal("display resize changed stage coordinates")
		}
		if got, _ := r.Bounds(target, true); got != box {
			t.Fatal("display resize changed collision bounds")
		}
		if got, ok := r.Hit(scene, 2, -2); ok != hitOK || got.Source != hit.Source {
			t.Fatal("display resize changed mouse hit")
		}
		if r.TouchingColor(scene, target, color.NRGBA{G: 255, A: 255}, nil) != touching {
			t.Fatal("display resize changed color sensing")
		}
		if !bytes.Equal(before, r.Draw(scene).Pix) {
			t.Fatal("display rendering changed base rendering or pen")
		}
	}
}

func TestHighQualityPenKeepsFractionalSpritePositions(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	im.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	costume := &media.Costume{Image: im, Width: 1, Height: 1, CenterX: .5, CenterY: .5}
	r := New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
	target := vm.DisplayTarget{State: vm.TargetState{X: 10.25, Y: -7.75, Visible: true, Size: 100, Direction: 90, StretchX: 100, StretchY: 100}}
	for _, mode := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}, {false, false}} {
		highQuality, disableRounding := mode[0], mode[1]
		view := r.DisplayView()
		view.HighQualityPen = highQuality
		view.DisablePositionRounding = disableRounding
		r.SetView(view)
		x, y := 10.0, -8.0
		if highQuality || disableRounding {
			x, y = 10.25, -7.75
		}
		box, ok := r.Bounds(target, false)
		if !ok || box.Left != x-.5 || box.Top != y+.5 {
			t.Fatalf("highQuality=%v: bounds=%+v", highQuality, box)
		}
		for _, stamp := range []bool{false, true} {
			var sprite DisplaySprite
			if stamp {
				sprite, ok = r.PenSprite(target)
			} else {
				sprite, ok = r.DisplaySprite(target, 480, 360)
			}
			want := [2]float64{240 + x - .5, 180 - y - .5}
			if !ok || math.Abs(sprite.Corners[0][0]-want[0]) > 1e-9 || math.Abs(sprite.Corners[0][1]-want[1]) > 1e-9 {
				t.Fatalf("highQuality=%v stamp=%v: corner=%v, want %v", highQuality, stamp, sprite.Corners[0], want)
			}
		}
		if _, hit := r.Hit([]vm.DisplayTarget{target}, 9.6, -8); hit == (highQuality || disableRounding) {
			t.Fatalf("highQuality=%v: hit test did not follow displayed position", highQuality)
		}
	}
}

func TestHighQualityPenEmptyCostumeBounds(t *testing.T) {
	r := New(&media.Library{})
	view := r.DisplayView()
	view.HighQualityPen = true
	r.SetView(view)
	target := vm.DisplayTarget{State: vm.TargetState{X: -.25, Y: .75}}
	box, ok := r.Bounds(target, false)
	if !ok || box != (vm.Bounds{Left: -.25, Right: -.25, Bottom: .75, Top: .75}) {
		t.Fatalf("empty costume lost fractional position: %+v", box)
	}
}

func TestDisplayDrawsTextureDetailInsteadOfUpscalingStage(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 8, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 8; x++ {
			c := color.NRGBA{A: 255}
			if x%2 == 0 {
				c.R = 255
			} else {
				c.B = 255
			}
			im.SetNRGBA(x, y, c)
		}
	}
	costume := &media.Costume{Image: im, Width: 4, Height: 1, CenterX: 2, CenterY: .5}
	r := New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
	target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, Direction: 90, StretchX: 100, StretchY: 100}}
	output := r.DrawAt([]vm.DisplayTarget{target}, 960, 720)
	for x := 0; x < 8; x++ {
		want := color.RGBA{B: 255, A: 255}
		if x%2 == 0 {
			want = color.RGBA{R: 255, A: 255}
		}
		if got := output.RGBAAt(476+x, 359); got != want {
			t.Fatalf("native texture detail at %d: got %v, want %v", x, got, want)
		}
	}
}

func TestDisplayFilteringDoesNotBleedTransparentRGB(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	im.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	im.SetNRGBA(1, 0, color.NRGBA{B: 255})
	got := filteredPixel(im, .5, .5)
	if got.R != 255 || got.G != 0 || got.B != 0 || got.A < 127 || got.A > 128 {
		t.Fatalf("transparent neighbor left a colored halo: %v", got)
	}
	if got := filteredPixel(im, 0, .5); got != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("edge was not clamped: %v", got)
	}
}

func TestDisplayEnlargedBitmapKeepsSharpPixels(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		im.SetNRGBA(0, y, color.NRGBA{R: 255, A: 255})
		im.SetNRGBA(1, y, color.NRGBA{B: 255, A: 255})
	}
	var data bytes.Buffer
	if err := png.Encode(&data, im); err != nil {
		t.Fatal(err)
	}
	costume, err := media.DecodeCostume(project.Costume{DataFormat: "png", RotationCenterX: 1, RotationCenterY: 1}, data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	r := New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
	r.SetView(vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, Color: "#ffffff"})
	target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 400, Direction: 90, StretchX: 100, StretchY: 100}}
	output := r.DrawAt([]vm.DisplayTarget{target}, 192, 144)
	for x, want := range map[int]color.RGBA{95: {R: 255, A: 255}, 96: {B: 255, A: 255}} {
		if got := output.RGBAAt(x, 72); got != want {
			t.Errorf("enlarged bitmap pixel at %d: got %v, want %v", x, got, want)
		}
	}
}

func TestDisplayKeepsPixelationAndGraphicEffects(t *testing.T) {
	r, target := optimizationFixture()
	tr, ok := r.transform(target)
	if !ok {
		t.Fatal("transform failed")
	}
	effects := vm.GraphicEffects{Pixelate: 20, Color: 25, Brightness: 15, Ghost: 37}
	for y := -8.0; y < 8; y += .5 {
		for x := -8.0; x < 8; x += .5 {
			if got, want := tr.sampleFiltered(x, y, effects, false, true), tr.sample(x, y, effects, false); got != want {
				t.Fatalf("pixelation changed at %g,%g: %v != %v", x, y, got, want)
			}
		}
	}
	tr, _ = r.transform(vm.DisplayTarget{State: target.State})
	base := tr.sampleFiltered(0, 0, vm.GraphicEffects{}, false, true)
	effect := vm.GraphicEffects{Color: 40, Ghost: 50}
	if got := tr.sampleFiltered(0, 0, effect, false, true); got != effectColor(base, effect) {
		t.Fatal("effects were applied before texture filtering")
	}
}

func TestDisplayBufferLimitsAndReuse(t *testing.T) {
	w, h := DisplaySize(100000, 50000)
	if w > 8192 || h > 8192 || w*h > 16<<20 || w != 2*h {
		t.Fatalf("unbounded or distorted framebuffer: %dx%d", w, h)
	}
	r := New(&media.Library{})
	first := r.DrawAt(nil, 960, 720)
	if next := r.DrawAt(nil, 960, 720); first != next {
		t.Fatal("display buffer reallocated every frame")
	}
	if next := r.DrawAt(nil, 0, 0); next.Bounds() != image.Rect(0, 0, 1, 1) {
		t.Fatal("minimized window did not have a valid framebuffer")
	}
}

func BenchmarkDisplayDrawNative(b *testing.B) {
	r, target := optimizationFixture()
	target.State.Size = 3000
	for b.Loop() {
		r.DrawAt([]vm.DisplayTarget{target}, 480, 360)
	}
}

func BenchmarkDisplayDrawHiDPI(b *testing.B) {
	r, target := optimizationFixture()
	target.State.Size = 3000
	for b.Loop() {
		r.DrawAt([]vm.DisplayTarget{target}, 960, 720)
	}
}
