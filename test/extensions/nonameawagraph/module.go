// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
)

func New() e.Extension {
	points := []string{"x1", "y1", "x2", "y2"}
	return e.Extension{ID: "nonameawagraph", Blocks: []e.Block{
		{Opcode: "nonameawagraph_ray_direction2", Kind: "reporter", Inputs: points, Handler: func(c *e.Context) (e.Result, error) {
			dx, dy := c.Number("x2")-c.Number("x1"), c.Number("y2")-c.Number("y1")
			return e.Result{Value: math.Atan2(dx, dy) * 180 / math.Pi}, nil
		}},
		{Opcode: "nonameawagraph_ray_direction", Kind: "reporter", Inputs: points, Handler: func(c *e.Context) (e.Result, error) {
			dx, dy := c.Number("x2")-c.Number("x1"), c.Number("y2")-c.Number("y1")
			angle := 0.0
			if dx != 0 || dy != 0 {
				angle = 180 / math.Pi * math.Atan(dx/dy)
				if dy < 0 {
					angle += 180
				}
			}
			return e.Result{Value: angle}, nil
		}},
		{Opcode: "nonameawagraph_line_section", Kind: "reporter", Inputs: points, Handler: func(c *e.Context) (e.Result, error) {
			dx, dy := c.Number("x1")-c.Number("x2"), c.Number("y1")-c.Number("y2")
			return e.Result{Value: math.Sqrt(dx*dx + dy*dy)}, nil
		}},
		{Opcode: "nonameawagraph_pi", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: math.Pi}, nil }},
	}}
}
