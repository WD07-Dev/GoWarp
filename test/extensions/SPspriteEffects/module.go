// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
)

func cmd(op string, inputs []string, fn e.Handler) e.Block {
	return e.Block{Opcode: op, Kind: "command", Inputs: inputs, Handler: fn}
}

func New() e.Extension {
	return e.Extension{ID: "SPspriteEffects", Blocks: []e.Block{cmd("SPspriteEffects_removeAllFilters", nil, func(c *e.Context) (e.Result, error) {
		c.State["filters"] = nil
		return e.Result{}, nil
	})}}
}
