package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"gowarp/core/project"
)

func bitmapDisplayFixture(t testing.TB) (*Library, string) {
	t.Helper()
	dir := t.TempDir()
	var costumes []project.Costume
	for i, name := range []string{"first.png", "next.png", "last.png"} {
		pixels := image.NewNRGBA(image.Rect(0, 0, 512, 512))
		pixels.SetNRGBA(17, 23, color.NRGBA{R: uint8(30 + i), A: 127})
		var data bytes.Buffer
		if err := png.Encode(&data, pixels); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		costumes = append(costumes, project.Costume{MD5Ext: name, DataFormat: "png", BitmapResolution: 2})
	}
	if err := os.WriteFile(filepath.Join(dir, "vector.svg"), []byte(`<svg width="8" height="4"><rect width="8" height="4" fill="red"/></svg>`), 0600); err != nil {
		t.Fatal(err)
	}
	costumes = append(costumes, project.Costume{MD5Ext: "vector.svg", DataFormat: "svg"})
	p := &project.Project{Targets: []*project.Target{{Costumes: costumes}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	if err := lib.EnableDisplayPreparation(dir, p); err != nil {
		t.Fatal(err)
	}
	return lib, dir
}

func resumeBitmapWorker(t *testing.T, lib *Library, dir string) {
	t.Helper()
	target := &project.Target{}
	for _, source := range lib.cache.costumes {
		target.Costumes = append(target.Costumes, source.meta)
	}
	reader, err := project.OpenAssets(dir, &project.Project{Targets: []*project.Target{target}})
	if err != nil {
		t.Fatal(err)
	}
	display := lib.cache.display
	ctx, cancel := context.WithCancel(context.Background())
	display.cancel = cancel
	display.bitmapDone = make(chan struct{})
	go display.decodeRequests(ctx, reader, lib.customFont, display.bitmapJobs, display.bitmapLookahead, display.bitmaps, display.bitmapDone)
}

func TestSelectedBitmapOvertakesQueuedLookahead(t *testing.T) {
	lib, dir := bitmapDisplayFixture(t)
	display := lib.cache.display
	display.cancel()
	<-display.done
	<-display.bitmapDone
	lib.PrepareDisplayCostume(lib.Costumes[0][0], nil)
	lib.PrepareDisplayBitmap(lib.Costumes[0][2])
	resumeBitmapWorker(t, lib, dir)
	for _, want := range []string{"image:png:last.png", "image:png:next.png"} {
		select {
		case result := <-display.bitmaps:
			if result.request.key != want || result.err != nil || result.pixels == nil {
				t.Fatalf("prepared %q before %q: error=%v", result.request.key, want, result.err)
			}
			lib.acceptDisplayResult(result, nil)
		case <-time.After(2 * time.Second):
			t.Fatalf("bitmap %q did not finish", want)
		}
	}
	if len(display.bitmapPending) != 0 {
		t.Fatal("completed requests kept their reservations")
	}
}

func TestSelectedBitmapReplacesQueuedLookahead(t *testing.T) {
	lib, dir := largeBitmapDisplayFixture(t)
	display := lib.cache.display
	display.cancel()
	<-display.done
	<-display.bitmapDone
	lib.PrepareDisplayCostume(lib.Costumes[0][0], nil)
	if !lib.PrepareDisplayBitmap(lib.Costumes[0][3]) {
		t.Fatal("unused queued frames blocked the image needed now")
	}
	var bytes int64
	for _, work := range display.bitmapPending {
		bytes += work.bytes
	}
	if bytes > displayBitmapBudget {
		t.Fatal("priority preparation exceeded the decoded-pixel budget")
	}
	resumeBitmapWorker(t, lib, dir)
	select {
	case result := <-display.bitmaps:
		if result.request.key != "image:png:large3.png" || result.err != nil || result.pixels == nil ||
			result.pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 91, A: 127}) {
			t.Fatalf("current image was not prepared first: key=%s error=%v", result.request.key, result.err)
		}
		lib.acceptDisplayResult(result, nil)
	case <-time.After(5 * time.Second):
		t.Fatal("current image did not finish")
	}
}

func TestSelectedBitmapKeepsRunningDecodeReservations(t *testing.T) {
	lib, _ := largeBitmapDisplayFixture(t)
	display := lib.cache.display
	display.cancel()
	<-display.done
	<-display.bitmapDone
	lib.PrepareDisplayCostume(lib.Costumes[0][0], nil)
	for _, work := range display.bitmapPending {
		// Hold both decodes in progress; their pixel memory cannot be reclaimed yet.
		if !work.state.CompareAndSwap(bitmapQueued, bitmapWorkerOwned) {
			t.Fatal("fixture did not leave the decode queued")
		}
	}
	if lib.PrepareDisplayBitmap(lib.Costumes[0][3]) {
		t.Fatal("current request reused memory reserved by running decodes")
	}
	if len(display.bitmapPending) != 2 {
		t.Fatal("running decodes lost their reservations")
	}
}

func TestAbandonedSelectedBitmapReleasesCompletedReservation(t *testing.T) {
	lib, _ := largeBitmapDisplayFixture(t)
	cache := lib.cache
	cache.limit = cache.bytes
	lib.PrepareDisplayCostume(lib.Costumes[0][0], nil)
	for range 2 {
		select {
		case result := <-cache.display.bitmaps:
			lib.acceptDisplayResult(result, nil)
		case <-time.After(5 * time.Second):
			t.Fatal("lookahead did not finish")
		}
	}
	// Selection can change again before the renderer consumes the prepared pixels.
	lib.PrepareDisplayBitmap(lib.Costumes[0][1])
	lib.PrepareDisplayBitmap(lib.Costumes[0][2])
	if !lib.PrepareDisplayBitmap(lib.Costumes[0][3]) {
		t.Fatal("unused selected results permanently occupied the preparation budget")
	}
}

