package desktop

import (
	_ "embed"
	"image"
	"image/color"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"
)

const pendingPenLimit = 4096
const penStampBatchSize = 1024
const penLineBatchSize = 1024
const penQueuedQuadLimit = 4096

/*
deferredPen is the VM-facing recorder. GPU resource resolution stays in the
display path except at sensing, view-change and bounded-queue barriers.
*/
type deferredPen struct {
	*penGPU
}

type recordedPen struct {
	command vm.PenCommand
}

func (p deferredPen) Pen(cmd vm.PenCommand) {
	if cmd.Kind == "clear" {
		clear(p.commands)
		p.commands = p.commands[:0]
		p.penGPU.Pen(cmd)
		return
	}
	if (cmd.Kind != "stamp" && cmd.Kind != "line") ||
		(cmd.Kind == "stamp" && len(cmd.Points) != 0) {
		p.replayCommands()
		p.penGPU.Pen(cmd)
		return
	}
	if cmd.Kind == "stamp" && cmd.Target.State.Effects.Ghost >= 100 {
		return
	}
	if len(p.commands) == pendingPenLimit {
		p.replayCommands()
	}
	if p.commands == nil {
		p.commands = make([]recordedPen, 0, pendingPenLimit)
	}
	entry := recordedPen{command: cmd}
	entry.command.Target.State.Variables = nil
	entry.command.Target.State.Lists = nil
	// TextSkin is immutable once published; line coordinates are stored by value.
	if cmd.Kind == "line" {
		entry.command.Target = vm.DisplayTarget{State: vm.TargetState{Clipbox: cmd.Target.State.Clipbox, BlendMode: cmd.Target.State.BlendMode}}
	}
	entry.command.Points = nil
	p.commands = append(p.commands, entry)
	if cmd.Kind == "stamp" {
		p.prepareRecordedBitmap(cmd.Target)
	}
}

// Start selected bitmap decoding before the script reaches Draw or a stamp.
func (p *penGPU) PrepareCostume(source, index int) {
	if source < 0 || source >= len(p.Assets.Costumes) {
		return
	}
	costumes := p.Assets.Costumes[source]
	if index < 0 || index >= len(costumes) {
		return
	}
	costume := costumes[index]
	if !costume.Bitmap || costume.Image != nil {
		return
	}
	identity, _ := p.Assets.CostumeImageCacheKey(costume, 1)
	key := stageTextureKey{library: p.Assets, image: identity}
	if _, ok := p.gpu.textures[key]; ok || p.gpu.transientTextures[key].image != nil {
		return
	}
	p.Assets.PrepareDisplayBitmap(costume)
}

/*
Start decoding while the VM continues recording. Pixels and GPU uploads are
still consumed in replay order; speculative work uses the library's budget.
*/
func (p *penGPU) prepareRecordedBitmap(target vm.DisplayTarget) {
	state := target.State
	view := p.DisplayView()
	if (state.Text != nil && state.Text.Visible) ||
		(state.MaskMode && !(state.Name == view.MaskTarget && state.CloneID == 0)) ||
		target.Source < 0 || target.Source >= len(p.Assets.Costumes) {
		return
	}
	costumes := p.Assets.Costumes[target.Source]
	if state.CurrentCostume < 0 || state.CurrentCostume >= len(costumes) {
		return
	}
	costume := costumes[state.CurrentCostume]
	if !costume.Bitmap || costume.Image != nil {
		return
	}
	identity, _ := p.Assets.CostumeImageCacheKey(costume, 1)
	key := stageTextureKey{library: p.Assets, image: identity}
	if _, ok := p.gpu.textures[key]; ok || p.gpu.transientTextures[key].image != nil {
		return
	}
	d, ok := p.stampSprite(target)
	if !ok || !d.Costume.Bitmap {
		return
	}
	size := p.layerSize()
	if render.PenClipRectangle(state.Clipbox, view, size.X, size.Y).Empty() {
		return
	}
	key.region = p.gpu.spriteTextureRegion(p.Renderer, d, size, state.Effects)
	if _, ok := p.gpu.textures[key]; ok || p.gpu.transientTextures[key].image != nil {
		return
	}
	p.Assets.PrepareDisplayBitmap(d.Costume)
}

func (p *penGPU) replayCommands() {
	commands := p.commands
	if len(commands) == 0 {
		return
	}
	p.replaying = commands
	defer func() {
		p.replaying = nil
		p.replayIndex = 0
		if p.gpu.cacheRenderer == nil && !p.parallelTick {
			p.gpu.releaseTransientTexture()
			p.gpu.protectTextures = false
		}
	}()
	// Detach the queue so a nested flush cannot replay these commands twice.
	p.commands = p.commands[:0]
	for i := range commands {
		p.replayIndex = i
		cmd := commands[i].command
		p.Pen(cmd)
	}
	clear(commands)
}

