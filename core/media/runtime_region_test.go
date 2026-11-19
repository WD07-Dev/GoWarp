package media

import (
	"container/list"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

func TestPlaybackBitmapRegionDoesNotExpandWholeImage(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		pixels := image.NewNRGBA(image.Rect(0, 0, 96, 80))
		for y := 0; y < 80; y++ {
			for x := 0; x < 96; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x / 17), G: uint8(y / 11), B: 123, A: uint8(y / 19 * 60)})
			}
		}
		bitmap := packPlaybackBitmap(&cachedAsset{image: pixels, repeated: repeated}, 1<<20)
		if bitmap == nil {
			t.Fatal("fixture did not compress")
		}
		costume := &Costume{Bitmap: true}
		cache := &runtimeCache{
			ctx: context.Background(), entries: map[string]*list.Element{}, limit: 1 << 20,
			costumes: map[*Costume]costumeSource{costume: {key: "bitmap"}},
			playback: map[string]*playbackBitmap{"bitmap": bitmap},
		}
		lib := &Library{cache: cache}
		scale := 1
		if repeated {
			scale = 2
		}
		for _, region := range []image.Rectangle{
			image.Rect(3, 5, 41, 37), image.Rect(-9, -5, 19, 21),
			image.Rect(81, 71, 200, 180), image.Rect(300, 300, 320, 320),
			image.Rect(0, 0, 96*scale, 80*scale),
		} {
			got := lib.CostumeImageRegionAtScale(costume, 1, region)
			want := region.Intersect(image.Rect(0, 0, 96*scale, 80*scale))
			if want.Empty() {
				if got != nil {
					t.Fatal("empty region returned pixels")
				}
				continue
			}
			if got == nil || got.Bounds() != want {
				t.Fatalf("region bounds differ: want %v", want)
			}
			for y := want.Min.Y; y < want.Max.Y; y++ {
				for x := want.Min.X; x < want.Max.X; x++ {
					if got.NRGBAAt(x, y) != pixels.NRGBAAt(x/scale, y/scale) {
						t.Fatalf("repeated=%v pixel mismatch at %d,%d", repeated, x, y)
					}
				}
			}
		}
		if len(cache.entries) != 0 || cache.bytes != 0 {
			t.Fatal("cropping restored the full image into the cache")
		}
	}
}

func BenchmarkPlaybackBitmapRegion(b *testing.B) {
	costume := &Costume{Bitmap: true}
	cache := &runtimeCache{
		ctx: context.Background(), entries: map[string]*list.Element{}, limit: 1 << 20,
		costumes: map[*Costume]costumeSource{costume: {key: "bitmap"}},
		playback: map[string]*playbackBitmap{"bitmap": {
			size: image.Pt(4096, 4096),
			runs: []uint32{4096 * 4096, binary.LittleEndian.Uint32([]byte{127, 23, 84, 128})},
		}},
	}
	lib := &Library{cache: cache}
	b.ReportAllocs()
	for b.Loop() {
		lib.CostumeImageRegionAtScale(costume, 1, image.Rect(11, 13, 75, 77))
	}
}

func TestDisplayBitmapRegionMatchesOriginalPixels(t *testing.T) {
	compact := image.NewNRGBA(image.Rect(7, 11, 39, 43))
	for y := 11; y < 43; y++ {
		for x := 7; x < 39; x++ {
			compact.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 7), G: uint8(y * 5), A: 128})
		}
	}
	costume := &Costume{Bitmap: true, Width: 64, Height: 64}
	cache := &runtimeCache{ctx: context.Background(), entries: map[string]*list.Element{}, limit: 1 << 20, costumes: map[*Costume]costumeSource{costume: {key: "bitmap"}}}
	cache.put(&cachedAsset{key: "bitmap", image: compact, repeated: true, bytes: len(compact.Pix)})
	lib := &Library{cache: cache}
	for _, region := range []image.Rectangle{image.Rect(1, 3, 18, 20), image.Rect(0, 0, 64, 64), image.Rect(-2, -3, 9, 11), image.Rect(90, 90, 100, 100)} {
		got := lib.CostumeImageRegionAtScale(costume, 1, region)
		want := region.Intersect(image.Rect(0, 0, 64, 64))
		if want.Empty() {
			if got != nil {
				t.Fatal("empty region returned pixels")
			}
			continue
		}
		if got.Bounds() != want {
			t.Fatalf("bounds=%v want %v", got.Bounds(), want)
		}
		for y := want.Min.Y; y < want.Max.Y; y++ {
			for x := want.Min.X; x < want.Max.X; x++ {
				if got.NRGBAAt(x, y) != compact.NRGBAAt(7+x/2, 11+y/2) {
					t.Fatalf("pixel mismatch %d,%d", x, y)
				}
			}
		}
	}
	if cache.get("bitmap").original.Value() != nil {
		t.Fatal("region extraction expanded full bitmap")
	}
}

func BenchmarkCompactBitmapRegion(b *testing.B) {
	compact := image.NewNRGBA(image.Rect(0, 0, 2048, 2048))
	costume := &Costume{Bitmap: true, Width: 4096, Height: 4096}
	cache := &runtimeCache{ctx: context.Background(), entries: map[string]*list.Element{}, limit: 32 << 20, costumes: map[*Costume]costumeSource{costume: {key: "bitmap"}}}
	cache.put(&cachedAsset{key: "bitmap", image: compact, repeated: true, bytes: len(compact.Pix)})
	lib := &Library{cache: cache}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		lib.CostumeImageRegionAtScale(costume, 1, image.Rect(11, 13, 75, 77))
	}
}

func TestCostumeRegionPreservesStridedPixels(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(7, 11, 39, 43))
	for y := 11; y < 43; y++ {
		for x := 7; x < 39; x++ {
			pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), A: 255})
		}
	}
	for _, bitmap := range []bool{false, true} {
		costume := &Costume{Image: pixels, Bitmap: bitmap, Width: 32, Height: 32}
		lib := &Library{}
		got := lib.CostumeImageRegionAtScale(costume, 1, image.Rect(3, 5, 19, 23))
		if got == nil || got.Bounds() != image.Rect(3, 5, 19, 23) {
			t.Fatal("region bounds changed")
		}
		for y := 5; y < 23; y++ {
			for x := 3; x < 19; x++ {
				if got.NRGBAAt(x, y) != pixels.NRGBAAt(x+7, y+11) {
					t.Fatalf("strided pixel mismatch at %d,%d", x, y)
				}
			}
		}
	}
}
