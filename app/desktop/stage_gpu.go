package desktop

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"
)

//go:embed stage_gpu.kage
var stageShaderSource []byte

const (
	stageTextureBudget         = 64 << 20
	stageTextureMaxBudget      = 128 << 20
	stageTextureReuseWindow    = 4096
	stageTextureHistoryLimit   = 256
	stageSmallTextureLimit     = 64 << 10
	stageSmallTextureReserve   = 4 << 20
	stageTransientTextureLimit = 4 << 20
)

type stageTextureKey struct {
	library *media.Library
	image   string
	region  image.Rectangle
}
type stageTexture struct {
	image     *ebiten.Image
	bytes     int
	used      uint64
	reused    bool
	spared    bool
	needed    bool
	unmanaged bool
}

// stageGPU owns native display resources. CPU collision pixels stay in Renderer.
type stageGPU struct {
	transparent     bool
	measureUploads  bool
	protectTextures bool
	cacheScene      []vm.DisplayTarget
	upcomingScene   []vm.DisplayTarget
	preparedScene   bool
	cacheRenderer   *render.Renderer
	cacheSize       image.Point
	uploadCount     int
	uploadBytes     int64
	uploadTime      time.Duration
	prepareTime     time.Duration
	pixelTime       time.Duration
	textureTime     time.Duration
	writeTime       time.Duration
	penSubmitTime   time.Duration
	evictions       int
	canvas          canvasGPU
	shader          *ebiten.Shader
	mask            *ebiten.Image
	textures        map[stageTextureKey]stageTexture
	evictedTextures map[stageTextureKey]uint64
	repeatedPixels  map[*ebiten.Image]bool
	textureContent  map[*ebiten.Image]image.Rectangle
	bytes           int
	clock           uint64
	pen             *ebiten.Image
	penRenderer     *render.Renderer
	penRevision     uint64
	nativePen       *penGPU

	transientTextures map[stageTextureKey]stageTexture
	transientBytes    int
	uniforms          map[string]any
	logicalSize       [2]float32
	uvEffects         [4]float32
	colorEffects      [4]float32
	tintColor         [3]float32
}

func (g *stageGPU) Close() {
	g.releaseTransientTexture()
	g.canvas.close()
	if g.mask != nil {
		g.mask.Deallocate()
		g.mask = nil
	}
	if g.nativePen != nil {
		for _, child := range g.nativePen.layers {
			child.closeLayer()
		}
		g.nativePen.layers = nil
		g.nativePen.closeSurfaces()
		if g.nativePen.stampShader != nil {
			g.nativePen.stampShader.Deallocate()
			g.nativePen.stampShader = nil
		}
		if g.nativePen.shader != nil {
			g.nativePen.shader.Deallocate()
			g.nativePen.shader = nil
		}
		if g.nativePen.image != nil {
			g.nativePen.image.Deallocate()
		}
		if g.nativePen.resizeSpare != nil {
			g.nativePen.resizeSpare.Deallocate()
			g.nativePen.resizeSpare = nil
		}
		if g.nativePen.checkpoint != nil {
			g.nativePen.checkpoint.Deallocate()
			g.nativePen.checkpoint = nil
		}
		g.nativePen.checkpointPending = false
		g.nativePen.pending = nil
		g.nativePen.historyDepthOnly = nil
		g.nativePen.commands = nil
		g.nativePen.image = nil
		g.nativePen.pixels = nil
		g.nativePen.stampTexture = nil
		g.nativePen.stampVertices = nil
		g.nativePen.stampIndices = nil
		g.nativePen.lineVertices = nil
		g.nativePen.lineIndices = nil
		g.nativePen.uploads = nil
		g.nativePen.draws = nil
		g.nativePen.drawVertices = nil
		g.nativePen.drawIndices = nil
	}
	for _, t := range g.textures {
		t.image.Deallocate()
	}
	if g.pen != nil {
		g.pen.Deallocate()
	}
	if g.shader != nil {
		g.shader.Deallocate()
	}
	*g = stageGPU{}
}

// Overflow textures are shared by stamps within a frame, then released.
func (g *stageGPU) releaseTransientTexture() {
	for key := range g.transientTextures {
		g.evictTransientTexture(key)
	}
}

func (g *stageGPU) evictTransientTexture(key stageTextureKey) {
	texture := g.transientTextures[key]
	g.rememberTexture(key, texture.used)
	if p := g.nativePen; p != nil {
		p.flushTexture(texture.image)
		delete(p.uploads, texture.image)
	}
	delete(g.repeatedPixels, texture.image)
	delete(g.textureContent, texture.image)
	texture.image.Deallocate()
	g.transientBytes -= texture.bytes
	delete(g.transientTextures, key)
}

func (g *stageGPU) cachedTexture(key stageTextureKey) *ebiten.Image {
	if texture, ok := g.transientTextures[key]; ok {
		texture.used = g.clock
		g.transientTextures[key] = texture
		return texture.image
	}
	if texture, ok := g.textures[key]; ok {
		texture.used = g.clock
		texture.reused = true
		texture.spared = false
		texture.needed = texture.needed || g.protectTextures
		g.textures[key] = texture
		return texture.image
	}
	return nil
}

func (g *stageGPU) recentTexture(texture stageTexture) bool {
	return texture.reused && g.clock-texture.used <= stageTextureReuseWindow
}

