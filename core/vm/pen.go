package vm

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	"gowarp/core/project"
)

func ParseColor(value any) color.NRGBA {
	if s, ok := value.(string); ok && strings.HasPrefix(s, "#") {
		s = s[1:]
		if len(s) == 3 {
			s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
		}
		if len(s) != 6 {
			return color.NRGBA{A: 255}
		}
		n, e := strconv.ParseUint(s, 16, 24)
		if e != nil {
			return color.NRGBA{A: 255}
		}
		return color.NRGBA{
			R: uint8(n >> 16),
			G: uint8(n >> 8),
			B: uint8(n),
			A: 255,
		}
	}
	n := number(value)
	var bits uint32
	if finite(n) && math.Abs(n) < 4294967296 {
		bits = uint32(int64(n))
	}
	a := uint8(bits >> 24)
	if a == 0 {
		a = 255
	}
	return color.NRGBA{
		R: uint8(bits >> 16),
		G: uint8(bits >> 8),
		B: uint8(bits),
		A: a,
	}
}

func rgbHSV(c color.NRGBA) (h, s, v float64) {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	v = math.Max(r, math.Max(g, b))
	low := math.Min(r, math.Min(g, b))
	delta := v - low
	if delta == 0 {
		return 0, 0, v * 100
	}
	s = delta / v * 100
	switch v {
	case r:
		h = (g - b) / delta
	case g:
		h = (b-r)/delta + 2
	default:
		h = (r-g)/delta + 4
	}
	h = math.Mod(h*100/6+100, 100)
	return h, s, v * 100
}

func penColor(p PenState) color.NRGBA {
	h := math.Mod(p.Hue, 100)
	if h < 0 {
		h += 100
	}
	h *= 6 / 100.0
	s, v := clamp(p.Saturation, 0, 100)/100, clamp(p.Brightness, 0, 100)/100
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h, 2)-1))
	m := v - c
	r, g, b := 0.0, 0.0, 0.0
	switch int(h) {
	case 0:
		r, g = c, x
	case 1:
		r, g = x, c
	case 2:
		g, b = c, x
	case 3:
		g, b = x, c
	case 4:
		r, b = x, c
	default:
		r, b = c, x
	}
	return color.NRGBA{
		R: uint8(math.Floor((r + m) * 255)),
		G: uint8(math.Floor((g + m) * 255)),
		B: uint8(math.Floor((b + m) * 255)),
		A: uint8(math.Floor((1 - clamp(p.Transparency, 0, 100)/100) * 255)),
	}
}

func (r *Runtime) penLine(t *target, x0, y0, x1, y1 float64) {
	if r.graphics != nil {
		r.redrawRequested = true
		r.graphics.Pen(PenCommand{
			Kind:   "line",
			Layer:  r.penDestination(t),
			Target: display(t),
			Line:   [4]float64{x0, y0, x1, y1},
			Size:   t.state.Pen.Size,
			Color:  penColor(t.state.Pen),
		})
	}
}

func (r *Runtime) penDraw(t *target, kind string, points ...float64) {
	if r.graphics != nil {
		destinationTarget := t
		if kind == "clear" {
			destinationTarget = nil
		}
		r.redrawRequested = true
		r.graphics.Pen(PenCommand{
			Kind:   kind,
			Layer:  r.penDestination(destinationTarget),
			Target: display(t),
			Points: points,
			Size:   t.state.Pen.Size,
			Color:  penColor(t.state.Pen),
		})
	}
}

func (r *Runtime) stamp(t *target) error {
	if r.graphics == nil {
		return fmt.Errorf("pen_stamp requires graphics")
	}
	r.penDraw(t, "stamp")
	return nil
}

func (r *Runtime) penDown(t *target) error {
	if r.graphics == nil {
		return fmt.Errorf("pen_penDown requires graphics")
	}
	t.state.Pen.Down = true
	r.penLine(t, t.state.X, t.state.Y, t.state.X, t.state.Y)
	return nil
}

func (p *PenState) setColor(value any) {
	c := ParseColor(value)
	p.Hue, p.Saturation, p.Brightness = rgbHSV(c)
	p.Transparency = 100 * (1 - float64(c.A)/255)
	p.Shade = p.Brightness / 2
}

