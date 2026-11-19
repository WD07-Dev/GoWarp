package render

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"testing"

	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
)

/*
These reference functions retain the original per-pixel arithmetic to detect
pixel and collision changes when optimizing the renderer's hot paths.
*/
func referenceSample(tr transform, x, y float64, e vm.GraphicEffects, ignoreGhost bool) color.NRGBA {
	dx, dy := x-tr.x, y-tr.y
	u := ((tr.cos*dx+tr.sin*dy)/tr.sx + tr.c.CenterX) / tr.c.Width
	v := (tr.c.CenterY - (-tr.sin*dx+tr.cos*dy)/tr.sy) / tr.c.Height
	if u < 0 || v < 0 || u >= 1 || v >= 1 {
		return color.NRGBA{}
	}
	u, v = referenceEffectUV(u, v, tr.c.Width, tr.c.Height, e)
	if math.IsNaN(u) || math.IsNaN(v) || math.IsInf(u, 0) || math.IsInf(v, 0) {
		return color.NRGBA{}
	}
	u = math.Max(0, math.Min(math.Nextafter(1, 0), u))
	v = math.Max(0, math.Min(math.Nextafter(1, 0), v))
	c := tr.c.Image.NRGBAAt(int(u*float64(tr.c.Image.Bounds().Dx())), int(v*float64(tr.c.Image.Bounds().Dy())))
	if ignoreGhost {
		return c
	}
	return referenceEffectColor(c, e)
}

func referenceEffectUV(u, v, w, h float64, e vm.GraphicEffects) (float64, float64) {
	mosaic := math.Max(1, math.Min(512, math.Floor((math.Abs(e.Mosaic)+10)/10+.5)))
	if mosaic != 1 {
		u, v = u*mosaic, v*mosaic
		u -= math.Floor(u)
		v -= math.Floor(v)
	}
	if pixels := math.Abs(e.Pixelate) / 10; pixels > 0 {
		nx, ny := w/pixels, h/pixels
		u, v = (math.Floor(u*nx)+.5)/nx, (math.Floor(v*ny)+.5)/ny
	}
	if e.Whirl != 0 {
		dx, dy := u-.5, v-.5
		factor := math.Max(1-math.Hypot(dx, dy)/.5, 0)
		s, c := math.Sincos(-e.Whirl * math.Pi / 180 * factor * factor)
		u, v = c*dx+s*dy+.5, -s*dx+c*dy+.5
	}
	if e.Fisheye != 0 {
		dx, dy := (u-.5)*2, (v-.5)*2
		length := math.Hypot(dx, dy)
		if length > 0 {
			r := math.Pow(math.Min(length, 1), math.Max(0, (e.Fisheye+100)/100)) * math.Max(1, length)
			u, v = .5+.5*r*dx/length, .5+.5*r*dy/length
		}
	}
	return u, v
}

