// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
)

func New() e.Extension {
	return e.Extension{ID: "nkmoremotion", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "nkmoremotion_directionto2", Kind: "reporter", Inputs: []string{"X", "Y"}, Handler: func(c *e.Context) (e.Result, error) {
			x, err := c.Call("target.get", "x")
			if err != nil {
				return e.Result{}, err
			}
			y, err := c.Call("target.get", "y")
			if err != nil {
				return e.Result{}, err
			}
			return e.Result{Value: math.Atan2(c.Number("X")-e.Number(x), c.Number("Y")-e.Number(y)) * 180 / math.Pi}, nil
		}},
	}}
}
