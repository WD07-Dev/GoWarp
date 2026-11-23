package render

import (
	"bytes"
	"context"
	"encoding/json"
	"gowarp/core/project"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"gowarp/core/media"
	"gowarp/core/vm"
)

func TestClearAndStampReusePenBuffer(t *testing.T) {
	r, target := optimizationFixture()
	command := vm.PenCommand{Kind: "stamp", Target: target}
	r.Pen(command)
	want := append([]byte(nil), r.pen.Pix...)
	buffer := r.pen
	_, before := r.DisplayPen()
	for i := 0; i < 3; i++ {
		r.Pen(vm.PenCommand{Kind: "clear"})
		if r.pen != buffer {
			t.Fatal("clearing discarded the pen buffer")
		}
		for _, value := range r.pen.Pix {
			if value != 0 {
				t.Fatal("clearing left old pixels")
			}
		}
		r.Pen(command)
		if r.pen != buffer || !bytes.Equal(want, r.pen.Pix) {
			t.Fatal("reusing the pen buffer changed stamped pixels")
		}
	}
	_, after := r.DisplayPen()
	if after <= before {
		t.Fatal("pen updates were not published")
	}
}

func TestRestorePenReusesPixels(t *testing.T) {
	r := New(&media.Library{})
	pixels := image.NewRGBA(r.frame.Bounds())
	r.RestorePen(pixels)
	pixels.SetRGBA(10, 10, color.RGBA{R: 64, A: 128})
	if allocs := testing.AllocsPerRun(5, func() { r.RestorePen(pixels) }); allocs > 0 {
		t.Fatalf("restoring unchanged-size pixels allocated %.0f objects", allocs)
	}
	if got := r.pen.NRGBAAt(10, 10); got != (color.NRGBA{R: 127, A: 128}) {
		t.Fatalf("restored sensing pixel: %v", got)
	}
	pixels.SetRGBA(10, 10, color.RGBA{})
	r.RestorePen(pixels)
	if r.pen.NRGBAAt(10, 10) != (color.NRGBA{}) || r.penComposite.RGBAAt(10, 10) != (color.RGBA{}) {
		t.Fatal("restoring transparent pixels retained the previous snapshot")
	}
}

func TestCopyPenPixelMatchesColorModel(t *testing.T) {
	src := image.NewRGBA(image.Rect(-1, -1, 2, 2))
	dst := image.NewNRGBA(src.Bounds())
	for a := 0; a < 256; a++ {
		for v := 0; v < 256; v++ {
			c := color.RGBA{R: uint8(v), G: uint8(255 - v), B: uint8(v / 2), A: uint8(a)}
			src.SetRGBA(0, 0, c)
			copyPenPixel(dst, src, 0, 0)
			want := color.NRGBAModel.Convert(c).(color.NRGBA)
			if got := dst.NRGBAAt(0, 0); got != want {
				t.Fatalf("source=%v: got %v, want %v", c, got, want)
			}
		}
	}
}

func TestFullyTransparentStampDoesNotAllocatePen(t *testing.T) {
	r, target := optimizationFixture()
	target.State.Effects = vm.GraphicEffects{Ghost: 100, Color: 50, Brightness: 20}
	r.Pen(vm.PenCommand{Kind: "stamp", Target: target})
	if r.pen != nil {
		t.Fatal("invisible stamp allocated a pen buffer")
	}
}

