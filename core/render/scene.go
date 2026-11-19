// Package render provides a deterministic CPU stage renderer and hit testing.
package render

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"gowarp/core/media"
	"gowarp/core/vm"
)

const Width = 480

const Height = 360

type Renderer struct {
	colorCandidates []colorCandidate

	textSkinBytes    int
	textSkins        map[textSkinKey]*media.Costume
	Assets           *media.Library
	frame            *image.RGBA
	displayFrame     *image.RGBA
	view             vm.View
	pen              *image.NRGBA
	penComposite     *image.RGBA
	penRevision      uint64
	penClearRevision uint64
	penSurfaces      map[string]*penSurface
	penDepth         []float64
	hulls            map[*media.Costume][]hullPoint
	shapedHull       shapedHullCache
}

func New(lib *media.Library) *Renderer {
	return &Renderer{
		Assets: lib,
		frame:  image.NewRGBA(image.Rect(0, 0, Width, Height)),
		view: vm.View{
			Width:     480,
			Height:    360,
			Zoom:      100,
			Direction: 90,
			Color:     "#ffffff",
		},
	}
}

type transform struct {
	c                      *media.Costume
	image                  *image.NRGBA
	x, y, cos, sin, sx, sy float64
}

func (r *Renderer) transform(t vm.DisplayTarget) (transform, bool) {
	tr, ok := r.transformGeometry(t)
	if !ok {
		return tr, false
	}
	// Hold pixels during CPU rendering or collision even if the asset LRU evicts them.
	tr.image = r.Assets.CostumeImage(tr.c)
	return tr, tr.image != nil
}

func (r *Renderer) transformGeometry(t vm.DisplayTarget) (transform, bool) {
	if t.Source < 0 || t.Source >= len(r.Assets.Costumes) {
		return transform{}, false
	}
	costumes := r.Assets.Costumes[t.Source]
	i := t.State.CurrentCostume
	var c *media.Costume
	if t.State.Text != nil && t.State.Text.Visible {
		c = r.textCostume(t)
		if c == nil {
			return transform{}, false
		}
	} else {
		if i < 0 || i >= len(costumes) {
			return transform{}, false
		}
		c = costumes[i]
	}
	s := t.State
	tr := transform{
		c:  c,
		x:  s.X,
		y:  s.Y,
		sx: s.Size / 100 * s.StretchX / 100,
		sy: s.Size / 100 * s.StretchY / 100,
	}
	if !r.view.HighQualityPen && !r.view.DisablePositionRounding {
		tr.x, tr.y = math.Floor(tr.x+.5), math.Floor(tr.y+.5)
	}
	direction := s.Direction
	if s.IsStage {
		tr.x, tr.y, direction = 0, 0, 90
	}
	switch s.RotationStyle {
	case "left-right":
		direction = 90
		if s.Direction < 0 {
			tr.sx = -tr.sx
		}
	case "don't rotate":
		direction = 90
	}
	tr.sin, tr.cos = math.Sincos((90 - direction) * math.Pi / 180)
	if c.Width <= 0 || c.Height <= 0 {
		return transform{}, false
	}
	for _, v := range []float64{tr.x, tr.y, tr.sx, tr.sy, tr.sin, tr.cos, c.CenterX, c.CenterY} {
		if !finite(v) {
			return transform{}, false
		}
	}
	return tr, true
}

func (tr *transform) sample(x, y float64, e vm.GraphicEffects, ignoreGhost bool) color.NRGBA {
	return tr.sampleFiltered(x, y, e, ignoreGhost, false)
}

