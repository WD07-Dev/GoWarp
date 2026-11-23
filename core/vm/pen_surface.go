package vm

import (
	"fmt"
	"math"

	"gowarp/core/extension"
)

// Bound offscreen storage across the project, not per sprite or clone.
const maxSurfacePixels = 64 << 20

func (c *extensionContext) penSurfaceCall(operation string, args []any) (any, error) {
	r := c.r
	a := hostArgs(args)
	name := a.string(0)
	surface := &PenSurfaceCommand{Name: name}
	command := PenCommand{Surface: surface}
	switch operation {
	case "graphics.surface.exists":
		_, exists := r.visual.surfaces[name]
		return name == StageSurface || exists, nil
	case "graphics.surface.create":
		if name == StageSurface {
			return nil, nil
		}
		width, height := math.Floor(a.number(1)), math.Floor(a.number(2))
		if !finite(width) || !finite(height) || width < 1 || height < 1 || width > 4096 || height > 4096 {
			return nil, fmt.Errorf("render texture dimensions must be 1..4096")
		}
		previous := r.visual.surfaces[name]
		pixels := r.visual.surfacePixels - previous[0]*previous[1] + int(width)*int(height)
		if pixels > maxSurfacePixels {
			return nil, fmt.Errorf("render textures exceed the 64M-pixel project limit")
		}
		if r.visual.surfaces == nil {
			r.visual.surfaces = make(map[string][2]int)
		}
		surface.Width, surface.Height = int(width), int(height)
		r.visual.surfaces[name] = [2]int{surface.Width, surface.Height}
		r.visual.surfacePixels = pixels
		command.Kind = "surface-create"
	case "graphics.surface.clear", "graphics.surface.delete":
		size, exists := r.visual.surfaces[name]
		if !exists {
			return nil, nil
		}
		command.Kind = "surface-clear"
		if operation == "graphics.surface.delete" {
			delete(r.visual.surfaces, name)
			r.visual.surfacePixels -= size[0] * size[1]
			command.Kind = "surface-delete"
		}
	case "graphics.surface.reset":
		r.visual.surfaces = nil
		r.visual.surfacePixels = 0
		command.Kind = "surface-reset"
	case "graphics.surface.triangle":
		if c.th == nil || c.th.target == nil {
			return nil, fmt.Errorf("drawing a triangle requires a current target")
		}
		if _, exists := r.visual.surfaces[name]; name != StageSurface && !exists {
			return nil, nil
		}
		triangle, ok := a.get(1).(extension.SurfaceTriangle)
		if len(args) != 2 || !ok {
			return nil, fmt.Errorf("graphics.surface.triangle requires a destination and SurfaceTriangle")
		}
		for i, value := range triangle.Vertices {
			vertex := &surface.Vertices[i]
			*vertex = PenVertex(value)
			fields := [...]*float64{&vertex.X, &vertex.Y, &vertex.U, &vertex.V,
				&vertex.R, &vertex.G, &vertex.B, &vertex.A, &vertex.Z, &vertex.W}
			for _, field := range fields {
				if math.IsNaN(*field) {
					*field = 0
				}
				if !finite(*field) {
					return nil, nil
				}
			}
			if vertex.W <= 0 {
				return nil, nil
			}
		}
		surface.Linear = triangle.Linear
		surface.MultiplyTextureAlpha = triangle.MultiplyTextureAlpha
		textureSource := triangle.Texture
		if textureSource.Kind != "" {
			surface.Textured = true
			switch textureSource.Kind {
			case "surface":
				surface.Texture = textureSource.Surface
				_, exists := r.visual.surfaces[surface.Texture]
				if surface.Texture == surface.Name || (!exists && surface.Texture != StageSurface) {
					return nil, nil
				}
			case "costume":
				target, err := c.logicTarget(c.th.target, textureSource.Target)
				if err != nil {
					return nil, err
				}
				index := extension.Number(textureSource.Costume)
				if !finite(index) || index != math.Trunc(index) || index < 0 || index >= float64(len(target.source.Costumes)) {
					return nil, nil
				}
				snapshot := display(target)
				snapshot.State.CurrentCostume = int(index)
				snapshot.State.Variables = nil
				snapshot.State.Lists = nil
				surface.Costume = &snapshot
			default:
				return nil, fmt.Errorf("unknown triangle texture kind %q", textureSource.Kind)
			}
		}
		command.Kind = "surface-triangle"
		if name == StageSurface {
			command.Layer = r.penDestination(c.th.target)
		}
		if name == StageSurface {
			r.redrawRequested = true
		}
	default:
		return nil, fmt.Errorf("unsupported extension host operation %q", operation)
	}
	if r.graphics != nil {
		r.graphics.Pen(command)
	}
	return nil, nil
}