func TestPenCollisionAndCamera(t *testing.T) {
	renderer := New(&media.Library{Costumes: [][]*media.Costume{{solid(color.NRGBA{R: 255, A: 255})}, {solid(color.NRGBA{B: 255, A: 255})}}})
	var graphics vm.Graphics = renderer
	a := vm.DisplayTarget{
		Source: 0,
		State: vm.TargetState{
			Name:      "A",
			Size:      100,
			StretchX:  100,
			StretchY:  100,
			Direction: 90,
			Visible:   true,
		},
	}
	b := vm.DisplayTarget{
		Source: 1,
		State: vm.TargetState{
			Name:      "B",
			Size:      100,
			StretchX:  100,
			StretchY:  100,
			Direction: 90,
			Visible:   true,
		},
	}
	if !graphics.Touching([]vm.DisplayTarget{a, b}, a, "B", 0, 0) {
		t.Fatal("pixel overlap missing")
	}
	b.State.X = 10
	if graphics.Touching([]vm.DisplayTarget{a, b}, a, "B", 0, 0) {
		t.Fatal("disjoint sprites touch")
	}
	graphics.Pen(vm.PenCommand{
		Kind:  "line",
		Line:  [4]float64{-20, 0, 20, 0},
		Size:  3,
		Color: color.NRGBA{G: 255, A: 255},
	})
	if !graphics.TouchingColor([]vm.DisplayTarget{a}, a, color.NRGBA{G: 255, A: 255}, nil) {
		t.Fatal("same-frame pen color not sensed")
	}
	graphics.Pen(vm.PenCommand{Kind: "clear"})
	if graphics.TouchingColor([]vm.DisplayTarget{a}, a, color.NRGBA{G: 255, A: 255}, nil) {
		t.Fatal("clear pen still sensed")
	}
	a.State.Effects.Ghost = 100
	if !graphics.Touching([]vm.DisplayTarget{a}, a, "_mouse_", 0, 0) {
		t.Fatal("ghost incorrectly disables sensing")
	}
	a.State.Effects.Ghost = 0
	a.State.X = 30
	graphics.SetView(vm.View{
		Width:     480,
		Height:    360,
		X:         30,
		Zoom:      200,
		Direction: 90,
		Color:     "#fff",
	})
	if got := renderer.Draw([]vm.DisplayTarget{a}).RGBAAt(240, 180); got.R != 255 || got.G != 0 {
		t.Fatalf("camera pixel: %v", got)
	}
	x, y := renderer.FromScreen(240, 180)
	if x != 30 || y != 0 {
		t.Fatalf("camera mouse %v,%v", x, y)
	}
}

func TestColorMaskUsesPremultipliedCostumeColor(t *testing.T) {
	r := New(&media.Library{Costumes: [][]*media.Costume{{solid(color.NRGBA{R: 255, A: 128})}, {solid(color.NRGBA{B: 255, A: 255})}}})
	a := vm.DisplayTarget{
		Source: 0,
		State: vm.TargetState{
			Name:      "A",
			Size:      100,
			StretchX:  100,
			StretchY:  100,
			Direction: 90,
			Visible:   true,
		},
	}
	b := a
	b.Source, b.State.Name = 1, "B"
	blue := color.NRGBA{B: 255, A: 255}
	mask := color.NRGBA{R: 128, A: 255}
	if !r.TouchingColor([]vm.DisplayTarget{b, a}, a, blue, &mask) {
		t.Fatal("translucent red mask did not detect underlying blue")
	}
	mask.R = 255
	if r.TouchingColor([]vm.DisplayTarget{b, a}, a, blue, &mask) {
		t.Fatal("unpremultiplied mask incorrectly matched")
	}
}

func TestStageResizeKeepsPenWorldPosition(t *testing.T) {
	r := New(&media.Library{})
	r.Pen(vm.PenCommand{
		Kind:  "line",
		Line:  [4]float64{30, 20, 30, 20},
		Size:  5,
		Color: color.NRGBA{B: 255, A: 255},
	})
	r.SetView(vm.View{
		Width:     600,
		Height:    400,
		Zoom:      100,
		Direction: 90,
		Color:     "#ffffff",
	})
	w, h := r.Size()
	if w != 600 || h != 400 {
		t.Fatalf("stage size = %d,%d", w, h)
	}
	frame := r.Draw(nil)
	if got := frame.RGBAAt(330, 180); got.B != 255 || got.R != 0 {
		t.Fatalf("resized pen pixel = %v", got)
	}
	if got := frame.RGBAAt(270, 160); got.R != 255 || got.G != 255 || got.B != 255 {
		t.Fatalf("pen stayed at old screen position: %v", got)
	}
}

func TestLooseBoundsUseCostumeGeometryWithoutPixels(t *testing.T) {
	lib := &media.Library{Costumes: [][]*media.Costume{{{Width: 20, Height: 10, CenterX: 10, CenterY: 5}}}}
	renderer := New(lib)
	target := vm.DisplayTarget{State: vm.TargetState{X: 30, Y: -20, Size: 200, StretchX: 100, StretchY: 100, Direction: 90}}
	got, ok := renderer.Bounds(target, false)
	want := vm.Bounds{Left: 10, Right: 50, Bottom: -30, Top: -10}
	if !ok || got != want {
		t.Fatalf("bounds without decoded pixels = %+v, %v; want %+v", got, ok, want)
	}
}