// Retain only identities so returning animation frames can earn extra cache space.
func (g *stageGPU) rememberTexture(key stageTextureKey, used uint64) {
	if g.clock-used > stageTextureReuseWindow {
		return
	}
	if g.evictedTextures == nil {
		g.evictedTextures = make(map[stageTextureKey]uint64)
	}
	if len(g.evictedTextures) >= stageTextureHistoryLimit {
		var oldest stageTextureKey
		age := ^uint64(0)
		for key, used := range g.evictedTextures {
			if used < age {
				oldest, age = key, used
			}
		}
		delete(g.evictedTextures, oldest)
	}
	g.evictedTextures[key] = used
}

// Upload straight alpha for shader filtering without retaining the CPU pixels.
func stageUpload(dst *ebiten.Image, src *image.NRGBA) {
	stageUploadPixels(dst, src, false)
}

func stageUploadPixels(dst *ebiten.Image, src *image.NRGBA, premultiplied bool) {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	// Bound upload chunks because Ebitengine retains pooled buffers after texture eviction.
	const uploadBytes = 256 << 10
	rows := min(h, max(1, uploadBytes/(w*4)))
	var buffer *image.RGBA
	if premultiplied || src.Stride != w*4 {
		buffer = image.NewRGBA(image.Rect(0, 0, w, rows))
	}
	for y := 0; y < h; y += rows {
		n := min(rows, h-y)
		var pixels []byte
		switch {
		case premultiplied:
			draw.Draw(buffer, image.Rect(0, 0, w, n), src, src.Bounds().Min.Add(image.Pt(0, y)), draw.Src)
			pixels = buffer.Pix[:n*w*4]
		case buffer != nil:
			pixels = buffer.Pix[:n*w*4]
			for row := 0; row < n; row++ {
				copy(pixels[row*w*4:(row+1)*w*4], src.Pix[(y+row)*src.Stride:(y+row)*src.Stride+w*4])
			}
		default:
			pixels = src.Pix[y*src.Stride : y*src.Stride+n*w*4]
		}
		region := image.Rect(0, y, w, y+n).Add(dst.Bounds().Min)
		clippedImage(dst, region).WritePixels(pixels)
	}
}

func (g *stageGPU) texture(r *render.Renderer, d render.DisplaySprite) (*ebiten.Image, bool) {
	return g.textureRegion(r, d, image.Rectangle{})
}

// Uniform rasters use one texel while geometry and effects keep the original dimensions.
func compactTexturePixels(pixels *image.NRGBA) *image.NRGBA {
	bounds := pixels.Bounds()
	if bounds.Empty() || bounds.Size() == image.Pt(1, 1) {
		return pixels
	}
	row := pixels.Pix[:bounds.Dx()*4]
	for x := 4; x < len(row); x += 4 {
		if !bytes.Equal(row[:4], row[x:x+4]) {
			return pixels
		}
	}
	for y := 1; y < bounds.Dy(); y++ {
		if !bytes.Equal(row, pixels.Pix[y*pixels.Stride:y*pixels.Stride+len(row)]) {
			return pixels
		}
	}
	return pixels.SubImage(image.Rectangle{Min: bounds.Min, Max: bounds.Min.Add(image.Pt(1, 1))}).(*image.NRGBA)
}

func textureContentBounds(pixels *image.NRGBA) image.Rectangle {
	w, h := pixels.Rect.Dx(), pixels.Rect.Dy()
	left, top, right, bottom := w, h, 0, 0
	// Trimming is optional. Bound host work for large sparse uploads.
	remaining := 256 << 10
	for y := 0; y < h; y++ {
		if w > remaining {
			return image.Rect(0, 0, w, h)
		}
		remaining -= w
		row := pixels.Pix[y*pixels.Stride : y*pixels.Stride+w*4]
		for x := 0; x < w; x++ {
			if row[x*4+3] == 0 {
				continue
			}
			left, top, bottom = min(left, x), min(top, y), y+1
			right = max(right, x+1)
			for end := w - 1; end > x; end-- {
				if row[end*4+3] != 0 {
					remaining += end - x - 1
					right = max(right, end+1)
					break
				}
			}
			break
		}
	}
	if right == 0 {
		return image.Rectangle{}
	}
	// Keep transparent neighbors for linear filtering and compacted pixels.
	return image.Rect(left, top, right, bottom).Inset(-2).Intersect(image.Rect(0, 0, w, h))
}

func (g *stageGPU) trimStampVertices(vertices *[4]ebiten.Vertex, texture *ebiten.Image) {
	bounds, ok := g.textureContent[texture]
	if !ok {
		return
	}
	if bounds.Empty() {
		*vertices = [4]ebiten.Vertex{}
		return
	}
	w, h := float32(texture.Bounds().Dx()), float32(texture.Bounds().Dy())
	if bounds == image.Rect(0, 0, int(w), int(h)) {
		return
	}
	original := *vertices
	/*
		Moving quad edges can change nearest sampling at texel boundaries.
		Restrict that path to pixel-aligned, axis-aligned integer scaling.
	*/
	if int(original[0].Custom3)%2 != 0 {
		for _, vertex := range original {
			if math.Trunc(float64(vertex.DstX)) != float64(vertex.DstX) || math.Trunc(float64(vertex.DstY)) != float64(vertex.DstY) ||
				math.Abs(float64(vertex.DstX)) > 65536 || math.Abs(float64(vertex.DstY)) > 65536 {
				return
			}
		}
		ax, ay := original[1].DstX-original[0].DstX, original[1].DstY-original[0].DstY
		bx, by := original[3].DstX-original[0].DstX, original[3].DstY-original[0].DstY
		if !((ay == 0 && bx == 0) || (ax == 0 && by == 0)) {
			return
		}
		sx, sy := math.Abs(float64(ax+ay))/float64(w), math.Abs(float64(bx+by))/float64(h)
		if sx < 1 || sy < 1 || sx > 16 || sy > 16 || math.Trunc(sx) != sx || math.Trunc(sy) != sy {
			return
		}
	}
	for i, point := range [4]image.Point{bounds.Min, {X: bounds.Max.X, Y: bounds.Min.Y}, bounds.Max, {X: bounds.Min.X, Y: bounds.Max.Y}} {
		x, y := float32(point.X), float32(point.Y)
		u, v := float64(x)/float64(w), float64(y)/float64(h)
		vertices[i].DstX = float32(float64(original[0].DstX) + u*float64(original[1].DstX-original[0].DstX) + v*float64(original[3].DstX-original[0].DstX))
		vertices[i].DstY = float32(float64(original[0].DstY) + u*float64(original[1].DstY-original[0].DstY) + v*float64(original[3].DstY-original[0].DstY))
		vertices[i].SrcX, vertices[i].SrcY = x, y
		vertices[i].Custom0, vertices[i].Custom1 = x, y
	}
}

