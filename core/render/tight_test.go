package render

import (
	"image"
	"image/color"
	"math"
	"testing"

	"gowarp/core/media"
	"gowarp/core/vm"
)

func TestTightBoundsRotatedDiagonal(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := 0; i < 4; i++ {
		pixels.SetNRGBA(i, i, color.NRGBA{A: 255})
	}
	costume := &media.Costume{
		Image:   pixels,
		Width:   4,
		Height:  4,
		CenterX: 2,
		CenterY: 2,
	}
	renderer := New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
	target := vm.DisplayTarget{
		Source: 0,
		State: vm.TargetState{
			Visible:   true,
			Size:      100,
			StretchX:  100,
			StretchY:  100,
			Direction: 45,
		},
	}
	got, ok := renderer.Bounds(target, true)
	if !ok {
		t.Fatal("missing diagonal bounds")
	}
	/*
		The touching hull's row extremes rotate into a narrow diagonal; transparent
		corners of the unrotated crop must not enlarge its perpendicular extent.
	*/
	want := vm.Bounds{
		Left:   -2.121320343559643,
		Right:  2.121320343559643,
		Bottom: -1.4142135623730951,
		Top:    1.4142135623730951,
	}
	checkTightBounds(t, got, want)
}

func TestCachedTightBoundsSurviveReleasedPixels(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := 0; i < 4; i++ {
		pixels.SetNRGBA(i, i, color.NRGBA{A: 255})
	}
	costume := &media.Costume{Image: pixels, Width: 4, Height: 4, CenterX: 2, CenterY: 2}
	renderer := New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
	target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
	if _, ok := renderer.Bounds(target, true); !ok {
		t.Fatal("initial hull unavailable")
	}
	costume.Image = nil
	target.State.Direction = 45
	target.State.Effects.Ghost = 50
	got, ok := renderer.Bounds(target, true)
	if !ok {
		t.Fatal("cached hull still required decoded pixels")
	}
	checkTightBounds(t, got, vm.Bounds{Left: -2.121320343559643, Right: 2.121320343559643, Bottom: -1.4142135623730951, Top: 1.4142135623730951})
	target.State.Effects.Mosaic = 10
	if _, ok := renderer.Bounds(target, true); ok {
		t.Fatal("shape effect reused an incompatible cached hull")
	}
	target.State.Visible = false
	target.State.Effects = vm.GraphicEffects{}
	if _, ok := renderer.Bounds(target, true); ok {
		t.Fatal("hidden costume with missing pixels was accepted")
	}
}

func TestTightBoundsHiddenAndTransparentUseFastBounds(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		if hidden {
			pixels.SetNRGBA(0, 0, color.NRGBA{A: 255})
		}
		costume := &media.Costume{
			Image:   pixels,
			Width:   8,
			Height:  8,
			CenterX: 4,
			CenterY: 4,
		}
		renderer := New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
		target := vm.DisplayTarget{
			Source: 0,
			State: vm.TargetState{
				Visible:   !hidden,
				Size:      100,
				StretchX:  100,
				StretchY:  100,
				Direction: 90,
				X:         239,
			},
		}
		got, ok := renderer.Bounds(target, true)
		if !ok {
			t.Fatalf("missing fallback for hidden=%v", hidden)
		}
		checkTightBounds(t, got, vm.Bounds{
			Left:   235,
			Right:  243,
			Bottom: -4,
			Top:    4,
		})
		if !renderer.Touching([]vm.DisplayTarget{target}, target, "_edge_", 0, 0) {
			t.Fatalf("missing edge contact for hidden=%v", hidden)
		}
	}
}

func TestTightBoundsShapeEffectsInvalidateHull(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	pixels.SetNRGBA(0, 0, color.NRGBA{A: 255})
	costume := &media.Costume{
		Image:   pixels,
		Width:   8,
		Height:  8,
		CenterX: 4,
		CenterY: 4,
	}
	renderer := New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
	target := vm.DisplayTarget{
		Source: 0,
		State: vm.TargetState{
			Visible:   true,
			Size:      100,
			StretchX:  100,
			StretchY:  100,
			Direction: 90,
		},
	}
	got, ok := renderer.Bounds(target, true)
	if !ok {
		t.Fatal("missing initial bounds")
	}
	checkTightBounds(t, got, vm.Bounds{
		Left:   -3.5,
		Right:  -2.5,
		Bottom: 2.5,
		Top:    3.5,
	})
	target.State.Effects.Mosaic = 10
	target.State.Effects.Ghost = 100
	got, ok = renderer.Bounds(target, true)
	if !ok {
		t.Fatal("missing mosaic bounds")
	}
	checkTightBounds(t, got, vm.Bounds{
		Left:   -3.5,
		Right:  .5,
		Bottom: -.5,
		Top:    3.5,
	})
	target.State.Effects = vm.GraphicEffects{}
	got, ok = renderer.Bounds(target, true)
	if !ok {
		t.Fatal("missing reset bounds")
	}
	checkTightBounds(t, got, vm.Bounds{
		Left:   -3.5,
		Right:  -2.5,
		Bottom: 2.5,
		Top:    3.5,
	})
}

func checkTightBounds(t *testing.T, got, want vm.Bounds) {
	t.Helper()
	if math.Abs(got.Left-want.Left) > 1e-6 || math.Abs(got.Right-want.Right) > 1e-6 || math.Abs(got.Bottom-want.Bottom) > 1e-6 || math.Abs(got.Top-want.Top) > 1e-6 {
		t.Fatalf("bounds %+v, want %+v", got, want)
	}
}

func TestTightBoundsRotatedConcaveCostume(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	for i := 0; i < 5; i++ {
		pixels.SetNRGBA(i, 0, color.NRGBA{A: 255})
		pixels.SetNRGBA(0, i, color.NRGBA{A: 255})
	}
	costume := &media.Costume{
		Image:   pixels,
		Width:   5,
		Height:  5,
		CenterX: 2.5,
		CenterY: 2.5,
	}
	renderer := New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
	target := vm.DisplayTarget{
		Source: 0,
		State: vm.TargetState{
			Visible:   true,
			Size:      100,
			StretchX:  100,
			StretchY:  100,
			Direction: 45,
		},
	}
	got, ok := renderer.Bounds(target, true)
	if !ok {
		t.Fatal("missing concave costume bounds")
	}
	checkTightBounds(t, got, vm.Bounds{
		Left:   -2.8284271247461903,
		Right:  .7071067811865476,
		Bottom: -2.8284271247461903,
		Top:    2.8284271247461903,
	})
}
