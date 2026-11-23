// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
)

func New() e.Extension {
	position := []string{"X", "Y", "PX", "PY", "ANGLE"}
	velocity := []string{"VX", "VY"}
	points := []string{"SX", "SY", "EX", "EY"}
	return e.Extension{ID: "pivotmove", Blocks: []e.Block{
		{Opcode: "pivotmove_movePivotTo", Kind: "command", Inputs: position, Handler: movePivotTo},
		{Opcode: "pivotmove_finalX", Kind: "reporter", Inputs: position, Handler: func(c *e.Context) (e.Result, error) { x, _ := calculatePosition(c); return e.Result{Value: x}, nil }},
		{Opcode: "pivotmove_finalY", Kind: "reporter", Inputs: position, Handler: func(c *e.Context) (e.Result, error) { _, y := calculatePosition(c); return e.Result{Value: y}, nil }},
		{Opcode: "pivotmove_speedFromVelocity", Kind: "reporter", Inputs: velocity, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: math.Hypot(c.Number("VX"), c.Number("VY"))}, nil
		}},
		{Opcode: "pivotmove_directionFromVelocity", Kind: "reporter", Inputs: velocity, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: direction(c.Number("VX"), c.Number("VY"))}, nil
		}},
		{Opcode: "pivotmove_directionBetweenPoints", Kind: "reporter", Inputs: points, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: direction(c.Number("EX")-c.Number("SX"), c.Number("EY")-c.Number("SY"))}, nil
		}},
		{Opcode: "pivotmove_distanceBetweenPoints", Kind: "reporter", Inputs: points, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: math.Hypot(c.Number("EX")-c.Number("SX"), c.Number("EY")-c.Number("SY"))}, nil
		}},
	}}
}

func calculatePosition(c *e.Context) (float64, float64) {
	radians := (90 - c.Number("ANGLE")) * math.Pi / 180
	sin, cos := math.Sincos(radians)
	px, py := c.Number("PX"), c.Number("PY")
	return c.Number("X") - (px*cos - py*sin), c.Number("Y") - (px*sin + py*cos)
}

func direction(x, y float64) float64 {
	if x == 0 && y == 0 {
		return 0
	}
	return math.Atan2(x, y) * 180 / math.Pi
}

func movePivotTo(c *e.Context) (e.Result, error) {
	stage, err := c.Call("target.get", "isStage")
	if err != nil {
		return e.Result{}, err
	}
	if e.Bool(stage) {
		return e.Result{}, nil
	}
	x, y := calculatePosition(c)
	if _, err = c.Call("target.set", "direction", c.Number("ANGLE")); err != nil {
		return e.Result{}, err
	}
	_, err = c.Call("target.setXY", x, y)
	return e.Result{}, err
}
