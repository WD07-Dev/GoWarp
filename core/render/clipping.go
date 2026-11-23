package render

import (
	"image"
	"image/color"
	"math"

	"gowarp/core/vm"
)

/*
ClipRectangle follows WebGL scissor's truncation and stage coordinates.
Camera transforms do not affect the clipbox.
*/
func ClipRectangle(box *vm.Bounds, view vm.View, width, height int) image.Rectangle {
	full := image.Rect(0, 0, width, height)
	if box == nil {
		return full
	}
	x1 := int((box.Left/view.Width + .5) * float64(width))
	x2 := int((box.Right/view.Width + .5) * float64(width))
	y1 := int((box.Bottom/view.Height + .5) * float64(height))
	y2 := int((box.Top/view.Height + .5) * float64(height))
	return image.Rect(x1, height-y2, x2, height-y1).Intersect(full)
}

func blendModeRGBA(dst *image.RGBA, x, y int, c color.NRGBA, mode string) {
	if mode == "" || mode == "default" {
		d := dst.RGBAAt(x, y)
		a := uint32(c.A)
		mix := func(s, v uint8) uint8 {
			return uint8((uint32(s)*a + uint32(v)*(255-a) + 127) / 255)
		}
		alpha := uint8(a + (uint32(d.A)*(255-a)+127)/255)
		dst.SetRGBA(x, y, color.RGBA{mix(c.R, d.R), mix(c.G, d.G), mix(c.B, d.B), alpha})
		return
	}
	d := dst.RGBAAt(x, y)
	a := float64(c.A) / 255
	src := [4]float64{float64(c.R) * a, float64(c.G) * a, float64(c.B) * a, float64(c.A)}
	old := [4]float64{float64(d.R), float64(d.G), float64(d.B), float64(d.A)}
	out := [4]uint8{}
	for i := range out {
		v := old[i]
		if mode == "additive" {
			if i < 3 {
				v += src[i]
			}
		} else if mode == "multiply" {
			v = src[i]*old[i]/255 + old[i]*(1-a)
		}
		out[i] = uint8(math.Min(255, math.Max(0, math.Round(v))))
	}
	dst.SetRGBA(x, y, color.RGBA{out[0], out[1], out[2], out[3]})
}

func blendModeNRGBA(dst *image.NRGBA, x, y int, c color.NRGBA, mode string) {
	if mode == "" || mode == "default" {
		blendNRGBA(dst, x, y, c)
		return
	}
	d := dst.NRGBAAt(x, y)
	a, da := float64(c.A)/255, float64(d.A)/255
	alpha := da
	if mode == "multiply" {
		alpha = a*da + da*(1-a)
	}
	out := color.NRGBA{A: uint8(math.Round(alpha * 255))}
	if alpha > 0 {
		s := [3]uint8{c.R, c.G, c.B}
		v := [3]uint8{d.R, d.G, d.B}
		o := [3]uint8{}
		for i := range o {
			n := float64(v[i]) * da
			if mode == "additive" {
				n += float64(s[i]) * a
			} else {
				n = float64(s[i])*a*float64(v[i])*da/255 + float64(v[i])*da*(1-a)
			}
			o[i] = uint8(math.Round(math.Min(255, math.Max(0, n/alpha))))
		}
		out.R, out.G, out.B = o[0], o[1], o[2]
	}
	dst.SetNRGBA(x, y, out)
}

/*
DisplayPenComposite retains RGB above alpha from additive blending,
which NRGBA cannot represent.
*/
func (r *Renderer) DisplayPenComposite() *image.RGBA {
	return r.penComposite
}

func copyPenPixel(dst *image.NRGBA, src *image.RGBA, x, y int) {
	c := src.RGBA64At(x, y)
	// Additive RGB can remain at zero alpha; color.NRGBAModel discards it.
	if c.A == 0 {
		c = color.RGBA64{}
	}
	dst.SetRGBA64(x, y, c)
}

func blendPremultiplied(dst *image.RGBA, x, y int, c color.RGBA) {
	d := dst.RGBAAt(x, y)
	f := float64(255-c.A) / 255
	add := func(s, v uint8) uint8 {
		return uint8(math.Min(255, math.Round(float64(s)+float64(v)*f)))
	}
	dst.SetRGBA(x, y, color.RGBA{add(c.R, d.R), add(c.G, d.G), add(c.B, d.B), add(c.A, d.A)})
}

// Pen framebuffers invert the scissor origin before truncating pixel coordinates.
func PenClipRectangle(box *vm.Bounds, view vm.View, width, height int) image.Rectangle {
	if box == nil {
		return image.Rect(0, 0, width, height)
	}
	x := int((box.Left/view.Width + .5) * float64(width))
	x2 := int((box.Right/view.Width + .5) * float64(width))
	y := int((-box.Top/view.Height + .5) * float64(height))
	h := int((box.Top/view.Height+.5)*float64(height)) - int((box.Bottom/view.Height+.5)*float64(height))
	return image.Rect(x, y, x2, y+h).Intersect(image.Rect(0, 0, width, height))
}