func stampOutsideBounds(vertices [4]ebiten.Vertex, bounds image.Rectangle) bool {
	left, right := vertices[0].DstX, vertices[0].DstX
	top, bottom := vertices[0].DstY, vertices[0].DstY
	for _, vertex := range vertices[1:] {
		left, right = min(left, vertex.DstX), max(right, vertex.DstX)
		top, bottom = min(top, vertex.DstY), max(bottom, vertex.DstY)
	}
	// Keep a pixel of slack at clip edges for rasterization precision.
	return left == right || top == bottom || right < float32(bounds.Min.X)-1 ||
		bottom < float32(bounds.Min.Y)-1 || left > float32(bounds.Max.X)+1 || top > float32(bounds.Max.Y)+1
}

// Crop large bitmaps to the viewport without reducing their resolution.
func (g *stageGPU) spriteTexture(r *render.Renderer, d *render.DisplaySprite, viewport image.Point, effects vm.GraphicEffects) (*ebiten.Image, bool) {
	region := g.spriteTextureRegion(r, *d, viewport, effects)
	if region.Empty() {
		return g.texture(r, *d)
	}
	size := r.Assets.BitmapSize(d.Costume)
	origin := d.Corners[0]
	dx := [2]float64{d.Corners[1][0] - origin[0], d.Corners[1][1] - origin[1]}
	dy := [2]float64{d.Corners[3][0] - origin[0], d.Corners[3][1] - origin[1]}
	for i, point := range [4]image.Point{region.Min, {X: region.Max.X, Y: region.Min.Y}, region.Max, {X: region.Min.X, Y: region.Max.Y}} {
		u, v := float64(point.X)/float64(size.X), float64(point.Y)/float64(size.Y)
		d.Corners[i] = [2]float64{origin[0] + u*dx[0] + v*dy[0], origin[1] + u*dx[1] + v*dy[1]}
	}
	return g.textureRegion(r, *d, region)
}

// Use identical crop selection for drawing and cache protection.
func (g *stageGPU) spriteTextureRegion(r *render.Renderer, d render.DisplaySprite, viewport image.Point, effects vm.GraphicEffects) image.Rectangle {
	size := r.Assets.BitmapSize(d.Costume)
	if (size.X <= 2048 && size.Y <= 2048) ||
		effects.Mosaic != 0 || effects.Pixelate != 0 || effects.Whirl != 0 || effects.Fisheye != 0 {
		return image.Rectangle{}
	}
	region := visibleTextureRegion(d.Corners, size, viewport)
	if region.Empty() || region == (image.Rectangle{Max: size}) {
		return image.Rectangle{}
	}
	identity, _ := r.Assets.CostumeImageCacheKey(d.Costume, d.Density)
	// Reuse full uploads until cache pressure favors a smaller viewport crop.
	if !g.protectTextures {
		if _, ok := g.textures[stageTextureKey{library: r.Assets, image: identity}]; ok {
			return image.Rectangle{}
		}
	}
	cached := image.Rectangle{}
	var newest uint64
	for key, texture := range g.textures {
		if key.library == r.Assets && key.image == identity && region.In(key.region) && texture.used > newest {
			cached, newest = key.region, texture.used
		}
	}
	if !cached.Empty() {
		region = cached
	} else {
		// Leave a margin so small camera movements reuse the same upload.
		const tile = 256
		region = image.Rect(
			max(0, (region.Min.X-128)/tile*tile),
			max(0, (region.Min.Y-128)/tile*tile),
			min(size.X, (region.Max.X+128+tile-1)/tile*tile),
			min(size.Y, (region.Max.Y+128+tile-1)/tile*tile),
		)
	}
	return region
}

func visibleTextureRegion(corners [4][2]float64, size, viewport image.Point) image.Rectangle {
	x, y := corners[0][0], corners[0][1]
	ax, ay := corners[1][0]-x, corners[1][1]-y
	bx, by := corners[3][0]-x, corners[3][1]-y
	determinant := ax*by - ay*bx
	/*
		Mirrored quads can change nearest-pixel tie breaking after cropping.
		Keep their original geometry and upload path.
	*/
	if determinant <= 0 || math.IsNaN(determinant) || math.IsInf(determinant, 0) {
		return image.Rectangle{}
	}
	minU, minV, maxU, maxV := 1.0, 1.0, 0.0, 0.0
	for _, point := range [4]image.Point{{}, {X: viewport.X}, viewport, {Y: viewport.Y}} {
		px, py := float64(point.X)-x, float64(point.Y)-y
		u, v := (px*by-py*bx)/determinant, (ax*py-ay*px)/determinant
		if math.IsNaN(u) || math.IsNaN(v) || math.IsInf(u, 0) || math.IsInf(v, 0) {
			return image.Rectangle{}
		}
		minU, minV = math.Min(minU, u), math.Min(minV, v)
		maxU, maxV = math.Max(maxU, u), math.Max(maxV, v)
	}
	/*
		Include neighboring samples for filtering. Upload margins are added only
		after checking whether a cached region already covers these pixels.
	*/
	left := math.Floor(math.Max(0, minU)*float64(size.X)) - 2
	top := math.Floor(math.Max(0, minV)*float64(size.Y)) - 2
	right := math.Ceil(math.Min(1, maxU)*float64(size.X)) + 2
	bottom := math.Ceil(math.Min(1, maxV)*float64(size.Y)) + 2
	return image.Rect(max(0, int(left)), max(0, int(top)), min(size.X, int(right)), min(size.Y, int(bottom)))
}