func TestDisplayPreparationBitmapDoesNotWaitForSVG(t *testing.T) {
	lib, _ := bitmapDisplayFixture(t)
	display := lib.cache.display
	vector := lib.Costumes[0][3]
	/*
		Fill the SVG queue as a resize can do for several visible vector
		costumes. Leave its result unconsumed to model a busy/unfinished raster.
	*/
	for _, density := range []float64{2, 4, 8, 16} {
		key, _ := lib.CostumeImageCacheKey(vector, density)
		display.pending[key] = true
		display.jobs <- displayRequest{key: key, name: "vector.svg", density: density, costume: vector}
	}
	lib.PrepareDisplayBitmap(lib.Costumes[0][1])
	select {
	case result := <-display.bitmaps:
		if result.err != nil || result.pixels == nil || result.pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 31, A: 127}) {
			t.Fatalf("invalid next bitmap: %v", result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SVG preparation prevented the next bitmap from decoding")
	}
}

func TestDisplayPreparationSelectedBitmapIsReadyBeforeUse(t *testing.T) {
	lib, dir := bitmapDisplayFixture(t)
	costume := lib.Costumes[0][2]
	if !lib.PrepareDisplayBitmap(costume) {
		t.Fatal("selected bitmap was not queued")
	}
	if lib.PrepareDisplayBitmap(costume) {
		t.Fatal("selected bitmap was queued twice")
	}
	var result displayResult
	select {
	case result = <-lib.cache.display.bitmaps:
	case <-time.After(2 * time.Second):
		t.Fatal("selected bitmap was not prepared")
	}
	if result.err != nil {
		t.Fatal(result.err)
	}
	lib.acceptDisplayResult(result, nil)
	if err := os.Remove(filepath.Join(dir, "last.png")); err != nil {
		t.Fatal(err)
	}
	pixels := lib.CostumeImage(costume)
	if pixels == nil || pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 32, A: 127}) {
		t.Fatalf("prepared image was not reused: %v", lib.Err())
	}
	if lib.PrepareDisplayBitmap(costume) {
		t.Fatal("cached image was queued again")
	}
}

