// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
)

func New() e.Extension {
	return e.Extension{ID: "percentcalc", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "percentcalc_calculatePercent", Kind: "reporter", Inputs: []string{"n", "n2"}, Handler: func(c *e.Context) (e.Result, error) {
			if c.Number("n") == 0 {
				return e.Result{Value: float64(0)}, nil
			}
			return e.Result{Value: c.Number("n2") / c.Number("n") * 100}, nil
		}},
	}}
}
