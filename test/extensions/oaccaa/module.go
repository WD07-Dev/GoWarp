// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"fmt"
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
func New() e.Extension {
	return e.Extension{ID: "oaccaa", Blocks: []e.Block{menu("oaccaa_menu_setMode_mode_Menu", "setMode_mode_Menu"), cmd("oaccaa_setMode", []string{"mode"}, func(c *e.Context) (e.Result, error) {
		m := c.String("mode")
		if m != "normal" && m != "no_aa" {
			return e.Result{}, fmt.Errorf("unsupported pen draw mode %q", m)
		}
		return call(c, "graphics.penAntialias", m == "normal")
	})}}
}
