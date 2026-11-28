package render

import (
	"image"
	"math"

	"gowarp/core/media"
	"gowarp/core/vm"
)

/*
DisplaySprite describes a textured quad without loading any CPU pixels. Its
corners run clockwise from the costume's top left in display pixel space.
*/
type DisplaySprite struct {
	Costume *media.Costume
	Text    *TextLayout
	Corners [4][2]float64
	Density float64
}

// DisplayView exposes the camera without changing the logical collision frame.
func (r *Renderer) DisplayView() vm.View {
	return r.view
}

// DisplayPen returns read-only logical pixels and a revision for GPU uploads.
func (r *Renderer) DisplayPen() (*image.NRGBA, uint64) {
	return r.pen, r.penRevision
}

/*
PenSprite maps a stamp to the logical pen buffer, independently of the camera.
Match the CPU stamp's base costume raster even when the window is enlarged.
*/
func (r *Renderer) PenSprite(t vm.DisplayTarget) (DisplaySprite, bool) {
	tr, ok := r.transformGeometry(t)
	if !ok || tr.sx == 0 || tr.sy == 0 {
		return DisplaySprite{}, false
	}
	d := DisplaySprite{Costume: tr.c, Density: 1}
	for i, uv := range [4][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		lx, ly := (uv[0]*tr.c.Width-tr.c.CenterX)*tr.sx, (tr.c.CenterY-uv[1]*tr.c.Height)*tr.sy
		d.Corners[i][0] = r.view.Width/2 + tr.x + tr.cos*lx - tr.sin*ly
		d.Corners[i][1] = r.view.Height/2 - tr.y - tr.sin*lx - tr.cos*ly
		if !finite(d.Corners[i][0]) || !finite(d.Corners[i][1]) {
			return DisplaySprite{}, false
		}
	}
	return d, true
}

func (r *Renderer) DisplaySprite(t vm.DisplayTarget, width, height int) (DisplaySprite, bool) {
	var tr transform
	var ok bool
	var text *TextLayout
	if t.Source >= 0 && t.Source < len(r.Assets.Costumes) && t.State.Text != nil && t.State.Text.Visible {
		var c *media.Costume
		text, c = r.displayText(t)
		if c != nil {
			tr, ok = r.costumeTransform(t, c)
		}
	} else {
		tr, ok = r.transformGeometry(t)
	}
	if !ok || tr.sx == 0 || tr.sy == 0 {
		return DisplaySprite{}, false
	}
	d := DisplaySprite{Costume: tr.c, Text: text}
	for i, uv := range [4][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		lx, ly := (uv[0]*tr.c.Width-tr.c.CenterX)*tr.sx, (tr.c.CenterY-uv[1]*tr.c.Height)*tr.sy
		x, y := tr.x+tr.cos*lx-tr.sin*ly, tr.y+tr.sin*lx+tr.cos*ly
		d.Corners[i][0], d.Corners[i][1] = r.DisplayPoint(x, y, width, height)
		if !finite(d.Corners[i][0]) || !finite(d.Corners[i][1]) {
			return DisplaySprite{}, false
		}
	}
	d.Density = math.Max(float64(width)/r.view.Width, float64(height)/r.view.Height) * math.Abs(r.view.Zoom/100) * math.Max(math.Abs(tr.sx), math.Abs(tr.sy))
	if t.State.RenderQuality > 0 {
		d.Density = math.Min(d.Density, t.State.RenderQuality)
	}
	return d, true
}

// DisplayPoint maps a world point directly to the requested display dimensions.
func (r *Renderer) DisplayPoint(x, y float64, width, height int) (float64, float64) {
	v := r.view
	x, y = x-v.X, y-v.Y
	s, c := math.Sincos((90 - v.Direction) * math.Pi / 180)
	return ((c*x-s*y)*v.Zoom/100/v.Width + .5) * float64(width), (.5 - (s*x+c*y)*v.Zoom/100/v.Height) * float64(height)
}
