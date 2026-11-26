package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gowarp/core/project"
)

func TestPreparedBitmapSurvivesPixelEviction(t *testing.T) {
	dir := t.TempDir()
	want := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for y := 18; y < 42; y++ {
		for x := 22; x < 64; x++ {
			want.SetNRGBA(x, y, color.NRGBA{R: 123, G: 45, B: 67, A: 89})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, want); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sprite.png")
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{
		{MD5Ext: "sprite.png", DataFormat: "png", BitmapResolution: 2},
		{MD5Ext: "sprite.png", DataFormat: "png", BitmapResolution: 2, RotationCenterX: 100},
	}}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	limit := lib.cache.limit
	if err := lib.PrepareCostumePlayback(nil); err != nil {
		t.Fatal(err)
	}
	stored := 0
	for _, bitmap := range lib.cache.playback {
		stored += cap(bitmap.runs) * 4
	}
	if len(lib.cache.playback) != 1 || lib.cache.limit+stored > limit {
		t.Fatalf("aliases duplicated preparation or exceeded the cache allowance: assets=%d bytes=%d", len(lib.cache.playback), stored)
	}
	reservedLimit := lib.cache.limit
	if err := lib.PrepareCostumePlayback(nil); err != nil || lib.cache.limit != reservedLimit {
		t.Fatal("repeated preparation changed the reservation", err)
	}
	if err := lib.EnableDisplayPreparation(dir, p); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, costume := range lib.Costumes[0] {
		clear(lib.cache.entries)
		lib.cache.recent.Init()
		lib.cache.bytes = 0
		if !lib.PrepareDisplayBitmap(costume) {
			t.Fatal("packed bitmap was not queued for pixel preparation")
		}
		key, _ := lib.CostumeImageCacheKey(costume, 1)
		work := lib.cache.display.bitmapPending[key]
		select {
		case <-work.done:
		case <-time.After(2 * time.Second):
			t.Fatal("packed bitmap preparation did not finish")
		}
		if work.result.err != nil || work.result.asset == nil {
			t.Fatalf("packed bitmap reread the removed PNG: %v", work.result.err)
		}
		asset := work.result.asset
		prepared := asset.image
		pixels, repeated := lib.DisplayCostumeImage(costume, 1)
		if pixels != prepared {
			t.Fatal("drawing expanded the packed bitmap again instead of reusing prepared pixels")
		}
		if asset.original.Value() != nil {
			t.Fatal("display unnecessarily reconstructed full collision pixels")
		}
		if pixels == nil || !repeated {
			t.Fatalf("display reread the PNG after eviction: repeated=%v err=%v", repeated, lib.Err())
		}
		if pixels.NRGBAAt(11, 9) != (color.NRGBA{R: 123, G: 45, B: 67, A: 89}) {
			t.Fatal("prepared display changed translucent pixels")
		}
		got := lib.CostumeImage(costume)
		if got == nil || !bytes.Equal(got.Pix, want.Pix) {
			t.Fatal("prepared collision pixels differ from the original")
		}
	}
}

func TestPackedBitmapLookaheadUsesPreparedPixels(t *testing.T) {
	lib, dir := bitmapDisplayFixture(t)
	if err := lib.PrepareCostumePlayback(nil); err != nil {
		t.Fatal(err)
	}
	first, next := lib.Costumes[0][0], lib.Costumes[0][1]
	key, _ := lib.CostumeImageCacheKey(next, 1)
	lib.PrepareDisplayCostume(first, nil)
	work := lib.cache.display.bitmapPending[key]
	if work == nil {
		t.Fatal("packed animation frame was excluded from lookahead")
	}
	select {
	case <-work.done:
	case <-time.After(2 * time.Second):
		t.Fatal("packed animation preparation did not finish")
	}
	if work.result.err != nil {
		t.Fatal(work.result.err)
	}
	prepared := work.result.asset.image
	if err := os.Remove(filepath.Join(dir, "next.png")); err != nil {
		t.Fatal(err)
	}
	lib.PrepareDisplayCostume(first, nil)
	pixels, _ := lib.DisplayCostumeImage(next, 1)
	if pixels != prepared || pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 31, A: 127}) {
		t.Fatal("packed animation did not reuse the exact prepared pixels")
	}
}

