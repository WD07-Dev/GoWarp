// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
)

func New() e.Extension {
	return e.Extension{ID: "SPmbpCST", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "SPmbpCST_$-,.___SPmbpCST_TEMP_BLOCK", Kind: "command", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{}, nil
		}},
	}}
}