func (p deferredPen) beginParallelTick() {
	p.parallelTick = true
}

func (p deferredPen) endParallelTick() {
	p.parallelTick = false
	if p.gpu.cacheRenderer == nil {
		p.gpu.releaseTransientTexture()
		p.gpu.protectTextures = false
	}
}

// Prefetch upcoming stamps on a cache miss, including stamps from hidden sprites.
func (p *penGPU) prepareStampBitmaps(current string) {
	size := p.layerSize()
	view := p.DisplayView()
	var candidates [16]*media.Costume
	var identities [16]string
	count := 0
	for i := p.replayIndex + 1; i < min(len(p.replaying), p.replayIndex+513); i++ {
		cmd := p.replaying[i].command
		state := cmd.Target.State
		if cmd.Kind != "stamp" || state.Effects.Ghost >= 100 || (state.Text != nil && state.Text.Visible) ||
			(state.MaskMode && !(state.Name == view.MaskTarget && state.CloneID == 0)) {
			continue
		}
		d, ok := p.stampSprite(cmd.Target)
		if !ok || !d.Costume.Bitmap || render.PenClipRectangle(state.Clipbox, view, size.X, size.Y).Empty() {
			continue
		}
		identity, _ := p.Assets.CostumeImageCacheKey(d.Costume, 1)
		duplicate := identity == current
		for _, seen := range identities[:count] {
			duplicate = duplicate || identity == seen
		}
		if duplicate {
			continue
		}
		region := p.gpu.spriteTextureRegion(p.Renderer, d, size, state.Effects)
		key := stageTextureKey{library: p.Assets, image: identity, region: region}
		if _, ok := p.gpu.textures[key]; ok || p.gpu.transientTextures[key].image != nil {
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
		if p.Assets.PrepareDisplayBitmap(candidates[i]) {
			queued++
			if queued == 4 {
				break
			}
		}
	}
}

type penDrawBatch struct {
	texture                *ebiten.Image
	shader                 *ebiten.Shader
	clip                   image.Rectangle
	blend                  string
	vertexStart, vertexEnd int
	indexStart, indexEnd   int
}

//go:embed pen_gpu.kage
var penShaderSource []byte

//go:embed stamp_gpu.kage
var stampShaderSource []byte

// Keep draw order and retain command history for CPU color sensing.
type penGPU struct {
	*render.Renderer
	gpu           *stageGPU
	image         *ebiten.Image
	resizeSpare   *ebiten.Image
	pending       []vm.PenCommand
	commands      []recordedPen
	replaying     []recordedPen
	replayIndex   int
	shader        *ebiten.Shader
	stampShader   *ebiten.Shader
	dirty         bool
	clearPending  bool
	pixels        *image.RGBA
	keepHistory   bool
	parallelTick  bool
	cpuStale      bool
	stampTexture  *ebiten.Image
	stampVertices []ebiten.Vertex
	stampIndices  []uint16
	lineVertices  []ebiten.Vertex
	lineIndices   []uint16
	lineClip      image.Rectangle
	lineBlend     string
	uploads       map[*ebiten.Image]bool
	draws         []penDrawBatch
	drawVertices  []ebiten.Vertex
	drawIndices   []uint16
	outputSize    image.Point
	scale         float64

	surfaceShader     *ebiten.Shader
	surfaces          map[string]*ebiten.Image
	surfaceCPU        map[string]bool
	surfaceHistory    bool
	historyDepthOnly  []bool
	checkpoint        *ebiten.Image
	checkpointPending bool
}

func (g *stageGPU) enablePen(r *render.Renderer) (*penGPU, error) {
	if err := g.initShader(); err != nil {
		return nil, err
	}
	p := &penGPU{Renderer: r, gpu: g, keepHistory: true, scale: 1}
	shader, err := ebiten.NewShader(penShaderSource)
	if err != nil {
		return nil, err
	}
	p.shader = shader
	p.stampShader, err = ebiten.NewShader(stampShaderSource)
	if err != nil {
		p.shader.Deallocate()
		return nil, err
	}
	p.surfaceShader, err = ebiten.NewShader(penSurfaceShaderSource)
	if err != nil {
		p.shader.Deallocate()
		p.stampShader.Deallocate()
		return nil, err
	}
	p.upload()
	g.nativePen = p
	return p, nil
}

func (p *penGPU) flush() {
	p.replayCommands()
	p.flushStamps()
	p.flushLines()
	p.flushClear([4]*ebiten.Image{})
	p.restoreCheckpoint()
	if !p.keepHistory && !p.surfaceHistory {
		if p.cpuStale && p.image != nil {
			p.readPenImage(p.image)
		}
		return
	}
	for _, command := range p.pending {
		p.Renderer.Pen(command)
	}
	clear(p.pending)
	p.pending = p.pending[:0]
}

func (p *penGPU) readPenImage(source *ebiten.Image) {
	w, h := p.Renderer.Size()
	bounds := image.Rect(0, 0, w, h)
	if p.pixels == nil || p.pixels.Bounds() != bounds {
		p.pixels = image.NewRGBA(bounds)
	}
	if source.Bounds() == bounds {
		source.ReadPixels(p.pixels.Pix)
	} else {
		logical := ebiten.NewImage(w, h)
		options := ebiten.DrawImageOptions{}
		options.GeoM.Scale(float64(w)/float64(source.Bounds().Dx()), float64(h)/float64(source.Bounds().Dy()))
		logical.DrawImage(source, &options)
		logical.ReadPixels(p.pixels.Pix)
		logical.Deallocate()
	}
	p.Renderer.RestorePen(p.pixels)
	p.cpuStale = false
}

func (p *penGPU) restoreCheckpoint() {
	if p.checkpointPending {
		p.readPenImage(p.checkpoint)
		p.checkpointPending = false
	}
}

func (p *penGPU) checkpointHistory() {
	if p.keepHistory || !p.surfaceHistory || p.dirty || p.surfaceCPU[vm.StageSurface] {
		p.flush()
		return
	}
	p.flushStamps()
	p.flushLines()
	p.flushClear([4]*ebiten.Image{})
	p.preserveSurfaceHistory(true)
	if p.image == nil {
		return
	}
	// Keep one logical-size GPU snapshot; CPU consumers restore it before replay.
	w, h := p.Renderer.Size()
	if p.checkpoint != nil && p.checkpoint.Bounds().Size() != image.Pt(w, h) {
		p.checkpoint.Deallocate()
		p.checkpoint = nil
	}
	if p.checkpoint == nil {
		p.checkpoint = ebiten.NewImage(w, h)
	}
	options := ebiten.DrawImageOptions{Blend: ebiten.BlendCopy}
	options.GeoM.Scale(float64(w)/float64(p.image.Bounds().Dx()), float64(h)/float64(p.image.Bounds().Dy()))
	p.checkpoint.DrawImage(p.image, &options)
	p.checkpointPending, p.cpuStale = true, true
}

func (p *penGPU) upload() {
	// The CPU snapshot already includes any pending clear.
	p.clearPending = false
	pixels, _ := p.DisplayPen()
	size := p.layerSize()
	if p.image != nil && (pixels == nil || p.image.Bounds().Size() != size) {
		p.image.Deallocate()
		p.image = nil
	}
	if pixels != nil {
		if composite := p.Renderer.DisplayPenComposite(); composite != nil {
			if p.image == nil {
				p.image = ebiten.NewImage(size.X, size.Y)
			}
			if p.scale == 1 {
				p.image.WritePixels(composite.Pix)
			} else {
				source := ebiten.NewImage(composite.Bounds().Dx(), composite.Bounds().Dy())
				source.WritePixels(composite.Pix)
				options := ebiten.DrawImageOptions{}
				options.GeoM.Scale(float64(size.X)/float64(composite.Bounds().Dx()), float64(size.Y)/float64(composite.Bounds().Dy()))
				options.Blend = ebiten.BlendCopy
				p.image.DrawImage(source, &options)
				source.Deallocate()
			}
			p.dirty, p.cpuStale = false, false
			return
		}
		if p.image == nil {
			p.image = ebiten.NewImage(size.X, size.Y)
		}
		if p.scale != 1 {
			source := ebiten.NewImage(pixels.Bounds().Dx(), pixels.Bounds().Dy())
			stageUploadPixels(source, pixels, true)
			options := ebiten.DrawImageOptions{}
			options.GeoM.Scale(float64(size.X)/float64(pixels.Bounds().Dx()), float64(size.Y)/float64(pixels.Bounds().Dy()))
			p.image.Clear()
			p.image.DrawImage(source, &options)
			source.Deallocate()
			p.dirty, p.cpuStale = false, false
			return
		}
		stageUploadPixels(p.image, pixels, true)
	}
	p.dirty = false
	p.cpuStale = false
}

func (p *penGPU) SetView(view vm.View) {
	old := p.DisplayView()
	if old.Width != view.Width || old.Height != view.Height ||
		old.HighQualityPen != view.HighQualityPen || old.DisablePositionRounding != view.DisablePositionRounding ||
		old.PenNoAntialias != view.PenNoAntialias || old.NoAntialias != view.NoAntialias || old.MaskTarget != view.MaskTarget {
		p.replayCommands()
	}
	if old.HighQualityPen != view.HighQualityPen || old.NoAntialias != view.NoAntialias {
		p.flushStamps()
	}
	resize := old.Width != view.Width || old.Height != view.Height
	roundingChanged := (old.HighQualityPen || old.DisablePositionRounding) != (view.HighQualityPen || view.DisablePositionRounding)
	if old.MaskTarget != view.MaskTarget || resize || ((p.keepHistory || p.surfaceHistory) && (old.PenNoAntialias != view.PenNoAntialias || old.NoAntialias != view.NoAntialias || roundingChanged)) {
		// Replay existing stamps using the mode active when they were drawn.
		p.flush()
	}
	p.Renderer.SetView(view)
	if resize {
		p.upload()
	}
	p.setOutputSize(p.outputSize.X, p.outputSize.Y)
}

func (p *penGPU) layerSize() image.Point {
	v := p.DisplayView()
	return image.Pt(int(math.Ceil(v.Width*p.scale)), int(math.Ceil(v.Height*p.scale)))
}

/*
Display resolution never changes the logical pen used for color sensing.
Resize the existing GPU layer without retaining/replaying historical stamps.
*/
func (p *penGPU) setOutputSize(width, height int) {
	p.outputSize = image.Pt(width, height)
	v := p.DisplayView()
	scale := 1.0
	if v.HighQualityPen {
		width, height = render.DisplaySize(width, height)
		scale = math.Max(1, math.Min(float64(width)/v.Width, float64(height)/v.Height))
	} else if p.resizeSpare != nil {
		p.resizeSpare.Deallocate()
		p.resizeSpare = nil
	}
	if scale == p.scale {
		return
	}
	// Marks recorded before a display resize keep their original rasterization.
	p.replayCommands()
	p.flushStamps()
	p.flushLines()
	p.flushClear([4]*ebiten.Image{})
	p.scale = scale
	if p.image == nil {
		return
	}
	size := p.layerSize()
	old := p.image
	p.image, p.resizeSpare = p.resizeSpare, nil
	if p.image != nil && p.image.Bounds().Size() != size {
		p.image.Deallocate()
		p.image = nil
	}
	if p.image == nil {
		p.image = ebiten.NewImage(size.X, size.Y)
	} else {
		p.image.Clear()
	}
	options := ebiten.DrawImageOptions{}
	options.GeoM.Scale(float64(size.X)/float64(old.Bounds().Dx()), float64(size.Y)/float64(old.Bounds().Dy()))
	p.image.DrawImage(old, &options)
	// Retain one small spare buffer for repeated resolution changes.
	const reuseLimit = 2048
	if v.HighQualityPen && size.X <= reuseLimit && size.Y <= reuseLimit &&
		old.Bounds().Dx() <= reuseLimit && old.Bounds().Dy() <= reuseLimit {
		p.resizeSpare = old
	} else {
		old.Deallocate()
	}
}

func (p *penGPU) TouchingColor(scene []vm.DisplayTarget, target vm.DisplayTarget, wanted color.NRGBA, mask *color.NRGBA) bool {
	p.flush()
	return p.Renderer.TouchingColor(scene, target, wanted, mask)
}

func (p *penGPU) Pen(cmd vm.PenCommand) {
	if p.penSurface(cmd) {
		return
	}
	if cmd.Kind == "clear" {
		if p.surfaceHistory {
			p.preserveSurfaceHistory(false)
			delete(p.surfaceCPU, vm.StageSurface)
		}
		p.checkpointPending = false
		if p.gpu != nil && p.gpu.measureUploads {
			start := time.Now()
			defer func() { p.gpu.penSubmitTime += time.Since(start) }()
		}
		p.discardDraws()
		p.stampTexture = nil
		p.stampVertices = p.stampVertices[:0]
		p.stampIndices = p.stampIndices[:0]
		p.lineVertices = p.lineVertices[:0]
		p.lineIndices = p.lineIndices[:0]
		clear(p.pending)
		p.pending = p.pending[:0]
		p.Renderer.Pen(cmd)
		p.dirty = false
		p.cpuStale = false
		p.clearPending = p.image != nil
		return
	}
	if cmd.Kind == "line" && p.line(cmd) {
		return
	}
	if cmd.Kind != "stamp" {
		p.flush()
		_, before := p.DisplayPen()
		p.Renderer.Pen(cmd)
		_, after := p.DisplayPen()
		p.dirty = p.dirty || before != after
		return
	}
	if cmd.Target.State.MaskMode && !(cmd.Target.State.Name == p.DisplayView().MaskTarget && cmd.Target.State.CloneID == 0) {
		return
	}
	if cmd.Target.State.Effects.Ghost >= 100 {
		return
	}
	for _, n := range cmd.Points {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return
		}
	}
	d, ok := p.stampSprite(cmd.Target)
	if !ok {
		return
	}
	view := p.DisplayView()
	size := p.layerSize()
	clip := render.PenClipRectangle(cmd.Target.State.Clipbox, view, size.X, size.Y)
	if clip.Empty() {
		/*
			A fractional display scale can round away a clip that still covers
			logical pixels used by color sensing.
		*/
		if p.keepHistory || p.surfaceHistory {
			width, height := p.Renderer.Size()
			if !render.PenClipRectangle(cmd.Target.State.Clipbox, view, width, height).Empty() {
				p.remember(cmd)
			}
		}
		return
	}
	effects := cmd.Target.State.Effects
	ordinary := !math.IsNaN(effects.Brightness) && effects.Whirl == 0 &&
		effects.Fisheye == 0 && effects.Mosaic == 0 && effects.Pixelate == 0 && !math.IsNaN(effects.Ghost)
	texture, temporary := p.gpu.spriteTexture(p.Renderer, &d, size, effects)
	if texture == nil {
		return
	}
	p.queueLines()
	// Upload CPU fallback drawings before blending another GPU stamp.
	if p.dirty {
		p.upload()
	}
	if p.image == nil {
		p.image = ebiten.NewImage(size.X, size.Y)
	}
	nearest := view.NoAntialias || d.Costume.Bitmap || !view.HighQualityPen
	if cmd.Target.State.Clipbox != nil || (cmd.Target.State.BlendMode != "" && cmd.Target.State.BlendMode != "default") {
		p.flushStamps()
		p.flushClear([4]*ebiten.Image{texture})
		p.gpu.quadVisual(clippedImage(p.image, clip), texture, d.Corners, d.Costume.Width, d.Costume.Height, effects, nearest, false, cmd.Target.State.BlendMode, nil)
	} else if ordinary {
		p.stamp(texture, d.Corners, effects, nearest)
	} else {
		p.flushStamps()
		p.flushClear([4]*ebiten.Image{texture})
		p.gpu.quad(p.image, texture, d.Corners, d.Costume.Width, d.Costume.Height, effects, nearest, false)
	}
	if temporary {
		p.flushStamps()
		delete(p.gpu.repeatedPixels, texture)
		delete(p.gpu.textureContent, texture)
		texture.Deallocate()
	}
	p.remember(cmd)
}