func TestPlaybackBitmapKeepsExactNRGBA(t *testing.T) {
	// An odd size and a subimage exercise row stride and nonzero origins.
	full := image.NewNRGBA(image.Rect(0, 0, 140, 140))
	pixels := full.SubImage(image.Rect(3, 7, 132, 138)).(*image.NRGBA)
	for y := 7; y < 138; y++ {
		for x := 3; x < 132; x++ {
			if x < 37 {
				pixels.SetNRGBA(x, y, color.NRGBA{R: 79, G: 113, B: 25})
			} else if y%2 == 0 {
				pixels.SetNRGBA(x, y, color.NRGBA{R: 250, G: 10, A: 37})
			}
		}
	}
	bitmap := packPlaybackBitmap(&cachedAsset{image: pixels}, 1<<20)
	if bitmap == nil {
		t.Fatal("repeated pixels were not prepared")
	}
	c := &runtimeCache{playback: map[string]*playbackBitmap{"test": bitmap}}
	got := c.playbackAsset("test").image
	for y := 0; y < 131; y++ {
		for x := 0; x < 129; x++ {
			if got.NRGBAAt(x, y) != pixels.NRGBAAt(x+3, y+7) {
				t.Fatalf("changed RGBA at %d,%d", x, y)
			}
		}
	}
}

func TestPlaybackBitmapRejectsExpansion(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), A: 255})
		}
	}
	if packPlaybackBitmap(&cachedAsset{image: pixels}, 1<<20) != nil {
		t.Fatal("stored runs larger than the useful pixel reduction")
	}
	if packPlaybackBitmap(&cachedAsset{image: image.NewNRGBA(image.Rect(0, 0, 64, 64))}, 7) != nil {
		t.Fatal("stored data beyond the remaining byte budget")
	}
}

func indexedPlaybackFixture() *image.NRGBA {
	pixels := image.NewNRGBA(image.Rect(0, 0, 800, 450))
	for y := 0; y < 450; y++ {
		for x := 0; x < 800; x++ {
			i := uint32((x/2 + y/2*17) % 237)
			binary.LittleEndian.PutUint32(pixels.Pix[y*pixels.Stride+x*4:], i|((i*3%256)<<8)|((i*7%256)<<16)|((i%4*85)<<24))
		}
	}
	return pixels
}

func TestPlaybackBitmapPacksIndexedPixelsWithinBudget(t *testing.T) {
	want := indexedPlaybackFixture()
	asset := newCostumeAsset("indexed", want, true)
	bitmap := packPlaybackBitmap(asset, 1<<20)
	if bitmap == nil {
		t.Fatal("few-color image required PNG decoding despite fitting in the packed budget")
	}
	stored := len(bitmap.runs)*4 + len(bitmap.indices) + len(bitmap.palette)*4
	if stored > len(asset.image.Pix)/2 || stored == 0 {
		t.Fatalf("packed image did not reduce storage: %d", stored)
	}
	if packPlaybackBitmap(asset, stored-1) != nil {
		t.Fatal("indexed pixels exceeded the available budget")
	}
	got := bitmap.decodeAsset("indexed")
	if !got.repeated || !bytes.Equal(got.image.Pix, asset.image.Pix) {
		t.Fatal("packed pixels changed color, alpha or compact sampling")
	}
	if !bytes.Equal(got.originalPixels().Pix, want.Pix) {
		t.Fatal("indexed pixels changed collision-sized pixels")
	}
	region := image.Rect(3, 5, 177, 123)
	cropped := bitmap.decodeRegion(region)
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			if cropped.NRGBAAt(x, y) != want.NRGBAAt(x, y) {
				t.Fatalf("indexed crop changed pixel at %d,%d", x, y)
			}
		}
	}
}

