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
func rep(op string, inputs []string, fn e.Handler) e.Block {
	return e.Block{Opcode: op, Kind: "reporter", Inputs: inputs, Handler: fn}
}
func menu(op, key string) e.Block {
	return rep(op, nil, func(c *e.Context) (e.Result, error) {
		return e.Result{Value: c.Field(key)}, nil
	})
}

// The original filter chain is independent of Scratch effects. This project's
// only filter operation is clearing that chain; no filter creation is registered.
func New() e.Extension {
	return e.Extension{ID: "SPspriteEffects", Blocks: []e.Block{cmd("SPspriteEffects_removeAllFilters", nil, func(c *e.Context) (e.Result, error) {
		c.State["filters"] = nil
		return e.Result{}, nil
	})}}
}