func TestDisplayPreparationCompletesBurstWhileHostIsBusy(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty.svg"), []byte(`<svg width="1" height="1"></svg>`), 0600); err != nil {
		t.Fatal(err)
	}
	costumes := []project.Costume{{MD5Ext: "empty.svg", DataFormat: "svg"}}
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("burst%d.png", i)
		pixels := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		pixels.SetNRGBA(0, 0, color.NRGBA{R: uint8(i + 1), A: 255})
		var data bytes.Buffer
		if err := png.Encode(&data, pixels); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		costumes = append(costumes, project.Costume{MD5Ext: name, DataFormat: "png"})
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: costumes}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if err := lib.EnableDisplayPreparation(dir, p); err != nil {
		t.Fatal(err)
	}
	display := lib.cache.display
	for _, costume := range lib.Costumes[0][1:] {
		if !lib.PrepareDisplayBitmap(costume) {
			t.Fatal("upcoming bitmap was not queued")
		}
	}
	/*
		Rendering can be busy uploading the current image. Preparation should
		progress without requiring it to consume each earlier result first.
	*/
	for i := 0; i < 4; i++ {
		work := display.bitmapPending[fmt.Sprintf("image:png:burst%d.png", i)]
		select {
		case <-work.done:
			if work.result.err != nil || work.result.pixels.NRGBAAt(0, 0).R != uint8(i+1) {
				t.Fatalf("incorrect prepared pixels: %v", work.result.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("bitmap burst stalled behind the host's result handoff")
		}
	}
	// Demand may consume a later image before its mailbox notification.
	if err := os.Remove(filepath.Join(dir, "burst3.png")); err != nil {
		t.Fatal(err)
	}
	pixels := lib.CostumeImage(lib.Costumes[0][4])
	if pixels == nil || pixels.NRGBAAt(0, 0).R != 4 {
		t.Fatalf("out-of-order prepared image was not reused: %v", lib.Err())
	}
}

func TestDisplayPreparationSelectedBitmapWithoutAnimation(t *testing.T) {
	dir := t.TempDir()
	var costumes []project.Costume
	for i, size := range []image.Point{{4, 4}, {37, 29}, {2049, 2048}} {
		name := fmt.Sprintf("image%d.png", i)
		pixels := image.NewNRGBA(image.Rectangle{Max: size})
		pixels.SetNRGBA(0, 0, color.NRGBA{R: uint8(i + 1), A: 255})
		var data bytes.Buffer
		if err := png.Encode(&data, pixels); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		costumes = append(costumes, project.Costume{MD5Ext: name, DataFormat: "png"})
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: costumes}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if err := lib.EnableDisplayPreparation(dir, p); err != nil {
		t.Fatal(err)
	}
	if lib.PrepareDisplayBitmap(lib.Costumes[0][2]) {
		t.Fatal("oversized speculative decode was queued")
	}
	if !lib.PrepareDisplayBitmap(lib.Costumes[0][1]) {
		t.Fatal("non-animation bitmap was not queued")
	}
	select {
	case result := <-lib.cache.display.bitmaps:
		if result.err != nil || result.pixels == nil || result.pixels.NRGBAAt(0, 0).R != 2 {
			t.Fatalf("incorrect selected bitmap: %v", result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker was not started for non-animation bitmaps")
	}
}

func TestDisplayPreparationLargeBitmapsShareQueueBudget(t *testing.T) {
	dir := t.TempDir()
	var costumes []project.Costume
	for i, size := range []image.Point{{1, 1}, {2048, 1024}, {2048, 1024}, {2048, 1024}, {512, 512}, {512, 512}} {
		name := fmt.Sprintf("budget%d.png", i)
		pixels := image.NewNRGBA(image.Rectangle{Max: size})
		pixels.SetNRGBA(0, 0, color.NRGBA{R: uint8(i + 1), A: 255})
		var data bytes.Buffer
		if err := png.Encode(&data, pixels); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		costumes = append(costumes, project.Costume{MD5Ext: name, DataFormat: "png"})
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: costumes}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if err := lib.EnableDisplayPreparation(dir, p); err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{1, 2} {
		if !lib.PrepareDisplayBitmap(lib.Costumes[0][i]) {
			t.Fatalf("large upcoming image %d was not queued", i)
		}
	}

	display := lib.cache.display
	/*
		Hold completion away from the host to model two unfinished decodes.
		Both selected and adjacent-frame requests must share the same budget.
	*/
	mailbox := display.bitmaps
	display.bitmaps = make(chan displayResult)
	if lib.PrepareDisplayBitmap(lib.Costumes[0][3]) {
		t.Fatal("three 8 MiB images exceeded the shared preparation budget")
	}
	lib.PrepareDisplayCostume(lib.Costumes[0][4], nil)
	display.bitmaps = mailbox
	if display.bitmapPending["image:png:budget5.png"] != nil {
		t.Fatal("animation lookahead bypassed the shared preparation budget")
	}
	var result displayResult
	work := display.bitmapPending["image:png:budget1.png"]
	select {
	case <-work.done:
		result = work.result
	case <-time.After(5 * time.Second):
		t.Fatal("large image preparation timed out")
	}
	if result.err != nil || result.pixels == nil || result.pixels.NRGBAAt(0, 0).R != 2 {
		t.Fatalf("incorrect prepared image: %v", result.err)
	}
	lib.acceptDisplayResult(result, nil)
	if !lib.PrepareDisplayBitmap(lib.Costumes[0][3]) {
		t.Fatal("consuming a result did not release its queue reservation")
	}
	lib.acceptDisplayResult(result, nil)
	display.bitmaps = make(chan displayResult)
	queued := lib.PrepareDisplayBitmap(lib.Costumes[0][5])
	display.bitmaps = mailbox
	if queued {
		t.Fatal("duplicate handoff released the same reservation twice")
	}
	if err := os.Remove(filepath.Join(dir, "budget1.png")); err != nil {
		t.Fatal(err)
	}
	if pixels := lib.CostumeImage(lib.Costumes[0][1]); pixels == nil || pixels.NRGBAAt(0, 0).R != 2 {
		t.Fatalf("large prepared pixels were not reused: %v", lib.Err())
	}
}

func TestDisplayPreparationSkipsUploadedBitmapsAndResumesAfterEviction(t *testing.T) {
	lib, _ := bitmapDisplayFixture(t)
	display := lib.cache.display
	uploaded := map[string]bool{"image:png:next.png": true, "image:png:last.png": true}
	lookup := func(key string) bool { return uploaded[key] }
	// These images have left the CPU cache but are still available to draw.
	for i := 0; i < 20; i++ {
		lib.DisplayCostumeImageKey(lib.Costumes[0][0], 1, lookup)
	}
	if len(display.bitmapPending) != 0 {
		t.Fatalf("uploaded frames were queued for decoding again: %v", display.bitmapPending)
	}
	delete(uploaded, "image:png:next.png")
	lib.PrepareDisplayCostume(lib.Costumes[0][0], lookup)
	if display.bitmapPending["image:png:next.png"] == nil || display.bitmapPending["image:png:last.png"] != nil {
		t.Fatalf("lookahead did not follow GPU eviction: %v", display.bitmapPending)
	}
	select {
	case result := <-display.bitmaps:
		if result.err != nil || result.pixels == nil || result.pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 31, A: 127}) {
			t.Fatalf("evicted frame was not decoded correctly: %v", result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("evicted frame was not prepared")
	}
}

func TestDisplayPreparationDiscardsBitmapUploadedDuringDecode(t *testing.T) {
	lib, _ := bitmapDisplayFixture(t)
	display := lib.cache.display
	lib.PrepareDisplayCostume(lib.Costumes[0][0], nil)
	var result displayResult
	select {
	case result = <-display.bitmaps:
	case <-time.After(2 * time.Second):
		t.Fatal("lookahead did not finish")
	}
	/*
		Hold a completed result until the requested pixels have been uploaded.
		Do not let the other worker result race this handoff.
	*/
	ready := make(chan displayResult, 1)
	ready <- result
	display.bitmaps = ready
	lookup := func(key string) bool { return key == result.request.key }
	lib.PrepareDisplayCostume(lib.Costumes[0][3], lookup)
	if lib.cache.entries[result.request.key] != nil {
		t.Fatal("redundant lookahead pixels consumed CPU cache space")
	}
	if display.bitmapPending[result.request.key] != nil {
		t.Fatal("discarded result left its request pending")
	}
}

func TestDisplayPreparationDecodesNextBitmapBeforeUse(t *testing.T) {
	lib, dir := bitmapDisplayFixture(t)
	// Keep the current frame and leave room for both speculative frames.
	lib.cache.limit = 3 << 20
	deadline := time.Now().Add(2 * time.Second)
	for (lib.cache.get("image:png:next.png") == nil || lib.cache.get("image:png:last.png") == nil) && time.Now().Before(deadline) {
		lib.DisplayCostumeImageKey(lib.Costumes[0][0], 1, nil)
		time.Sleep(time.Millisecond)
	}
	if lib.cache.get("image:png:next.png") == nil || lib.cache.get("image:png:last.png") == nil {
		t.Fatal("displaying a bitmap did not prepare the next animation frame")
	}
	// A prepared frame must be usable without another archive read/decode.
	if err := os.Remove(filepath.Join(dir, "next.png")); err != nil {
		t.Fatal(err)
	}
	pixels := lib.CostumeImage(lib.Costumes[0][1])
	if pixels == nil || pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 31, A: 127}) {
		t.Fatalf("prepared bitmap lost its exact pixels: %v", lib.Err())
	}
	if lib.cache.bytes > lib.cache.limit {
		t.Fatal("prepared frames escaped the shared memory limit")
	}
}

func TestDisplayPreparationIgnoresUnusedBitmapError(t *testing.T) {
	lib, dir := bitmapDisplayFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "next.png"), []byte("invalid PNG"), 0600); err != nil {
		t.Fatal(err)
	}
	// Completion order is independent; wait for the bad frame's result too.
	deadline := time.Now().Add(2 * time.Second)
	for (lib.cache.get("image:png:last.png") == nil || !lib.cache.display.failed["image:png:next.png"]) && time.Now().Before(deadline) {
		lib.DisplayCostumeImageKey(lib.Costumes[0][0], 1, nil)
		time.Sleep(time.Millisecond)
	}
	if lib.cache.get("image:png:last.png") == nil || lib.Err() != nil {
		t.Fatalf("unused corrupt frame stopped display preparation: %v", lib.Err())
	}
	for i := 0; i < 10; i++ {
		lib.PrepareDisplayCostume(lib.Costumes[0][0], nil)
	}
	if lib.cache.display.bitmapPending["image:png:next.png"] != nil {
		t.Fatal("failed speculative bitmap was queued again")
	}
	if lib.CostumeImage(lib.Costumes[0][1]) != nil || lib.Err() == nil {
		t.Fatal("actually requesting the corrupt frame must still report its error")
	}
}