func TestIndexedPlaybackAccountsForPaletteStorage(t *testing.T) {
	dir := t.TempDir()
	pixels := indexedPlaybackFixture()
	var data bytes.Buffer
	if err := png.Encode(&data, pixels); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "next.png"), data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{{MD5Ext: "next.png", DataFormat: "png"}}}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	limit := lib.cache.limit
	if err := lib.PrepareCostumePlayback(nil); err != nil {
		t.Fatal(err)
	}
	stored := 0
	indexed := false
	for _, bitmap := range lib.cache.playback {
		stored += cap(bitmap.runs)*4 + cap(bitmap.indices) + cap(bitmap.palette)*4
		indexed = indexed || bitmap.indices != nil
	}
	if !indexed || stored > 16<<20 || lib.cache.limit+stored != limit {
		t.Fatalf("packed storage escaped the shared limit: indexed=%v stored=%d limit=%d original=%d", indexed, stored, lib.cache.limit, limit)
	}
}

func TestPlaybackOverflowUsesTemporaryStorage(t *testing.T) {
	dir := t.TempDir()
	pixels := indexedPlaybackFixture()
	var data bytes.Buffer
	if err := png.Encode(&data, pixels); err != nil {
		t.Fatal(err)
	}
	p := &project.Project{Targets: []*project.Target{{}}}
	for _, name := range []string{"first.png", "second.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		p.Targets[0].Costumes = append(p.Targets[0].Costumes, project.Costume{MD5Ext: name, DataFormat: "png"})
	}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	lib.cache.limit = 256 << 10
	if err := lib.PrepareCostumePlayback(nil); err != nil {
		t.Fatal(err)
	}
	if len(lib.cache.playback) != 2 {
		t.Fatal("images beyond the RAM budget were left dependent on PNG decoding")
	}
	if lib.cache.playbackFile == nil {
		t.Fatal("overflow increased RAM storage instead of using the temporary file")
	}
	name := lib.cache.playbackFile.Name()
	resident := 0
	for _, bitmap := range lib.cache.playback {
		resident += bitmap.residentBytes()
	}
	if lib.cache.limit+resident != 256<<10 || resident > 64<<10 {
		t.Fatal("temporary playback escaped the original memory budget")
	}
	if err := lib.EnableDisplayPreparation(dir, p); err != nil {
		t.Fatal(err)
	}
	for i, costume := range lib.Costumes[0] {
		if err := os.Remove(filepath.Join(dir, p.Targets[0].Costumes[i].MD5Ext)); err != nil {
			t.Fatal(err)
		}
		if !lib.PrepareDisplayBitmap(costume) {
			t.Fatal("disk-backed image was not prepared by the worker")
		}
	}
	for _, costume := range lib.Costumes[0] {
		key, _ := lib.CostumeImageCacheKey(costume, 1)
		select {
		case <-lib.cache.display.bitmapPending[key].done:
		case <-time.After(2 * time.Second):
			t.Fatal("disk-backed preparation timed out")
		}
		got := lib.CostumeImage(costume)
		if got == nil || !bytes.Equal(got.Pix, pixels.Pix) {
			t.Fatalf("disk playback changed pixels: %v", lib.Err())
		}
		crop := lib.CostumeImageRegionAtScale(costume, 1, image.Rect(3, 5, 41, 39))
		if crop == nil || crop.NRGBAAt(17, 23) != pixels.NRGBAAt(17, 23) {
			t.Fatal("disk playback crop changed pixels")
		}
	}
	if err := lib.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("temporary playback file survived Close", err)
	}
}

func TestStoredPlaybackRoundTrip(t *testing.T) {
	for name, pixels := range map[string]*image.NRGBA{
		"runs": image.NewNRGBA(image.Rect(0, 0, 128, 128)), "indexed": indexedPlaybackFixture(),
	} {
		t.Run(name, func(t *testing.T) {
			asset := newCostumeAsset(name, pixels, true)
			bitmap := packPlaybackBitmap(asset, 1<<20)
			file, err := os.CreateTemp(t.TempDir(), "playback")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := bitmap.store(file, 37); err != nil {
				t.Fatal(err)
			}
			if bitmap.residentBytes() != 0 {
				t.Fatal("disk-backed pixels retained the RAM payload")
			}
			loaded, err := bitmap.load()
			if err != nil || !bytes.Equal(loaded.decodeAsset(name).image.Pix, asset.image.Pix) {
				t.Fatal("disk round trip changed pixels", err)
			}
		})
	}
}

