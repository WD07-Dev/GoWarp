package render

import (
	"image/color"
	"math"

	"gowarp/core/vm"
)

func (r *Renderer) PenLayer(name string) *Renderer {
	if name == "" || name == vm.StageSurface {
		return r
	}
	return r.layers[name]
}

func (r *Renderer) layerCommand(cmd vm.PenCommand) bool {
	switch cmd.Kind {
	case "layer-create":
		if r.layers == nil {
			r.layers = make(map[string]*Renderer)
		}
		if r.layers[cmd.Layer] == nil {
			layer := New(r.Assets)
			layer.surfaceParent = r
			layer.SetView(r.view)
			r.layers[cmd.Layer] = layer
		}
		return true
	case "layer-delete":
		delete(r.layers, cmd.Layer)
		return true
	case "layer-quality":
		return true // CPU sensing always uses logical stage pixels.
	}
	if cmd.Layer == "" || cmd.Layer == vm.StageSurface {
		return false
	}
	if layer := r.layers[cmd.Layer]; layer != nil {
		cmd.Layer = ""
		layer.Pen(cmd)
	}
	return true
}

func (r *Renderer) drawLayer(target vm.DisplayTarget) {
	layer := r.PenLayer(target.State.RenderLayer)
	if layer == nil || layer.pen == nil {
		return
	}
	camera := r.screenToWorld()
	for y := 0; y < r.frame.Bounds().Dy(); y++ {
		row := camera.rowY(float64(y) + .5)
		for x := 0; x < r.frame.Bounds().Dx(); x++ {
			wx, wy := camera.point(float64(x)+.5, row)
			blendRGBA(r.frame, x, y, layer.layerAt(target.State.Transform, wx, wy))
		}
	}
}

func (r *Renderer) layerAt(tr *vm.RenderTransform, x, y float64) color.NRGBA {
	if tr != nil {
		if tr.Scale == 0 {
			return color.NRGBA{}
		}
		sn, cs := math.Sincos(tr.Rotation * math.Pi / 180)
		x, y = (x*cs+y*sn)/tr.Scale+tr.X, (-x*sn+y*cs)/tr.Scale+tr.Y
	}
	return r.penAt(x, y)
}