// Use the same geometry for uploads and cache planning without loading pixels.
func (p *penGPU) stampSprite(target vm.DisplayTarget) (render.DisplaySprite, bool) {
	d, ok := p.PenSprite(target)
	if !ok {
		return render.DisplaySprite{}, false
	}
	view := p.DisplayView()
	size := p.layerSize()
	for i := range d.Corners {
		d.Corners[i][0] *= p.scale
		d.Corners[i][1] *= p.scale
	}
	if view.HighQualityPen {
		d.Density = math.Max(math.Hypot(d.Corners[1][0]-d.Corners[0][0], d.Corners[1][1]-d.Corners[0][1])/d.Costume.Width,
			math.Hypot(d.Corners[3][0]-d.Corners[0][0], d.Corners[3][1]-d.Corners[0][1])/d.Costume.Height)
		if target.State.RenderQuality > 0 {
			d.Density = math.Min(d.Density, target.State.RenderQuality)
		}
	}
	if quadOutsideViewport(d.Corners, size.X, size.Y) {
		return render.DisplaySprite{}, false
	}
	return d, true
}

/*
Per-vertex color effects let Ebitengine batch stamps, including stamps
from different costumes on the same atlas, without per-stamp uniforms.
*/
func (p *penGPU) stamp(texture *ebiten.Image, corners [4][2]float64, effects vm.GraphicEffects, nearest bool) {
	filter := float32(boolInt(nearest))
	if p.gpu != nil && p.gpu.repeatedPixels[texture] {
		filter += 2
	}
	vertices := stampQuadVertices(texture, corners, effects, filter)
	if p.gpu != nil {
		p.gpu.trimStampVertices(&vertices, texture)
	}
	if p.image != nil && stampOutsideBounds(vertices, p.image.Bounds()) {
		return
	}
	if p.stampTexture != texture || len(p.stampVertices) == penStampBatchSize*4 {
		p.queueStamps()
	}
	p.stampTexture = texture
	if p.stampVertices == nil {
		p.stampVertices = make([]ebiten.Vertex, 0, penStampBatchSize*4)
		p.stampIndices = make([]uint16, 0, penStampBatchSize*6)
	}
	base := uint16(len(p.stampVertices))
	p.stampVertices = append(p.stampVertices, vertices[:]...)
	p.stampIndices = append(p.stampIndices, base, base+1, base+2, base, base+2, base+3)
}