func TestDisplayPreparationBitmapFallbackAndClose(t *testing.T) {
	lib, _ := bitmapDisplayFixture(t)
	lib.PrepareDisplayCostume(lib.Costumes[0][0], nil)
	/*
		Demand loading must return the requested pixels even if lookahead has
		not completed. A late worker result must not replace those pixels.
	*/
	pixels := lib.CostumeImage(lib.Costumes[0][1])
	if pixels == nil || pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 31, A: 127}) {
		t.Fatal("pending lookahead substituted a different frame")
	}
	closed := make(chan error, 1)
	go func() { closed <- lib.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bitmap worker could not close with pending results")
	}
}

func displayRuntimeFixture(t *testing.T, ctx context.Context) *Library {
	t.Helper()
	dir := t.TempDir()
	data := []byte(`<svg width="8" height="4"><rect x="2.25" width="0.25" height="4" fill="red"/></svg>`)
	if err := os.WriteFile(filepath.Join(dir, "fine.svg"), data, 0600); err != nil {
		t.Fatal(err)
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{{MD5Ext: "fine.svg", DataFormat: "svg"}}}}}
	lib, err := LoadRuntime(ctx, dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	if err := lib.EnableDisplayPreparation(dir, p); err != nil {
		t.Fatal(err)
	}
	return lib
}

func waitDisplayDensity(t *testing.T, lib *Library, costume *Costume, scale float64) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		key, density := lib.DisplayCostumeImageKey(costume, scale, nil)
		if lib.Err() != nil {
			t.Fatal(lib.Err())
		}
		if density == scale {
			return key
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("display image never became ready")
	return ""
}

func TestDisplayPreparationKeepsBaseUntilScaledPixelsAreReady(t *testing.T) {
	lib := displayRuntimeFixture(t, context.Background())
	costume := lib.Costumes[0][0]
	base := lib.CostumeImage(costume)
	before := append([]byte(nil), base.Pix...)
	key, density := lib.DisplayCostumeImageKey(costume, 4, nil)
	if key != "image:svg:fine.svg" || density != 1 {
		t.Fatalf("first request waited for a raster: %q, %g", key, density)
	}
	if lib.CostumeImageAtScale(costume, density) != base {
		t.Fatal("pending display request lost its base image")
	}
	key = waitDisplayDensity(t, lib, costume, 4)
	if key != "svg:fine.svg:4" {
		t.Fatalf("scaled pixels reused the fallback texture identity: %q", key)
	}
	scaled := lib.CostumeImageAtScale(costume, 4)
	if scaled.Bounds() != image.Rect(0, 0, 32, 16) || scaled.NRGBAAt(9, 8).R != 255 || scaled.NRGBAAt(9, 8).A != 255 {
		t.Fatal("worker did not rasterize the quarter-pixel vector at full resolution")
	}
	if lib.CostumeImage(costume) != base || !bytes.Equal(base.Pix, before) {
		t.Fatal("display preparation changed collision pixels")
	}
}

func TestDisplayPreparationUsesCachedIntermediateScale(t *testing.T) {
	lib := displayRuntimeFixture(t, context.Background())
	costume := lib.Costumes[0][0]
	intermediate := lib.CostumeImageAtScale(costume, 2)
	key, density := lib.DisplayCostumeImageKey(costume, 8, nil)
	if key != "svg:fine.svg:2" || density != 2 || lib.CostumeImageAtScale(costume, density) != intermediate {
		t.Fatalf("cached intermediate image was ignored: %q, %g", key, density)
	}
	waitDisplayDensity(t, lib, costume, 8)
	if lib.cache.bytes > lib.cache.limit {
		t.Fatal("worker result escaped the shared cache budget")
	}
}

func TestDisplayPreparationKeepsUploadedSVGAfterCPUEviction(t *testing.T) {
	lib := displayRuntimeFixture(t, context.Background())
	costume := lib.Costumes[0][0]
	lib.CostumeImageAtScale(costume, 2)
	intermediateKey, _ := lib.CostumeImageCacheKey(costume, 2)
	// Other assets evict CPU pixels while the display retains its 2x texture.
	lib.cache.put(&cachedAsset{key: "other", bytes: lib.cache.limit})
	uploaded := func(key string) bool { return key == intermediateKey }
	key, density := lib.DisplayCostumeImageKey(costume, 8, uploaded)
	if key != intermediateKey || density != 2 {
		t.Fatalf("uploaded intermediate texture was ignored after CPU eviction: %q density=%g", key, density)
	}
	if !lib.cache.display.pending["svg:fine.svg:8"] {
		t.Fatal("using an uploaded fallback prevented higher-resolution preparation")
	}
	waitDisplayDensity(t, lib, costume, 8)
	key, density = lib.DisplayCostumeImageKey(costume, 8, uploaded)
	if key != "svg:fine.svg:8" || density != 8 {
		t.Fatalf("uploaded fallback hid completed high-resolution pixels: %q density=%g", key, density)
	}
}