func (g *stageGPU) textureRegion(r *render.Renderer, d render.DisplaySprite, region image.Rectangle) (*ebiten.Image, bool) {
	uploaded := func(identity string) bool {
		// Cropped textures cannot replace the complete image for lookahead.
		key := stageTextureKey{library: r.Assets, image: identity}
		_, ok := g.textures[key]
		return ok || g.transientTextures[key].image != nil
	}
	identity, density := r.Assets.CostumeImageCacheKey(d.Costume, d.Density)
	key := stageTextureKey{library: r.Assets, image: identity, region: region}
	g.clock++
	if texture := g.cachedTexture(key); texture != nil {
		r.Assets.PrepareDisplayCostume(d.Costume, uploaded)
		return texture, false
	}
	var phaseStart time.Time
	if g.measureUploads {
		started := time.Now()
		phaseStart = started
		defer func() { g.uploadTime += time.Since(started) }()
	}
	if p := g.nativePen; p != nil && len(p.replaying) != 0 {
		p.prepareStampBitmaps(identity)
	}
	g.prepareSceneBitmaps(r, identity)
	identity, density = r.Assets.DisplayCostumeImageKey(d.Costume, density, uploaded)
	key.image = identity
	if texture := g.cachedTexture(key); texture != nil {
		if g.measureUploads {
			g.prepareTime += time.Since(phaseStart)
		}
		return texture, false
	}
	var pixels *image.NRGBA
	repeated := false
	if region.Empty() {
		pixels, repeated = r.Assets.DisplayCostumeImage(d.Costume, density)
	} else {
		// Regions are expressed in original pixels, including odd offsets.
		pixels = r.Assets.CostumeImageRegionAtScale(d.Costume, density, region)
	}
	if g.measureUploads {
		now := time.Now()
		g.prepareTime += now.Sub(phaseStart)
		phaseStart = now
	}
	if pixels == nil {
		return nil, false
	}
	pixels = compactTexturePixels(pixels)
	if pixels.Bounds().Size() == image.Pt(1, 1) {
		repeated = false
	} else if d.Costume.Bitmap && !region.Empty() {
		pixels, repeated = media.CompactRepeatedPixels(pixels)
	}
	width, height := pixels.Bounds().Dx(), pixels.Bounds().Dy()
	content := textureContentBounds(pixels)
	bytes := width * height * 4
	if g.measureUploads {
		g.uploadCount++
		g.uploadBytes += int64(bytes)
	}
	// Keep small images on the atlas; large entries can pin oversized padded allocations.
	unmanaged := repeated || bytes >= 1<<20
	if unmanaged {
		// Charge standalone textures for their power-of-two GPU allocation.
		gpuWidth, gpuHeight := 16, 16
		for gpuWidth < width {
			gpuWidth *= 2
		}
		for gpuHeight < height {
			gpuHeight *= 2
		}
		bytes = gpuWidth * gpuHeight * 4
	}
	if g.measureUploads {
		now := time.Now()
		g.pixelTime += now.Sub(phaseStart)
		phaseStart = now
	}
	// Oversized temporary uploads must not evict the reusable working set.
	var im *ebiten.Image
	previous, seen := g.evictedTextures[key]
	reused := seen && g.clock-previous <= stageTextureReuseWindow
	delete(g.evictedTextures, key)
	budget := stageTextureBudget
	if g.bytes+bytes > budget {
		reusedBytes := 0
		if reused {
			reusedBytes = bytes
		}
		for _, texture := range g.textures {
			if g.recentTexture(texture) {
				reusedBytes += texture.bytes
			}
		}
		budget = max(budget, min(stageTextureMaxBudget, reusedBytes))
	}
	temporary := bytes > stageTextureBudget
	if !temporary && g.bytes+bytes > budget && !g.protectTextures && (g.cacheRenderer != nil || (g.nativePen != nil && len(g.nativePen.replaying) != 0)) {
		g.protectTextures = true
		g.protectSceneTextures(g.cacheRenderer, g.cacheScene, g.cacheSize.X, g.cacheSize.Y)
	}
	for !temporary && g.bytes+bytes > budget && len(g.textures) > 0 {
		var oldest, oldestLarge, oldestMatching stageTextureKey
		age, largeAge, matchingAge := ^uint64(0), ^uint64(0), ^uint64(0)
		smallBytes := 0
		if bytes <= stageSmallTextureLimit {
			smallBytes = bytes
		}
		hasCandidate := false
		reusedCandidate := false
		for k, t := range g.textures {
			if t.bytes <= stageSmallTextureLimit {
				smallBytes += t.bytes
			}
			if g.protectTextures && t.needed {
				continue
			}
			recent := g.recentTexture(t)
			if hasCandidate && !reusedCandidate && recent {
				continue
			}
			if hasCandidate && reusedCandidate && !recent {
				age, largeAge, matchingAge = ^uint64(0), ^uint64(0), ^uint64(0)
			}
			hasCandidate = true
			reusedCandidate = recent
			if unmanaged && t.unmanaged && t.used < matchingAge && t.image.Bounds().Size() == image.Pt(width, height) {
				oldestMatching, matchingAge = k, t.used
			}
			if t.used < age {
				oldest, age = k, t.used
			}
			if t.bytes > stageSmallTextureLimit && t.used < largeAge {
				oldestLarge, largeAge = k, t.used
			}
		}
		if !hasCandidate || (reusedCandidate && !reused) {
			// A one-use image must not flush the repeated working set.
			temporary = true
			break
		}
		// Reserve space for small textures that may be costly to decode again.
		if smallBytes <= stageSmallTextureReserve && largeAge != ^uint64(0) {
			oldest = oldestLarge
		}
		// Prefer storage that the next animation frame can reuse.
		if smallBytes <= stageSmallTextureReserve && matchingAge != ^uint64(0) && oldestMatching != oldest {
			candidate := g.textures[oldest]
			// Spare each entry only once so unused sizes cannot pin the cache.
			if !candidate.spared {
				candidate.spared = true
				g.textures[oldest] = candidate
				oldest = oldestMatching
			}
		}
		t := g.textures[oldest]
		if g.nativePen != nil {
			g.nativePen.flushTexture(t.image)
		}
		g.rememberTexture(oldest, t.used)
		delete(g.repeatedPixels, t.image)
		delete(g.textureContent, t.image)
		if g.nativePen != nil {
			delete(g.nativePen.uploads, t.image)
		}
		// Queued GPU draws precede the new upload into this standalone texture.
		if unmanaged && t.unmanaged && im == nil && t.image.Bounds().Size() == image.Pt(width, height) {
			im = t.image
		} else {
			t.image.Deallocate()
		}
		g.bytes -= t.bytes
		delete(g.textures, oldest)
		if g.measureUploads {
			g.evictions++
		}
	}
	retainTemporary := temporary && bytes <= stageTransientTextureLimit &&
		(g.cacheRenderer != nil || (g.nativePen != nil && len(g.nativePen.replaying) != 0))
	if retainTemporary {
		for len(g.transientTextures) >= 4 || g.transientBytes+bytes > stageTransientTextureLimit {
			var oldest stageTextureKey
			age := ^uint64(0)
			for key, texture := range g.transientTextures {
				if texture.used < age {
					oldest, age = key, texture.used
				}
			}
			g.evictTransientTexture(oldest)
		}
	} else if temporary {
		g.releaseTransientTexture()
	}
	if im == nil {
		im = ebiten.NewImageWithOptions(image.Rect(0, 0, width, height), &ebiten.NewImageOptions{Unmanaged: unmanaged})
	}
	if g.measureUploads {
		now := time.Now()
		g.textureTime += now.Sub(phaseStart)
		phaseStart = now
	}
	stageUpload(im, pixels)
	if g.textureContent == nil {
		g.textureContent = make(map[*ebiten.Image]image.Rectangle)
	}
	g.textureContent[im] = content
	if g.measureUploads {
		g.writeTime += time.Since(phaseStart)
	}
	if repeated {
		if g.repeatedPixels == nil {
			g.repeatedPixels = make(map[*ebiten.Image]bool)
		}
		g.repeatedPixels[im] = true
	}
	if temporary {
		if retainTemporary {
			if g.transientTextures == nil {
				g.transientTextures = make(map[stageTextureKey]stageTexture)
			}
			g.transientTextures[key] = stageTexture{image: im, bytes: bytes, used: g.clock}
			g.transientBytes += bytes
			return im, false
		}
		g.rememberTexture(key, g.clock)
		return im, true
	}
	if g.textures == nil {
		g.textures = make(map[stageTextureKey]stageTexture)
	}
	if p := g.nativePen; p != nil {
		if p.uploads == nil {
			p.uploads = make(map[*ebiten.Image]bool)
		}
		p.uploads[im] = true
	}
	g.textures[key] = stageTexture{image: im, bytes: bytes, used: g.clock, reused: reused, unmanaged: unmanaged, needed: g.protectTextures}
	g.bytes += bytes
	return im, false
}

