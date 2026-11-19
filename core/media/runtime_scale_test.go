package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"gowarp/core/project"
)

func scaledRuntimeFixture(t testing.TB, ctx context.Context, data []byte, meta project.Costume) *Library {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, meta.MD5Ext), data, 0600); err != nil {
		t.Fatal(err)
	}
	shifted := meta
	shifted.RotationCenterX += 3
	p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{meta, shifted}}}}
	lib, err := LoadRuntime(ctx, dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	return lib
}

func TestRuntimeSVGScaleRerendersSubpixelGeometry(t *testing.T) {
	data := []byte(`<svg viewBox="10 20 8 4"><rect x="12.25" y="20" width="0.25" height="4" fill="red"/></svg>`)
	lib := scaledRuntimeFixture(t, context.Background(), data, project.Costume{MD5Ext: "fine.svg", DataFormat: "svg", RotationCenterX: 14, RotationCenterY: 22})
	costume, shifted := lib.Costumes[0][0], lib.Costumes[0][1]
	base := lib.CostumeImage(costume)
	pixels := append([]byte(nil), base.Pix...)
	scaled := lib.CostumeImageAtScale(costume, 3)
	if scaled == nil || scaled.Bounds() != image.Rect(0, 0, 32, 16) {
		t.Fatalf("adaptive image bounds: %v, error: %v", scaled, lib.Err())
	}
	if p := scaled.NRGBAAt(9, 8); p != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("quarter-pixel vector was enlarged from base pixels: %v", p)
	}
	if scaled.NRGBAAt(8, 8).A != 0 || scaled.NRGBAAt(10, 8).A != 0 {
		t.Fatal("subpixel vector edge or viewBox origin shifted")
	}
	if lib.CostumeImageAtScale(shifted, 4) != scaled || lib.CostumeImageAtScale(costume, 2.5) != scaled {
		t.Fatal("same asset and density bucket were not shared")
	}
	if costume.Width != 8 || costume.Height != 4 || costume.CenterX != 4 || costume.CenterY != 2 || shifted.CenterX != 7 {
		t.Fatal("display raster changed logical geometry")
	}
	if lib.CostumeImage(costume) != base || !bytes.Equal(base.Pix, pixels) || costume.Image != nil {
		t.Fatal("display raster replaced the collision image")
	}
}

func TestRuntimeSVGScaleMixedContentAndClipping(t *testing.T) {
	uri := embeddedImage(t, "png", color.NRGBA{B: 255, A: 255})
	data := []byte(`<svg viewBox="10 20 40 24"><defs><clipPath id="clip"><rect x="2" y="2" width="4" height="4"/></clipPath></defs><g transform="translate(11 21)"><image width="8" height="8" href="` + uri + `" clip-path="url(#clip)"/><rect x="10.25" width="0.25" height="8" fill="red"/><text x="14" y="16" font-size="12" fill="#00ff00">Hi</text></g></svg>`)
	lib := scaledRuntimeFixture(t, context.Background(), data, project.Costume{MD5Ext: "mixed.svg", DataFormat: "svg"})
	im := lib.CostumeImageAtScale(lib.Costumes[0][0], 4)
	if im == nil || im.Bounds() != image.Rect(0, 0, 160, 96) {
		t.Fatalf("mixed adaptive image: %v, %v", im, lib.Err())
	}
	if p := im.NRGBAAt(16, 16); p != (color.NRGBA{B: 255, A: 255}) {
		t.Fatalf("scaled embedded image or clip origin: %v", p)
	}
	for _, p := range []image.Point{{8, 16}, {28, 16}, {16, 8}, {16, 28}} {
		if im.NRGBAAt(p.X, p.Y).A != 0 {
			t.Fatalf("scaled image escaped clip at %v", p)
		}
	}
	if p := im.NRGBAAt(45, 16); p != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("mixed vector did not rerender at requested density: %v", p)
	}
	green := 0
	for y := 12; y < 72; y++ {
		for x := 60; x < 120; x++ {
			if p := im.NRGBAAt(x, y); p.G > 200 && p.A > 200 {
				green++
			}
		}
	}
	if green < 100 {
		t.Fatalf("scaled text missing: %d green pixels", green)
	}
}