func TestBrokenPlaybackStorageFallsBackToSource(t *testing.T) {
	for _, worker := range []bool{false, true} {
		t.Run(fmt.Sprint(worker), func(t *testing.T) {
			lib, _ := bitmapDisplayFixture(t)
			if err := lib.PrepareCostumePlayback(nil); err != nil {
				t.Fatal(err)
			}
			file, err := os.CreateTemp(t.TempDir(), "playback")
			if err != nil {
				t.Fatal(err)
			}
			lib.cache.playbackFile = file
			costume := lib.Costumes[0][1]
			key, _ := lib.CostumeImageCacheKey(costume, 1)
			if err := lib.cache.playback[key].store(file, 0); err != nil {
				t.Fatal(err)
			}
			if err := file.Truncate(0); err != nil {
				t.Fatal(err)
			}
			if worker {
				if !lib.PrepareDisplayBitmap(costume) {
					t.Fatal("preparation was not queued")
				}
				select {
				case <-lib.cache.display.bitmapPending[key].done:
				case <-time.After(2 * time.Second):
					t.Fatal("fallback preparation timed out")
				}
			}
			pixels := lib.CostumeImage(costume)
			if lib.Err() != nil || pixels == nil || pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 31, A: 127}) {
				t.Fatal("optional storage failure broke the project", lib.Err())
			}
		})
	}
}

func TestPlaybackWithoutWritableTemporaryDirectory(t *testing.T) {
	lib, _ := bitmapDisplayFixture(t)
	missing := filepath.Join(t.TempDir(), "missing")
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, missing)
	}
	lib.cache.limit = 1
	if err := lib.PrepareCostumePlayback(nil); err != nil {
		t.Fatal("optional storage failure prevented loading", err)
	}
	if lib.cache.playbackFile != nil || len(lib.cache.playback) != 0 {
		t.Fatal("unavailable disk storage was retained")
	}
	pixels := lib.CostumeImage(lib.Costumes[0][1])
	if pixels == nil || pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 31, A: 127}) {
		t.Fatal("project failed without temporary storage", lib.Err())
	}
}

func TestIndexedPlaybackHandlesSubimageAndPaletteLimit(t *testing.T) {
	pixels := indexedPlaybackFixture().SubImage(image.Rect(3, 5, 605, 301)).(*image.NRGBA)
	bitmap := packPlaybackBitmap(&cachedAsset{image: pixels}, 1<<20)
	if bitmap == nil || bitmap.indices == nil {
		t.Fatal("subimage did not use indexed storage")
	}
	got := bitmap.decodeAsset("subimage").image
	for y := 0; y < got.Bounds().Dy(); y++ {
		for x := 0; x < got.Bounds().Dx(); x++ {
			if got.NRGBAAt(x, y) != pixels.NRGBAAt(x+3, y+5) {
				t.Fatalf("row stride changed pixel %d,%d", x, y)
			}
		}
	}
	for _, colors := range []int{256, 257} {
		pixels := image.NewNRGBA(image.Rect(0, 0, 64, 64))
		for i := 0; i < 4096; i++ {
			binary.LittleEndian.PutUint32(pixels.Pix[i*4:], uint32(i%colors))
		}
		packed := packPlaybackBitmap(&cachedAsset{image: pixels}, 1<<20)
		if (packed != nil) != (colors == 256) {
			t.Fatalf("incorrect palette limit for %d colors", colors)
		}
	}
}

func BenchmarkIndexedBitmapPlayback(b *testing.B) {
	pixels := indexedPlaybackFixture()
	var data bytes.Buffer
	if err := png.Encode(&data, pixels); err != nil {
		b.Fatal(err)
	}
	bitmap := packPlaybackBitmap(newCostumeAsset("indexed", pixels, true), 1<<20)
	if bitmap == nil || bitmap.indices == nil {
		b.Fatal("image was not indexed")
	}
	b.Logf("source=%d compact=%d indexed=%d bytes", len(pixels.Pix), bitmap.size.X*bitmap.size.Y*4, bitmap.residentBytes())
	b.Run("PNG", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			decoded, err := DecodeCostume(project.Costume{DataFormat: "png"}, data.Bytes())
			if err != nil {
				b.Fatal(err)
			}
			newCostumeAsset("indexed", decoded.Image, true)
		}
	})
	b.Run("Indexed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			bitmap.decodeAsset("indexed")
		}
	})
	b.Run("Stored", func(b *testing.B) {
		file, err := os.CreateTemp(b.TempDir(), "playback")
		if err != nil {
			b.Fatal(err)
		}
		defer file.Close()
		stored := *bitmap
		if err := stored.store(file, 0); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			loaded, err := stored.load()
			if err != nil {
				b.Fatal(err)
			}
			loaded.decodeAsset("indexed")
		}
	})
}

