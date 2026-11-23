package vm

import (
	"fmt"
	"math"
	"slices"
)

// RenderTransform changes drawable geometry without changing Scratch coordinates.
type RenderTransform struct {
	X, Y, Scale, Rotation float64
}

type renderLayer struct {
	target    *target
	placement string
}

func (r *Runtime) penDestination(t *target) string {
	if t != nil && t.penLayer != "" {
		if _, ok := r.visual.layers[t.penLayer]; ok {
			r.visual.activeLayer = t.penLayer
		}
	}
	if r.visual.activeLayer == StageSurface {
		return ""
	}
	return r.visual.activeLayer
}

func (c *extensionContext) layerCall(operation string, args []any) (any, error) {
	r, a := c.r, hostArgs(args)
	name := a.string(0)
	layer := r.visual.layers[name]
	command := func(kind string, size float64) {
		if r.graphics != nil {
			r.graphics.Pen(PenCommand{Kind: kind, Layer: name, Size: size})
		}
		r.redrawRequested = true
	}
	switch operation {
	case "graphics.layer.create":
		if layer != nil {
			return name, nil
		}
		if name == "" {
			return nil, fmt.Errorf("render layer name cannot be empty")
		}
		if float64(len(r.visual.layers)+1)*r.View().Width*r.View().Height > maxSurfacePixels {
			return nil, fmt.Errorf("render layers exceed the 64M-pixel project limit")
		}
		if r.visual.layers == nil {
			r.visual.layers = make(map[string]*renderLayer)
		}
		r.visual.nextLayerID++
		t := &target{owner: r, sourceIndex: -r.visual.nextLayerID, state: TargetState{Name: name, Visible: true, RenderLayer: name}}
		layer = &renderLayer{target: t, placement: "below"}
		r.visual.layers[name] = layer
		r.drawList = append(r.drawList, t)
		r.moveLayer(t, 1, false, false)
		if name != StageSurface {
			command("layer-create", 0)
		}
		return name, nil
	case "graphics.layer.exists":
		return layer != nil, nil
	case "graphics.layer.selected":
		if r.visual.activeLayer == "" {
			return StageSurface, nil
		}
		return r.visual.activeLayer, nil
	case "graphics.layer.bind":
		t, err := c.logicTarget(c.th.target, a.get(1))
		if err != nil {
			return nil, err
		}
		if name == "" {
			t.penLayer = ""
			return nil, nil
		}
		if layer != nil {
			t.penLayer = name
			r.visual.activeLayer = name
		}
		return nil, nil
	}
	if layer == nil {
		return nil, nil
	}
	switch operation {
	case "graphics.layer.select":
		r.visual.activeLayer = name
	case "graphics.layer.clear":
		if name == StageSurface {
			name = ""
		}
		command("clear", 0)
	case "graphics.layer.delete":
		if name == StageSurface {
			return nil, nil
		}
		delete(r.visual.layers, name)
		i := slices.Index(r.drawList, layer.target)
		if i >= 0 {
			r.drawList = slices.Delete(r.drawList, i, i+1)
			r.updateLayers()
		}
		if r.visual.activeLayer == name {
			r.visual.activeLayer = ""
		}
		command("layer-delete", 0)
	case "graphics.layer.visible":
		if len(args) == 1 {
			return layer.target.state.Visible, nil
		}
		layer.target.state.Visible = truth(a.get(1))
	case "graphics.layer.quality":
		q := a.number(1)
		if !finite(q) {
			return nil, fmt.Errorf("render layer quality must be finite")
		}
		command("layer-quality", clamp(q, .25, 4))
	case "graphics.layer.placement":
		layer.placement = a.string(1)
		switch layer.placement {
		case "above":
			r.moveLayer(layer.target, math.Inf(1), false, false)
		case "below", "unlocked":
			r.moveLayer(layer.target, 1, false, false)
		default:
			return nil, fmt.Errorf("unknown render layer placement %q", layer.placement)
		}
	case "graphics.layer.move":
		if layer.placement != "unlocked" {
			return nil, nil
		}
		t, err := c.logicTarget(c.th.target, a.get(2))
		if err != nil {
			return nil, err
		}
		from, to := layer.target.state.LayerOrder, t.state.LayerOrder
		if a.string(1) == "back" {
			if from < to {
				to--
			}
		} else if from > to {
			to++
		}
		r.moveLayer(layer.target, float64(to), false, false)
	default:
		return nil, fmt.Errorf("unsupported extension host operation %q", operation)
	}
	r.redrawRequested = true
	return nil, nil
}

func (c *extensionContext) cameraBindingCall(operation string, args []any) (any, error) {
	r, a := c.r, hostArgs(args)
	name := a.string(0)
	switch operation {
	case "camera.transform":
		if len(args) != 5 {
			return nil, fmt.Errorf("camera.transform requires name, x, y, scale, rotation")
		}
		v := RenderTransform{a.number(1), a.number(2), a.number(3), a.number(4)}
		if !finite(v.X) || !finite(v.Y) || !finite(v.Scale) || !finite(v.Rotation) {
			return nil, fmt.Errorf("camera transform must be finite")
		}
		if r.visual.cameras == nil {
			r.visual.cameras = make(map[string]*RenderTransform)
		}
		r.visual.cameras[name] = &v
	case "camera.bind":
		ref := a.get(1)
		if a.string(1) == "_all_" {
			for _, t := range r.drawList {
				t.state.Camera = name
			}
		} else if layer := r.visual.layers[a.string(1)]; layer != nil {
			layer.target.state.Camera = name
		} else {
			t, err := c.logicTarget(c.th.target, ref)
			if err != nil {
				return nil, err
			}
			t.state.Camera = name
		}
	case "camera.binding":
		if layer := r.visual.layers[name]; layer != nil {
			return layer.target.state.Camera, nil
		}
		t, err := c.logicTarget(c.th.target, a.get(0))
		if err != nil {
			return nil, err
		}
		return t.state.Camera, nil
	default:
		return nil, fmt.Errorf("unsupported extension host operation %q", operation)
	}
	r.redrawRequested = true
	return nil, nil
}
