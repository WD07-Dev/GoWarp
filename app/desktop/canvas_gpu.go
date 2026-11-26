package desktop

import (
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"math"

	"gowarp/core/extension"
	"gowarp/core/render"
	"gowarp/core/vm"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

//go:embed canvas_gpu.kage
var canvasShaderSource []byte

//go:embed canvas_blur.kage
var canvasBlurSource []byte

//go:embed canvas_copy.kage
var canvasCopySource []byte

//go:embed split_gpu.kage
var splitShaderSource []byte

const (
	maxCachedStageShaders      = 32
	maxCachedShaderSourceBytes = 4 << 20
)

type compiledStageShader struct {
	shader   *ebiten.Shader
	program  extension.ShaderProgram
	bind     extension.ShaderUniformBinding
	lastUsed uint64
}
type canvasGPU struct {
	frame, work, blur                      *ebiten.Image
	frameBacking, workBacking, blurBacking *ebiten.Image
	filter                                 *ebiten.Shader
	blurFilter                             *ebiten.Shader
	copy                                   *ebiten.Shader
	split                                  *ebiten.Shader
	programs                               map[[2]string]compiledStageShader
	programUse                             uint64
	programSourceBytes                     int
	uniforms                               map[string]any
	blurAmount                             float32
}

func (p *canvasGPU) compileProgram(source extension.ShaderSource) (compiledStageShader, error) {
	key := source.Key()
	p.programUse++
	if compiled, ok := p.programs[key]; ok {
		compiled.lastUsed = p.programUse
		p.programs[key] = compiled
		return compiled, nil
	}
	program, err := source.Compile()
	if err != nil {
		return compiledStageShader{}, err
	}
	shader, err := ebiten.NewShader([]byte(program.Code))
	if err != nil {
		return compiledStageShader{}, fmt.Errorf("stage shader: %w", err)
	}
	compiled := compiledStageShader{shader: shader, program: program, lastUsed: p.programUse}
	if program.NewUniforms != nil {
		compiled.bind = program.NewUniforms()
	}
	if p.programs == nil {
		p.programs = make(map[[2]string]compiledStageShader)
	}
	p.programs[key] = compiled
	p.programSourceBytes += len(key[1]) + len(program.Code)
	return compiled, nil
}

// Trim on the graphics thread after drawing; never retire a program in the current pipeline.
func (p *canvasGPU) trimPrograms(active *vm.StageShader) {
	if len(p.programs) <= maxCachedStageShaders && p.programSourceBytes <= maxCachedShaderSourceBytes {
		return
	}
	protected := make(map[[2]string]bool)
	for pass := active; pass != nil; pass = pass.Next {
		protected[pass.Source().Key()] = true
	}
	for len(p.programs) > maxCachedStageShaders || p.programSourceBytes > maxCachedShaderSourceBytes {
		var oldest [2]string
		var lastUsed uint64
		found := false
		for key, program := range p.programs {
			if protected[key] {
				continue
			}
			if !found || program.lastUsed < lastUsed {
				oldest = key
				lastUsed = program.lastUsed
				found = true
			}
		}
		if !found {
			break
		}
		program := p.programs[oldest]
		program.shader.Deallocate()
		p.programSourceBytes -= len(oldest[1]) + len(program.program.Code)
		delete(p.programs, oldest)
	}
}

func (compiled compiledStageShader) values(values map[string]any, w, h int, shaderTime, shaderElapsed float64, nearest bool) map[string]any {
	if compiled.bind != nil {
		return compiled.bind(values, extension.ShaderContext{Width: w, Height: h, Time: shaderTime, Elapsed: shaderElapsed, Nearest: nearest})
	}
	return values
}

func (p *canvasGPU) uniformValues(w, h int, c vm.CanvasEffects) map[string]any {
	if p.uniforms == nil {
		p.uniforms = map[string]any{
			"SourceSize":  make([]float32, 2),
			"Filters":     make([]float32, 4),
			"Filters2":    make([]float32, 4),
			"Blur":        make([]float32, 2),
			"BlurWeights": make([]float32, 16),
			"BlurRadius":  0,
		}
	}
	u := p.uniforms
	if blur := float32(c.Blur); blur != p.blurAmount {
		p.blurAmount = blur
		u["BlurRadius"] = canvasBlurKernel(blur, u["BlurWeights"].([]float32))
	}
	size := u["SourceSize"].([]float32)
	size[0], size[1] = float32(w), float32(h)
	filters := u["Filters"].([]float32)
	filters[0] = float32(math.Max(0, c.Contrast) / 100)
	filters[1] = float32(math.Max(0, c.Saturation) / 100)
	filters[2] = float32(c.Hue * math.Pi / 180)
	filters[3] = float32(math.Max(0, c.Brightness) / 100)
	extra := u["Filters2"].([]float32)
	extra[0] = float32(math.Max(0, math.Min(1, c.Invert/100)))
	extra[1] = float32(math.Max(0, math.Min(1, c.Sepia/100)))
	extra[2] = float32(math.Max(0, math.Min(1, 1-c.Transparency/100)))
	extra[3] = float32(math.Max(0, c.Radius) / 100)
	clear(u["Blur"].([]float32))
	u["Nearest"] = boolInt(c.Nearest)
	return u
}

// Combine overlapping bilinear taps without changing the Gaussian filter.
func canvasBlurKernel(blur float32, weights []float32) int {
	clear(weights)
	if !(blur >= 1.0/6 && blur <= 5) {
		return 0
	}
	weights[0] = 1
	total := float32(1)
	for i := 1; i <= 12; i++ {
		x := float32(i) * .25
		w := float32(math.Exp(-float64(x*x) / 2))
		offset := blur * x
		n := int(offset)
		fraction := offset - float32(n)
		weight := w * (1 - fraction)
		if n == 0 {
			weight *= 2
		}
		weights[n] += weight
		if fraction > 0 {
			weights[n+1] += w * fraction
		}
		total += 2 * w
	}
	for i := range weights {
		weights[i] /= total
	}
	return int(math.Ceil(float64(blur) * 3))
}

// Use the real driver path before playback; NewShader alone defers native compilation.
func (p *canvasGPU) warmupPrograms(sources []extension.ShaderSource) int {
	defer p.trimPrograms(nil)
	if len(sources) == 0 {
		return 0
	}
	src := ebiten.NewImage(1, 1)
	dst := ebiten.NewImage(1, 1)
	defer src.Deallocate()
	defer dst.Deallocate()
	src.Fill(color.White)
	var pixel [4]byte
	count, attempts, bytes := 0, 0, 0
	for _, source := range sources {
		if _, ok := p.programs[source.Key()]; ok {
			continue
		}
		if attempts >= 16 {
			break
		}
		if len(source.Code) > (256<<10)-bytes {
			continue
		}
		attempts++
		bytes += len(source.Code)
		compiled, err := p.compileProgram(source)
		if err != nil {
			continue
		}
		dst.DrawRectShader(1, 1, compiled.shader, &ebiten.DrawRectShaderOptions{
			Images: [4]*ebiten.Image{src}, Blend: ebiten.BlendCopy,
			Uniforms: compiled.values(nil, 1, 1, 0, 0, false),
		})
		dst.ReadPixels(pixel[:])
		count++
	}
	return count
}

func (p *canvasGPU) close() {
	for _, im := range []*ebiten.Image{p.frameBacking, p.workBacking, p.blurBacking} {
		if im != nil {
			im.Deallocate()
		}
	}
	if p.filter != nil {
		p.filter.Deallocate()
	}
	if p.blurFilter != nil {
		p.blurFilter.Deallocate()
	}
	if p.copy != nil {
		p.copy.Deallocate()
	}
	if p.split != nil {
		p.split.Deallocate()
	}
	for _, s := range p.programs {
		s.shader.Deallocate()
	}
	*p = canvasGPU{}
}

func canvasImage(im, backing **ebiten.Image, w, h int) {
	bounds := image.Rect(0, 0, w, h)
	if *im != nil && (*im).Bounds() == bounds {
		return
	}
	const reuseLimit = 2048
	if *backing == nil || !bounds.In((*backing).Bounds()) ||
		(*backing).Bounds().Dx() > reuseLimit || (*backing).Bounds().Dy() > reuseLimit {
		if *backing != nil {
			(*backing).Deallocate()
		}
		*backing = ebiten.NewImage(w, h)
	}
	*im = *backing
	if bounds != (*backing).Bounds() {
		*im = (*backing).SubImage(bounds).(*ebiten.Image)
	}
}

func canvasFiltered(c vm.CanvasEffects) bool {
	c = c.Defaults()
	return c.Blur != 0 || c.Contrast != 100 || c.Saturation != 100 || c.Hue != 0 || c.Brightness != 100 || c.Invert != 0 || c.Sepia != 0 || c.Transparency != 0 || c.ScaleX != 100 || c.ScaleY != 100 || c.SkewX != 0 || c.SkewY != 0 || c.OffsetX != 0 || c.OffsetY != 0 || c.Rotation != 0 || c.Radius != 0
}

func (g *stageGPU) Draw(dst *ebiten.Image, r *render.Renderer, scene []vm.DisplayTarget) error {
	v := r.DisplayView()
	p := &g.canvas
	defer p.trimPrograms(v.Shader)
	c := v.Canvas.Defaults()
	if v.NoAntialias {
		c.Nearest = true
	}
	filtered := canvasFiltered(c)
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	if c.RenderWidth > 0 && c.RenderHeight > 0 && !math.IsInf(c.RenderWidth+c.RenderHeight, 0) {
		w, h = render.DisplaySize(int(c.RenderWidth), int(c.RenderHeight))
	}
	if pen := g.nativePen; pen != nil && pen.Renderer == r {
		pen.setOutputSize(w, h)
	}
	direct := c.RenderWidth <= 0 && c.RenderHeight <= 0
	if !direct && w == dst.Bounds().Dx() && h == dst.Bounds().Dy() {
		direct = v.Color != "transparent" && vm.ParseColor(v.Color).A == 255
	}
	if v.Shader == nil && !filtered && direct && v.Split == nil {
		if err := g.drawScene(dst, r, scene); err != nil {
			return err
		}
		drawCanvasBorder(dst, c)
		return nil
	}
	canvasImage(&p.frame, &p.frameBacking, w, h)
	if err := g.drawScene(p.frame, r, scene); err != nil {
		return err
	}
	src := p.frame
	for pass := v.Shader; pass != nil; pass = pass.Next {
		for name, value := range pass.Overrides {
			pass.Uniforms[name] = value
		}
		compiled, err := p.compileProgram(pass.Source())
		if err != nil {
			return err
		}
		u := compiled.values(pass.Uniforms, w, h, v.ShaderTime, v.ShaderElapsed, c.Nearest)
		canvasImage(&p.work, &p.workBacking, w, h)
		p.work.Clear()
		p.work.DrawRectShader(w, h, compiled.shader, &ebiten.DrawRectShaderOptions{Images: [4]*ebiten.Image{src}, Uniforms: u})
		src = p.work
		p.work, p.frame = p.frame, p.work
		p.workBacking, p.frameBacking = p.frameBacking, p.workBacking
	}
	copyOnly := c.Nearest && !filtered && v.Shader == nil && v.Split == nil
	if copyOnly && p.copy == nil {
		var err error
		p.copy, err = ebiten.NewShader(canvasCopySource)
		if err != nil {
			return err
		}
	}
	if !copyOnly && p.filter == nil {
		var err error
		p.filter, err = ebiten.NewShader(canvasShaderSource)
		if err != nil {
			return err
		}
	}
	uniforms := p.uniformValues(w, h, c)
	if c.Blur >= 0x1p-126 {
		filter := p.filter
		if uniforms["BlurRadius"].(int) > 0 {
			if p.blurFilter == nil {
				var err error
				p.blurFilter, err = ebiten.NewShader(canvasBlurSource)
				if err != nil {
					return err
				}
			}
			filter = p.blurFilter
		}
		canvasImage(&p.blur, &p.blurBacking, w, h)
		other := p.frame
		if src == other {
			canvasImage(&p.work, &p.workBacking, w, h)
			other = p.work
		}
		uniforms["Nearest"] = 0
		for i, pass := range []*ebiten.Image{p.blur, other} {
			b := uniforms["Blur"].([]float32)
			clear(b)
			b[i] = float32(c.Blur)
			pass.Clear()
			pass.DrawRectShader(w, h, filter, &ebiten.DrawRectShaderOptions{Images: [4]*ebiten.Image{src}, Uniforms: uniforms})
			src = pass
		}
		clear(uniforms["Blur"].([]float32))
		uniforms["Nearest"] = boolInt(c.Nearest)
	}
	dst.Clear()
	if c.Background != "transparent" {
		dst.Fill(vm.ParseColor(c.Background))
	}
	options := ebiten.DrawTrianglesShaderOptions{Images: [4]*ebiten.Image{src}, Uniforms: uniforms}
	dw, dh := float64(dst.Bounds().Dx()), float64(dst.Bounds().Dy())
	skewX, skewY := math.Tan(c.SkewX*math.Pi/180), math.Tan(c.SkewY*math.Pi/180)
	si, co := math.Sincos(c.Rotation * math.Pi / 180)
	var vertices [4]ebiten.Vertex
	for i, uv := range [4][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		x, y := (uv[0]-.5)*dw+c.OffsetX*dw/100, (uv[1]-.5)*dh-c.OffsetY*dh/100
		x, y = x+skewX*y, y+skewY*x
		x, y = x*c.ScaleX/100, y*c.ScaleY/100
		x, y = x*co-y*si, x*si+y*co
		vertices[i] = ebiten.Vertex{DstX: float32(x + dw/2), DstY: float32(y + dh/2), SrcX: float32(uv[0] * float64(w)), SrcY: float32(uv[1] * float64(h))}
	}
	shader := p.filter
	if copyOnly {
		shader = p.copy
	}
	dst.DrawTrianglesShader(vertices[:], []uint16{0, 1, 2, 0, 2, 3}, shader, &options)
	if c.BorderWidth > 0 && c.BorderStyle != "none" {
		drawCanvasBorder(dst, c)
	}
	if v.Split != nil && len(v.Split.Effects) > 0 {
		if p.split == nil {
			var err error
			p.split, err = ebiten.NewShader(splitShaderSource)
			if err != nil {
				return err
			}
		}
		if !v.Split.ShowOriginal {
			dst.Clear()
		}
		for i, uv := range [4][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
			vertices[i] = ebiten.Vertex{DstX: float32(uv[0] * dw), DstY: float32(uv[1] * dh), SrcX: float32(uv[0] * float64(w)), SrcY: float32(uv[1] * float64(h))}
		}
		for _, effect := range v.Split.Effects {
			geometry := effect.Project(dw, dh, ebiten.Monitor().DeviceScaleFactor())
			dst.DrawTrianglesShader(vertices[:], []uint16{0, 1, 2, 0, 2, 3}, p.split, &ebiten.DrawTrianglesShaderOptions{Images: [4]*ebiten.Image{src}, Uniforms: map[string]any{"OutputSize": []float32{float32(dw), float32(dh)}, "Seam": []float32{float32(geometry.TopX), float32(geometry.BottomX)}, "Move": []float32{float32(geometry.MoveX), float32(geometry.MoveY)}}})
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func drawCanvasBorder(dst *ebiten.Image, c vm.CanvasEffects) {
	if c.BorderWidth <= 0 || math.IsNaN(c.BorderWidth) || math.IsInf(c.BorderWidth, 0) || c.BorderStyle == "none" {
		return
	}
	w, h := float32(dst.Bounds().Dx()), float32(dst.Bounds().Dy())
	size := float32(c.BorderWidth)
	col := color.Color(vm.ParseColor(c.BorderColor))
	if c.BorderStyle == "dotted" || c.BorderStyle == "dashed" {
		if c.Background != "transparent" {
			background := vm.ParseColor(c.Background)
			vector.StrokeRect(dst, size/2, size/2, w-size, h-size, size, background, false)
			if background.A == 255 && background == col {
				return
			}
		}
		step := size * 2
		if c.BorderStyle == "dashed" {
			step = size * 4
		}
		for x := size / 2; x < w; x += step {
			for _, y := range []float32{size / 2, h - size/2} {
				if c.BorderStyle == "dotted" {
					vector.FillCircle(dst, x, y, size/2, col, false)
				} else {
					vector.FillRect(dst, x-size/2, y-size/2, size*3, size, col, false)
				}
			}
		}
		for y := size / 2; y < h; y += step {
			for _, x := range []float32{size / 2, w - size/2} {
				if c.BorderStyle == "dotted" {
					vector.FillCircle(dst, x, y, size/2, col, false)
				} else {
					vector.FillRect(dst, x-size/2, y-size/2, size, size*3, col, false)
				}
			}
		}
		return
	}
	vector.StrokeRect(dst, size/2, size/2, w-size, h-size, size, col, false)
}