func TestPreparedSVGSurvivesPixelEviction(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`<svg width="257" height="129" xmlns="http://www.w3.org/2000/svg"><path d="M 8 7 L 241 23 L 31 113 Z" fill="#7b2d43" opacity="0.35"/></svg>`)
	path := filepath.Join(dir, "vector.svg")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	meta := project.Costume{MD5Ext: "vector.svg", DataFormat: "svg", RotationCenterX: 31, RotationCenterY: 17}
	p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{meta, meta}}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	want, err := DecodeCostume(meta, data)
	if err != nil {
		t.Fatal(err)
	}
	limit := lib.cache.limit
	if err := lib.PrepareCostumePlayback(nil); err != nil {
		t.Fatal(err)
	}
	stored := 0
	for _, bitmap := range lib.cache.playback {
		stored += cap(bitmap.runs) * 4
	}
	if stored == 0 || lib.cache.limit+stored > limit || len(lib.cache.playback) != 1 {
		t.Fatalf("SVG preparation missing or outside shared budget: entries=%d bytes=%d", len(lib.cache.playback), stored)
	}
	clear(lib.cache.entries)
	lib.cache.recent.Init()
	lib.cache.bytes = 0
	scaled := lib.CostumeImageAtScale(lib.Costumes[0][0], 2)
	if scaled == nil || scaled.Bounds().Size() != image.Pt(514, 258) {
		t.Fatal("prepared base SVG replaced the high-resolution raster")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, costume := range lib.Costumes[0] {
		clear(lib.cache.entries)
		lib.cache.recent.Init()
		lib.cache.bytes = 0
		got, repeated := lib.DisplayCostumeImage(costume, 1)
		if got == nil {
			t.Fatalf("SVG rasterized again after pixel eviction: %v", lib.Err())
		}
		if repeated || got.Bounds() != want.Image.Bounds() || !bytes.Equal(got.Pix, want.Image.Pix) {
			t.Fatal("prepared SVG changed its antialiasing, alpha, or dimensions")
		}
	}
}

func TestCostumePreparationReservesSVGWithinSharedBudget(t *testing.T) {
	dir := t.TempDir()
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewNRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bitmap.png"), pngData.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	svgPath := filepath.Join(dir, "vector.svg")
	if err := os.WriteFile(svgPath, []byte(`<svg width="64" height="64"><rect width="64" height="64" fill="red"/></svg>`), 0600); err != nil {
		t.Fatal(err)
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{
		{MD5Ext: "bitmap.png", DataFormat: "png"},
		{MD5Ext: "vector.svg", DataFormat: "svg"},
	}}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	// The shared allowance has room for just one eight-byte uniform raster.
	lib.cache.limit = 32
	if err := lib.PrepareCostumePlayback(nil); err != nil {
		t.Fatal(err)
	}
	if lib.cache.bytes > lib.cache.limit || lib.cache.limit != 24 {
		t.Fatal("prepared raster did not share the existing cache allowance")
	}
	if err := os.Remove(svgPath); err != nil {
		t.Fatal(err)
	}
	got, repeated := lib.DisplayCostumeImage(lib.Costumes[0][1], 1)
	if got == nil || repeated || got.NRGBAAt(31, 17) != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("earlier bitmap displaced prepared SVG: %v", lib.Err())
	}
}

