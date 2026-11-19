package render

import (
	"image"
	"image/color"
	"math"

	"gowarp/core/vm"
)

type penSurface struct {
	pixels *image.NRGBA
	// Store NDC depth minus one, so zero-initialized entries are the far plane.
	depth []float64
}

/*
PenSurface returns borrowed, read-only pixels for the named render surface.
The stage shares its pixels with the ordinary pen layer.
*/
func (r *Renderer) PenSurface(name string) *image.NRGBA {
	if name == vm.StageSurface {
		return r.pen
	}
	if surface := r.penSurfaces[name]; surface != nil {
		return surface.pixels
	}
	return nil
}

func (r *Renderer) penSurfaceCommand(cmd vm.PenCommand) bool {
	switch cmd.Kind {
	case "surface-reset":
		r.penSurfaces = nil
		return true
	case "surface-create", "surface-clear", "surface-delete", "surface-triangle":
	default:
		return false
	}
	s := cmd.Surface
	if s == nil {
		return true
	}
	switch cmd.Kind {
	case "surface-create":
		if s.Name == vm.StageSurface || s.Width < 1 || s.Height < 1 || s.Width > 4096 || s.Height > 4096 {
			return true
		}
		if r.penSurfaces == nil {
			r.penSurfaces = make(map[string]*penSurface)
		}
		r.penSurfaces[s.Name] = &penSurface{pixels: image.NewNRGBA(image.Rect(0, 0, s.Width, s.Height))}
	case "surface-clear":
		if s.Name == vm.StageSurface {
			r.Pen(vm.PenCommand{Kind: "clear"})
		} else if surface := r.penSurfaces[s.Name]; surface != nil {
			clear(surface.pixels.Pix)
			clear(surface.depth)
		}
	case "surface-delete":
		delete(r.penSurfaces, s.Name)
	case "surface-triangle":
		r.penSurfaceTriangle(s, false)
	}
	return true
}

func (r *Renderer) penSurfaceTexture(s *vm.PenSurfaceCommand) *image.NRGBA {
	if s.Costume == nil {
		if s.Texture == s.Name {
			return nil
		}
		return r.PenSurface(s.Texture)
	}
	// Sample the raw costume without sprite transforms or effects.
	t := s.Costume
	if r.Assets == nil || t.Source < 0 || t.Source >= len(r.Assets.Costumes) {
		return nil
	}
	costumes := r.Assets.Costumes[t.Source]
	index := t.State.CurrentCostume
	if index < 0 || index >= len(costumes) {
		return nil
	}
	return r.Assets.CostumeImage(costumes[index])
}

func penEdge(a, b vm.PenVertex, x, y float64) float64 {
	return (b.X-a.X)*(y-a.Y) - (b.Y-a.Y)*(x-a.X)
}

func penTopLeft(a, b vm.PenVertex) bool {
	return b.Y < a.Y || (b.Y == a.Y && b.X > a.X)
}

// Leave a pixel around each intersection; the existing edge tests decide coverage.
func penTriangleRowBounds(v [3]vm.PenVertex, y float64, minX, maxX int) (int, int) {
	left, right := float64(maxX), float64(minX)
	for i, a := range v {
		b := v[(i+1)%3]
		if a.Y == b.Y || y < math.Min(a.Y, b.Y) || y > math.Max(a.Y, b.Y) {
			continue
		}
		x := a.X + (y-a.Y)/(b.Y-a.Y)*(b.X-a.X)
		left, right = math.Min(left, x), math.Max(right, x)
	}
	if left > right {
		return minX, minX
	}
	lo := max(minX, int(math.Floor(left-.5))-1)
	hi := min(maxX, int(math.Ceil(right-.5))+2)
	return lo, max(lo, hi)
}

/*
PreservePenDepth replays triangle coverage without repainting colors already
stored in a GPU checkpoint. Texture alpha still determines depth coverage.
*/
func (r *Renderer) PreservePenDepth(s *vm.PenSurfaceCommand) {
	r.penSurfaceTriangle(s, true)
}