// Prefetch distinct upcoming images, bounding the scan through repeated clones.
func (g *stageGPU) prepareSceneBitmaps(r *render.Renderer, current string) {
	if g.preparedScene {
		return
	}
	g.preparedScene = true
	var candidates [16]*media.Costume
	var identities [16]string
	count := 0
	for i := 0; i < min(len(g.upcomingScene), 512); i++ {
		t := g.upcomingScene[i]
		if !t.State.Visible || t.State.Effects.Ghost >= 100 || (t.State.Text != nil && t.State.Text.Visible) {
			continue
		}
		d, ok := r.DisplaySprite(t, g.cacheSize.X, g.cacheSize.Y)
		if !ok || !d.Costume.Bitmap {
			continue
		}
		identity, _ := r.Assets.CostumeImageCacheKey(d.Costume, 1)
		duplicate := identity == current
		for _, seen := range identities[:count] {
			duplicate = duplicate || identity == seen
		}
		if duplicate {
			continue
		}
		region := g.spriteTextureRegion(r, d, g.cacheSize, t.State.Effects)
		key := stageTextureKey{library: r.Assets, image: identity, region: region}
		if _, ok := g.textures[key]; ok || g.transientTextures[key].image != nil {
			continue
		}
		if quadOutsideViewport(d.Corners, g.cacheSize.X, g.cacheSize.Y) ||
			render.ClipRectangle(t.State.Clipbox, r.DisplayView(), g.cacheSize.X, g.cacheSize.Y).Empty() {
			continue
		}
		candidates[count], identities[count] = d.Costume, identity
		count++
		if count == len(candidates) {
			break
		}
	}
	queued := 0
	for i := 0; i < count; i++ {
		if r.Assets.PrepareDisplayBitmap(candidates[i]) {
			queued++
			if queued == 4 {
				break
			}
		}
	}
}

