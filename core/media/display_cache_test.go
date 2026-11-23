package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"gowarp/core/project"
)

func bitmapCacheFixture(t testing.TB) *Library {
	t.Helper()
	pixels := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	pixels.SetNRGBA(2, 2, color.NRGBA{R: 255, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, pixels); err != nil {
		t.Fatal(err)
	}
	return scaledRuntimeFixture(t, context.Background(), data.Bytes(), project.Costume{
		MD5Ext: "0123456789abcdef0123456789abcdef.png", DataFormat: "png", BitmapResolution: 2,
	})
}

func TestRuntimeCostumeCacheHitsDoNotAllocate(t *testing.T) {
	for _, format := range []string{"png", "svg"} {
		t.Run(format, func(t *testing.T) {
			var lib *Library
			if format == "png" {
				lib = bitmapCacheFixture(t)
			} else {
				lib = scaledRuntimeFixture(t, context.Background(), []byte(`<svg width="8" height="4"><rect width="8" height="4" fill="red"/></svg>`), project.Costume{
					MD5Ext: "0123456789abcdef0123456789abcdef.svg", DataFormat: "svg",
				})
			}
			first, shifted := lib.Costumes[0][0], lib.Costumes[0][1]
			pixels := lib.CostumeImage(first)
			want := "image:" + format + ":0123456789abcdef0123456789abcdef." + format
			allocs := testing.AllocsPerRun(100, func() {
				key, density := lib.CostumeImageCacheKey(shifted, 1)
				if key != want || density != 1 || lib.CostumeImageAtScale(shifted, 1) != pixels {
					t.Fatal("duplicate costume did not share its cached pixels")
				}
			})
			if allocs != 0 {
				t.Fatalf("cached costume lookup allocates %.0f times", allocs)
			}
			if first.CenterX == shifted.CenterX || pixels.NRGBAAt(2, 2) != (color.NRGBA{R: 255, A: 255}) {
				t.Fatal("cache reuse changed costume geometry or pixels")
			}
		})
	}
}

func BenchmarkRuntimeCostumeCacheHits(b *testing.B) {
	lib := bitmapCacheFixture(b)
	costume := lib.Costumes[0][1]
	b.Run("DisplayIdentity", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			for range 1000 {
				lib.CostumeImageCacheKey(costume, 4)
			}
		}
	})
	b.Run("CachedPixels", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			for range 1000 {
				lib.CostumeImageAtScale(costume, 4)
			}
		}
	})
}

func TestDisplayCacheKeySharesCappedSVGRequests(t *testing.T) {
	lib := scaledRuntimeFixture(t, context.Background(), []byte(`<svg width="8" height="4"><rect width="8" height="4" fill="red"/></svg>`), project.Costume{MD5Ext: "red.svg", DataFormat: "svg"})
	lib.cache.limit = 2048
	costume := lib.Costumes[0][0]
	key, density := lib.CostumeImageCacheKey(costume, 4)
	for _, scale := range []float64{8, 64, 1e300} {
		got, d := lib.CostumeImageCacheKey(lib.Costumes[0][1], scale)
		if got != key || d != density {
			t.Errorf("scale %g did not share the capped raster identity", scale)
		}
		pixels := lib.CostumeImageAtScale(costume, scale)
		if pixels == nil || pixels.Bounds().Dx() != 32 || pixels.Bounds().Dy() != 16 {
			t.Errorf("scale %g cache identity disagreed with decoded raster", scale)
		}
	}
}
