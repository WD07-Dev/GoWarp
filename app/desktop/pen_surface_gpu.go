package desktop

import (
	_ "embed"
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/core/render"
	"gowarp/core/vm"
)

//go:embed pen_surface_gpu.kage
var penSurfaceShaderSource []byte

func (p *penGPU) closeSurfaces() {
	for _, surface := range p.surfaces {
		surface.Deallocate()
	}
	p.surfaces = nil
	p.surfaceCPU = nil
	if p.surfaceShader != nil {
		p.surfaceShader.Deallocate()
		p.surfaceShader = nil
	}
}

/*
Surface history includes draws whose results can later become texture sources.
A stage clear must replay these commands before discarding stage-only history.
*/
func (p *penGPU) flushSurfaceHistory() {
	p.restoreCheckpoint()
	for _, command := range p.pending {
		p.Renderer.Pen(command)
	}
	clear(p.pending)
	p.pending = p.pending[:0]
}

/*
Trace named textures backwards across clears and recreations. Checkpoints
also keep triangle depth; a stage clear can discard it.
*/
func (p *penGPU) preserveSurfaceHistory(keepDepth bool) {
	if cap(p.historyDepthOnly) < len(p.pending) {
		p.historyDepthOnly = make([]bool, len(p.pending))
	} else {
		p.historyDepthOnly = p.historyDepthOnly[:len(p.pending)]
	}
	needed := make(map[string]bool)
	for i := range p.pending {
		if s := p.pending[i].Surface; s != nil && s.Name != vm.StageSurface {
			needed[s.Name] = true
		}
	}
	start := len(p.pending)
	for i := len(p.pending) - 1; i >= 0; i-- {
		command := &p.pending[i]
		keep := true
		depthOnly := false
		s := command.Surface
		switch command.Kind {
		case "surface-reset":
			for name := range needed {
				if name != vm.StageSurface {
					needed[name] = false
				}
			}
		case "surface-create", "surface-clear", "surface-delete":
			if s != nil {
				needed[s.Name] = false
			}
		case "surface-triangle":
			keep = s != nil && (needed[s.Name] || (keepDepth && s.Name == vm.StageSurface))
			depthOnly = keep && s.Name == vm.StageSurface && !needed[s.Name]
			if keep && s.Textured && s.Costume == nil && s.Texture != s.Name {
				needed[s.Texture] = true
			}
		default:
			keep = needed[vm.StageSurface]
		}
		if keep {
			start--
			p.historyDepthOnly[start] = depthOnly
			if start != i {
				p.pending[start] = *command
			}
		}
	}
	if needed[vm.StageSurface] {
		p.restoreCheckpoint()
	}
	for i := start; i < len(p.pending); i++ {
		command := p.pending[i]
		if p.historyDepthOnly[i] {
			p.Renderer.PreservePenDepth(command.Surface)
		} else {
			p.Renderer.Pen(command)
		}
	}
	clear(p.pending)
	p.pending = p.pending[:0]
}

func (p *penGPU) penSurface(cmd vm.PenCommand) bool {
	switch cmd.Kind {
	case "surface-create", "surface-clear", "surface-delete", "surface-reset", "surface-triangle":
	default:
		return false
	}
	if cmd.Surface == nil && cmd.Kind != "surface-reset" {
		return true
	}
	if cmd.Kind == "surface-clear" && cmd.Surface.Name == vm.StageSurface {
		p.Pen(vm.PenCommand{Kind: "clear"})
		return true
	}
	if !p.surfaceHistory && cmd.Kind == "surface-triangle" {
		if !p.keepHistory {
			p.flush()
		}
		p.surfaceHistory = true
	}
	if p.surfaces == nil {
		p.surfaces = make(map[string]*ebiten.Image)
		p.surfaceCPU = make(map[string]bool)
	}
	s := cmd.Surface
	switch cmd.Kind {
	case "surface-reset":
		for name, surface := range p.surfaces {
			surface.Deallocate()
			delete(p.surfaces, name)
		}
		stageCPU := p.surfaceCPU[vm.StageSurface]
		clear(p.surfaceCPU)
		p.surfaceCPU[vm.StageSurface] = stageCPU
	case "surface-create":
		if s.Name == vm.StageSurface || s.Width <= 0 || s.Height <= 0 || s.Width > 4096 || s.Height > 4096 {
			return true
		}
		if old := p.surfaces[s.Name]; old != nil {
			old.Deallocate()
		}
		p.surfaces[s.Name] = ebiten.NewImage(s.Width, s.Height)
		delete(p.surfaceCPU, s.Name)
	case "surface-delete":
		if s.Name == vm.StageSurface {
			return true
		}
		if old := p.surfaces[s.Name]; old != nil {
			old.Deallocate()
			delete(p.surfaces, s.Name)
		}
		delete(p.surfaceCPU, s.Name)
	case "surface-clear":
		if surface := p.surfaces[s.Name]; surface != nil {
			surface.Clear()
		}
		delete(p.surfaceCPU, s.Name)
	case "surface-triangle":
		p.flushStamps()
		p.flushLines()
		p.flushClear([4]*ebiten.Image{})
		if p.dirty {
			p.upload()
		}
		p.surfaceTriangle(cmd)
		return true
	}
	if p.surfaceHistory {
		p.remember(cmd)
	} else {
		/*
			Lifecycle operations do not consume stage pixels. Loading/resetting
			Surface resources must leave ordinary pen projects on their existing fast path.
		*/
		p.Renderer.Pen(cmd)
	}
	return true
}