func TestDisplayPreparationDoesNotRerasterUploadedSVG(t *testing.T) {
	lib := displayRuntimeFixture(t, context.Background())
	costume := lib.Costumes[0][0]
	key, density := lib.DisplayCostumeImageKey(costume, 4, func(key string) bool {
		return key == "svg:fine.svg:4"
	})
	if key != "svg:fine.svg:4" || density != 4 || len(lib.cache.display.pending) != 0 {
		t.Fatalf("resident display texture triggered a new raster: %q density=%g pending=%v", key, density, lib.cache.display.pending)
	}
}

func TestDisplayPreparationCloseWithoutConsumingResult(t *testing.T) {
	lib := displayRuntimeFixture(t, context.Background())
	lib.DisplayCostumeImageKey(lib.Costumes[0][0], 8, nil)
	closed := make(chan error, 1)
	go func() { closed <- lib.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing the library waited for the game to consume a result")
	}
	if err := lib.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDisplayPreparationCancellationStopsWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lib := displayRuntimeFixture(t, ctx)
	lib.DisplayCostumeImageKey(lib.Costumes[0][0], 8, nil)
	cancel()
	select {
	case <-lib.cache.display.done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker outlived the canceled library")
	}
	lib.DisplayCostumeImageKey(lib.Costumes[0][0], 8, nil)
	if lib.Err() != context.Canceled {
		t.Fatalf("cancellation was hidden: %v", lib.Err())
	}
}

func TestDisplayPreparationBoundsRequestsAndCache(t *testing.T) {
	lib := displayRuntimeFixture(t, context.Background())
	lib.cache.limit = 64 << 10
	costume := lib.Costumes[0][0]
	for i := 0; i < 1000; i++ {
		for _, scale := range []float64{2, 4, 8, 16, 32, 64} {
			_, density := lib.DisplayCostumeImageKey(costume, scale, nil)
			pixels := lib.CostumeImageAtScale(costume, density)
			if pixels == nil {
				t.Fatal(lib.Err())
			}
			if len(lib.cache.display.pending) > 4 || lib.cache.bytes > lib.cache.limit {
				t.Fatal("repeated scale changes accumulated unbounded work or pixels")
			}
		}
	}
	// A full queue must not permanently suppress requests it couldn't accept.
	waitDisplayDensity(t, lib, costume, 16)
}

func TestBitmapLookaheadDoesNotRepeatUnusedEvictedFrames(t *testing.T) {
	lib, _ := bitmapDisplayFixture(t)
	display := lib.cache.display
	first, next := lib.Costumes[0][0], lib.Costumes[0][1]
	lib.PrepareDisplayCostume(first, nil)
	for i := 0; i < 2; i++ {
		var result displayResult
		select {
		case result = <-display.bitmaps:
		case <-time.After(2 * time.Second):
			t.Fatal("bitmap lookahead did not finish")
		}
		workerResults := display.bitmaps
		ready := make(chan displayResult, 1)
		ready <- result
		display.bitmaps = ready
		lib.PrepareDisplayCostume(lib.Costumes[0][3], nil)
		display.bitmaps = workerResults
	}
	/*
		Other active assets displace both speculative frames before either is
		used. Repeated draws of the unchanged first frame must not decode them
		again and displace those active assets in turn.
	*/
	lib.cache.limit = 1 << 20
	lib.cache.put(&cachedAsset{key: "active", bytes: 1 << 20})
	for i := 0; i < 20; i++ {
		lib.PrepareDisplayCostume(first, nil)
	}
	if len(display.bitmapPending) != 0 {
		t.Fatalf("unused evicted frames were queued again: %v", display.bitmapPending)
	}
	/*
		Once the animation actually advances, its next cycle may prepare the
		same frame again; suppressing unused work must not disable lookahead.
	*/
	lib.PrepareDisplayCostume(next, nil)
	lib.PrepareDisplayCostume(first, nil)
	if display.bitmapPending["image:png:next.png"] == nil {
		t.Fatal("used frame was not eligible for the next animation cycle")
	}
}

func TestBitmapLookaheadPreservesDemandCachedPixels(t *testing.T) {
	for _, capacity := range []int{1, 2} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			lib, dir := bitmapDisplayFixture(t)
			cache := lib.cache
			first := lib.Costumes[0][0]
			key := cache.costumes[first].key
			cache.limit = capacity * cache.entries[key].Value.(*cachedAsset).bytes
			lib.PrepareDisplayCostume(first, nil)
			for range 2 {
				select {
				case result := <-cache.display.bitmaps:
					lib.acceptDisplayResult(result, nil)
				case <-time.After(2 * time.Second):
					t.Fatal("bitmap lookahead did not finish")
				}
			}
			if cache.entries[key] == nil {
				t.Fatal("unused lookahead evicted the displayed image")
			}
			if capacity == 2 {
				/*
					One speculative image fits. Demand for the other must
					replace that unused image, not the older displayed image.
				*/
				for _, costume := range lib.Costumes[0][1:3] {
					if cache.entries[cache.costumes[costume].key] == nil {
						if lib.CostumeImage(costume) == nil {
							t.Fatal(lib.Err())
						}
						break
					}
				}
			}
			if err := os.Remove(filepath.Join(dir, "first.png")); err != nil {
				t.Fatal(err)
			}
			pixels := lib.CostumeImage(first)
			if pixels == nil || pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 30, A: 127}) {
				t.Fatalf("used pixels required another decode: %v", lib.Err())
			}
			if cache.bytes > cache.limit {
				t.Fatal("prefetch exceeded the existing cache budget")
			}
		})
	}
}

func TestDemandRetainsPendingSpeculativeBitmap(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(fmt.Sprint(selected), func(t *testing.T) {
			lib, dir := bitmapDisplayFixture(t)
			lib.cache.limit = 1 << 20
			first, next := lib.Costumes[0][0], lib.Costumes[0][1]
			key := lib.cache.costumes[next].key
			lib.PrepareDisplayCostume(first, nil)
			if selected {
				lib.PrepareDisplayBitmap(next)
				for lib.cache.display.bitmapPending[key] != nil {
					select {
					case result := <-lib.cache.display.bitmaps:
						lib.acceptDisplayResult(result, nil)
					case <-time.After(2 * time.Second):
						t.Fatal("selected bitmap was not prepared")
					}
				}
			} else if lib.CostumeImage(next) == nil {
				t.Fatal(lib.Err())
			}
			if err := os.Remove(filepath.Join(dir, "next.png")); err != nil {
				t.Fatal(err)
			}
			pixels := lib.CostumeImage(next)
			if pixels == nil || pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 31, A: 127}) {
				t.Fatalf("demanded bitmap was discarded as speculation: %v", lib.Err())
			}
		})
	}
}