func stampQuadVertices(texture *ebiten.Image, corners [4][2]float64, effects vm.GraphicEffects, filter float32) [4]ebiten.Vertex {
	width, height := float32(texture.Bounds().Dx()), float32(texture.Bounds().Dy())
	alpha := float32(1 - math.Max(0, math.Min(100, effects.Ghost))/100)
	brightness := float32(math.Max(-100, math.Min(100, effects.Brightness)) / 100)
	hue := float32(0)
	if effects.Color != 0 && !math.IsNaN(effects.Color) && !math.IsInf(effects.Color, 0) {
		hue = float32(math.Mod(effects.Color/200, 1))
		if hue == 0 {
			/*
				A full hue turn still applies Scratch's minimum saturation/value.
				Reserve zero for a disabled color effect.
			*/
			hue = 1
		}
	}
	var vertices [4]ebiten.Vertex
	for i, uv := range [4][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		vertices[i] = ebiten.Vertex{
			DstX: float32(corners[i][0]), DstY: float32(corners[i][1]),
			SrcX: uv[0] * width, SrcY: uv[1] * height,
			ColorR: width, ColorG: height, ColorB: hue, ColorA: alpha,
			Custom0: uv[0] * width, Custom1: uv[1] * height, Custom2: brightness, Custom3: filter,
		}
	}
	return vertices
}

