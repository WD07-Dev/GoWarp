package desktop

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"gowarp/core/vm"
)

func (p *penGPU) layerCommand(cmd vm.PenCommand) bool {
	name := cmd.Layer
	if name == vm.StageSurface {
		name = ""
	}
	switch cmd.Kind {
	case "layer-create":
		if p.layers == nil {
			p.layers = make(map[string]*penGPU)
		}
		if p.layers[name] != nil {
			return true
		}
		p.Renderer.Pen(cmd)
		if r := p.Renderer.PenLayer(name); r != nil && r != p.Renderer {
			p.layers[name] = &penGPU{Renderer: r, gpu: p.gpu, keepHistory: p.keepHistory, scale: 1, surfaceParent: p,
				shader: p.shader, stampShader: p.stampShader, surfaceShader: p.surfaceShader}
		}
		return true
	case "layer-delete":
		if child := p.layers[name]; child != nil {
			child.closeLayer()
			delete(p.layers, name)
		}
		p.Renderer.Pen(cmd)
		return true
	case "layer-quality":
		child := p
		if name != "" {
			child = p.layers[name]
		}
		if child != nil {
			child.quality = cmd.Size
			child.setOutputSize(p.outputSize.X, p.outputSize.Y)
		}
		return true
	}
	if name == "" {
		return false
	}
	if child := p.layers[name]; child != nil {
		cmd.Layer = ""
		child.keepHistory = p.keepHistory
		previous := p.gpu.nativePen
		p.gpu.nativePen = child
		defer func() { p.gpu.nativePen = previous }()
		child.Pen(cmd)
		// Shared costume textures may be evicted before another paper is drawn.
		child.flushStamps()
		child.flushLines()
	}
	return true
}

func (p *penGPU) closeLayer() {
	for _, child := range p.layers {
		child.closeLayer()
	}
	for _, img := range p.surfaces {
		img.Deallocate()
	}
	for _, img := range []*ebiten.Image{p.image, p.resizeSpare, p.checkpoint} {
		if img != nil {
			img.Deallocate()
		}
	}
	p.layers = nil
	p.surfaces = nil
	p.image, p.resizeSpare, p.checkpoint = nil, nil, nil
}

func (g *stageGPU) drawLayer(dst *ebiten.Image, target vm.DisplayTarget) {
	root := g.nativePen
	if root == nil {
		return
	}
	root.replayCommands()
	p := root
	if target.State.RenderLayer != vm.StageSurface {
		p = root.layers[target.State.RenderLayer]
	}
	if p == nil {
		return
	}
	p.flushStamps()
	p.flushLines()
	if p.dirty {
		p.upload()
	}
	p.flushClear([4]*ebiten.Image{})
	if p.image == nil {
		return
	}
	v := root.DisplayView()
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	var corners [4][2]float64
	for i, uv := range [4][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		x, y := (uv[0]-.5)*v.Width, (.5-uv[1])*v.Height
		if tr := target.State.Transform; tr != nil {
			sn, cs := math.Sincos(tr.Rotation * math.Pi / 180)
			x, y = x-tr.X, y-tr.Y
			x, y = tr.Scale*(x*cs-y*sn), tr.Scale*(x*sn+y*cs)
		}
		corners[i][0], corners[i][1] = root.DisplayPoint(x, y, w, h)
	}
	nearest := v.NoAntialias || v.Canvas.Nearest || math.Min(float64(w)/v.Width, float64(h)/v.Height) >= 1
	g.quad(dst, p.image, corners, float64(p.image.Bounds().Dx()), float64(p.image.Bounds().Dy()), vm.GraphicEffects{}, nearest, true)
}