func TestCostumeImageUsesCompletedLookahead(t *testing.T) {
	for name, limit := range map[string]int{"cached": 96 << 20, "too large for cache": 1} {
		t.Run(name, func(t *testing.T) {
			lib, dir := bitmapDisplayFixture(t)
			lib.cache.limit = limit
			display := lib.cache.display
			lib.PrepareDisplayBitmap(lib.Costumes[0][1])
			var result displayResult
			select {
			case result = <-display.bitmaps:
			case <-time.After(2 * time.Second):
				t.Fatal("lookahead did not finish")
			}
			/*
				Put the real completed decode back in the handoff, before the host has
				cached it. Demand loading must use it without reading the PNG again.
			*/
			ready := make(chan displayResult, 1)
			ready <- result
			display.bitmaps = ready
			if err := os.Remove(filepath.Join(dir, "next.png")); err != nil {
				t.Fatal(err)
			}
			pixels := lib.CostumeImage(lib.Costumes[0][1])
			if pixels == nil || pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 31, A: 127}) {
				t.Fatalf("completed lookahead was decoded again: %v", lib.Err())
			}
			if display.bitmapPending["image:png:next.png"] != nil {
				t.Fatal("consumed lookahead remained pending")
			}
		})
	}
}

func TestBitmapWorkerSkipsDemandLoadedRequests(t *testing.T) {
	lib, dir := bitmapDisplayFixture(t)
	display := lib.cache.display
	/*
		Hold the real worker before it starts a queued request, as when another
		image is still decoding. Demand loading must remain nonblocking.
	*/
	display.cancel()
	<-display.done
	<-display.bitmapDone
	lib.PrepareDisplayCostume(lib.Costumes[0][0], nil)
	pixels := lib.CostumeImage(lib.Costumes[0][1])
	if pixels == nil {
		t.Fatal(lib.Err())
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{
		{MD5Ext: "next.png", DataFormat: "png"},
		{MD5Ext: "last.png", DataFormat: "png"},
	}}}}
	reader, err := project.OpenAssets(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "next.png")); err != nil {
		reader.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	display.cancel = cancel
	display.bitmapDone = make(chan struct{})
	go display.decodeRequests(ctx, reader, lib.customFont, display.bitmapJobs, display.bitmapLookahead, display.bitmaps, display.bitmapDone)
	select {
	case result := <-display.bitmaps:
		if result.request.key != "image:png:next.png" || result.err != nil || result.pixels != nil {
			t.Fatalf("worker reread an already loaded image: key=%q err=%v pixels=%v",
				result.request.key, result.err, result.pixels != nil)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not acknowledge the obsolete request")
	}
	select {
	case result := <-display.bitmaps:
		if result.err != nil || result.pixels == nil || result.pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 32, A: 127}) {
			t.Fatalf("following frame was not prepared: %v", result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not advance to the following frame")
	}
}

func TestBitmapLookaheadKeepsCompactPixelsForDisplay(t *testing.T) {
	dir := t.TempDir()
	pixels := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for y := 22; y < 24; y++ {
		for x := 16; x < 18; x++ {
			pixels.SetNRGBA(x, y, color.NRGBA{R: 91, G: 27, A: 127})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixels); err != nil {
		t.Fatal(err)
	}
	target := &project.Target{}
	for _, name := range []string{"current.png", "next.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		target.Costumes = append(target.Costumes, project.Costume{MD5Ext: name, DataFormat: "png", BitmapResolution: 2})
	}
	p := &project.Project{Targets: []*project.Target{target}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if err := lib.EnableDisplayPreparation(dir, p); err != nil {
		t.Fatal(err)
	}
	key := "image:png:next.png"
	deadline := time.Now().Add(2 * time.Second)
	for lib.cache.entries[key] == nil && time.Now().Before(deadline) {
		lib.PrepareDisplayCostume(lib.Costumes[0][0], nil)
		time.Sleep(time.Millisecond)
	}
	if lib.cache.entries[key] == nil {
		t.Fatal("lookahead never finished")
	}
	if err := os.Remove(filepath.Join(dir, "next.png")); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	upload, repeated := lib.DisplayCostumeImage(lib.Costumes[0][1], 1)
	if !repeated || upload.Bounds().Size() != image.Pt(256, 256) || upload.NRGBAAt(8, 11) != (color.NRGBA{R: 91, G: 27, A: 127}) {
		t.Fatal("worker handoff discarded compact display pixels")
	}
	full := lib.CostumeImage(lib.Costumes[0][1])
	if full == nil || !bytes.Equal(full.Pix, pixels.Pix) {
		t.Fatal("worker handoff lost exact original pixels")
	}
}

func TestCostumeImageUsesFinishedDecodeBeforeMailboxDelivery(t *testing.T) {
	lib, dir := bitmapDisplayFixture(t)
	display := lib.cache.display
	lib.PrepareDisplayBitmap(lib.Costumes[0][1])
	var result displayResult
	select {
	case result = <-display.bitmaps:
	case <-time.After(2 * time.Second):
		t.Fatal("bitmap worker did not finish")
	}
	/*
		Keep the completed result out of the host mailbox. The worker's own
		completion must be reusable even when ordered handoff is held up.
	*/
	display.bitmaps = make(chan displayResult)
	if err := os.Remove(filepath.Join(dir, "next.png")); err != nil {
		t.Fatal(err)
	}
	got := lib.CostumeImage(lib.Costumes[0][1])
	if got == nil || got != result.pixels {
		t.Fatalf("finished bitmap was decoded again: %v", lib.Err())
	}
	before := lib.cache.bytes
	lib.acceptDisplayResult(result, nil)
	if lib.cache.bytes != before {
		t.Fatal("late mailbox delivery cached the same decode twice")
	}
}

func TestCostumeImageWaitForWorkerCanBeCanceled(t *testing.T) {
	lib, _ := bitmapDisplayFixture(t)
	display := lib.cache.display
	display.cancel()
	<-display.done
	<-display.bitmapDone
	lib.PrepareDisplayCostume(lib.Costumes[0][0], nil)
	/*
		Hold the exact requested frame in the worker-owned state. Host demand
		must honor cancellation instead of starting a second read or hanging.
	*/
	var request displayRequest
	select {
	case request = <-display.bitmapLookahead:
	case <-time.After(2 * time.Second):
		t.Fatal("animation lookahead was not queued")
	}
	if !request.work.state.CompareAndSwap(bitmapQueued, bitmapWorkerOwned) {
		t.Fatal("request was already claimed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	lib.cache.ctx = ctx
	if pixels := lib.CostumeImage(lib.Costumes[0][1]); pixels != nil || !errors.Is(lib.Err(), context.DeadlineExceeded) {
		t.Fatalf("worker wait ignored cancellation: pixels=%v error=%v", pixels != nil, lib.Err())
	}
}

func TestPackedBitmapDoesNotWaitForBusyWorker(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			lib, dir := bitmapDisplayFixture(t)
			display := lib.cache.display
			display.cancel()
			<-display.done
			<-display.bitmapDone
			if err := lib.PrepareCostumePlayback(nil); err != nil {
				t.Fatal(err)
			}
			costume := lib.Costumes[0][1]
			if !lib.PrepareDisplayBitmap(costume) {
				t.Fatal("bitmap was not queued")
			}
			request := <-display.bitmapJobs
			if !request.work.state.CompareAndSwap(bitmapQueued, bitmapWorkerOwned) {
				t.Fatal("worker could not claim the bitmap")
			}
			if err := os.Remove(filepath.Join(dir, "next.png")); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			lib.cache.ctx = ctx
			var pixels *image.NRGBA
			if collision {
				pixels = lib.CostumeImage(costume)
			} else {
				pixels, _ = lib.DisplayCostumeImage(costume, 1)
			}
			if pixels == nil || pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 31, A: 127}) {
				t.Fatalf("available packed pixels waited for the busy worker: %v", lib.Err())
			}
			// An unfinished worker must keep its reservation, including after host demand.
			if display.bitmapPending[request.key] != request.work {
				t.Fatal("unfinished worker lost its memory reservation")
			}
			cached := lib.cache.get(request.key)
			before := lib.cache.bytes
			result := displayResult{request: request, asset: request.playback.decodeAsset(request.key)}
			result.pixels = result.asset.image
			request.work.result = result
			close(request.work.done)
			lib.acceptDisplayResult(result, nil)
			if display.bitmapPending[request.key] != nil || lib.cache.bytes != before || lib.cache.get(request.key) != cached {
				t.Fatal("late worker result replaced demand pixels or leaked its reservation")
			}
		})
	}
}