func TestRuntimeSVGScaleCacheLimitsEvictionAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lib := scaledRuntimeFixture(t, ctx, []byte(`<svg width="8" height="4"><rect width="8" height="4" fill="red"/></svg>`), project.Costume{MD5Ext: "red.svg", DataFormat: "svg"})
	costume := lib.Costumes[0][0]
	base := lib.CostumeImage(costume)
	lib.cache.limit = 2048
	scaled := lib.CostumeImageAtScale(costume, 4)
	if scaled == nil || scaled.Bounds().Dx() != 32 || lib.cache.bytes > lib.cache.limit {
		t.Fatal("scaled raster did not respect shared cache budget")
	}
	if lib.CostumeImageAtScale(costume, 4) != scaled {
		t.Fatal("scaled raster was not cached")
	}
	if got := lib.CostumeImage(costume); got == nil || got.Bounds().Dx() != 8 {
		t.Fatal("collision raster could not reload after eviction")
	}
	if scaled.NRGBAAt(4, 4).R != 255 || base.NRGBAAt(1, 1).R != 255 {
		t.Fatal("eviction invalidated retained pixels")
	}
	for _, scale := range []float64{0, -1, 1, math.NaN(), math.Inf(1)} {
		if im := lib.CostumeImageAtScale(costume, scale); im == nil || im.Bounds().Dx() != 8 || lib.Err() != nil {
			t.Fatalf("invalid scale %v did not fall back to base: %v", scale, lib.Err())
		}
	}
	var limited *image.NRGBA
	for _, scale := range []float64{8, 64, 1e300} {
		im := lib.CostumeImageAtScale(costume, scale)
		if im == nil || im.Bounds() != image.Rect(0, 0, 32, 16) || lib.Err() != nil {
			t.Fatalf("over-budget scale %v did not retain the largest permitted raster: %v", scale, lib.Err())
		}
		if limited != nil && im != limited {
			t.Fatal("capped requests did not share their density bucket")
		}
		limited = im
	}
	if lib.cache.bytes > lib.cache.limit {
		t.Fatal("fallback exceeded cache budget")
	}
	cancel()
	if lib.CostumeImageAtScale(costume, 4) != nil || lib.Err() != context.Canceled {
		t.Fatal("adaptive raster ignored cancellation")
	}
}

func TestRuntimeBitmapScaleKeepsNativePixels(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	im.SetNRGBA(2, 2, color.NRGBA{R: 255, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, im); err != nil {
		t.Fatal(err)
	}
	lib := scaledRuntimeFixture(t, context.Background(), data.Bytes(), project.Costume{MD5Ext: "native.png", DataFormat: "png", BitmapResolution: 2})
	costume := lib.Costumes[0][0]
	base := lib.CostumeImage(costume)
	if lib.CostumeImageAtScale(costume, 4) != base || costume.Width != 4 || costume.Height != 2 {
		t.Fatal("bitmap display scale changed native pixels or logical size")
	}
	eager := &Library{}
	if eager.CostumeImageAtScale(&Costume{Image: im}, 4) != im {
		t.Fatal("eager library did not retain its base image")
	}
}

func TestRuntimeSVGScaleCapsRasterDimensions(t *testing.T) {
	lib := scaledRuntimeFixture(t, context.Background(), []byte(`<svg width="2048" height="1"><rect width="2048" height="1" fill="red"/></svg>`), project.Costume{MD5Ext: "wide.svg", DataFormat: "svg"})
	im := lib.CostumeImageAtScale(lib.Costumes[0][0], 64)
	if im == nil || im.Bounds() != image.Rect(0, 0, 8192, 4) || lib.Err() != nil {
		t.Fatalf("display density did not stop at the raster dimension limit: %v", lib.Err())
	}
	if p := im.NRGBAAt(8191, 3); p != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("capped raster lost edge pixels: %v", p)
	}
}