func quadOutsideViewport(corners [4][2]float64, width, height int) bool {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, point := range corners {
		minX, minY = math.Min(minX, point[0]), math.Min(minY, point[1])
		maxX, maxY = math.Max(maxX, point[0]), math.Max(maxY, point[1])
	}
	return maxX <= 0 || maxY <= 0 || minX >= float64(width) || minY >= float64(height)
}

func (g *stageGPU) quad(dst, src *ebiten.Image, corners [4][2]float64, width, height float64, e vm.GraphicEffects, nearest, premultiplied bool) {
	g.quadVisual(dst, src, corners, width, height, e, nearest, premultiplied, "", nil)
}

func (g *stageGPU) quadMask(dst *ebiten.Image, corners [4][2]float64) {
	vertices := [4]ebiten.Vertex{}
	for i, p := range corners {
		vertices[i] = ebiten.Vertex{DstX: float32(p[0]), DstY: float32(p[1])}
	}
	// HyperClipping writes the mask even for fully transparent fragments.
	dst.DrawTrianglesShader(vertices[:], []uint16{0, 1, 2, 0, 2, 3}, g.shader, &ebiten.DrawTrianglesShaderOptions{Uniforms: map[string]any{"WriteMask": true}})
}

func (g *stageGPU) quadVisual(dst, src *ebiten.Image, corners [4][2]float64, width, height float64, e vm.GraphicEffects, nearest, premultiplied bool, blend string, mask *ebiten.Image) {
	if g.nativePen != nil {
		delete(g.nativePen.uploads, src)
	}
	if g.measureUploads {
		start := time.Now()
		defer func() { g.penSubmitTime += time.Since(start) }()
	}
	if p := g.nativePen; p != nil && p.stampShader != nil && e.Tint == nil && !premultiplied && mask == nil &&
		e.Mosaic == 0 && e.Pixelate == 0 && e.Whirl == 0 && e.Fisheye == 0 {
		/*
			Per-vertex effects batch adjacent atlas sprites without reordering. Masks and UV distortion use
			the general path.
		*/
		filter := float32(4 + boolInt(nearest))
		if g.repeatedPixels[src] {
			filter += 2
		}
		vertices := stampQuadVertices(src, corners, e, filter)
		if blend == "" || blend == "default" {
			g.trimStampVertices(&vertices, src)
			if stampOutsideBounds(vertices, dst.Bounds()) {
				return
			}
		}
		dst.DrawTrianglesShader(vertices[:], []uint16{0, 1, 2, 0, 2, 3}, p.stampShader,
			&ebiten.DrawTrianglesShaderOptions{Images: [4]*ebiten.Image{src}, Blend: spriteBlend(blend)})
		return
	}
	w, h := float32(src.Bounds().Dx()), float32(src.Bounds().Dy())
	vertices := [4]ebiten.Vertex{}
	for i, uv := range [4][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		vertices[i] = ebiten.Vertex{DstX: float32(corners[i][0]), DstY: float32(corners[i][1]), SrcX: uv[0] * w, SrcY: uv[1] * h, Custom0: float32(corners[i][0]), Custom1: float32(corners[i][1])}
	}
	colorEffect := e.Color
	if math.IsNaN(colorEffect) || math.IsInf(colorEffect, 0) {
		colorEffect = 0
	}
	// DrawTrianglesShader copies uniforms, allowing the map and slices to be reused.
	if g.uniforms == nil {
		g.uniforms = map[string]any{
			"LogicalSize":  g.logicalSize[:],
			"UVEffects":    g.uvEffects[:],
			"ColorEffects": g.colorEffects[:],
			"TintColor":    g.tintColor[:],
		}
	}
	g.logicalSize = [2]float32{float32(width), float32(height)}
	g.uvEffects = [4]float32{float32(math.Max(1, math.Min(512, math.Floor((math.Abs(e.Mosaic)+10)/10+.5)))), float32(math.Abs(e.Pixelate) / 10), float32(-e.Whirl * math.Pi / 180), float32(math.Max(0, (e.Fisheye+100)/100))}
	g.colorEffects = [4]float32{float32(math.Mod(colorEffect/200, 1)), float32(math.Max(-100, math.Min(100, e.Brightness)) / 100), float32(1 - math.Max(0, math.Min(100, e.Ghost))/100), 0}
	g.tintColor = [3]float32{1, 1, 1}
	if e.Tint != nil {
		for i, channel := range e.Tint {
			g.tintColor[i] = float32(channel)
		}
	}
	g.uniforms["RepeatedPixels"] = g.repeatedPixels[src]
	g.uniforms["ClipMask"] = mask != nil
	g.uniforms["Nearest"] = nearest || e.Pixelate != 0
	g.uniforms["HasFisheye"] = e.Fisheye != 0
	g.uniforms["HasColorEffect"] = colorEffect != 0
	g.uniforms["HasTint"] = e.Tint != nil
	g.uniforms["Premultiplied"] = premultiplied
	options := ebiten.DrawTrianglesShaderOptions{Images: [4]*ebiten.Image{src, mask}, Blend: spriteBlend(blend), Uniforms: g.uniforms}
	dst.DrawTrianglesShader(vertices[:], []uint16{0, 1, 2, 0, 2, 3}, g.shader, &options)
}

