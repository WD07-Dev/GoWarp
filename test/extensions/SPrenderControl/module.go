// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
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
	return e.Extension{ID: "SPrenderControl", Blocks: []e.Block{menu("SPrenderControl_menu_TARGETS", "TARGETS"), rep("SPrenderControl_getID", []string{"TARGET"}, func(c *e.Context) (e.Result, error) {
		return call(c, "graphics.targetID", c.Args["TARGET"])
	}), cmd("SPrenderControl_setQuality", []string{"ID", "NUM"}, func(c *e.Context) (e.Result, error) {
		return call(c, "graphics.quality", c.Args["ID"], math.Max(1, math.Min(100, math.Floor(c.Number("NUM")+.5))))
	})}}
}