/*
Clear is a GPU draw too. Bind the next stamp sources here so Ebitengine
submits their uploads before clearing, avoiding a draw/upload fence on D3D12.
*/
func (p *penGPU) flushClear(images [4]*ebiten.Image) {
	if !p.clearPending {
		return
	}
	p.clearPending = false
	if p.image == nil {
		return
	}
	if images == [4]*ebiten.Image{} {
		p.image.Clear()
		return
	}
	for _, texture := range images {
		delete(p.uploads, texture)
	}
	w, h := float32(p.image.Bounds().Dx()), float32(p.image.Bounds().Dy())
	vertices := [4]ebiten.Vertex{{}, {DstX: w}, {DstX: w, DstY: h}, {DstY: h}}
	indices := [6]uint16{0, 1, 2, 0, 2, 3}
	// The line shader returns transparent pixels for zero vertex colors.
	p.image.DrawTrianglesShader(vertices[:], indices[:], p.shader,
		&ebiten.DrawTrianglesShaderOptions{Images: images, Blend: ebiten.BlendCopy})
}

// A texture must outlive every queued draw that samples it.
func (p *penGPU) flushTexture(texture *ebiten.Image) {
	if texture == nil {
		return
	}
	if p.stampTexture == texture && len(p.stampVertices) != 0 {
		p.flushStamps()
		return
	}
	for _, batch := range p.draws {
		if batch.texture == texture {
			p.flushStamps()
			return
		}
	}
}