func largeBitmapDisplayFixture(t *testing.T) (*Library, string) {
	t.Helper()
	dir := t.TempDir()
	pixels := image.NewNRGBA(image.Rect(0, 0, 2048, 1024))
	pixels.SetNRGBA(17, 23, color.NRGBA{R: 91, A: 127})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixels); err != nil {
		t.Fatal(err)
	}
	var costumes []project.Costume
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("large%d.png", i)
		if err := os.WriteFile(filepath.Join(dir, name), encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		costumes = append(costumes, project.Costume{MD5Ext: name, DataFormat: "png"})
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: costumes}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	if err := lib.EnableDisplayPreparation(dir, p); err != nil {
		t.Fatal(err)
	}
	return lib, dir
}

func TestLargeBitmapAnimationPreparesNextFrames(t *testing.T) {
	lib, dir := largeBitmapDisplayFixture(t)
	lib.PrepareDisplayCostume(lib.Costumes[0][0], nil)
	for _, index := range []int{1, 2} {
		name := fmt.Sprintf("large%d.png", index)
		work := lib.cache.display.bitmapPending["image:png:"+name]
		if work == nil {
			t.Fatalf("large animation frame %d was not prepared", index)
		}
		select {
		case <-work.done:
		case <-time.After(5 * time.Second):
			t.Fatal("large animation preparation timed out")
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		pixels, _ := lib.DisplayCostumeImage(lib.Costumes[0][index], 1)
		if pixels == nil || pixels.Bounds().Size() != image.Pt(2048, 1024) ||
			pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 91, A: 127}) {
			t.Fatalf("prepared frame lost its pixels: %v", lib.Err())
		}
	}
}

func TestBitmapLookaheadRefillsAfterCompletedBatch(t *testing.T) {
	lib, _ := largeBitmapDisplayFixture(t)
	display := lib.cache.display
	for _, index := range []int{1, 2} {
		if !lib.PrepareDisplayBitmap(lib.Costumes[0][index]) {
			t.Fatal("selected bitmap was not queued")
		}
	}
	// Deliver a real completed batch together, as after a busy host frame.
	results := make(chan displayResult, 4)
	for i := 0; i < 2; i++ {
		select {
		case result := <-display.bitmaps:
			if result.err != nil {
				t.Fatal(result.err)
			}
			results <- result
		case <-time.After(5 * time.Second):
			t.Fatal("bitmap preparation timed out")
		}
	}
	workerResults := display.bitmaps
	display.bitmaps = results
	defer func() { display.bitmaps = workerResults }()
	/*
		Use two existing adjacent requests so this test isolates handoff from
		the dimensions accepted by animation lookahead.
	*/
	display.next[lib.Costumes[0][3]] = nil
	for _, index := range []int{4, 5} {
		source := lib.cache.costumes[lib.Costumes[0][index]]
		display.next[lib.Costumes[0][3]] = append(display.next[lib.Costumes[0][3]],
			displayRequest{key: source.key, name: source.name, density: 1, bitmap: &source.meta})
	}
	lib.PrepareDisplayCostume(lib.Costumes[0][3], nil)
	for _, index := range []int{4, 5} {
		if display.bitmapPending[fmt.Sprintf("image:png:large%d.png", index)] == nil {
			t.Fatalf("completed batch kept the reservation for upcoming frame %d", index)
		}
	}
	lib.PrepareDisplayBitmap(lib.Costumes[0][3])
	var reserved int64
	for _, work := range display.bitmapPending {
		reserved += work.bytes
	}
	if reserved > displayBitmapBudget {
		t.Fatal("current image preparation exceeded the shared decoded-pixel budget")
	}
}

