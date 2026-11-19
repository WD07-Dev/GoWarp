package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"runtime"
	"sync"
	"sync/atomic"

	"gowarp/core/project"
)

// Queued bitmap jobs share this decoded-pixel budget.
const displayBitmapBudget = 16 << 20

type displayRequest struct {
	key, name string
	density   float64
	costume   *Costume
	bitmap    *project.Costume
	playback  *playbackBitmap
	work      *bitmapDecode
}

type displayResult struct {
	request displayRequest
	pixels  *image.NRGBA
	asset   *cachedAsset
	err     error
}

// One worker or host claims each decode. Per-image completion bypasses mailbox ordering.
type bitmapDecode struct {
	bytes    int64
	state    atomic.Uint32 // 0 queued, 1 worker owns it, 2 host owns it, 3 discarded lookahead
	done     chan struct{}
	result   displayResult
	consumed bool // host thread only
	retained bool // completed result still using its queue reservation
	urgent   bool // queued for current use; host thread only
}

type displayPreparation struct {
	noAntialias     bool // Immutable while workers run.
	cancel          context.CancelFunc
	done            chan struct{}
	jobs            chan displayRequest
	results         chan displayResult
	bitmaps         chan displayResult
	bitmapJobs      chan displayRequest
	bitmapLookahead chan displayRequest
	bitmapDone      chan struct{}
	// Only the host thread accesses pending and the library's LRU.
	pending       map[string]bool
	bitmapPending map[string]*bitmapDecode
	bitmapUnused  map[string]bool
	failed        map[string]bool
	next          map[*Costume][]displayRequest
}

func (d *displayPreparation) bitmapRoom(bytes int64) bool {
	if bytes <= 0 || bytes > displayBitmapBudget || len(d.bitmapPending) >= cap(d.bitmapJobs) {
		return false
	}
	for _, work := range d.bitmapPending {
		bytes += work.bytes
	}
	return bytes <= displayBitmapBudget
}

func (d *displayPreparation) makeBitmapRoom(bytes int64, urgent bool) bool {
	if d.bitmapRoom(bytes) {
		return true
	}
	for key, work := range d.bitmapPending {
		if !work.retained && (!urgent || !d.bitmapUnused[key] || !work.state.CompareAndSwap(0, 3)) {
			continue
		}
		work.consumed = true
		work.result = displayResult{}
		delete(d.bitmapPending, key)
		if d.bitmapRoom(bytes) {
			return true
		}
	}
	return false
}

/*
PrepareDisplayBitmap queues a selected image. The host owns the cache and joins only that decode
when needed.
*/
func (lib *Library) PrepareDisplayBitmap(costume *Costume) bool {
	cache := lib.cache
	if cache == nil || cache.display == nil || !cache.ready() || costume == nil || !costume.Bitmap {
		return false
	}
	display := cache.display
	size := lib.BitmapSize(costume)
	bytes := int64(size.X) * int64(size.Y) * 4
	if size.X <= 0 || size.Y <= 0 || bytes > displayBitmapBudget {
		return false
	}
	source, ok := cache.costumes[costume]
	if !ok {
		return false
	}
	delete(display.bitmapUnused, source.key)
	if costume.Image != nil || display.failed[source.key] || cache.entries[source.key] != nil {
		return false
	}
	work := display.bitmapPending[source.key]
	if work != nil && (work.urgent || work.state.Load() != 0) {
		return false
	}
	if work == nil && !display.bitmapRoom(bytes) {
		// Release completed reservations before stamps queue more work during VM execution.
	ready:
		for range cap(display.bitmapJobs) {
			select {
			case result := <-display.bitmaps:
				lib.acceptDisplayResult(result, nil)
			default:
				break ready
			}
		}
		if !cache.ready() || !display.makeBitmapRoom(bytes, true) {
			return false
		}
	}
	if work == nil {
		work = &bitmapDecode{done: make(chan struct{}), bytes: bytes}
	}
	request := displayRequest{key: source.key, name: source.name, density: 1, bitmap: &source.meta, playback: cache.playback[source.key], work: work}
	select {
	case display.bitmapJobs <- request:
		work.urgent = true
		display.bitmapPending[source.key] = work
		return true
	default:
		return false
	}
}