func TestRemoveFencingAllowsOffscreenTouching(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"comments":{"c":{"text":"{\"runtimeOptions\":{\"fencing\":false}} // _twconfig_"}}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	runtime := vm.New(&p)
	r := New(&media.Library{Costumes: [][]*media.Costume{{solid(color.NRGBA{R: 255, A: 255})}}})
	a := vm.DisplayTarget{State: vm.TargetState{Name: "A", X: 300, Y: 250, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, Visible: true}}
	b := a
	b.State.Name, b.State.CloneID = "B", 1
	scene := []vm.DisplayTarget{a, b}
	if r.Touching(scene, a, "B", 0, 0) {
		t.Fatal("default collision escaped stage")
	}
	runtime.SetGraphics(r)
	if !r.Touching(scene, a, "B", 0, 0) {
		t.Error("offscreen sprite collision missing")
	}
	if !r.TouchingColor(scene, a, color.NRGBA{R: 255, A: 255}, nil) {
		t.Error("offscreen color collision missing")
	}
}

func TestOffscreenCollisionScanDoesNotRepeatUnrepresentableSteps(t *testing.T) {
	r := New(&media.Library{})
	r.view.OffscreenTouching = true
	for _, box := range []vm.Bounds{
		{Left: 1e16, Right: 1e16, Bottom: 0, Top: 0},
		{Left: 0, Right: 0, Bottom: 1e16, Top: 1e16},
	} {
		samples := 0
		touched := r.scan(box, func(x, y float64) bool {
			samples++
			// Stop a broken scanner without hanging the test.
			return samples > 1
		})
		if touched || samples != 1 {
			t.Fatalf("scanner repeated a coordinate: samples=%d, touched=%v", samples, touched)
		}
	}
}

func BenchmarkOffscreenColorSensingManyClones(b *testing.B) {
	r := New(&media.Library{Costumes: [][]*media.Costume{{solid(color.NRGBA{R: 255, A: 255})}}})
	r.view.HighQualityPen = true
	r.view.DisablePositionRounding = true
	r.view.OffscreenTouching = true
	target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 1000, StretchX: 100, StretchY: 100, Direction: 90, X: 300.25, Y: 200.25}}
	scene := make([]vm.DisplayTarget, 301)
	scene[0] = target
	for i := 1; i < len(scene); i++ {
		scene[i] = target
		scene[i].State.CloneID = i
		scene[i].State.X += float64(i) * 100
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if r.TouchingColor(scene, target, color.NRGBA{G: 255, A: 255}, nil) {
			b.Fatal("separated sprites cannot touch green")
		}
	}
}

func TestColorCandidateCullingPreservesBackgroundAndLayerOrder(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	green := color.NRGBA{G: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	r := New(&media.Library{Costumes: [][]*media.Costume{{solid(red)}, {solid(green)}, {solid(blue)}}})
	r.view.HighQualityPen, r.view.OffscreenTouching = true, true
	target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, X: 300.25, Y: 200.25}}
	near, distant := target, target
	near.Source, distant.Source = 1, 2
	distant.State.X = 1000
	if !r.TouchingColor([]vm.DisplayTarget{target, distant}, target, white, nil) {
		t.Fatal("culling distant sprites removed the white background")
	}
	scene := []vm.DisplayTarget{distant, near, target}
	if !r.TouchingColor(scene, target, green, &red) {
		t.Fatal("lost overlapping masked color")
	}
	if r.TouchingColor(scene, target, blue, nil) {
		t.Fatal("distant blue sprite changed sampled color")
	}
	// An actual overlapping blue sprite must still occlude the green one.
	distant.State.X = target.State.X
	scene = []vm.DisplayTarget{near, distant, target}
	if !r.TouchingColor(scene, target, blue, nil) || r.TouchingColor(scene, target, green, nil) {
		t.Fatal("candidate filtering changed layer order")
	}
}

func TestColorSensingIgnoresUnavailableCandidatePixels(t *testing.T) {
	r := New(&media.Library{Costumes: [][]*media.Costume{
		{solid(color.NRGBA{R: 255, A: 255})},
		{{Width: 2, Height: 2, CenterX: 1, CenterY: 1}},
	}})
	target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
	other := target
	other.Source = 1
	if r.TouchingColor([]vm.DisplayTarget{target, other}, target, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, nil) {
		t.Fatal("unavailable candidate pixels enabled background sensing")
	}
}