func (p *penGPU) flushStamps() {
	if len(p.draws) > 0 {
		p.queueStamps()
		p.submitDraws()
	}
	if len(p.stampVertices) == 0 {
		return
	}
	if p.gpu != nil && p.gpu.measureUploads {
		start := time.Now()
		defer func() { p.gpu.penSubmitTime += time.Since(start) }()
	}
	p.flushClear([4]*ebiten.Image{p.stampTexture})
	delete(p.uploads, p.stampTexture)
	p.image.DrawTrianglesShader(p.stampVertices, p.stampIndices, p.stampShader,
		&ebiten.DrawTrianglesShaderOptions{Images: [4]*ebiten.Image{p.stampTexture}})
	p.stampTexture = nil
	p.stampVertices = p.stampVertices[:0]
	p.stampIndices = p.stampIndices[:0]
}

/*
Closed batches own geometry, not mutable targets or decoded costume pixels.
Only texture uploads can pass these batches; drawing order stays unchanged.
*/
func (p *penGPU) queueDraw(vertices []ebiten.Vertex, indices []uint16, texture *ebiten.Image, shader *ebiten.Shader, clip image.Rectangle, blend string) {
	if len(vertices) == 0 {
		return
	}
	if len(p.drawVertices)+len(vertices) > penQueuedQuadLimit*4 {
		p.submitDraws()
	}
	if p.drawVertices == nil {
		p.drawVertices = make([]ebiten.Vertex, 0, penQueuedQuadLimit*4)
		p.drawIndices = make([]uint16, 0, penQueuedQuadLimit*6)
	}
	batch := penDrawBatch{
		texture: texture, shader: shader,
		clip: clip, blend: blend,
		vertexStart: len(p.drawVertices), indexStart: len(p.drawIndices),
	}
	p.drawVertices = append(p.drawVertices, vertices...)
	p.drawIndices = append(p.drawIndices, indices...)
	batch.vertexEnd, batch.indexEnd = len(p.drawVertices), len(p.drawIndices)
	p.draws = append(p.draws, batch)
}