func BenchmarkBitmapPlaybackMiss(b *testing.B) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	for y := 50; y < 900; y++ {
		for x := 100; x < 800; x++ {
			pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(y / 8), G: 140, A: 127})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixels); err != nil {
		b.Fatal(err)
	}
	bitmap := packPlaybackBitmap(newCostumeAsset("test", pixels, true), 1<<20)
	if bitmap == nil {
		b.Fatal("bitmap was not prepared")
	}
	b.Run("PNG", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			decoded, err := DecodeCostume(project.Costume{DataFormat: "png"}, encoded.Bytes())
			if err != nil {
				b.Fatal(err)
			}
			newCostumeAsset("test", decoded.Image, true)
		}
	})
	b.Run("Prepared", func(b *testing.B) {
		c := &runtimeCache{playback: map[string]*playbackBitmap{"test": bitmap}}
		b.ReportAllocs()
		for b.Loop() {
			c.playbackAsset("test")
		}
	})
}

func BenchmarkPackedBitmapDisplay(b *testing.B) {
	for _, prepare := range []bool{false, true} {
		name := "OnDemand"
		if prepare {
			name = "Prepared"
		}
		b.Run(name, func(b *testing.B) {
			lib, _ := bitmapDisplayFixture(b)
			if err := lib.PrepareCostumePlayback(nil); err != nil {
				b.Fatal(err)
			}
			costume := lib.Costumes[0][1]
			key, _ := lib.CostumeImageCacheKey(costume, 1)
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				clear(lib.cache.entries)
				lib.cache.recent.Init()
				lib.cache.bytes = 0
				if prepare {
					if !lib.PrepareDisplayBitmap(costume) {
						b.Fatal("bitmap preparation not queued")
					}
					select {
					case <-lib.cache.display.bitmapPending[key].done:
					case <-time.After(2 * time.Second):
						b.Fatal("bitmap preparation did not finish")
					}
				}
				b.StartTimer()
				pixels, _ := lib.DisplayCostumeImage(costume, 1)
				if pixels == nil {
					b.Fatal(lib.Err())
				}
			}
		})
	}
}

func TestIndexedPlaybackRegionPixels(t *testing.T) {
	bitmap := packPlaybackBitmap(newCostumeAsset("indexed", indexedPlaybackFixture(), true), 1<<20)
	if bitmap == nil || bitmap.indices == nil {
		t.Fatal("expected indexed playback")
	}
	source := bitmap.decodeAsset("indexed").image
	for _, repeated := range []bool{false, true} {
		bitmap.repeated = repeated
		scale := 1
		if repeated {
			scale = 2
		}
		bounds := image.Rectangle{Max: bitmap.size.Mul(scale)}
		for _, region := range []image.Rectangle{
			bounds, image.Rect(1, 1, 50, 40), image.Rect(2, 2, 51, 41),
			image.Rect(1, 2, 2, 3), image.Rect(2, 1, 3, 2),
			bounds.Add(image.Pt(-3, -5)), bounds.Add(image.Pt(3, 5)),
			image.Rect(-5, -5, -1, -1),
		} {
			got := bitmap.decodeRegion(region)
			want := region.Intersect(bounds)
			if want.Empty() {
				if got != nil {
					t.Fatal("nonempty crop outside source")
				}
				continue
			}
			if got == nil || got.Bounds() != want {
				t.Fatalf("crop bounds: %v", got)
			}
			for y := want.Min.Y; y < want.Max.Y; y++ {
				for x := want.Min.X; x < want.Max.X; x++ {
					if got.NRGBAAt(x, y) != source.NRGBAAt(x/scale, y/scale) {
						t.Fatalf("repeated=%v region=%v pixel=(%d,%d)", repeated, region, x, y)
					}
				}
			}
		}
	}
}

func BenchmarkIndexedPlaybackRegion(b *testing.B) {
	bitmap := packPlaybackBitmap(newCostumeAsset("indexed", indexedPlaybackFixture(), true), 1<<20)
	for _, repeated := range []bool{false, true} {
		b.Run(fmt.Sprintf("repeated=%v", repeated), func(b *testing.B) {
			bitmap.repeated = repeated
			bounds := image.Rectangle{Max: bitmap.size}
			if repeated {
				bounds.Max = bounds.Max.Mul(2)
			}
			bounds.Min = image.Pt(1, 1)
			b.ReportAllocs()
			for b.Loop() {
				bitmap.decodeRegion(bounds)
			}
		})
	}
}