func TestRuntimeSVGScalePreservesStrokeWidthAndDashes(t *testing.T) {
	data := []byte(`<svg width="32" height="28"><circle cx="8" cy="8" r="4" fill="none" stroke="red" stroke-width="1"/><path d="M2 16.5h12" fill="none" stroke="red" stroke-width="0.5"/><g fill="none" stroke="red" stroke-width="0.5" stroke-dasharray="2 2" stroke-dashoffset="1"><path d="M2 21h28"/><path d="M2 25h28"/></g></svg>`)
	lib := scaledRuntimeFixture(t, context.Background(), data, project.Costume{MD5Ext: "strokes.svg", DataFormat: "svg"})
	costume := lib.Costumes[0][0]
	base := lib.CostumeImage(costume)
	pixels := append([]byte(nil), base.Pix...)
	im := lib.CostumeImageAtScale(costume, 4)
	if im == nil {
		t.Fatal(lib.Err())
	}
	t.Run("circle", func(t *testing.T) {
		for _, x := range []int{14, 15, 16, 17} {
			if p := im.NRGBAAt(x, 31); p.A < 240 || p.R != 255 {
				t.Fatalf("scaled circle stroke lost its four-pixel width at x=%d: %v", x, p)
			}
		}
		if im.NRGBAAt(12, 31).A != 0 || im.NRGBAAt(19, 31).A != 0 {
			t.Fatal("scaled circle stroke is too wide")
		}
	})
	t.Run("thin line", func(t *testing.T) {
		for _, y := range []int{65, 66} {
			if p := im.NRGBAAt(20, y); p.A != 255 || p.R != 255 {
				t.Fatalf("scaled half-unit line is not two pixels wide at y=%d: %v", y, p)
			}
		}
		if im.NRGBAAt(20, 64).A != 0 || im.NRGBAAt(20, 67).A != 0 {
			t.Fatal("scaled thin line is too wide")
		}
	})
	t.Run("inherited dashes", func(t *testing.T) {
		for _, y := range []int{83, 99} {
			for _, sample := range []struct {
				x     int
				alpha uint8
			}{{9, 255}, {11, 255}, {13, 0}, {19, 0}, {21, 255}, {27, 255}, {29, 0}} {
				if got := im.NRGBAAt(sample.x, y).A; got != sample.alpha {
					t.Fatalf("scaled dash length/offset at (%d,%d): alpha=%d, want %d", sample.x, y, got, sample.alpha)
				}
			}
		}
	})
	if lib.CostumeImage(costume) != base || !bytes.Equal(base.Pix, pixels) {
		t.Fatal("display stroke scaling changed collision pixels")
	}
}

func TestRuntimeSVGScalePreservesUserSpaceGradients(t *testing.T) {
	for _, gradient := range []struct {
		name, definition string
	}{
		{"linear", `<linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="8" y2="0"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>`},
		{"radial", `<radialGradient id="g" gradientUnits="userSpaceOnUse" cx="4" cy="4" r="4"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></radialGradient>`},
		{"object bounds", `<linearGradient id="g"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>`},
	} {
		t.Run(gradient.name, func(t *testing.T) {
			data := []byte(`<svg width="8" height="8"><defs>` + gradient.definition + `</defs><rect width="8" height="8" fill="url(#g)"/></svg>`)
			lib := scaledRuntimeFixture(t, context.Background(), data, project.Costume{MD5Ext: "gradient.svg", DataFormat: "svg"})
			costume := lib.Costumes[0][0]
			base := lib.CostumeImage(costume)
			scaled := lib.CostumeImageAtScale(costume, 4)
			if scaled == nil {
				t.Fatal(lib.Err())
			}
			for _, point := range []image.Point{{1, 3}, {3, 3}, {5, 3}, {6, 3}} {
				want := base.NRGBAAt(point.X, point.Y)
				got := scaled.NRGBAAt(point.X*4+1, point.Y*4+1)
				if math.Abs(float64(got.R)-float64(want.R)) > 12 || math.Abs(float64(got.B)-float64(want.B)) > 12 || got.A != want.A {
					t.Fatalf("gradient moved at logical point %v: got %v, base %v", point, got, want)
				}
			}
		})
	}
}

func TestRuntimeSVGScaleMixedGradientTransformAndOrigin(t *testing.T) {
	uri := embeddedImage(t, "png", color.NRGBA{B: 255, A: 255})
	data := []byte(`<svg viewBox="10 20 8 10"><defs><linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="4" y2="0" gradientTransform="matrix(2 0 0 1 10 20)"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient></defs><rect x="10" y="20" width="8" height="4" fill="url(#g)"/><path d="M10 26h8" fill="none" stroke="url(#g)" stroke-width="1"/><image x="10" y="28" width="2" height="2" href="` + uri + `"/></svg>`)
	lib := scaledRuntimeFixture(t, context.Background(), data, project.Costume{MD5Ext: "mixed-gradient.svg", DataFormat: "svg"})
	im := lib.CostumeImageAtScale(lib.Costumes[0][0], 4)
	if im == nil {
		t.Fatal(lib.Err())
	}
	for _, point := range []image.Point{{6, 6}, {6, 23}} {
		p := im.NRGBAAt(point.X, point.Y)
		if p.R < 201 || p.R > 205 || p.B < 49 || p.B > 53 || p.A != 255 {
			t.Fatalf("mixed fill/stroke gradient transform or viewport origin at %v: %v", point, p)
		}
	}
	if p := im.NRGBAAt(4, 36); p != (color.NRGBA{B: 255, A: 255}) {
		t.Fatalf("mixed gradient displaced embedded image: %v", p)
	}
}