func (r *Renderer) penSurfaceTriangle(s *vm.PenSurfaceCommand, depthOnly bool) {
	v := s.Vertices
	opaque := false
	for _, p := range v {
		for _, n := range [...]float64{p.X, p.Y, p.U, p.V, p.R, p.G, p.B, p.A, p.Z, p.W} {
			if !finite(n) {
				return
			}
		}
		if p.W <= 0 {
			return
		}
		opaque = opaque || p.A > 0
	}
	if !opaque {
		return
	}
	width, height := r.view.Width, r.view.Height
	dst, depth := r.pen, r.penDepth
	var surface *penSurface
	stage := s.Name == vm.StageSurface
	if !stage {
		surface = r.penSurfaces[s.Name]
		if surface == nil {
			return
		}
		dst, depth = surface.pixels, surface.depth
		width, height = float64(dst.Rect.Dx()), float64(dst.Rect.Dy())
	}
	for i := range v {
		v[i].X += width / 2
		v[i].Y = height/2 - v[i].Y
	}
	area := penEdge(v[0], v[1], v[2].X, v[2].Y)
	if area == 0 || !finite(area) {
		return
	}
	if area < 0 {
		v[1], v[2] = v[2], v[1]
		area = -area
	}
	minX := int(math.Max(0, math.Min(math.Ceil(width), math.Floor(math.Min(v[0].X, math.Min(v[1].X, v[2].X))))))
	maxX := int(math.Max(0, math.Min(math.Ceil(width), math.Ceil(math.Max(v[0].X, math.Max(v[1].X, v[2].X))))))
	minY := int(math.Max(0, math.Min(math.Ceil(height), math.Floor(math.Min(v[0].Y, math.Min(v[1].Y, v[2].Y))))))
	maxY := int(math.Max(0, math.Min(math.Ceil(height), math.Ceil(math.Max(v[0].Y, math.Max(v[1].Y, v[2].Y))))))
	if minX >= maxX || minY >= maxY {
		return
	}
	var texture *image.NRGBA
	linear := s.Linear && !r.view.NoAntialias
	if s.Textured {
		texture = r.penSurfaceTexture(s)
		if texture == nil || texture.Rect.Empty() {
			return
		}
	}
	invW := [3]float64{1 / v[0].W, 1 / v[1].W, 1 / v[2].W}
	z := [3]float64{v[0].Z * invW[0], v[1].Z * invW[1], v[2].Z * invW[2]}
	topLeft := [3]bool{penTopLeft(v[1], v[2]), penTopLeft(v[2], v[0]), penTopLeft(v[0], v[1])}
	stride := int(math.Ceil(width))
	scanRows := maxX-minX > 32
	for _, vertex := range v {
		// Keep the original path when extreme coordinates make intersections imprecise.
		scanRows = scanRows && math.Abs(vertex.X) <= 65536 && math.Abs(vertex.Y) <= 65536
	}
	changed := false
	for y := minY; y < maxY; y++ {
		left, right := minX, maxX
		if scanRows {
			left, right = penTriangleRowBounds(v, float64(y)+.5, minX, maxX)
		}
		for x := left; x < right; x++ {
			px, py := float64(x)+.5, float64(y)+.5
			e := [3]float64{penEdge(v[1], v[2], px, py), penEdge(v[2], v[0], px, py), penEdge(v[0], v[1], px, py)}
			if e[0] < 0 || (e[0] == 0 && !topLeft[0]) || e[1] < 0 || (e[1] == 0 && !topLeft[1]) || e[2] < 0 || (e[2] == 0 && !topLeft[2]) {
				continue
			}
			b0, b1, b2 := e[0]/area, e[1]/area, e[2]/area
			// Preserve constant depth exactly, particularly the near plane.
			depthValue := z[2] + b0*(z[0]-z[2]) + b1*(z[1]-z[2])
			if !finite(depthValue) || depthValue < -1 || depthValue > 1 {
				continue
			}
			index := y*stride + x
			if len(depth) > 0 && depthValue-1 > depth[index] {
				continue
			}
			b0 *= invW[0]
			b1 *= invW[1]
			b2 *= invW[2]
			sum := b0 + b1 + b2
			if !finite(sum) || sum <= 0 {
				continue
			}
			b0, b1, b2 = b0/sum, b1/sum, b2/sum
			ca := b0*v[0].A + b1*v[1].A + b2*v[2].A
			tr, tg, tb := 1.0, 1.0, 1.0
			if texture != nil {
				u := b0*v[0].U + b1*v[1].U + b2*v[2].U
				uv := b0*v[0].V + b1*v[1].V + b2*v[2].V
				var ta float64
				if depthOnly {
					ta = penTextureAlpha(texture, u, uv, linear)
				} else {
					tr, tg, tb, ta = penTextureSample(texture, u, uv, linear)
				}
				if !depthOnly && !s.MultiplyTextureAlpha && ta > 0 {
					tr, tg, tb = tr/ta, tg/ta, tb/ta
				}
				ca *= ta
			}
			if !(ca > 0) {
				continue
			}
			if depth == nil {
				depth = make([]float64, stride*int(math.Ceil(height)))
				if stage {
					r.penDepth = depth
				} else {
					surface.depth = depth
				}
			}
			depth[index] = depthValue - 1
			changed = true
			if depthOnly {
				continue
			}
			cr := (b0*v[0].R + b1*v[1].R + b2*v[2].R) * tr
			cg := (b0*v[0].G + b1*v[1].G + b2*v[2].G) * tg
			cb := (b0*v[0].B + b1*v[1].B + b2*v[2].B) * tb
			// Match the GPU: multiply RGB before clamping framebuffer channels.
			if ca > 1 {
				cr, cg, cb = cr*ca, cg*ca, cb*ca
			}
			c := color.NRGBA{R: penColorByte(cr), G: penColorByte(cg), B: penColorByte(cb), A: penColorByte(ca)}
			if dst == nil {
				dst = image.NewNRGBA(image.Rect(0, 0, stride, int(math.Ceil(height))))
				r.pen = dst
			}
			if stage && r.penComposite != nil {
				blendModeRGBA(r.penComposite, x, y, c, "")
				copyPenPixel(dst, r.penComposite, x, y)
			} else {
				blendNRGBA(dst, x, y, c)
			}
		}
	}
	if stage && changed {
		r.penRevision++
	}
}

