package render

import (
	"image"
	"image/color"
	"math"

	"gowarp/core/vm"
)

/*
DrawAt renders at display resolution without changing logical coordinates, pen storage or
collision sampling. The next display draw reuses its buffer.
*/
func (r *Renderer) DrawAt(scene []vm.DisplayTarget, width, height int) *image.RGBA {
	width, height = DisplaySize(width, height)
	bounds := image.Rect(0, 0, width, height)
	if r.displayFrame == nil || r.displayFrame.Bounds() != bounds {
		r.displayFrame = image.NewRGBA(bounds)
	}
	logical := r.frame
	r.frame = r.displayFrame
	defer func() {
		r.frame = logical
	}()
	return r.draw(scene, true)
}

// DisplaySize bounds the software framebuffer while retaining its aspect ratio.
func DisplaySize(width, height int) (int, int) {
	w, h := float64(max(1, width)), float64(max(1, height))
	// Bound one software framebuffer to 64 MiB, including oversized DPI windows.
	scale := math.Min(1, math.Min(8192/w, 8192/h))
	scale = math.Min(scale, math.Sqrt(float64(16<<20)/(w*h)))
	return max(1, int(math.Floor(w*scale))), max(1, int(math.Floor(h*scale)))
}

/*
Interpolate premultiplied colors so invisible RGB never creates edge halos.
Coordinates use texel centers and clamp-to-edge, like the reference textures.
*/
func filteredPixel(im *image.NRGBA, u, v float64) color.NRGBA {
	w, h := im.Bounds().Dx(), im.Bounds().Dy()
	x, y := u*float64(w)-.5, v*float64(h)-.5
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	x1, y1 := max(0, min(w-1, x0+1)), max(0, min(h-1, y0+1))
	x0, y0 = max(0, min(w-1, x0)), max(0, min(h-1, y0))
	x0, x1 = x0+im.Rect.Min.X, x1+im.Rect.Min.X
	y0, y1 = y0+im.Rect.Min.Y, y1+im.Rect.Min.Y
	first := im.NRGBAAt(x0, y0)
	if fx == 0 && fy == 0 {
		return first
	}
	colors := [4]color.NRGBA{
		first, im.NRGBAAt(x1, y0),
		im.NRGBAAt(x0, y1), im.NRGBAAt(x1, y1),
	}
	if colors[0] == colors[1] && colors[0] == colors[2] && colors[0] == colors[3] {
		return first
	}
	weights := [4]float64{(1 - fx) * (1 - fy), fx * (1 - fy), (1 - fx) * fy, fx * fy}
	var red, green, blue, alpha float64
	for i, c := range colors {
		weight := weights[i] * float64(c.A)
		alpha += weight
		red += weight * float64(c.R)
		green += weight * float64(c.G)
		blue += weight * float64(c.B)
	}
	if alpha <= 0 {
		return color.NRGBA{}
	}
	return color.NRGBA{
		R: uint8(math.Round(red / alpha)),
		G: uint8(math.Round(green / alpha)),
		B: uint8(math.Round(blue / alpha)),
		A: uint8(math.Round(alpha)),
	}
}