func (p *penGPU) surfaceTriangle(cmd vm.PenCommand) {
	s := cmd.Surface
	if p.surfaceParent != nil && s.Textured && s.Costume == nil && s.Texture != vm.StageSurface {
		// Root textures can change before this layer's next CPU readback.
		// Resolve both histories now, while the sampled texture is current.
		p.surfaceParent.flush()
		defer p.flush()
	}
	for _, v := range s.Vertices {
		for _, n := range [...]float64{v.X, v.Y, v.U, v.V, v.R, v.G, v.B, v.A, v.Z, v.W} {
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return
			}
		}
		if v.W <= 0 {
			return
		}
	}
	dst := p.surfaces[s.Name]
	scale := 1.0
	if s.Name == vm.StageSurface {
		if p.image == nil {
			size := p.layerSize()
			p.image = ebiten.NewImage(size.X, size.Y)
		}
		dst, scale = p.image, p.scale
	}
	if dst == nil {
		return
	}
	for _, v := range s.Vertices {
		if v.Z != -1 || v.W != 1 {
			p.surfaceCPU[s.Name] = true
		}
	}
	if p.surfaceCPU[s.Name] {
		p.flushSurfaceHistory()
		p.Renderer.Pen(cmd)
		if s.Name == vm.StageSurface {
			p.upload()
		} else if pixels := p.Renderer.PenSurface(s.Name); pixels != nil {
			stageUploadPixels(dst, pixels, true)
		}
		return
	}
	options := &ebiten.DrawTrianglesShaderOptions{Uniforms: map[string]any{
		"Textured": float32(boolInt(s.Textured)), "Linear": float32(boolInt(s.Linear && !p.DisplayView().NoAntialias)),
		"Premultiplied": float32(0), "Repeated": float32(0),
		"MultiplyTextureAlpha": float32(boolInt(s.MultiplyTextureAlpha)),
	}}
	if s.Textured {
		var texture *ebiten.Image
		temporary := false
		if s.Costume == nil && s.Texture != s.Name {
			texture = p.surfaces[s.Texture]
			if texture == nil && p.surfaceParent != nil && s.Texture != vm.StageSurface {
				texture = p.surfaceParent.surfaces[s.Texture]
			}
			if s.Texture == vm.StageSurface {
				texture = p.image
			}
		}
		if texture != nil {
			options.Uniforms["Premultiplied"] = float32(1)
		} else if s.Costume != nil && p.Assets != nil {
			target := s.Costume
			if target.Source >= 0 && target.Source < len(p.Assets.Costumes) {
				costumes := p.Assets.Costumes[target.Source]
				index := target.State.CurrentCostume
				if index >= 0 && index < len(costumes) {
					texture, temporary = p.gpu.textureRegion(p.Renderer, render.DisplaySprite{Costume: costumes[index], Density: 1}, image.Rectangle{})
					if p.gpu.repeatedPixels[texture] {
						options.Uniforms["Repeated"] = float32(1)
					}
				}
			}
		}
		if texture == nil {
			return
		}
		if temporary {
			defer func() {
				delete(p.gpu.repeatedPixels, texture)
				delete(p.gpu.textureContent, texture)
				texture.Deallocate()
			}()
		}
		options.Images[0] = texture
	}
	var vertices [3]ebiten.Vertex
	for i, v := range s.Vertices {
		vertices[i] = ebiten.Vertex{
			DstX:   float32(float64(dst.Bounds().Dx())/2 + v.X*scale),
			DstY:   float32(float64(dst.Bounds().Dy())/2 - v.Y*scale),
			ColorR: float32(v.R), ColorG: float32(v.G), ColorB: float32(v.B), ColorA: float32(v.A),
			Custom0: float32(v.U), Custom1: float32(v.V),
		}
	}
	dst.DrawTrianglesShader(vertices[:], []uint16{0, 1, 2}, p.surfaceShader, options)
	p.remember(cmd)
}
