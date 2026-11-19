// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
)

func New() e.Extension {
	return e.Extension{ID: "framedropdetector", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "framedropdetector_getFps", Kind: "reporter", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("host.frameMilliseconds")
			return e.Result{Value: math.Round(1000 / e.Number(v))}, err
		}},
		{Opcode: "framedropdetector_isDropped", Kind: "reporter", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("host.frameMilliseconds")
			threshold := 33.33
			if v, ok := c.State["threshold"]; ok {
				threshold = e.Number(v)
			}
			return e.Result{Value: e.Number(v) > threshold}, err
		}},
		{Opcode: "framedropdetector_setThreshold", Kind: "command", Inputs: []string{"MS"}, Handler: func(c *e.Context) (e.Result, error) {
			c.State["threshold"] = c.Number("MS")
			return e.Result{}, nil
		}},
	}}
}