func (tr *transform) sampleFiltered(x, y float64, e vm.GraphicEffects, ignoreGhost, smooth bool) color.NRGBA {
	dx, dy := x-tr.x, y-tr.y
	u := ((tr.cos*dx+tr.sin*dy)/tr.sx + tr.c.CenterX) / tr.c.Width
	v := (tr.c.CenterY - (-tr.sin*dx+tr.cos*dy)/tr.sy) / tr.c.Height
	if !(u >= 0 && v >= 0 && u < 1 && v < 1) {
		return color.NRGBA{}
	}
	if e.Mosaic != 0 || e.Pixelate != 0 || e.Whirl != 0 || e.Fisheye != 0 {
		u, v = effectUV(u, v, tr.c.Width, tr.c.Height, e)
		if !finite(u) || !finite(v) {
			return color.NRGBA{}
		}
		u = math.Max(0, math.Min(math.Nextafter(1, 0), u))
		v = math.Max(0, math.Min(math.Nextafter(1, 0), v))
	}
	var c color.NRGBA
	if smooth && !tr.c.Bitmap && e.Pixelate == 0 {
		c = filteredPixel(tr.image, u, v)
	} else {
		c = tr.image.NRGBAAt(int(u*float64(tr.image.Bounds().Dx())), int(v*float64(tr.image.Bounds().Dy())))
	}
	if ignoreGhost || (e.Color == 0 && e.Brightness == 0 && e.Ghost == 0) {
		return c
	}
	return effectColor(c, e)
}

func (r *Renderer) Draw(scene []vm.DisplayTarget) *image.RGBA {
	return r.draw(scene, false)
}

func (r *Renderer) draw(scene []vm.DisplayTarget, display bool) *image.RGBA {
	draw.Draw(r.frame, r.frame.Bounds(), image.NewUniform(vm.ParseColor(r.view.Color)), image.Point{}, draw.Src)
	camera := r.screenToWorld()
	penDrawn := false
	var stencil []bool
	if r.view.MaskTarget != "" {
		stencil = make([]bool, r.frame.Bounds().Dx()*r.frame.Bounds().Dy())
	}
	for _, t := range scene {
		if !t.State.IsStage && !penDrawn {
			r.drawPen()
			penDrawn = true
		}
		if !t.State.Visible {
			continue
		}
		tr, ok := r.transform(t)
		if !ok {
			continue
		}
		if display {
			density := math.Max(float64(r.frame.Bounds().Dx())/r.view.Width, float64(r.frame.Bounds().Dy())/r.view.Height)
			density *= math.Abs(r.view.Zoom/100) * math.Max(math.Abs(tr.sx), math.Abs(tr.sy))
			tr.image = r.Assets.CostumeImageAtScale(tr.c, density)
			if tr.image == nil {
				continue
			}
		}
		bounds := r.screenBounds(tr.worldBounds(image.Rectangle{})).Intersect(ClipRectangle(t.State.Clipbox, r.view, r.frame.Bounds().Dx(), r.frame.Bounds().Dy()))
		isMask := r.view.MaskTarget != "" && t.State.Name == r.view.MaskTarget && t.State.CloneID == 0
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			cameraY := camera.rowY(float64(y) + .5)
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				wx, wy := camera.point(float64(x)+.5, cameraY)
				c := tr.sampleFiltered(wx, wy, t.State.Effects, false, display && !r.view.NoAntialias)
				if stencil != nil {
					i := y*r.frame.Bounds().Dx() + x
					if t.State.IsStage {
						stencil[i] = false
					} else if isMask {
						/*
							Normal WebGL drawing writes stencil for the quad even
							when the costume or ghost effect makes it transparent.
						*/
						dx, dy := wx-tr.x, wy-tr.y
						u := ((tr.cos*dx+tr.sin*dy)/tr.sx + tr.c.CenterX) / tr.c.Width
						v := (tr.c.CenterY - (-tr.sin*dx+tr.cos*dy)/tr.sy) / tr.c.Height
						if u >= 0 && v >= 0 && u < 1 && v < 1 {
							stencil[i] = true
						}
					} else if t.State.MaskMode && !stencil[i] {
						continue
					}
				}
				blendModeRGBA(r.frame, x, y, c, t.State.BlendMode)
			}
		}
	}
	if !penDrawn {
		r.drawPen()
	}
	return r.frame
}

