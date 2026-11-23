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
	"runtime"
	"testing"

	"gowarp/core/project"
)

func TestBitmapSizeDoesNotDecodePixels(t *testing.T) {
	for _, resolution := range []float64{0, 1, 2, 1.5} {
		effective := resolution
		if effective <= 0 {
			effective = 1
		}
		costume := &Costume{Bitmap: true, Width: 2960 / effective, Height: 2720 / effective}
		lib := &Library{cache: &runtimeCache{costumes: map[*Costume]costumeSource{
			costume: {meta: project.Costume{BitmapResolution: resolution}},
		}}}
		// No reader is available: size lookup must use only the inspected metadata.
		if got := lib.BitmapSize(costume); got != image.Pt(2960, 2720) {
			t.Fatalf("resolution=%g: bitmap size=%v", resolution, got)
		}
		if costume.Image != nil {
			t.Fatal("size lookup decoded the costume")
		}
	}
	lib := &Library{}
	bitmap := &Costume{Bitmap: true, Image: image.NewNRGBA(image.Rect(10, 20, 30, 50))}
	if lib.BitmapSize(bitmap) != image.Pt(20, 30) || lib.BitmapSize(&Costume{}) != (image.Point{}) {
		t.Fatal("eager bitmap or non-bitmap size changed")
	}
}

func TestRuntimeLongWAVStreamsWithoutFullPCM(t *testing.T) {
	testRuntimeWAVStreamsWithoutFullPCM(t, 11, "wav")
}

func TestRuntimeShortWAVStreamsWithoutFullPCM(t *testing.T) {
	for _, format := range []string{"wav", "mp3"} {
		t.Run(format, func(t *testing.T) {
			testRuntimeWAVStreamsWithoutFullPCM(t, 2, format)
		})
	}
}

func BenchmarkShortPCM16WAVStart(b *testing.B) {
	data := wavFixture(1, 2, 4, 16, nil, make([]byte, 9*8000*4), nil)
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "effect.wav"), data, 0600); err != nil {
		b.Fatal(err)
	}
	p := &project.Project{Targets: []*project.Target{{Sounds: []project.Sound{{MD5Ext: "effect.wav", DataFormat: "wav"}}}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer lib.Close()
	b.Run("Direct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			stream, err := lib.Stream(0, 0, 44100)
			if err != nil {
				b.Fatal(err)
			}
			stream.Close()
		}
	})
	// The former first-play path expanded the entire sound before NewStream.
	b.Run("FullPCM", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sound, err := DecodeSound("wav", data)
			if err != nil {
				b.Fatal(err)
			}
			NewStream(sound, 44100).Close()
		}
	})
}

