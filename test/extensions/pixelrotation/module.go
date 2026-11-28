// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
)

func call(c *e.Context, op string, args ...any) (e.Result, error) {
	v, err := c.Call(op, args...)
	return e.Result{Value: v}, err
}
func cmd(op string, inputs []string, fn e.Handler) e.Block {
	return e.Block{Opcode: op, Kind: "command", Inputs: inputs, Handler: fn}
}
func New() e.Extension {
	return e.Extension{ID: "pixelrotation", Blocks: []e.Block{cmd("pixelrotation_disableSmoothing", nil, func(c *e.Context) (e.Result, error) {
		return call(c, "canvas.set", "nearest", true)
	}), cmd("pixelrotation_enableSmoothing", nil, func(c *e.Context) (e.Result, error) {
		return call(c, "canvas.set", "nearest", false)
	})}}
}
