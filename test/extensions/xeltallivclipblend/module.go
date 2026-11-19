// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import e "gowarp/core/extension"

func call(c *e.Context, op string, args ...any) (e.Result, error) {
	v, err := c.Call(op, args...)
	return e.Result{Value: v}, err
}
func cmd(op string, inputs []string, fn e.Handler) e.Block {
	return e.Block{Opcode: "xeltallivclipblend_" + op, Kind: "command", Inputs: inputs, Handler: fn}
}
func New() e.Extension {
	x := e.Extension{ID: "xeltallivclipblend", Events: map[string]e.Handler{"cancel": func(c *e.Context) (e.Result, error) { return call(c, "graphics.resetClipping") }}}
	x.Blocks = []e.Block{
		cmd("setClipbox", []string{"X1", "Y1", "X2", "Y2"}, func(c *e.Context) (e.Result, error) {
			return call(c, "graphics.clipbox", c.Number("X1"), c.Number("Y1"), c.Number("X2"), c.Number("Y2"))
		}),
		cmd("clearClipbox", nil, func(c *e.Context) (e.Result, error) { return call(c, "graphics.clipbox") }),
		cmd("setBlend2", []string{"BLENDMODE"}, func(c *e.Context) (e.Result, error) { return call(c, "graphics.blend", c.String("BLENDMODE")) }),
		{Opcode: "xeltallivclipblend_menu_blends2", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field("blends2")}, nil }},
	}
	return x
}