func referenceEffectColor(c color.NRGBA, e vm.GraphicEffects) color.NRGBA {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	if e.Color != 0 && !math.IsInf(e.Color, 0) && !math.IsNaN(e.Color) {
		max, min := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
		delta := max - min
		h, s := float64(0), float64(0)
		if max > 0 {
			s = delta / max
		}
		if delta > 0 {
			switch max {
			case r:
				h = (g - b) / delta
			case g:
				h = (b-r)/delta + 2
			default:
				h = (r-g)/delta + 4
			}
			h /= 6
		}
		if max < .055 {
			h, s, max = 0, 1, .055
		} else if s < .09 {
			h, s = 0, .09
		}
		h = math.Mod(h+e.Color/200, 1)
		if h < 0 {
			h++
		}
		chroma := max * s
		hue := func(n float64) float64 {
			return math.Max(0, math.Min(1, n))*chroma + max - chroma
		}
		r, g, b = hue(math.Abs(h*6-3)-1), hue(2-math.Abs(h*6-2)), hue(2-math.Abs(h*6-4))
	}
	brightness := math.Max(-100, math.Min(100, e.Brightness)) / 100
	channel := func(n float64) uint8 {
		if math.IsNaN(n) {
			return 0
		}
		return uint8(math.Round(math.Max(0, math.Min(1, n)) * 255))
	}
	c.R, c.G, c.B = channel(r+brightness), channel(g+brightness), channel(b+brightness)
	c.A = channel(float64(c.A) / 255 * (1 - math.Max(0, math.Min(100, e.Ghost))/100))
	return c
}
func referenceFromScreen(r *Renderer, x, y float64) (float64, float64) {
	v := r.view
	zoom := v.Zoom / 100
	if zoom == 0 || !finite(zoom) {
		return math.NaN(), math.NaN()
	}
	x = (x - float64(r.frame.Bounds().Dx())/2) * (v.Width / float64(r.frame.Bounds().Dx())) / zoom
	y = (float64(r.frame.Bounds().Dy())/2 - y) * (v.Height / float64(r.frame.Bounds().Dy())) / zoom
	s, c := math.Sincos((90 - v.Direction) * math.Pi / 180)
	return c*x + s*y + v.X, c*y - s*x + v.Y
}

func optimizationFixture() (*Renderer, vm.DisplayTarget) {
	im := image.NewNRGBA(image.Rect(0, 0, 13, 9))
	for y := 0; y < 9; y++ {
		for x := 0; x < 13; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(x * 19), uint8(y * 29), uint8((x + y) * 11), uint8((x*7 + y*11) % 5 * 63)})
		}
	}
	c := &media.Costume{Image: im, Width: 13, Height: 9, CenterX: 5.25, CenterY: 3.75}
	r := New(&media.Library{Costumes: [][]*media.Costume{{c}}})
	target := vm.DisplayTarget{Source: 0, State: vm.TargetState{Visible: true, Size: 175, StretchX: 130, StretchY: 80, Direction: 90, X: 1.75, Y: -2.25}}
	return r, target
}

func TestOptimizedPixelsMatchReference(t *testing.T) {
	effects := []vm.GraphicEffects{{}, {Ghost: 37}, {Brightness: -30}, {Color: 60}, {Mosaic: 20, Pixelate: 15, Whirl: 60, Fisheye: -40}, {Color: 35, Brightness: 22, Ghost: 51}, {Mosaic: math.NaN()}, {Ghost: math.NaN()}}
	for _, direction := range []float64{90, 0, 37, -90, 227} {
		for ei, e := range effects {
			t.Run(fmt.Sprintf("direction=%g/effect=%d", direction, ei), func(t *testing.T) {
				r, target := optimizationFixture()
				target.State.Direction = direction
				target.State.Effects = e
				if direction < 0 {
					target.State.StretchX = -130
				}
				r.SetView(vm.View{Width: 80.25, Height: 60.75, Zoom: 147, Direction: direction + 11, X: -3.5, Y: 2.25, Color: "#426193"})
				tr, ok := r.transform(target)
				if !ok {
					t.Fatal("transform failed")
				}
				want := image.NewRGBA(r.frame.Bounds())
				draw.Draw(want, want.Bounds(), image.NewUniform(vm.ParseColor(r.view.Color)), image.Point{}, draw.Src)
				bounds := r.screenBounds(tr.worldBounds(image.Rectangle{}))
				for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
					for x := bounds.Min.X; x < bounds.Max.X; x++ {
						wx, wy := referenceFromScreen(r, float64(x)+.5, float64(y)+.5)
						blendRGBA(want, x, y, referenceSample(tr, wx, wy, e, false))
					}
				}
				got := r.Draw([]vm.DisplayTarget{target})
				if !bytes.Equal(got.Pix, want.Pix) {
					t.Fatal("rendered pixels differ from original arithmetic")
				}
				for y := -15.0; y <= 15; y += .5 {
					for x := -15.0; x <= 15; x += .5 {
						for _, ignoreGhost := range []bool{false, true} {
							if got, want := tr.sample(x, y, e, ignoreGhost), referenceSample(tr, x, y, e, ignoreGhost); got != want {
								t.Fatalf("sample (%g,%g), ignoreGhost=%v: got %v want %v", x, y, ignoreGhost, got, want)
							}
						}
					}
				}
			})
		}
	}
}