/*
EnableDisplayPreparation starts SVG and bitmap workers with independent readers. Call during
loading, before transferring the library to the game thread.
*/
func (lib *Library) EnableDisplayPreparation(path string, p *project.Project) error {
	cache := lib.cache
	if cache == nil || cache.display != nil {
		return nil
	}
	if !cache.ready() {
		return cache.err
	}
	reader, err := project.OpenAssets(path, p)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(cache.ctx)
	display := &displayPreparation{
		noAntialias: lib.noAntialias,
		cancel:      cancel, done: make(chan struct{}),
		jobs: make(chan displayRequest, 4), results: make(chan displayResult),
		bitmaps:         make(chan displayResult, 4),
		pending:         make(map[string]bool),
		bitmapJobs:      make(chan displayRequest, 4),
		bitmapLookahead: make(chan displayRequest, 4),
		bitmapPending:   make(map[string]*bitmapDecode),
		bitmapUnused:    make(map[string]bool),
		failed:          make(map[string]bool),
		next:            make(map[*Costume][]displayRequest),
	}
	// Look two frames ahead for large, equally sized animation bitmaps within the shared budget.
	hasBitmaps := false
	for _, costumes := range lib.Costumes {
		for i, costume := range costumes {
			size := lib.BitmapSize(costume)
			bytes := int64(size.X) * int64(size.Y) * 4
			hasBitmaps = hasBitmaps || bytes > 0 && bytes <= displayBitmapBudget
			if bytes < 1<<20 || bytes > displayBitmapBudget {
				continue
			}
			for j := i + 1; j < len(costumes) && j <= i+2; j++ {
				candidate := costumes[j]
				if lib.BitmapSize(candidate) != size {
					break
				}
				source, ok := cache.costumes[candidate]
				if ok {
					display.next[costume] = append(display.next[costume], displayRequest{
						key: source.key, name: source.name, density: 1, bitmap: &source.meta,
					})
				}
			}
		}
	}
	// Separate readers keep SVG scaling from blocking bitmap workers; only the host mutates the LRU.
	if hasBitmaps {
		// Leave a CPU for the host; workers share four jobs and 16 MiB.
		var readers []*project.AssetReader
		for range min(2, max(1, runtime.GOMAXPROCS(0)-1)) {
			bitmapReader, err := project.OpenAssets(path, p)
			if err != nil {
				cancel()
				reader.Close()
				for _, opened := range readers {
					opened.Close()
				}
				return err
			}
			readers = append(readers, bitmapReader)
		}
		display.bitmapDone = make(chan struct{})
		var workers sync.WaitGroup
		jobs, results := display.bitmapJobs, display.bitmaps
		for i, bitmapReader := range readers {
			// Speculation uses at most one worker; current images can use both.
			var lookahead <-chan displayRequest
			if i == 0 {
				lookahead = display.bitmapLookahead
			}
			workers.Go(func() {
				display.run(ctx, bitmapReader, lib.customFont, jobs, lookahead, results, nil)
			})
		}
		go func() {
			workers.Wait()
			close(display.bitmapDone)
		}()
	}
	cache.display = display
	// Workers share immutable fonts, but own glyph buffers and mutable raster state.
	go display.run(ctx, reader, lib.customFont, display.jobs, nil, display.results, display.done)
	return nil
}

func (d *displayPreparation) run(ctx context.Context, reader *project.AssetReader, fonts svgFontResolver, jobs, lookahead <-chan displayRequest, results chan<- displayResult, done chan<- struct{}) {
	if done != nil {
		defer close(done)
	}
	defer reader.Close()
	for {
		var request displayRequest
		select {
		case request = <-jobs:
		default:
			select {
			case <-ctx.Done():
				return
			case request = <-jobs:
			case request = <-lookahead:
			}
		}
		if ctx.Err() != nil {
			return
		}
		result := displayResult{request: request}
		decode := request.work == nil || request.work.state.CompareAndSwap(0, 1)
		if !decode && request.work.state.Load() != 2 {
			// A promoted request is decoded once; discarded speculation needs no handoff.
			continue
		}
		if decode && request.playback != nil {
			if bitmap, err := request.playback.load(); err == nil {
				result.asset = bitmap.asset(request.key)
				result.pixels = result.asset.image
			}
		}
		if decode && result.asset == nil {
			data, err := reader.Read(request.name)
			result.err = err
			if err == nil && ctx.Err() == nil && request.bitmap != nil {
				decoded, err := decodeCostume(*request.bitmap, data, fonts)
				result.err = err
				if err == nil {
					result.pixels = decoded.Image
					result.asset = newCostumeAsset(request.key, decoded.Image, true)
				}
			} else if err == nil && ctx.Err() == nil {
				/*
					As in CostumeImageAtScale, an unsupported high-resolution raster
					falls back to base pixels. Archive read failures remain errors.
				*/
				result.pixels, _, _ = rasterSVGWithAntialias(data, request.density, fonts, d.noAntialias)
			}
		}
		if decode && request.work != nil {
			if result.err == nil && ctx.Err() != nil {
				result.err = ctx.Err()
			}
			request.work.result = result
			close(request.work.done)
		}
		// Bitmap results fit their reserved queue budget. SVG results retain an unbuffered handoff.
		select {
		case <-ctx.Done():
			return
		case results <- result:
		}
	}
}