func (r *Runtime) penStatement(t *target, b *project.Block, input func(string) (any, error)) (bool, error) {
	p := &t.state.Pen
	op := b.Opcode
	switch op {
	case "pen_stamp":
		return true, r.stamp(t)
	case "pen_penDown":
		return true, r.penDown(t)
	case "pen_clear", "pen_drawTriangle":
		var points []float64
		if op == "pen_drawTriangle" {
			for _, key := range []string{"X0", "Y0", "X1", "Y1", "X2", "Y2"} {
				v, e := input(key)
				if e != nil {
					return true, e
				}
				points = append(points, number(v))
			}
		}
		if r.graphics == nil {
			return true, fmt.Errorf("%s requires graphics", op)
		}
		switch op {
		case "pen_clear":
			r.penDraw(t, "clear")
		case "pen_drawTriangle":
			r.penDraw(t, "triangle", points...)
		}
		return true, nil
	case "pen_penUp":
		p.Down = false
		return true, nil
	case "pen_setPenColorToColor":
		v, e := input("COLOR")
		if e != nil {
			return true, e
		}
		p.setColor(v)
		return true, nil
	case "pen_setPenSizeTo", "pen_changePenSizeBy":
		v, e := input("SIZE")
		if e != nil {
			return true, e
		}
		p.setSize(number(v), op == "pen_changePenSizeBy", r.options.MiscLimits)
		return true, nil
	case "pen_setPenColorParamTo", "pen_changePenColorParamBy":
		param, e := input("COLOR_PARAM")
		if e != nil {
			return true, e
		}
		v, e := input("VALUE")
		if e != nil {
			return true, e
		}
		p.setColorParam(text(param), number(v), op == "pen_changePenColorParamBy")
		return true, nil
	case "pen_setPenHueToNumber", "pen_changePenHueBy", "pen_setPenShadeToNumber", "pen_changePenShadeBy":
		key := "HUE"
		if strings.Contains(op, "Shade") {
			key = "SHADE"
		}
		v, e := input(key)
		if e != nil {
			return true, e
		}
		n := number(v)
		if key == "HUE" {
			n /= 2
			if strings.HasPrefix(op, "pen_change") {
				n += p.Hue
			} else {
				p.Transparency = 0
			}
			p.Hue = math.Mod(n, 100)
			if p.Hue < 0 {
				p.Hue += 100
			}
		} else {
			if strings.HasPrefix(op, "pen_change") {
				n += p.Shade
			}
			p.Shade = math.Mod(n, 200)
			if p.Shade < 0 {
				p.Shade += 200
			}
		}
		base := penColor(PenState{
			Hue:        p.Hue,
			Saturation: 100,
			Brightness: 100,
		})
		shade := p.Shade
		if shade > 100 {
			shade = 200 - shade
		}
		mix := func(v uint8) uint8 {
			if shade < 50 {
				return uint8(float64(v) * (10 + shade) / 60)
			}
			return uint8(float64(v) + (255-float64(v))*(shade-50)/60)
		}
		base.R, base.G, base.B = mix(base.R), mix(base.G), mix(base.B)
		p.Hue, p.Saturation, p.Brightness = rgbHSV(base)
		return true, nil
	}
	return false, nil
}

type undefinedValue = project.Undefined

func (p PenState) MarshalJSON() ([]byte, error) {
	safe := func(n float64) any {
		if !finite(n) {
			return text(n)
		}
		return n
	}
	return json.Marshal(map[string]any{
		"down":         p.Down,
		"size":         safe(p.Size),
		"hue":          safe(p.Hue),
		"saturation":   safe(p.Saturation),
		"brightness":   safe(p.Brightness),
		"transparency": safe(p.Transparency),
		"shade":        safe(p.Shade),
	})
}

func (p *PenState) setSize(value float64, change, limited bool) {
	if change {
		value += p.Size
	}
	if limited {
		p.Size = clamp(value, 1, 1200)
	} else {
		p.Size = math.Max(0, value)
	}
}

func (p *PenState) setColorParam(param string, value float64, change bool) {
	var slot *float64
	switch param {
	case "color":
		slot = &p.Hue
	case "saturation":
		slot = &p.Saturation
	case "brightness":
		slot = &p.Brightness
	case "transparency":
		slot = &p.Transparency
	default:
		return
	}
	if change {
		value += *slot
	}
	if param == "color" {
		value = math.Mod(value, 100)
		if value < 0 {
			value += 100
		}
	} else {
		value = clamp(value, 0, 100)
	}
	*slot = value
}