func testRuntimeWAVStreamsWithoutFullPCM(t *testing.T, seconds int, format string) {
	t.Helper()
	for _, channels := range []int{1, 2} {
		frames := seconds * 8000
		payload := make([]byte, frames*channels*2)
		for i := 0; i < frames*channels; i++ {
			binary.LittleEndian.PutUint16(payload[i*2:], uint16(i*7919))
		}
		data := wavFixture(1, uint16(channels), uint16(channels*2), 16, nil, payload, nil)
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "music.wav"), data, 0600); err != nil {
			t.Fatal(err)
		}
		p := &project.Project{Targets: []*project.Target{{Sounds: []project.Sound{{MD5Ext: "music.wav", DataFormat: format}}}}}
		lib, err := LoadRuntime(context.Background(), dir, p, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { lib.Close() })
		initial, err := lib.Stream(0, 0, 11025)
		if err != nil {
			t.Fatal(err)
		}
		initial.Close()
		if len(initial.sound.Samples) != 0 || initial.wav == nil {
			t.Fatalf("starting %ds WAV expanded the full PCM buffer", seconds)
		}
		sound, err := DecodeSound("wav", data)
		if err != nil {
			t.Fatal(err)
		}
		/*
			Loading already read the track to inspect its header. Playback should
			reuse those source bytes without another archive read or PCM expansion.
		*/
		cachedBytes := lib.cache.bytes
		if err := lib.cache.reader.Close(); err != nil {
			t.Fatal(err)
		}
		for _, pitch := range []float64{-120, 0, 120} {
			stream, err := lib.Stream(0, 0, 11025)
			if err != nil {
				t.Fatal(err)
			}
			if len(stream.sound.Samples) != 0 || lib.cache.bytes != cachedBytes {
				t.Fatal("starting WAV decoded and retained the full PCM buffer")
			}
			reference := NewStream(sound, 11025)
			for _, s := range []*Stream{stream, reference} {
				s.Set(73, pitch, -30)
				s.Playback(float64(seconds)-.02, float64(seconds), true)
			}
			for i := 0; i < 100; i++ {
				size := []int{1, 7, 19, 4096}[i%4]
				got, want := make([]byte, size), make([]byte, size)
				n, err := stream.Read(got)
				wn, we := reference.Read(want)
				if n != wn || err != we || !bytes.Equal(got[:n], want[:wn]) {
					t.Fatalf("channels=%d pitch=%g chunk=%d: streamed %d/%v != cached %d/%v", channels, pitch, i, n, err, wn, we)
				}
				if i == 70 {
					stream.SetLoop(false)
					reference.SetLoop(false)
				}
			}
		}
		active, err := lib.Stream(0, 0, 8000)
		if err != nil {
			t.Fatal(err)
		}
		lib.cache.limit = 1
		lib.cache.put(&cachedAsset{key: "evict", bytes: 1})
		if lib.cache.bytes > lib.cache.limit {
			t.Fatal("retained source bytes exceeded the cache budget")
		}
		/*
			The active stream still owns the encoded bytes. Replaying it after
			image-cache pressure must not reread the archive (already closed).
		*/
		runtime.GC()
		restarted, err := lib.Stream(0, 0, 8000)
		if err != nil {
			t.Fatalf("replaying active music reread its evicted source: %v", err)
		}
		if lib.cache.bytes > lib.cache.limit {
			t.Fatal("reusing active music bypassed the cache memory limit")
		}
		first, second := make([]byte, 128), make([]byte, 128)
		if _, err := active.Read(first); err != nil {
			t.Fatal(err)
		}
		if _, err := restarted.Read(second); err != nil || !bytes.Equal(first, second) {
			t.Fatalf("replayed stream shared its playback cursor: %v", err)
		}
		active.Playback(0, 0, false)
		if err := lib.Close(); err != nil {
			t.Fatal(err)
		}
		got, want := make([]byte, 4096), make([]byte, 4096)
		n, err := active.Read(got)
		wn, we := NewStream(sound, 8000).Read(want)
		if n != wn || err != we || !bytes.Equal(got[:n], want[:wn]) {
			t.Fatal("cache eviction or library close invalidated the playing WAV")
		}
	}
}