func (g *stageGPU) drawPen(dst *ebiten.Image, r *render.Renderer) {
	if p := g.nativePen; p != nil && p.Renderer == r {
		p.replayCommands()
		p.flushStamps()
		p.flushLines()
		if p.dirty {
			p.upload()
		}
		p.flushClear([4]*ebiten.Image{})
		if p.image != nil {
			g.drawPenImage(dst, p.image, r, true)
		}
		return
	}
	pixels, revision := r.DisplayPen()
	if pixels == nil {
		if g.pen != nil {
			g.pen.Deallocate()
			g.pen = nil
		}
		g.penRenderer = r
		g.penRevision = revision
		return
	}
	if g.pen == nil || g.pen.Bounds().Size() != pixels.Bounds().Size() {
		if g.pen != nil {
			g.pen.Deallocate()
		}
		g.pen = ebiten.NewImage(pixels.Bounds().Dx(), pixels.Bounds().Dy())
		g.penRenderer = nil
	}
	if g.penRenderer != r || g.penRevision != revision {
		if composite := r.DisplayPenComposite(); composite != nil {
			g.pen.WritePixels(composite.Pix)
		} else {
			stageUpload(g.pen, pixels)
		}
		g.penRenderer = r
		g.penRevision = revision
	}
	g.drawPenImage(dst, g.pen, r, r.DisplayPenComposite() != nil)
}

func (g *stageGPU) drawPenImage(dst, pen *ebiten.Image, r *render.Renderer, premultiplied bool) {
	v := r.DisplayView()
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	// PenSkin uses linear filtering below the logical stage resolution.
	scale := math.Max(float64(w)/v.Width, float64(h)/v.Height) * math.Abs(v.Zoom/100)
	nearest := v.NoAntialias || v.Canvas.Nearest || scale >= 1
	var corners [4][2]float64
	for i, uv := range [4][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		corners[i][0], corners[i][1] = r.DisplayPoint((uv[0]-.5)*v.Width, (.5-uv[1])*v.Height, w, h)
	}
	if premultiplied && nearest {
		// The GPU pen already has final premultiplied colors; composite it directly.
		pw, ph := float32(pen.Bounds().Dx()), float32(pen.Bounds().Dy())
		var vertices [4]ebiten.Vertex
		for i, uv := range [4][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
			vertices[i] = ebiten.Vertex{
				DstX: float32(corners[i][0]), DstY: float32(corners[i][1]),
				SrcX: uv[0] * pw, SrcY: uv[1] * ph,
				ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1,
			}
		}
		dst.DrawTriangles(vertices[:], []uint16{0, 1, 2, 0, 2, 3}, pen, nil)
		return
	}
	g.quad(dst, pen, corners, float64(pen.Bounds().Dx()), float64(pen.Bounds().Dy()), vm.GraphicEffects{}, nearest, premultiplied)
}

func (g *stageGPU) initShader() error {
	if g.shader == nil {
		shader, err := ebiten.NewShader(stageShaderSource)
		if err != nil {
			return err
		}
		g.shader = shader
	}
	return nil
}

/*
Warm each shader in a separate pixel so clears cannot discard its work. The caller flushes by
reading.
*/
func (g *stageGPU) drawShaderWarmup(dst *ebiten.Image) error {
	for _, program := range []struct {
		shader **ebiten.Shader
		source []byte
	}{
		{&g.canvas.copy, canvasCopySource},
		{&g.canvas.filter, canvasShaderSource},
		{&g.canvas.split, splitShaderSource},
	} {
		if *program.shader == nil {
			shader, err := ebiten.NewShader(program.source)
			if err != nil {
				return err
			}
			*program.shader = shader
		}
	}
	src := ebiten.NewImage(1, 1)
	defer src.Deallocate()
	src.WritePixels([]byte{255, 0, 0, 255})
	shaders := [6]*ebiten.Shader{
		g.shader, g.nativePen.stampShader, g.nativePen.shader,
		g.canvas.copy, g.canvas.filter, g.canvas.split,
	}
	indices := []uint16{0, 1, 2, 0, 2, 3}
	for i, shader := range shaders {
		var vertices [4]ebiten.Vertex
		for j, uv := range [4][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
			v := ebiten.Vertex{
				DstX: float32(i) + uv[0], DstY: uv[1], SrcX: uv[0], SrcY: uv[1],
				ColorR: 1, ColorG: 1, ColorA: 1,
				Custom0: uv[0], Custom1: uv[1], Custom3: 1,
			}
			if i == 2 {
				v.SrcX, v.SrcY = v.DstX, v.DstY
				v.ColorR, v.ColorG, v.ColorB, v.ColorA = 0, 0, 1, 255
				v.Custom0, v.Custom1, v.Custom2 = 0, 0, 1
			}
			vertices[j] = v
		}
		options := ebiten.DrawTrianglesShaderOptions{Images: [4]*ebiten.Image{src}, Blend: ebiten.BlendCopy}
		switch i {
		case 0:
			options.Uniforms = map[string]any{"WriteMask": true}
		case 3:
			options.Uniforms = map[string]any{"SourceSize": []float32{1, 1}}
		case 4:
			options.Uniforms = map[string]any{
				"SourceSize": []float32{1, 1}, "Nearest": 1,
				"Filters": []float32{1, 1, 0, 1}, "Filters2": []float32{0, 0, 1, 0},
			}
		case 5:
			options.Uniforms = map[string]any{"OutputSize": []float32{1, 1}}
		}
		dst.DrawTrianglesShader(vertices[:], indices, shader, &options)
	}
	return nil
}

