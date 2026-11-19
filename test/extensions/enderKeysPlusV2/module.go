// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
)

func New() e.Extension {
	return e.Extension{ID: "enderKeysPlusV2", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "enderKeysPlusV2_currentKeyPressed", Kind: "reporter", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("input.keys")
			if err != nil {
				return e.Result{}, err
			}
			keys := v.([]string)
			if len(keys) == 0 {
				return e.Result{Value: "None"}, nil
			}
			return e.Result{Value: keys[len(keys)-1]}, nil
		}},
	}}
}