func (p *penGPU) queueStamps() {
	p.queueDraw(p.stampVertices, p.stampIndices, p.stampTexture, p.stampShader, image.Rectangle{}, "")
	p.stampTexture = nil
	p.stampVertices = p.stampVertices[:0]
	p.stampIndices = p.stampIndices[:0]
}

func (p *penGPU) queueLines() {
	p.queueDraw(p.lineVertices, p.lineIndices, nil, p.shader, p.lineClip, p.lineBlend)
	p.lineVertices = p.lineVertices[:0]
	p.lineIndices = p.lineIndices[:0]
}

func (p *penGPU) submitDraws() {
	if len(p.draws) == 0 {
		return
	}
	if p.gpu != nil && p.gpu.measureUploads {
		start := time.Now()
		defer func() { p.gpu.penSubmitTime += time.Since(start) }()
	}
	for i, batch := range p.draws {
		images := [4]*ebiten.Image{batch.texture}
		if p.uploads[batch.texture] || ((batch.texture == nil || p.clearPending) && len(p.uploads) > 0) {
			delete(p.uploads, batch.texture)
			/*
				Unused source slots submit upcoming uploads before this draw.
				Both pen shaders leave slots 1–3 unsampled.
			*/
			slot := 1
			for _, next := range p.draws[i+1:] {
				if !p.uploads[next.texture] {
					continue
				}
				images[slot] = next.texture
				delete(p.uploads, next.texture)
				slot++
				if slot == len(images) {
					break
				}
			}
		}
		p.flushClear(images)
		dst := p.image
		if !batch.clip.Empty() {
			dst = clippedImage(dst, batch.clip)
		}
		dst.DrawTrianglesShader(p.drawVertices[batch.vertexStart:batch.vertexEnd],
			p.drawIndices[batch.indexStart:batch.indexEnd], batch.shader,
			&ebiten.DrawTrianglesShaderOptions{Images: images, Blend: spriteBlend(batch.blend)})
	}
	p.discardDraws()
}

func (p *penGPU) discardDraws() {
	clear(p.draws)
	p.draws = p.draws[:0]
	p.drawVertices = p.drawVertices[:0]
	p.drawIndices = p.drawIndices[:0]
}

func (p *penGPU) remember(cmd vm.PenCommand) {
	if !p.keepHistory && !p.surfaceHistory {
		p.cpuStale = true
		return
	}
	// Pending commands must not retain sprite variables or caller-owned points.
	cmd.Target.State.Variables = nil
	cmd.Target.State.Lists = nil
	cmd.Points = append([]float64(nil), cmd.Points...)
	p.pending = append(p.pending, cmd)
	if len(p.pending) >= pendingPenLimit {
		p.checkpointHistory()
	}
}

func (p *penGPU) flushLines() {
	if len(p.draws) > 0 {
		p.queueLines()
		p.submitDraws()
	}
	if len(p.lineVertices) == 0 {
		return
	}
	if p.gpu != nil && p.gpu.measureUploads {
		start := time.Now()
		defer func() { p.gpu.penSubmitTime += time.Since(start) }()
	}
	p.flushClear([4]*ebiten.Image{})
	clippedImage(p.image, p.lineClip).DrawTrianglesShader(p.lineVertices, p.lineIndices, p.shader,
		&ebiten.DrawTrianglesShaderOptions{Blend: spriteBlend(p.lineBlend)})
	p.lineVertices = p.lineVertices[:0]
	p.lineIndices = p.lineIndices[:0]
}