func (r *Renderer) Hit(scene []vm.DisplayTarget, x, y float64) (vm.DisplayTarget, bool) {
	for i := len(scene) - 1; i >= 0; i-- {
		t := scene[i]
		if t.State.IsStage || !t.State.Visible {
			continue
		}
		tr, ok := r.transform(t)
		if ok && tr.sample(x, y, t.State.Effects, true).A > 0 {
			return t, true
		}
	}
	return vm.DisplayTarget{}, false
}

func effectUV(u, v, w, h float64, e vm.GraphicEffects) (float64, float64) {
	if e.Mosaic == 0 && e.Pixelate == 0 && e.Whirl == 0 && e.Fisheye == 0 {
		return u, v
	}
	mosaic := math.Max(1, math.Min(512, math.Floor((math.Abs(e.Mosaic)+10)/10+.5)))
	if mosaic != 1 {
		u, v = u*mosaic, v*mosaic
		u -= math.Floor(u)
		v -= math.Floor(v)
	}
	if pixels := math.Abs(e.Pixelate) / 10; pixels > 0 {
		nx, ny := w/pixels, h/pixels
		u, v = (math.Floor(u*nx)+.5)/nx, (math.Floor(v*ny)+.5)/ny
	}
	if e.Whirl != 0 {
		dx, dy := u-.5, v-.5
		factor := math.Max(1-math.Hypot(dx, dy)/.5, 0)
		s, c := math.Sincos(-e.Whirl * math.Pi / 180 * factor * factor)
		u, v = c*dx+s*dy+.5, -s*dx+c*dy+.5
	}
	if e.Fisheye != 0 {
		dx, dy := (u-.5)*2, (v-.5)*2
		length := math.Hypot(dx, dy)
		if length > 0 {
			r := math.Pow(math.Min(length, 1), math.Max(0, (e.Fisheye+100)/100)) * math.Max(1, length)
			u, v = .5+.5*r*dx/length, .5+.5*r*dy/length
		}
	}
	return u, v
}

func effectColor(c color.NRGBA, e vm.GraphicEffects) color.NRGBA {
	if e.Color == 0 && e.Brightness == 0 && e.Ghost == 0 {
		return c
	}
	if e.Color == 0 && e.Brightness == 0 {
		alpha := float64(c.A) / 255 * (1 - math.Max(0, math.Min(100, e.Ghost))/100)
		if math.IsNaN(alpha) {
			c.A = 0
		} else {
			c.A = uint8(math.Round(math.Max(0, math.Min(1, alpha)) * 255))
		}
		return c
	}
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	if e.Color != 0 && finite(e.Color) {
		max, min := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
		delta := max - min
		h, s := float64(0), float64(0)
		if max > 0 {
			s = delta / max
		}
		if delta > 0 {
			switch max {
			case r:
				h = (g - b) / delta
			case g:
				h = (b-r)/delta + 2
			default:
				h = (r-g)/delta + 4
			}
			h /= 6
		}
		if max < .055 {
			h, s, max = 0, 1, .055
		} else if s < .09 {
			h, s = 0, .09
		}
		h = math.Mod(h+e.Color/200, 1)
		if h < 0 {
			h++
		}
		chroma := max * s
		hue := func(n float64) float64 {
			return math.Max(0, math.Min(1, n))*chroma + max - chroma
		}
		r, g, b = hue(math.Abs(h*6-3)-1), hue(2-math.Abs(h*6-2)), hue(2-math.Abs(h*6-4))
	}
	brightness := math.Max(-100, math.Min(100, e.Brightness)) / 100
	channel := func(n float64) uint8 {
		if math.IsNaN(n) {
			return 0
		}
		return uint8(math.Round(math.Max(0, math.Min(1, n)) * 255))
	}
	c.R, c.G, c.B = channel(r+brightness), channel(g+brightness), channel(b+brightness)
	c.A = channel(float64(c.A) / 255 * (1 - math.Max(0, math.Min(100, e.Ghost))/100))
	return c
}