// Protect scene and stamp textures without loading pixels, including hidden stamps.
func (g *stageGPU) protectSceneTextures(r *render.Renderer, scene []vm.DisplayTarget, width, height int) {
	for key, t := range g.textures {
		if t.needed {
			t.needed = false
			g.textures[key] = t
		}
	}
	for _, t := range scene {
		if !t.State.Visible || t.State.Effects.Ghost >= 100 {
			continue
		}
		// Text geometry can rasterize; defer it to the actual draw.
		if t.State.Text != nil && t.State.Text.Visible {
			continue
		}
		d, ok := r.DisplaySprite(t, width, height)
		if !ok {
			continue
		}
		if quadOutsideViewport(d.Corners, width, height) || render.ClipRectangle(t.State.Clipbox, r.DisplayView(), width, height).Empty() {
			continue
		}
		g.protectCostumeTexture(r, d, image.Pt(width, height), t.State.Effects)
	}
	if p := g.nativePen; p != nil {
		commands := p.commands
		if len(p.replaying) != 0 {
			commands = p.replaying
		}
		for _, entry := range commands {
			t := entry.command.Target
			if entry.command.Kind != "stamp" || t.State.Effects.Ghost >= 100 ||
				(t.State.Text != nil && t.State.Text.Visible) ||
				(t.State.MaskMode && !(t.State.Name == p.DisplayView().MaskTarget && t.State.CloneID == 0)) {
				continue
			}
			d, ok := p.stampSprite(t)
			size := p.layerSize()
			if ok && !render.PenClipRectangle(t.State.Clipbox, p.DisplayView(), size.X, size.Y).Empty() {
				g.protectCostumeTexture(p.Renderer, d, size, t.State.Effects)
			}
		}
	}
}

func (g *stageGPU) protectCostumeTexture(r *render.Renderer, d render.DisplaySprite, viewport image.Point, effects vm.GraphicEffects) {
	region := g.spriteTextureRegion(r, d, viewport, effects)
	identity, density := r.Assets.CostumeImageCacheKey(d.Costume, d.Density)
	for {
		if !region.Empty() {
			/*
				A crop replaces the full upload under pressure; recent use must
				not keep both representations resident just to reuse the full one.
			*/
			fullKey := stageTextureKey{library: r.Assets, image: identity}
			if full, ok := g.textures[fullKey]; ok && !full.needed {
				full.reused = false
				g.textures[fullKey] = full
			}
		}
		key := stageTextureKey{library: r.Assets, image: identity, region: region}
		if cached, ok := g.textures[key]; ok {
			cached.needed = true
			g.textures[key] = cached
			break
		}
		if density <= 1 {
			break
		}
		identity, density = r.Assets.CostumeImageCacheKey(d.Costume, density/2)
	}
}

func (g *stageGPU) drawScene(dst *ebiten.Image, r *render.Renderer, scene []vm.DisplayTarget) error {
	if err := g.initShader(); err != nil {
		return err
	}
	if g.transparent {
		dst.Clear()
	} else {
		dst.Fill(vm.ParseColor(r.DisplayView().Color))
	}
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	g.cacheRenderer, g.cacheScene, g.cacheSize = r, scene, image.Pt(w, h)
	defer func() {
		g.releaseTransientTexture()
		g.protectTextures = false
		g.cacheRenderer, g.cacheScene = nil, nil
		g.upcomingScene = nil
		g.preparedScene = false
	}()
	penDrawn := false
	for _, t := range scene {
		if t.State.RenderLayer == vm.StageSurface {
			penDrawn = true
			break
		}
	}
	if r.DisplayView().MaskTarget != "" {
		if g.mask == nil || g.mask.Bounds().Size() != dst.Bounds().Size() {
			if g.mask != nil {
				g.mask.Deallocate()
			}
			g.mask = ebiten.NewImage(w, h)
		}
		g.mask.Clear()
	}
	for i, t := range scene {
		g.upcomingScene = scene[i+1:]
		g.preparedScene = false
		if !t.State.IsStage && !penDrawn {
			g.drawPen(dst, r)
			penDrawn = true
		}
		isMask := r.DisplayView().MaskTarget != "" && t.State.Name == r.DisplayView().MaskTarget && t.State.CloneID == 0
		if !t.State.Visible || (t.State.Effects.Ghost >= 100 && !isMask) {
			continue
		}
		if t.State.RenderLayer != "" {
			g.drawLayer(dst, t)
			continue
		}
		d, ok := r.DisplaySprite(t, w, h)
		if !ok {
			continue
		}
		if quadOutsideViewport(d.Corners, w, h) {
			continue
		}
		clip := render.ClipRectangle(t.State.Clipbox, r.DisplayView(), w, h)
		if clip.Empty() {
			continue
		}
		if isMask {
			g.quadMask(clippedImage(g.mask, clip), d.Corners)
		}
		if t.State.Effects.Ghost >= 100 {
			continue
		}
		texture, temporary := g.spriteTexture(r, &d, image.Pt(w, h), t.State.Effects)
		if texture == nil {
			continue
		}
		target := clippedImage(dst, clip)
		var mask *ebiten.Image
		if t.State.MaskMode && !isMask {
			mask = g.mask
		}
		g.quadVisual(target, texture, d.Corners, d.Costume.Width, d.Costume.Height, t.State.Effects, d.Costume.Bitmap || r.DisplayView().NoAntialias || r.DisplayView().Canvas.Nearest, false, t.State.BlendMode, mask)
		if temporary {
			delete(g.repeatedPixels, texture)
			delete(g.textureContent, texture)
			texture.Deallocate()
		}
	}
	if !penDrawn {
		g.drawPen(dst, r)
	}
	return r.Assets.Err()
}
