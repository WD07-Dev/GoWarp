package render

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"runtime"
	"testing"

	"gowarp/core/media"
	"gowarp/core/vm"
)

func shapedHullFixture() (*Renderer, vm.DisplayTarget) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			if (x-32)*(x-32)+(y-32)*(y-32) < 400 {
				pixels.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
			}
		}
	}
	c := &media.Costume{Image: pixels, Width: 64, Height: 64, CenterX: 32, CenterY: 32}
	r := New(&media.Library{Costumes: [][]*media.Costume{{c}}})
	target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, Effects: vm.GraphicEffects{Whirl: 65}}}
	return r, target
}

func TestShapedCloneBoundsReuseHull(t *testing.T) {
	r, target := shapedHullFixture()
	if _, ok := r.Bounds(target, true); !ok {
		t.Fatal("initial bounds unavailable")
	}
	allocations := testing.AllocsPerRun(20, func() {
		target.State.CloneID++
		target.State.X++
		target.State.Direction++
		target.State.Effects.Ghost++
		if _, ok := r.Bounds(target, true); !ok {
			t.Fatal("clone bounds unavailable")
		}
	})
	if allocations != 0 {
		t.Fatalf("unchanged shaped costume rebuilt its hull: %g allocations", allocations)
	}
}

func TestShapedHullCachePreservesBounds(t *testing.T) {
	r, target := shapedHullFixture()
	for _, effects := range []vm.GraphicEffects{
		{Whirl: 65}, {Whirl: 65, Ghost: 100}, {Mosaic: 20}, {Pixelate: 50},
		{Fisheye: -70}, {Whirl: -80, Mosaic: 10, Pixelate: 35, Fisheye: 45}, {},
		{Mosaic: math.NaN()}, {Whirl: math.Inf(1)}, {Whirl: 65},
	} {
		target.State.Effects = effects
		for _, direction := range []float64{90, 37, -90} {
			target.State.Direction = direction
			target.State.X += 1.25
			target.State.Y -= 2.5
			target.State.StretchX = -80
			target.State.StretchY = 135
			target.State.Size = 125
			want, wantOK := New(r.Assets).Bounds(target, true)
			got, gotOK := r.Bounds(target, true)
			if got != want || gotOK != wantOK {
				t.Fatalf("effects %+v direction %g: got %+v/%v, want %+v/%v", effects, direction, got, gotOK, want, wantOK)
			}
		}
	}
	c := r.Assets.Costumes[0][0]
	c.Image = image.NewNRGBA(image.Rect(0, 0, 64, 64))
	c.Image.SetNRGBA(0, 0, color.NRGBA{A: 255})
	for _, width := range []float64{64, 32} {
		c.Width = width
		want, wantOK := New(r.Assets).Bounds(target, true)
		got, gotOK := r.Bounds(target, true)
		if got != want || gotOK != wantOK {
			t.Fatalf("replaced costume: got %+v/%v, want %+v/%v", got, gotOK, want, wantOK)
		}
	}
	c.Image = nil
	if _, ok := r.Bounds(target, true); ok {
		t.Fatal("cache accepted missing costume pixels")
	}
}

func TestShapedHullCacheDoesNotRetainPixels(t *testing.T) {
	r := func() *Renderer {
		r, target := shapedHullFixture()
		r.Bounds(target, true)
		r.Assets.Costumes[0][0].Image = nil
		return r
	}()
	runtime.GC()
	if r.shapedHull.image.Value() != nil {
		t.Fatal("shaped hull retained evicted image pixels")
	}
}

func TestShapedHullCacheLimitsRetainedPoints(t *testing.T) {
	r, target := shapedHullFixture()
	c := r.Assets.Costumes[0][0]
	c.Width, c.Height = 2, 5000
	c.Image = image.NewNRGBA(image.Rect(0, 0, 2, 5000))
	for i := range c.Image.Pix {
		c.Image.Pix[i] = 255
	}
	target.State.Effects = vm.GraphicEffects{Mosaic: 10}
	if _, ok := r.Bounds(target, true); !ok {
		t.Fatal("large silhouette bounds unavailable")
	}
	if cap(r.shapedHull.points)*16 > 128<<10 {
		t.Fatal("shaped hull cache exceeds its memory budget")
	}
}

func TestShapedHullCacheHiddenAndTransparentFallback(t *testing.T) {
	r, target := shapedHullFixture()
	r.Bounds(target, true)
	target.State.Visible = false
	got, ok := r.Bounds(target, true)
	want, wantOK := r.Bounds(target, false)
	if got != want || ok != wantOK {
		t.Fatalf("hidden bounds %+v/%v, want %+v/%v", got, ok, want, wantOK)
	}
	target.State.Visible = true
	r.Assets.Costumes[0][0].Image = image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for i := 0; i < 2; i++ {
		got, ok = r.Bounds(target, true)
		if got != want || ok != wantOK {
			t.Fatalf("transparent bounds %+v/%v, want %+v/%v", got, ok, want, wantOK)
		}
	}
}

func BenchmarkCloneTightBounds(b *testing.B) {
	for _, clones := range []int{1, 100, 1000} {
		for _, shaped := range []bool{false, true} {
			b.Run(fmt.Sprintf("clones=%d/shaped=%v", clones, shaped), func(b *testing.B) {
				r, target := shapedHullFixture()
				if !shaped {
					target.State.Effects = vm.GraphicEffects{}
				}
				r.Bounds(target, true)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					for clone := 0; clone < clones; clone++ {
						target.State.CloneID = clone + 1
						target.State.X = float64(clone%480) - 240
						target.State.Y = float64(clone%360) - 180
						target.State.Direction = float64(clone % 360)
						r.Bounds(target, true)
					}
				}
			})
		}
	}
}