func (p *penGPU) line(cmd vm.PenCommand) bool {
	// Match the reference renderer without a GPU readback for zero-width lines.
	if cmd.Size == 0 {
		cmd.Size = 1
	}
	/*
		Very large coordinates lose subpixel precision in a float32 shader.
		Keep the CPU path for those uncommon commands.
	*/
	if cmd.Size <= 0 || cmd.Size > 65536 || math.IsNaN(cmd.Size) {
		return false
	}
	for _, n := range cmd.Line {
		if math.Abs(n) > 65536 || math.IsNaN(n) {
			return false
		}
	}
	x0, y0, x1, y1 := cmd.Line[0], cmd.Line[1], cmd.Line[2], cmd.Line[3]
	if cmd.Size == 1 || cmd.Size == 3 {
		x0, y0, x1, y1 = x0+.5, y0+.5, x1+.5, y1+.5
	}
	view := p.DisplayView()
	x0, x1 = x0+view.Width/2, x1+view.Width/2
	y0, y1 = view.Height/2-y0, view.Height/2-y1
	x0, x1, y0, y1 = x0*p.scale, x1*p.scale, y0*p.scale, y1*p.scale
	size := p.layerSize()
	radius := cmd.Size * p.scale / 2
	bounds := image.Rect(
		int(math.Floor(math.Min(x0, x1)-radius-1)),
		int(math.Floor(math.Min(y0, y1)-radius-1)),
		int(math.Ceil(math.Max(x0, x1)+radius+1)),
		int(math.Ceil(math.Max(y0, y1)+radius+1)),
	).Intersect(image.Rectangle{Max: size})
	if bounds.Empty() || cmd.Color.A == 0 {
		return true
	}
	clip := render.PenClipRectangle(cmd.Target.State.Clipbox, view, size.X, size.Y)
	if clip.Empty() {
		p.remember(cmd)
		return true
	}
	blend := cmd.Target.State.BlendMode
	if blend == "default" {
		blend = ""
	}
	if len(p.lineVertices) > 0 && (p.lineClip != clip || p.lineBlend != blend) {
		p.queueLines()
	}
	// Mixed lines and stamps share a batch; pure lines use their own shader.
	mixed := clip == (image.Rectangle{Max: size}) && blend == "" && len(p.stampVertices) > 0 && len(p.stampVertices) < penStampBatchSize*4
	if !mixed {
		p.queueStamps()
	}
	if p.dirty {
		p.upload()
	}
	if p.image == nil {
		p.image = ebiten.NewImage(size.X, size.Y)
	}
	if !mixed && len(p.lineVertices) == penLineBatchSize*4 {
		p.queueLines()
	}
	if !mixed && len(p.lineVertices) == 0 {
		p.lineClip, p.lineBlend = clip, blend
	}
	if !mixed && p.lineVertices == nil {
		p.lineVertices = make([]ebiten.Vertex, 0, penLineBatchSize*4)
		p.lineIndices = make([]uint16, 0, penLineBatchSize*6)
	}
	length := math.Hypot(x1-x0, y1-y0)
	cos, sin := 1.0, 0.0
	if length > 0 {
		cos, sin = (x1-x0)/length, (y1-y0)/length
	}
	/*
		A narrow rotated quad avoids shading an entire bounding box for
		diagonal lines. Per-vertex parameters keep different lines batchable.
	*/
	margin := radius + 1
	base := uint16(len(p.lineVertices))
	if mixed {
		base = uint16(len(p.stampVertices))
	}
	for _, point := range [4][2]float64{{-margin, -margin}, {length + margin, -margin}, {length + margin, margin}, {-margin, margin}} {
		x, y := point[0], point[1]
		vertex := ebiten.Vertex{
			DstX: float32(x0 + cos*x - sin*y), DstY: float32(y0 + sin*x + cos*y),
			SrcX: float32(x0), SrcY: float32(y0),
			ColorR: float32(cmd.Color.R) / 255, ColorG: float32(cmd.Color.G) / 255, ColorB: float32(cmd.Color.B) / 255, ColorA: float32(cmd.Color.A),
			Custom0: float32(x1 - x0), Custom1: float32(y1 - y0), Custom2: float32(radius), Custom3: float32(boolInt(view.PenNoAntialias || view.NoAntialias)),
		}
		if mixed {
			// Pack color pairs to free channels for the atlas-independent start point.
			vertex.SrcX, vertex.SrcY = 0, 0
			vertex.ColorR = float32(uint16(cmd.Color.R)<<8 | uint16(cmd.Color.G))
			vertex.ColorA = float32(uint16(cmd.Color.B)<<8 | uint16(cmd.Color.A))
			vertex.ColorG, vertex.ColorB = float32(x0), float32(y0)
			vertex.Custom3 = -1 - float32(boolInt(view.PenNoAntialias || view.NoAntialias))
			p.stampVertices = append(p.stampVertices, vertex)
		} else {
			// The line shader measures from the destination subimage origin.
			vertex.SrcX -= float32(clip.Min.X)
			vertex.SrcY -= float32(clip.Min.Y)
			p.lineVertices = append(p.lineVertices, vertex)
		}
	}
	if mixed {
		p.stampIndices = append(p.stampIndices, base, base+1, base+2, base, base+2, base+3)
	} else {
		p.lineIndices = append(p.lineIndices, base, base+1, base+2, base, base+2, base+3)
	}
	cmd.Target = vm.DisplayTarget{State: vm.TargetState{Clipbox: cmd.Target.State.Clipbox, BlendMode: cmd.Target.State.BlendMode}}
	p.remember(cmd)
	return true
}