func TestRuntimeCostumesLoadOnDemandAndEvict(t *testing.T) {
	dir := t.TempDir()
	svg := []byte(`<svg width="8" height="4" viewBox="10 20 8 4"><rect x="10" y="20" width="8" height="4" fill="#ff0000"/></svg>`)
	for _, name := range []string{"red.svg", "other.svg"} {
		if err := os.WriteFile(filepath.Join(dir, name), svg, 0600); err != nil {
			t.Fatal(err)
		}
	}
	meta := project.Costume{MD5Ext: "red.svg", DataFormat: "svg", RotationCenterX: 14, RotationCenterY: 22}
	other := meta
	other.MD5Ext = "other.svg"
	shifted := meta
	shifted.RotationCenterX = 16
	p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{meta, shifted, other}}}}
	var progress []LoadProgress
	lib, err := LoadRuntime(context.Background(), dir, p, func(p LoadProgress) {
		progress = append(progress, p)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	first, second, third := lib.Costumes[0][0], lib.Costumes[0][1], lib.Costumes[0][2]
	if first.CenterX != 4 || first.CenterY != 2 || second.CenterX != 6 {
		t.Fatal("costume metadata changed")
	}
	if third.Image != nil {
		t.Fatal("unused costume eagerly decoded")
	}
	a, b := lib.CostumeImage(first), lib.CostumeImage(second)
	if a != b || a.NRGBAAt(2, 2) != (color.NRGBA{R: 255, A: 255}) {
		t.Fatal("duplicate image not shared")
	}
	lib.cache.limit = 128
	if lib.CostumeImage(third) == nil || lib.Err() != nil {
		t.Fatal(lib.Err())
	}
	if lib.cache.bytes > 128 {
		t.Fatalf("cache bytes=%d", lib.cache.bytes)
	}
	if a.NRGBAAt(2, 2).R != 255 {
		t.Fatal("eviction invalidated an in-use image")
	}
	if lib.CostumeImage(first).NRGBAAt(2, 2).R != 255 {
		t.Fatal("evicted image did not reload")
	}
	if len(progress) == 0 || progress[len(progress)-1].Done != progress[len(progress)-1].Total {
		t.Fatal("incomplete progress")
	}
	if err := lib.Close(); err != nil {
		t.Fatal(err)
	}
	if lib.CostumeImage(first) != nil || lib.Err() == nil {
		t.Fatal("closed library remained usable")
	}
}

func TestRuntimeLoadCancellationAndDeferredError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadRuntime(ctx, t.TempDir(), &project.Project{}, nil); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	dir := t.TempDir()
	for _, name := range []string{"good.svg", "bad.svg"} {
		content := `<svg width="4" height="4"><rect width="4" height="4"/></svg>`
		if name == "bad.svg" {
			content = `<svg width="4" height="4"><foreignObject>bad</foreignObject></svg>`
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{{MD5Ext: "good.svg", DataFormat: "svg"}, {MD5Ext: "bad.svg", DataFormat: "svg"}}}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if lib.CostumeImage(lib.Costumes[0][1]) != nil || lib.Err() == nil {
		t.Fatal("deferred decode error was hidden")
	}
}

func TestRuntimeSoundDurationAndPlaybackSurviveEviction(t *testing.T) {
	dir := t.TempDir()
	data := wavFixture(1, 1, 2, 16, nil, []byte{0, 64, 0, 192, 0, 64, 0, 192}, nil)
	if err := os.WriteFile(filepath.Join(dir, "tone.wav"), data, 0600); err != nil {
		t.Fatal(err)
	}
	p := &project.Project{Targets: []*project.Target{{Sounds: []project.Sound{{MD5Ext: "tone.wav", DataFormat: "wav", Rate: 1, SampleCount: 999}}}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	meta := lib.Sounds[0][0]
	if meta.Samples != nil || meta.Duration() != .0005 {
		t.Fatalf("duration %v; wanted decoded header, not stale metadata", meta.Duration())
	}
	sound, err := lib.Sound(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	stream := NewStream(sound, 8000)
	lib.cache.limit = 1
	lib.cache.put(&cachedAsset{key: "evict", bytes: 1})
	replayed, err := lib.Sound(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if replayed != stream.sound {
		t.Fatal("replaying an in-use sound decoded a second PCM buffer after cache eviction")
	}
	if lib.cache.bytes > lib.cache.limit {
		t.Fatal("sharing active samples exceeded the cache budget")
	}
	if err := lib.Close(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32)
	if n, err := stream.Read(buf); n != 32 || err != nil {
		t.Fatalf("active stream lost samples: %d %v", n, err)
	}
	if meta.Samples != nil {
		t.Fatal("PCM retained in permanent metadata")
	}
}

func TestRuntimeADPCMDurationMatchesDecodedSound(t *testing.T) {
	fact := uint32(7)
	data := wavFixture(0x11, 1, 8, 4, []byte{9, 0}, []byte{0, 0, 0, 0, 0x10, 0x32, 0x54, 0x76}, &fact)
	info, err := inspectSound("wav", data)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSound("wav", data)
	if err != nil {
		t.Fatal(err)
	}
	if info.Duration() != decoded.Duration() {
		t.Fatalf("duration %v != %v", info.Duration(), decoded.Duration())
	}
}

func TestPCMReadDoesNotAllocatePerFrame(t *testing.T) {
	stream := NewStream(&Sound{Rate: 8000, Samples: make([]float32, 24000)}, 8000)
	buf := make([]byte, 4096)
	allocs := testing.AllocsPerRun(10, func() {
		if _, err := stream.Read(buf); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("audio buffer allocates %.0f times", allocs)
	}
}

func referencedCostumeFixture(t *testing.T) (string, *project.Project) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"start.svg", "bad.svg"} {
		data := `<svg width="1" height="1"><rect width="1" height="1"/></svg>`
		if name == "bad.svg" {
			data = `<svg width="4" height="4"><foreignObject>bad</foreignObject></svg>`
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	pixels := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	pixels.SetNRGBA(17, 23, color.NRGBA{R: 91, G: 27, A: 127})
	var data bytes.Buffer
	if err := png.Encode(&data, pixels); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"used.png", "unused.png", "backdrop.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	start := project.Costume{Name: "start", MD5Ext: "start.svg", DataFormat: "svg"}
	return dir, &project.Project{Targets: []*project.Target{
		{IsStage: true, Costumes: []project.Costume{start, {Name: "background", MD5Ext: "backdrop.png", DataFormat: "png"}}},
		{Costumes: []project.Costume{
			start,
			{Name: "used", MD5Ext: "used.png", DataFormat: "png", BitmapResolution: 2, RotationCenterX: 12},
			{Name: "alias", MD5Ext: "used.png", DataFormat: "png", BitmapResolution: 2, RotationCenterX: 20},
			{Name: "unused", MD5Ext: "unused.png", DataFormat: "png"},
			{Name: "bad", MD5Ext: "bad.svg", DataFormat: "svg"},
		}, Blocks: project.Blocks{
			"costume":  {Opcode: "looks_costume", Fields: map[string][]any{"COSTUME": {"used", nil}}},
			"alias":    {Opcode: "looks_costume", Fields: map[string][]any{"COSTUME": {"alias", nil}}},
			"backdrop": {Opcode: "looks_backdrops", Fields: map[string][]any{"BACKDROP": {"background", nil}}},
			"bad":      {Opcode: "looks_costume", Fields: map[string][]any{"COSTUME": {"bad", nil}}},
		}},
	}}
}

func TestRuntimePreparesNamedCostumesWithoutChangingPixels(t *testing.T) {
	dir, p := referencedCostumeFixture(t)
	last := 0.0
	lib, err := LoadRuntime(context.Background(), dir, p, func(progress LoadProgress) {
		if progress.Total <= 0 {
			t.Fatal("loading progress has no total")
		}
		ratio := float64(progress.Done) / float64(progress.Total)
		if ratio < last || ratio > 1 {
			t.Errorf("loading progress moved backward or exceeded 100%%: %g -> %g", last, ratio)
		}
		last = ratio
	})
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if last != 1 {
		t.Fatal("loading progress did not finish")
	}
	for _, name := range []string{"used.png", "unused.png", "backdrop.png"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	a := lib.CostumeImage(lib.Costumes[1][1])
	b := lib.CostumeImage(lib.Costumes[1][2])
	backdrop := lib.CostumeImage(lib.Costumes[0][1])
	if a == nil || a != b || backdrop == nil {
		t.Fatalf("named costume/backdrop reread its source: %v", lib.Err())
	}
	if a.NRGBAAt(17, 23) != (color.NRGBA{R: 91, G: 27, A: 127}) || lib.Costumes[1][1].CenterX != 6 || lib.Costumes[1][2].CenterX != 10 {
		t.Fatal("preparation changed pixels or reused another costume's origin")
	}
	if lib.CostumeImage(lib.Costumes[1][3]) != nil || lib.Err() == nil {
		t.Fatal("unreferenced costume was eagerly decoded")
	}
}

func TestRuntimePreparationKeepsUnusedErrorsDeferred(t *testing.T) {
	dir, p := referencedCostumeFixture(t)
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatalf("unused malformed costume prevented loading: %v", err)
	}
	defer lib.Close()
	if lib.Err() != nil {
		t.Fatal(lib.Err())
	}
	if lib.CostumeImage(lib.Costumes[1][4]) != nil || lib.Err() == nil {
		t.Fatal("actually requesting a malformed costume must still fail")
	}
}

func TestRuntimePreparationDoesNotEvictExistingAssets(t *testing.T) {
	dir, p := referencedCostumeFixture(t)
	blocks := p.Targets[1].Blocks
	p.Targets[1].Blocks = nil
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	initial := lib.CostumeImage(lib.Costumes[0][0])
	/*
		Leave room for just one prepared image. Existing first-frame pixels
		must survive, and the pass must not churn through the remaining images.
	*/
	lib.cache.limit = lib.cache.bytes + 32*32*4
	p.Targets[1].Blocks = blocks
	if err := lib.prepareReferencedCostumes(p, nil); err != nil {
		t.Fatal(err)
	}
	if lib.cache.bytes > lib.cache.limit {
		t.Fatal("preparation exceeded the shared cache budget")
	}
	if err := os.Remove(filepath.Join(dir, "start.svg")); err != nil {
		t.Fatal(err)
	}
	if lib.CostumeImage(lib.Costumes[0][0]) != initial {
		t.Fatal("speculative preparation evicted first-frame pixels")
	}
	prepared := 0
	for _, key := range []string{"image:png:used.png", "image:png:backdrop.png"} {
		if lib.cache.entries[key] != nil {
			prepared++
		}
	}
	if prepared != 1 {
		t.Fatalf("prepared %d images in space for one", prepared)
	}
}

func TestRuntimePreparationBoundsExtraMemory(t *testing.T) {
	dir, p := referencedCostumeFixture(t)
	target := p.Targets[1]
	target.Blocks = project.Blocks{}
	pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	pixels.SetNRGBA(0, 0, color.NRGBA{R: 1, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, pixels); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("frame%d.png", i)
		if err := os.WriteFile(filepath.Join(dir, name), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		target.Costumes = append(target.Costumes, project.Costume{Name: name, MD5Ext: name, DataFormat: "png"})
		target.Blocks[name] = &project.Block{Opcode: "looks_costume", Fields: map[string][]any{"COSTUME": {name, nil}}}
	}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	prepared := 0
	for i := 0; i < 6; i++ {
		if lib.cache.entries[fmt.Sprintf("image:png:frame%d.png", i)] != nil {
			prepared++
		}
	}
	/*
		The pass can retain four 4 MiB images, not all 24 MiB merely because
		the shared LRU still has free capacity.
	*/
	if prepared != 4 || lib.cache.bytes > (16<<20)+4 {
		t.Fatalf("unbounded optional preparation: images=%d bytes=%d", prepared, lib.cache.bytes)
	}
}

func TestRuntimePreparationHonorsCancellation(t *testing.T) {
	dir, p := referencedCostumeFixture(t)
	blocks := p.Targets[1].Blocks
	p.Targets[1].Blocks = nil
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lib, err := LoadRuntime(ctx, dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	p.Targets[1].Blocks = blocks
	err = lib.prepareReferencedCostumes(p, func(LoadProgress) { cancel() })
	if err != context.Canceled {
		t.Fatalf("preparation ignored cancellation: %v", err)
	}
}

func TestRuntimeRepeatedBitmapsSurviveCacheCycling(t *testing.T) {
	dir := t.TempDir()
	pixels := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for y := 22; y < 24; y++ {
		for x := 16; x < 18; x++ {
			pixels.SetNRGBA(x, y, color.NRGBA{R: 91, G: 27, A: 127})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, pixels); err != nil {
		t.Fatal(err)
	}
	target := &project.Target{}
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("frame%d.png", i)
		if err := os.WriteFile(filepath.Join(dir, name), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		target.Costumes = append(target.Costumes, project.Costume{MD5Ext: name, DataFormat: "png", BitmapResolution: 2})
	}
	lib, err := LoadRuntime(context.Background(), dir, &project.Project{Targets: []*project.Target{target}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	lib.cache.limit = 2 << 20
	for _, costume := range lib.Costumes[0] {
		if lib.CostumeImage(costume) == nil {
			t.Fatal(lib.Err())
		}
	}
	/*
		No live caller needs full pixels now. The cache must retain the exact
		compact data, not rely on a weak reference surviving collection.
	*/
	runtime.GC()
	for _, meta := range target.Costumes {
		if err := os.Remove(filepath.Join(dir, meta.MD5Ext)); err != nil {
			t.Fatal(err)
		}
	}
	for _, costume := range lib.Costumes[0] {
		upload, repeated := lib.DisplayCostumeImage(costume, 1)
		if upload == nil || !repeated || upload.Bounds().Size() != image.Pt(256, 256) {
			t.Fatal("display expanded compact cached pixels")
		}
		if allocations := testing.AllocsPerRun(20, func() {
			again, repeated := lib.DisplayCostumeImage(costume, 1)
			if again != upload || !repeated {
				t.Fatal("display replaced reusable upload pixels")
			}
		}); allocations != 0 {
			t.Fatalf("cached display lookup allocated %g times", allocations)
		}
		got := lib.CostumeImage(costume)
		if got == nil {
			t.Fatalf("cycling images reread an evicted PNG: %v", lib.Err())
		}
		if again := lib.CostumeImage(costume); again != got {
			t.Fatal("live original pixels were reconstructed twice")
		}
		if got.Bounds() != pixels.Bounds() || !bytes.Equal(got.Pix, pixels.Pix) {
			t.Fatal("cache compaction changed original collision pixels or alpha")
		}
	}
	if lib.cache.bytes > lib.cache.limit {
		t.Fatal("compact images escaped the existing cache budget")
	}
}

func TestLargeRepeatedBitmapCompaction(t *testing.T) {
	for _, size := range []image.Point{{1280, 960}, {1826, 1076}, {2048, 2048}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			pixels := image.NewNRGBA(image.Rectangle{Max: size})
			for y := 0; y < size.Y; y++ {
				for x := 0; x < size.X; x++ {
					pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x / 2), G: uint8(y / 2), B: 193, A: uint8((x/2 + y/2) * 11)})
				}
			}
			asset := newCostumeAsset("large", pixels, true)
			if !asset.repeated || asset.bytes != len(pixels.Pix)/4 {
				t.Fatalf("repeated bitmap retained redundant storage: bytes=%d want=%d", asset.bytes, len(pixels.Pix)/4)
			}
			for y := 0; y < size.Y; y++ {
				for x := 0; x < size.X; x++ {
					if asset.image.NRGBAAt(x/2, y/2) != pixels.NRGBAAt(x, y) {
						t.Fatalf("pixel changed at %d,%d", x, y)
					}
				}
			}
			if got := asset.originalPixels(); got != pixels {
				t.Fatal("live collision pixels were replaced")
			}
			last := len(pixels.Pix) - 4
			for channel := 0; channel < 4; channel++ {
				pixels.Pix[last+channel] ^= 1
				if got, repeated := CompactRepeatedPixels(pixels); repeated || got != pixels {
					t.Fatalf("discarded detail in channel %d", channel)
				}
				pixels.Pix[last+channel] ^= 1
			}
		})
	}
}

func TestRuntimePreparesDynamicStampCostumes(t *testing.T) {
	dir, p := referencedCostumeFixture(t)
	target := p.Targets[1]
	target.Costumes = target.Costumes[:1]
	target.Blocks = project.Blocks{"stamp": {Opcode: "pen_stamp"}}
	pixels := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	pixels.SetNRGBA(3, 5, color.NRGBA{R: 91, G: 27, A: 127})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixels); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 70; i++ {
		name := fmt.Sprintf("stamp%d.png", i)
		if err := os.WriteFile(filepath.Join(dir, name), encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		target.Costumes = append(target.Costumes, project.Costume{Name: name, MD5Ext: name, DataFormat: "png"})
	}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	for i := 0; i < 70; i++ {
		if err := os.Remove(filepath.Join(dir, fmt.Sprintf("stamp%d.png", i))); err != nil {
			t.Fatal(err)
		}
		got := lib.CostumeImage(lib.Costumes[1][i+1])
		if got == nil || got.NRGBAAt(3, 5) != (color.NRGBA{R: 91, G: 27, A: 127}) {
			t.Fatalf("dynamic stamp %d needed a decode during gameplay: %v", i, lib.Err())
		}
	}
}

func TestRuntimeStampPreparationBoundsSVGWork(t *testing.T) {
	dir, p := referencedCostumeFixture(t)
	target := p.Targets[1]
	target.Costumes = target.Costumes[:1]
	target.Blocks = project.Blocks{"stamp": {Opcode: "pen_stamp"}}
	for i := 0; i < 70; i++ {
		name := fmt.Sprintf("stamp%d.svg", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`<svg width="8" height="8"><rect width="8" height="8" fill="red"/></svg>`), 0600); err != nil {
			t.Fatal(err)
		}
		target.Costumes = append(target.Costumes, project.Costume{MD5Ext: name, DataFormat: "svg"})
	}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	prepared := 0
	for i := 0; i < 70; i++ {
		if lib.cache.entries[fmt.Sprintf("image:svg:stamp%d.svg", i)] != nil {
			prepared++
		}
	}
	if prepared == 0 || prepared > 64 {
		t.Fatalf("small SVGs must be prepared with bounded raster work: %d", prepared)
	}
}

func TestRuntimePreparationUsesCompactImageBudget(t *testing.T) {
	dir, p := referencedCostumeFixture(t)
	target := p.Targets[1]
	target.Blocks = nil
	pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	for y := 22; y < 24; y++ {
		for x := 16; x < 18; x++ {
			pixels.SetNRGBA(x, y, color.NRGBA{R: 91, A: 127})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, pixels); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("compact%d.png", i)
		if err := os.WriteFile(filepath.Join(dir, name), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		target.Costumes = append(target.Costumes, project.Costume{Name: name, MD5Ext: name, DataFormat: "png"})
	}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	initial := lib.cache.bytes
	lib.cache.limit = initial + 3<<20
	target.Blocks = project.Blocks{}
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("compact%d.png", i)
		target.Blocks[name] = &project.Block{Opcode: "looks_costume", Fields: map[string][]any{"COSTUME": {name, nil}}}
	}
	if err := lib.prepareReferencedCostumes(p, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("compact%d.png", i)
		entry := lib.cache.get("image:png:" + name)
		if entry == nil {
			t.Fatalf("image %d was left for first-use decoding despite fitting the compact budget", i)
		}
		if !entry.repeated || entry.image.NRGBAAt(8, 11) != (color.NRGBA{R: 91, A: 127}) {
			t.Fatal("preparation changed image pixels")
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if lib.cache.entries["image:png:compact3.png"] != nil || lib.cache.bytes > lib.cache.limit {
		t.Fatal("preparation exceeded its memory budget")
	}
	if lib.cache.bytes-initial != 3<<20 {
		t.Fatalf("retained %d bytes, want 3 MiB", lib.cache.bytes-initial)
	}
}