func TestNeutralEffectsMatchReference(t *testing.T) {
	for channel := 0; channel < 256; channel++ {
		for alpha := 0; alpha < 256; alpha++ {
			c := color.NRGBA{uint8(channel), uint8(255 - channel), uint8(channel / 2), uint8(alpha)}
			if got, want := effectColor(c, vm.GraphicEffects{}), referenceEffectColor(c, vm.GraphicEffects{}); got != want {
				t.Fatalf("neutral color: got %v want %v", got, want)
			}
		}
	}
}

func TestGhostOnlyEffectsMatchReference(t *testing.T) {
	for _, ghost := range []float64{-20, 0, 37.5, 100, 120, math.NaN()} {
		for channel := 0; channel < 256; channel++ {
			for alpha := 0; alpha < 256; alpha++ {
				c := color.NRGBA{uint8(channel), uint8(255 - channel), uint8(channel / 2), uint8(alpha)}
				e := vm.GraphicEffects{Ghost: ghost}
				if got, want := effectColor(c, e), referenceEffectColor(c, e); got != want {
					t.Fatalf("ghost %g: got %v want %v", ghost, got, want)
				}
			}
		}
	}
}

func TestPenBlendMatchesReference(t *testing.T) {
	pixel := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	for srcAlpha := 0; srcAlpha < 256; srcAlpha++ {
		for dstAlpha := 0; dstAlpha < 256; dstAlpha++ {
			src := color.NRGBA{173, 59, 241, uint8(srcAlpha)}
			dst := color.NRGBA{27, 193, 108, uint8(dstAlpha)}
			want := dst
			if srcAlpha != 0 {
				a, b := float64(src.A)/255, float64(dst.A)/255
				out := a + b*(1-a)
				mix := func(n, m uint8) uint8 {
					return uint8(math.Round((float64(n)*a + float64(m)*b*(1-a)) / out))
				}
				want = color.NRGBA{mix(src.R, dst.R), mix(src.G, dst.G), mix(src.B, dst.B), uint8(math.Round(out * 255))}
			}
			pixel.SetNRGBA(0, 0, dst)
			blendNRGBA(pixel, 0, 0, src)
			if got := pixel.NRGBAAt(0, 0); got != want {
				t.Fatalf("alphas %d/%d: got %v want %v", srcAlpha, dstAlpha, got, want)
			}
		}
	}
}

func TestPenStampsMatchReference(t *testing.T) {
	for _, direction := range []float64{90, 37} {
		for _, stretch := range []float64{-130, 80, 175} {
			for _, effects := range []vm.GraphicEffects{{}, {Ghost: 37}, {Color: 80, Brightness: -20}, {Mosaic: 20}, {Ghost: 100}} {
				r, target := optimizationFixture()
				r.SetView(vm.View{Width: 40.25, Height: 30.75, Zoom: 100, Direction: 90})
				target.State.Direction = direction
				target.State.StretchX = stretch
				target.State.Effects = effects
				tr, ok := r.transform(target)
				if !ok {
					t.Fatal("transform failed")
				}
				want := image.NewNRGBA(image.Rect(0, 0, 41, 31))
				for pass := 0; pass < 2; pass++ {
					r.Pen(vm.PenCommand{Kind: "stamp", Target: target})
					for y := 0; y < want.Bounds().Dy(); y++ {
						for x := 0; x < want.Bounds().Dx(); x++ {
							wx, wy := float64(x)+.5-r.view.Width/2, r.view.Height/2-float64(y)-.5
							blendNRGBA(want, x, y, referenceSample(tr, wx, wy, effects, false))
						}
					}
				}
				if r.pen == nil {
					if effects.Ghost != 100 {
						t.Fatal("visible stamp is missing")
					}
					continue
				}
				if !bytes.Equal(r.pen.Pix, want.Pix) {
					t.Fatalf("stamp differs: direction=%g stretch=%g effects=%+v", direction, stretch, effects)
				}
			}
		}
	}
}