func TestSelectedBitmapPreparationRefillsCompletedBatch(t *testing.T) {
	lib, dir := largeBitmapDisplayFixture(t)
	display := lib.cache.display
	for _, index := range []int{1, 2} {
		if !lib.PrepareDisplayBitmap(lib.Costumes[0][index]) {
			t.Fatal("initial image was not queued")
		}
	}
	ready := make(chan displayResult, 4)
	for i := 0; i < 2; i++ {
		select {
		case result := <-display.bitmaps:
			if result.err != nil {
				t.Fatal(result.err)
			}
			ready <- result
		case <-time.After(5 * time.Second):
			t.Fatal("selected images did not finish")
		}
	}
	workerResults := display.bitmaps
	display.bitmaps = ready
	defer func() { display.bitmaps = workerResults }()
	if !lib.PrepareDisplayBitmap(lib.Costumes[0][3]) {
		t.Fatal("completed images blocked preparation until the next draw")
	}
	for _, index := range []int{1, 2} {
		if err := os.Remove(filepath.Join(dir, fmt.Sprintf("large%d.png", index))); err != nil {
			t.Fatal(err)
		}
		pixels := lib.CostumeImage(lib.Costumes[0][index])
		if pixels == nil || pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 91, A: 127}) {
			t.Fatal("handoff lost prepared image pixels", lib.Err())
		}
	}
	if lib.cache.bytes > lib.cache.limit {
		t.Fatal("handoff exceeded the existing cache budget")
	}
}

func TestCompletedBitmapLookaheadSurvivesFullCache(t *testing.T) {
	lib, dir := bitmapDisplayFixture(t)
	cache := lib.cache
	first, next := lib.Costumes[0][0], lib.Costumes[0][1]
	firstKey := cache.costumes[first].key
	cache.limit = cache.entries[firstKey].Value.(*cachedAsset).bytes
	lib.PrepareDisplayCostume(first, nil)
	for range 2 {
		select {
		case result := <-cache.display.bitmaps:
			lib.acceptDisplayResult(result, nil)
		case <-time.After(2 * time.Second):
			t.Fatal("lookahead did not finish")
		}
	}
	if cache.entries[firstKey] == nil || cache.bytes > cache.limit {
		t.Fatal("speculation displaced demand pixels or exceeded the cache budget")
	}
	if err := os.Remove(filepath.Join(dir, "next.png")); err != nil {
		t.Fatal(err)
	}
	pixels := lib.CostumeImage(next)
	if pixels == nil || pixels.NRGBAAt(17, 23) != (color.NRGBA{R: 31, A: 127}) {
		t.Fatalf("completed lookahead was discarded and decoded again: %v", lib.Err())
	}
}

func TestCompletedBitmapLookaheadReleasesReservationForNewDemand(t *testing.T) {
	lib, _ := largeBitmapDisplayFixture(t)
	cache := lib.cache
	first := lib.Costumes[0][0]
	cache.limit = cache.bytes
	lib.PrepareDisplayCostume(first, nil)
	for range 2 {
		select {
		case result := <-cache.display.bitmaps:
			lib.acceptDisplayResult(result, nil)
		case <-time.After(5 * time.Second):
			t.Fatal("lookahead did not finish")
		}
	}
	if !lib.PrepareDisplayBitmap(lib.Costumes[0][3]) {
		t.Fatal("unused completed lookahead blocked new demand")
	}
	var reserved int64
	for _, work := range cache.display.bitmapPending {
		reserved += work.bytes
	}
	if reserved > displayBitmapBudget || len(cache.display.bitmapPending) > cap(cache.display.bitmapJobs) {
		t.Fatal("retained results exceeded the existing preparation budget")
	}
	if cache.entries[cache.costumes[first].key] == nil {
		t.Fatal("new preparation evicted the displayed image")
	}
}

func TestCompletedBitmapLookaheadReleasesSlotsForNextAnimation(t *testing.T) {
	dir := t.TempDir()
	pixels := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	pixels.SetNRGBA(17, 23, color.NRGBA{R: 91, A: 127})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixels); err != nil {
		t.Fatal(err)
	}
	var costumes []project.Costume
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("frame%d.png", i)
		if err := os.WriteFile(filepath.Join(dir, name), encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		costumes = append(costumes, project.Costume{MD5Ext: name, DataFormat: "png"})
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: costumes}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if err := lib.EnableDisplayPreparation(dir, p); err != nil {
		t.Fatal(err)
	}
	lib.cache.limit = lib.cache.bytes
	display := lib.cache.display
	for _, index := range []int{0, 3} {
		lib.PrepareDisplayCostume(lib.Costumes[0][index], nil)
		for range 2 {
			select {
			case result := <-display.bitmaps:
				lib.acceptDisplayResult(result, nil)
			case <-time.After(2 * time.Second):
				t.Fatal("lookahead did not finish")
			}
		}
	}
	if len(display.bitmapPending) != 4 {
		t.Fatal("fixture did not fill the preparation slots")
	}
	lib.PrepareDisplayCostume(lib.Costumes[0][2], nil)
	key, _ := lib.CostumeImageCacheKey(lib.Costumes[0][3], 1)
	if display.bitmapPending[key] == nil {
		t.Fatal("completed lookahead blocked the next animation frame")
	}
	if len(display.bitmapPending) > cap(display.bitmapJobs) {
		t.Fatal("animation lookahead exceeded the preparation slots")
	}
}