func penColorByte(v float64) uint8 {
	return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255))
}

// Depth replay needs coverage, not premultiplied RGB.
func penTextureAlpha(im *image.NRGBA, u, v float64, linear bool) float64 {
	if !finite(u) || !finite(v) {
		return 0
	}
	u, v = u-math.Floor(u), v-math.Floor(v)
	w, h := im.Rect.Dx(), im.Rect.Dy()
	if !linear {
		x, y := int(u*float64(w)), int(v*float64(h))
		if x < 0 || y < 0 || x >= w || y >= h {
			return 0
		}
		return float64(im.Pix[y*im.Stride+x*4+3]) / 255
	}
	x, y := u*float64(w)-.5, v*float64(h)-.5
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	left, right := max(0, min(w-1, x0))*4+3, max(0, min(w-1, x0+1))*4+3
	top, bottom := max(0, min(h-1, y0))*im.Stride, max(0, min(h-1, y0+1))*im.Stride
	alpha := float64(im.Pix[top+left]) * ((1 - fx) * (1 - fy) / 255)
	alpha += float64(im.Pix[top+right]) * (fx * (1 - fy) / 255)
	alpha += float64(im.Pix[bottom+left]) * ((1 - fx) * fy / 255)
	alpha += float64(im.Pix[bottom+right]) * (fx * fy / 255)
	return alpha
}

// UVs wrap before filtering; sampling itself clamps to the edge, like WebGL.
func penTextureSample(im *image.NRGBA, u, v float64, linear bool) (r, g, b, a float64) {
	if !finite(u) || !finite(v) {
		return
	}
	u, v = u-math.Floor(u), v-math.Floor(v)
	w, h := im.Rect.Dx(), im.Rect.Dy()
	if !linear {
		c := im.NRGBAAt(im.Rect.Min.X+int(u*float64(w)), im.Rect.Min.Y+int(v*float64(h)))
		alpha := float64(c.A) / 255
		return float64(c.R) / 255 * alpha, float64(c.G) / 255 * alpha, float64(c.B) / 255 * alpha, alpha
	}
	x, y := u*float64(w)-.5, v*float64(h)-.5
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	for j := 0; j < 2; j++ {
		wy := 1 - fy
		if j == 1 {
			wy = fy
		}
		for i := 0; i < 2; i++ {
			wx := 1 - fx
			if i == 1 {
				wx = fx
			}
			c := im.NRGBAAt(im.Rect.Min.X+max(0, min(w-1, x0+i)), im.Rect.Min.Y+max(0, min(h-1, y0+j)))
			weight := wx * wy / 255
			// Source textures are premultiplied before texture filtering.
			alpha := float64(c.A) / 255
			r += float64(c.R) * alpha * weight
			g += float64(c.G) * alpha * weight
			b += float64(c.B) * alpha * weight
			a += float64(c.A) * weight
		}
	}
	return
}