func BenchmarkPenStampFrame(b *testing.B) {
	for _, ghost := range []float64{0, 50, 100} {
		b.Run(fmt.Sprintf("ghost=%g", ghost), func(b *testing.B) {
			img := image.NewNRGBA(image.Rect(0, 0, 480, 360))
			draw.Draw(img, img.Bounds(), image.NewUniform(color.NRGBA{128, 64, 192, 255}), image.Point{}, draw.Src)
			costume := &media.Costume{Image: img, Width: 480, Height: 360, CenterX: 240, CenterY: 180}
			r := New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
			target := vm.DisplayTarget{State: vm.TargetState{
				Size: 100, StretchX: 100, StretchY: 100, Direction: 90,
				Effects: vm.GraphicEffects{Ghost: ghost},
			}}
			command := vm.PenCommand{Kind: "stamp", Target: target}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				r.Pen(vm.PenCommand{Kind: "clear"})
				r.Pen(command)
			}
		})
	}
}

func TestDrawPenMatchesReferenceCamera(t *testing.T) {
	for _, direction := range []float64{90, 37, -130} {
		r, _ := optimizationFixture()
		r.SetView(vm.View{Width: 80.25, Height: 60.75, Zoom: 147, Direction: direction, X: -3.5, Y: 2.25, Color: "#426193"})
		r.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{-12, -8, 20, 15}, Size: 7, Color: color.NRGBA{R: 200, G: 13, B: 53, A: 197}})
		want := image.NewRGBA(r.frame.Bounds())
		draw.Draw(want, want.Bounds(), image.NewUniform(vm.ParseColor(r.view.Color)), image.Point{}, draw.Src)
		for y := 0; y < want.Bounds().Dy(); y++ {
			for x := 0; x < want.Bounds().Dx(); x++ {
				wx, wy := referenceFromScreen(r, float64(x)+.5, float64(y)+.5)
				blendRGBA(want, x, y, r.penAt(wx, wy))
			}
		}
		if got := r.Draw(nil); !bytes.Equal(got.Pix, want.Pix) {
			t.Fatalf("pen pixels differ for direction %g", direction)
		}
	}
}

func BenchmarkDrawNeutral(b *testing.B) {
	r, target := optimizationFixture()
	target.State.Size = 3000
	for b.Loop() {
		r.Draw([]vm.DisplayTarget{target})
	}
}

func TestTransformPinsCostumePixels(t *testing.T) {
	r, target := optimizationFixture()
	tr, ok := r.transform(target)
	if !ok {
		t.Fatal("transform failed")
	}
	want := tr.sample(2, -2, vm.GraphicEffects{}, false)
	wantBounds := tr.worldBounds(image.Rect(1, 1, 10, 7))
	wantHull := touchingHull(tr, vm.GraphicEffects{})
	tr.c.Image = nil
	if tr.image == nil {
		t.Fatal("transform did not retain its pixels")
	}
	if got := tr.sample(2, -2, vm.GraphicEffects{}, false); got != want {
		t.Fatalf("pinned sample = %v, want %v", got, want)
	}
	if got := tr.worldBounds(image.Rect(1, 1, 10, 7)); got != wantBounds {
		t.Fatalf("pinned cropped bounds = %v, want %v", got, wantBounds)
	}
	gotHull := touchingHull(tr, vm.GraphicEffects{})
	if len(gotHull) != len(wantHull) {
		t.Fatalf("pinned hull length = %d, want %d", len(gotHull), len(wantHull))
	}
	for i, point := range gotHull {
		if point != wantHull[i] {
			t.Fatalf("pinned hull point = %v, want %v", point, wantHull[i])
		}
	}
}

