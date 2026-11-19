package vm

import (
	"fmt"
	"math"

	"gowarp/core/extension"
)

func (c *extensionContext) graphicsCall(operation string, args []any) (any, error) {
	r, t := c.r, c.th.target
	a := hostArgs(args)
	switch operation {
	case "graphics.penColor":
		color := penColor(t.state.Pen)
		return []float64{float64(color.R) / 255, float64(color.G) / 255, float64(color.B) / 255, float64(color.A) / 255}, nil
	case "graphics.clipbox":
		if len(args) == 0 {
			t.state.Clipbox = nil
		} else {
			x1, y1, x2, y2 := a.number(0), a.number(1), a.number(2), a.number(3)
			t.state.Clipbox = &Bounds{
				Left:   math.Min(x1, x2),
				Right:  math.Max(x1, x2),
				Bottom: math.Min(y1, y2),
				Top:    math.Max(y1, y2),
			}
		}
		t.requestRedraw()
		return nil, nil
	case "graphics.blend":
		if len(args) == 0 {
			return t.state.BlendMode, nil
		}
		mode := a.string(0)
		switch mode {
		case "default", "additive", "multiply":
			t.state.BlendMode = mode
		default:
			return nil, fmt.Errorf("unsupported blend mode %q", mode)
		}
		t.requestRedraw()
		return nil, nil
	case "graphics.maskTarget":
		if len(args) == 0 {
			return r.options.View.MaskTarget, nil
		}
		name := a.string(0)
		if r.cloneSource(t, name) == nil {
			return nil, fmt.Errorf("mask sprite %q does not exist", name)
		}
		r.options.View.MaskTarget = name
		r.extensionViewChanged()
		return nil, nil
	case "graphics.maskMode":
		if len(args) == 0 {
			return t.state.MaskMode, nil
		}
		t.state.MaskMode = extension.Bool(a.get(0)) && r.options.View.MaskTarget != ""
		t.requestRedraw()
		return nil, nil
	case "graphics.resetClipping":
		for _, other := range r.targets {
			other.state.Clipbox = nil
			other.state.BlendMode = ""
			other.state.MaskMode = false
		}
		r.extensionViewChanged()
		return nil, nil
	case "camera.dimensions":
		width, height := a.number(0), a.number(1)
		if !validStage(width, height) {
			return nil, fmt.Errorf("stage dimensions must be 1..4096 with at most 16M pixels")
		}
		r.options.View.Width, r.options.View.Height = width, height
		r.extensionViewChanged()
		return nil, nil
	case "camera.get", "camera.set":
		view := &r.options.View
		key := a.string(0)
		if key == "color" {
			if operation == "camera.get" {
				return view.Color, nil
			}
			view.Color = a.string(1)
			r.extensionViewChanged()
			return nil, nil
		}
		var value *float64
		switch key {
		case "x":
			value = &view.X
		case "y":
			value = &view.Y
		case "zoom":
			value = &view.Zoom
		case "direction":
			value = &view.Direction
		case "width":
			value = &view.Width
		case "height":
			value = &view.Height
		default:
			return nil, fmt.Errorf("unknown camera property %q", key)
		}
		if operation == "camera.get" {
			return *value, nil
		}
		next := a.number(1)
		if key == "width" || key == "height" {
			width, height := view.Width, view.Height
			if key == "width" {
				width = next
			} else {
				height = next
			}
			if !validStage(width, height) {
				return nil, fmt.Errorf("stage dimensions must be 1..4096 with at most 16M pixels")
			}
		}
		*value = next
		r.extensionViewChanged()
		return nil, nil
	case "graphics.penAntialias":
		r.options.View.PenNoAntialias = !extension.Bool(a.get(0))
		r.extensionViewChanged()
		return nil, nil
	case "graphics.targetID":
		other := r.cloneSource(t, a.string(0))
		if other == nil {
			return "", nil
		}
		return float64(other.sourceIndex + other.state.CloneID*r.sourceCount), nil
	case "graphics.quality":
		id := a.number(0)
		for _, other := range r.targets {
			if float64(other.sourceIndex+other.state.CloneID*r.sourceCount) != id {
				continue
			}
			other.state.RenderQuality = clamp(math.Floor(a.number(1)+.5), 1, 100)
			other.requestRedraw()
			break
		}
		return nil, nil
	case "graphics.stamp":
		if other := r.cloneSource(t, a.string(0)); other != nil && r.graphics != nil {
			r.redrawRequested = true
			r.graphics.Pen(PenCommand{Kind: "stamp", Target: display(other)})
		}
		return nil, nil
	case "stretch.get", "stretch.set":
		axis := a.string(0)
		var value *float64
		switch axis {
		case "x":
			value = &t.state.StretchX
		case "y":
			value = &t.state.StretchY
		default:
			return nil, fmt.Errorf("unknown stretch axis %q", axis)
		}
		if operation == "stretch.get" {
			return *value, nil
		}
		*value = numeric(a.get(1))
		t.requestRedraw()
		return nil, nil
	case "canvas.get", "canvas.set":
		canvas := r.options.View.Canvas.Defaults()
		key := a.string(0)
		value := canvas.slot(key)
		switch {
		case value != nil:
			if operation == "canvas.get" {
				return *value, nil
			}
			*value = a.number(1)
		case key == "nearest":
			if operation == "canvas.get" {
				return canvas.Nearest, nil
			}
			canvas.Nearest = extension.Bool(a.get(1))
		default:
			var value *string
			switch key {
			case "border style":
				value = &canvas.BorderStyle
			case "border color":
				value = &canvas.BorderColor
			case "background":
				value = &canvas.Background
			default:
				return nil, fmt.Errorf("unknown canvas property %q", key)
			}
			if operation == "canvas.get" {
				return *value, nil
			}
			*value = a.string(1)
		}
		r.options.View.Canvas = canvas
		r.extensionViewChanged()
		return nil, nil
	case "graphics.screenSplits":
		parts, ok := a.get(0).([]extension.ScreenSplit)
		if !ok && a.get(0) != nil {
			return nil, fmt.Errorf("graphics.screenSplits requires ScreenSplit values")
		}
		for _, part := range parts {
			if part == nil {
				return nil, fmt.Errorf("graphics.screenSplits requires non-nil parts")
			}
		}
		r.logic.splits = append([]extension.ScreenSplit(nil), parts...)
		r.logic.splitOriginal = extension.Bool(a.get(1))
		r.logic.splitView = nil
		r.extensionViewChanged()
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported extension host operation %q", operation)
}

func (r *Runtime) extensionViewChanged() {
	r.redrawRequested = true
	if r.graphics != nil {
		r.graphics.SetView(r.View())
	}
}