func TestStampIgnoresNonfinitePenWidth(t *testing.T) {
	r, target := optimizationFixture()
	r.Pen(vm.PenCommand{Kind: "stamp", Target: target, Size: 1})
	want := append([]byte(nil), r.pen.Pix...)
	for _, width := range []float64{math.Inf(1), math.NaN(), 0} {
		r.Pen(vm.PenCommand{Kind: "clear"})
		r.Pen(vm.PenCommand{Kind: "stamp", Target: target, Size: width})
		if !bytes.Equal(want, r.pen.Pix) {
			t.Errorf("line width %g changed a stamp", width)
		}
	}
}

func TestZeroWidthPenUsesReferenceDefaultWidth(t *testing.T) {
	r := New(&media.Library{})
	command := vm.PenCommand{Kind: "line", Size: 1, Line: [4]float64{-10, 0, 10, 0}, Color: color.NRGBA{R: 255, A: 255}}
	r.Pen(command)
	want := append([]byte(nil), r.pen.Pix...)
	r.Pen(vm.PenCommand{Kind: "clear"})
	command.Size = 0
	r.Pen(command)
	if !bytes.Equal(want, r.pen.Pix) {
		t.Fatal("zero width differs from TurboWarp's one-pixel drawing fallback")
	}
}

func TestPenLineRejectsNonfiniteCoordinates(t *testing.T) {
	for _, invalid := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for coordinate := range 4 {
			r := New(&media.Library{})
			command := vm.PenCommand{Kind: "line", Line: [4]float64{-10, -2, 10, 2}, Size: 3, Color: color.NRGBA{R: 255, A: 255}}
			command.Line[coordinate] = invalid
			r.Pen(command)
			if r.pen != nil {
				t.Fatalf("coordinate %d=%g allocated a pen layer", coordinate, invalid)
			}
		}
	}
}

func TestTouchingRejectsGeometryBeforeLoadingCostumes(t *testing.T) {
	for _, name := range []string{"disjoint", "offscreen", "hidden", "missing target"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			pixels := image.NewNRGBA(image.Rect(0, 0, 4, 4))
			pixels.SetNRGBA(2, 2, color.NRGBA{R: 255, A: 255})
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, pixels); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "collision.png")
			if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			costumes := []project.Costume{{}, {MD5Ext: "collision.png", DataFormat: "png", BitmapResolution: 1, RotationCenterX: 2, RotationCenterY: 2}}
			p := &project.Project{Targets: []*project.Target{{Costumes: costumes}, {Costumes: costumes}}}
			lib, err := media.LoadRuntime(context.Background(), dir, p, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer lib.Close()
			r := New(lib)
			a := vm.DisplayTarget{Source: 0, State: vm.TargetState{Name: "A", CurrentCostume: 1, Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
			b := a
			b.Source, b.State.Name = 1, "B"
			query := "B"
			switch name {
			case "disjoint":
				b.State.X = 100
			case "offscreen":
				a.State.X, b.State.X = 1000, 1000
			case "hidden":
				a.State.Visible = false
			case "missing target":
				query = "absent"
			}
			/*
				Metadata is available, but these non-current costumes have never been
				decoded. Removing the source detects an unnecessary pixel request.
			*/
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if r.Touching([]vm.DisplayTarget{a, b}, a, query, 0, 0) {
				t.Fatal("geometrically excluded sprites touched")
			}
			if err := lib.Err(); err != nil {
				t.Fatalf("excluded collision loaded pixels: %v", err)
			}
			if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			a.State.Visible = true
			a.State.X, b.State.X = 0, 0
			if !r.Touching([]vm.DisplayTarget{a, b}, a, "B", 0, 0) {
				t.Fatal("overlapping opaque pixels did not touch")
			}
			b.State.X = 1
			if r.Touching([]vm.DisplayTarget{a, b}, a, "B", 0, 0) {
				t.Fatal("overlapping geometry ignored transparent pixels")
			}
			r.view.OffscreenTouching = true
			a.State.X, b.State.X = 1000, 1000
			if !r.Touching([]vm.DisplayTarget{a, b}, a, "B", 0, 0) {
				t.Fatal("offscreen sensing setting was ignored")
			}
		})
	}
}