/*
PrepareDisplayCostume advances lookahead even on GPU hits. Call on the host thread; uploaded
reports complete GPU textures.
*/
func (lib *Library) PrepareDisplayCostume(costume *Costume, uploaded func(string) bool) {
	cache := lib.cache
	if cache == nil || cache.display == nil || !cache.ready() {
		return
	}
	display := cache.display
	// Retry evicted lookahead only after actual use, avoiding repeated decoding of an unchanged frame.
	if source, ok := cache.costumes[costume]; ok {
		delete(display.bitmapUnused, source.key)
		if display.bitmapPending[source.key] != nil && (uploaded == nil || !uploaded(source.key)) {
			lib.PrepareDisplayBitmap(costume)
		}
	}
	// Bound result draining so continuously finishing workers cannot monopolize the host.
ready:
	for range cap(display.bitmapJobs) + 1 {
		var result displayResult
		select {
		case result = <-display.results:
		case result = <-display.bitmaps:
		default:
			break ready
		}
		lib.acceptDisplayResult(result, uploaded)
		if !cache.ready() {
			return
		}
	}
	for _, request := range display.next[costume] {
		if display.bitmapPending[request.key] != nil || display.bitmapUnused[request.key] || display.failed[request.key] || cache.entries[request.key] != nil {
			continue
		}
		// GPU-resident images need no CPU copy.
		if uploaded != nil && uploaded(request.key) {
			continue
		}
		size := lib.BitmapSize(costume)
		bytes := int64(size.X) * int64(size.Y) * 4
		if !display.makeBitmapRoom(bytes, false) {
			continue
		}
		request.work = &bitmapDecode{done: make(chan struct{}), bytes: bytes}
		request.playback = cache.playback[request.key]
		select {
		case display.bitmapLookahead <- request:
			display.bitmapPending[request.key] = request.work
			display.bitmapUnused[request.key] = true
		default:
		}
	}
}

/*
acceptDisplayResult runs only on the host, including when a demand read
catches up with a finished bitmap before the next display preparation call.
*/
func (lib *Library) acceptDisplayResult(result displayResult, uploaded func(string) bool) {
	cache := lib.cache
	display := cache.display
	key := result.request.key
	if work := result.request.work; work != nil && !work.consumed &&
		display.bitmapPending[key] == work && display.bitmapUnused[key] &&
		result.err == nil && result.asset != nil && cache.entries[key] == nil &&
		cache.bytes+result.asset.bytes > cache.limit && (uploaded == nil || !uploaded(key)) {
		/*
			Retain completed lookahead in its queue reservation when the LRU is full. New jobs may reclaim it
			without evicting demand-loaded pixels.
		*/
		work.retained = true
		return
	}
	if result.request.bitmap == nil {
		delete(display.pending, result.request.key)
	} else {
		if work := result.request.work; work != nil {
			if work.consumed {
				return
			}
			work.consumed = true
			work.result = displayResult{}
			if display.bitmapPending[result.request.key] == work {
				delete(display.bitmapPending, result.request.key)
			}
		} else {
			delete(display.bitmapPending, result.request.key)
		}
	}
	if result.err != nil {
		/*
			Speculation must not fail a project that never uses that costume.
			An actual request still uses the normal synchronous error path.
		*/
		if result.request.bitmap == nil {
			cache.err = result.err
		} else {
			display.failed[result.request.key] = true
		}
		return
	}
	if cache.get(result.request.key) == nil && (result.request.bitmap == nil || uploaded == nil || !uploaded(result.request.key)) {
		pixels := result.pixels
		if pixels == nil && result.request.bitmap == nil {
			pixels = lib.CostumeImage(result.request.costume)
		}
		if pixels != nil {
			asset := result.asset
			if asset == nil {
				asset = newCostumeAsset(result.request.key, pixels, result.request.bitmap != nil)
			}
			if result.request.bitmap != nil && display.bitmapUnused[result.request.key] {
				cache.putPrefetched(asset)
			} else {
				cache.put(asset)
			}
		}
	}
}

/*
DisplayCostumeImageKey queues SVG scaling and returns the key and density of usable pixels,
never the pending request. Call only on the host thread.
*/
func (lib *Library) DisplayCostumeImageKey(costume *Costume, scale float64, uploaded func(string) bool) (string, float64) {
	lib.PrepareDisplayCostume(costume, uploaded)
	key, density := lib.CostumeImageCacheKey(costume, scale)
	cache := lib.cache
	if cache == nil || cache.display == nil || !cache.ready() {
		return key, density
	}
	display := cache.display
	if density == 1 || cache.get(key) != nil || (uploaded != nil && uploaded(key)) {
		return key, density
	}
	if !display.pending[key] && len(display.pending) < cap(display.jobs) {
		request := displayRequest{key: key, name: cache.costumes[costume].name, density: density, costume: costume}
		select {
		case display.jobs <- request:
			display.pending[key] = true
		default:
		}
	}
	for density > 1 {
		key, density = lib.CostumeImageCacheKey(costume, density/2)
		// Reuse an intermediate GPU raster even if its CPU pixels were evicted.
		if density == 1 || cache.get(key) != nil || (uploaded != nil && uploaded(key)) {
			return key, density
		}
	}
	return key, density
}