func TestCachedCameraMatchesOriginalArithmetic(t *testing.T) {
	r, _ := optimizationFixture()
	equal := func(a, b float64) bool {
		return math.Float64bits(a) == math.Float64bits(b) || (math.IsNaN(a) && math.IsNaN(b))
	}
	for _, zoom := range []float64{0, 25, 100, 147, -300, math.Inf(1), math.NaN()} {
		for _, direction := range []float64{90, 0, 37, -181, math.Inf(1), math.NaN()} {
			r.SetView(vm.View{Width: 80.25, Height: 60.75, Zoom: zoom, Direction: direction, X: -3.5, Y: 2.25})
			camera := r.screenToWorld()
			for _, position := range []float64{0, .5, 17.5, 31, 100, math.NaN()} {
				wantX, wantY := referenceFromScreen(r, position, position)
				x, y := camera.point(position, camera.rowY(position))
				if !equal(x, wantX) || !equal(y, wantY) {
					t.Fatalf("camera at zoom %g direction %g: (%g,%g), want (%g,%g)", zoom, direction, x, y, wantX, wantY)
				}
			}
		}
	}
}

func TestLazyCostumeRenderingAndMetadataSize(t *testing.T) {
	dir := t.TempDir()
	for name, svg := range map[string]string{
		"good.svg": `<svg width="4" height="4"><rect width="4" height="4" fill="#ff0000"/></svg>`,
		"bad.svg":  `<svg width="7" height="5"><foreignObject>bad</foreignObject></svg>`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(svg), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{
		{MD5Ext: "good.svg", DataFormat: "svg", RotationCenterX: 2, RotationCenterY: 2},
		{MD5Ext: "bad.svg", DataFormat: "svg"},
	}}}}
	lib, err := media.LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	r := New(lib)
	target := vm.DisplayTarget{Source: 0, State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
	if lib.Costumes[0][0].Image != nil {
		t.Fatal("fixture must use separately cached image pixels")
	}
	if got := r.Draw([]vm.DisplayTarget{target}).RGBAAt(240, 180); got != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("lazy costume pixel: %v", got)
	}
	if _, ok := r.Hit([]vm.DisplayTarget{target}, 0, 0); !ok {
		t.Fatal("lazy costume was not hit")
	}
	target.State.CurrentCostume = 1
	if w, h := r.CostumeSize(target); w != 7 || h != 5 {
		t.Fatalf("lazy metadata size: %g,%g", w, h)
	}
	if lib.Err() != nil {
		t.Fatalf("CostumeSize decoded an unused costume: %v", lib.Err())
	}
	if _, ok := r.transform(target); ok || lib.Err() == nil {
		t.Fatal("deferred decode failure was not reported")
	}
}

func TestColorSensingDoesNotDecodeDistantCostumes(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{
		"good.svg":   `<svg width="4" height="4"><rect width="4" height="4" fill="red"/></svg>`,
		"unused.svg": `<svg width="7" height="5"><foreignObject>unused</foreignObject></svg>`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{
		{MD5Ext: "good.svg", DataFormat: "svg", RotationCenterX: 2, RotationCenterY: 2},
		{MD5Ext: "unused.svg", DataFormat: "svg"},
	}}}}
	lib, err := media.LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	r := New(lib)
	r.view.HighQualityPen, r.view.OffscreenTouching = true, true
	target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, X: 300.25}}
	distant := target
	distant.State.CloneID, distant.State.CurrentCostume, distant.State.X = 1, 1, 1000
	if r.TouchingColor([]vm.DisplayTarget{target, distant}, target, color.NRGBA{G: 255, A: 255}, nil) {
		t.Fatal("distant costume touched the target")
	}
	if err := lib.Err(); err != nil {
		t.Fatalf("decoded unrelated collision pixels: %v", err)
	}
}