/*
DisplayCostumeImage returns upload pixels and whether each represents an identical source 2x2
block. CPU rendering and collision use CostumeImage or CostumeImageAtScale.
*/
func (lib *Library) DisplayCostumeImage(costume *Costume, scale float64) (*image.NRGBA, bool) {
	if !costume.Bitmap {
		return lib.CostumeImageAtScale(costume, scale), false
	}
	cache := lib.cache
	if cache != nil && !cache.ready() {
		return nil, false
	}
	if cache != nil && costume.Image == nil {
		if source, ok := cache.costumes[costume]; ok {
			if entry := cache.get(source.key); entry != nil {
				return entry.image, entry.repeated
			}
			if entry, err := lib.preparedDisplayBitmap(source.key); err != nil {
				cache.err = fmt.Errorf("costume %q: %w", source.meta.Name, err)
				return nil, false
			} else if entry != nil {
				return entry.image, entry.repeated
			}
			if entry := cache.playbackAsset(source.key); entry != nil {
				return entry.image, entry.repeated
			}
		}
	}
	pixels := lib.CostumeImage(costume)
	if pixels == nil {
		return nil, false
	}
	if cache != nil && costume.Image == nil {
		if source, ok := cache.costumes[costume]; ok {
			if entry := cache.get(source.key); entry != nil {
				return entry.image, entry.repeated
			}
		}
	}
	return CompactRepeatedPixels(pixels)
}

// Consume prepared pixels directly; display need not reconstruct the collision-sized image.
func (lib *Library) preparedDisplayBitmap(key string) (*cachedAsset, error) {
	cache := lib.cache
	display := cache.display
	if display == nil || display.bitmapPending[key] == nil {
		return nil, nil
	}
	work := display.bitmapPending[key]
	delete(display.bitmapUnused, key)
	for remaining := len(display.bitmapPending); remaining > 0; remaining-- {
		select {
		case result := <-display.bitmaps:
			lib.acceptDisplayResult(result, nil)
			if result.request.work == work && (result.asset != nil || result.err != nil) {
				return result.asset, result.err
			}
		default:
			remaining = 0
		}
	}
	work = display.bitmapPending[key]
	if work == nil || work.state.CompareAndSwap(0, 2) || work.state.Load() != 1 {
		return nil, nil
	}
	if bitmap := cache.playback[key]; bitmap != nil && bitmap.stored == nil {
		select {
		case <-work.done:
		default:
			/*
				Resident packed pixels need no I/O. Restore them on demand rather than
				waiting for a worker to run. Keep its reservation until it completes.
			*/
			return nil, nil
		}
	}
	// Wait only for this image. Queued work is claimed by the host above.
	select {
	case <-work.done:
		result := work.result
		lib.acceptDisplayResult(result, nil)
		return result.asset, result.err
	case <-cache.ctx.Done():
		return nil, cache.ctx.Err()
	}
}

/*
CompactRepeatedPixels losslessly replaces identical 2x2 blocks with one texel. Shaders reconstruct
the original sampling grid.
*/
func CompactRepeatedPixels(pixels *image.NRGBA) (*image.NRGBA, bool) {
	w, h := pixels.Bounds().Dx(), pixels.Bounds().Dy()
	/*
		Include large backgrounds and effect canvases while bounding the scan.
		The compact pixels still count against the existing cache limits.
	*/
	if w%2 != 0 || h%2 != 0 || w*h*4 < 1<<20 || w*h*4 > 16<<20 {
		return pixels, false
	}
	for y := 0; y < h; y += 2 {
		row := pixels.Pix[y*pixels.Stride : y*pixels.Stride+w*4]
		next := pixels.Pix[(y+1)*pixels.Stride : (y+1)*pixels.Stride+w*4]
		if !bytes.Equal(row, next) {
			return pixels, false
		}
		for x := 0; x < len(row); x += 8 {
			if !bytes.Equal(row[x:x+4], row[x+4:x+8]) {
				return pixels, false
			}
		}
	}
	compact := image.NewNRGBA(image.Rect(0, 0, w/2, h/2))
	for y := 0; y < h; y += 2 {
		for x := 0; x < w; x += 2 {
			src := y*pixels.Stride + x*4
			dst := y/2*compact.Stride + x/2*4
			copy(compact.Pix[dst:dst+4], pixels.Pix[src:src+4])
		}
	}
	return compact, true
}
