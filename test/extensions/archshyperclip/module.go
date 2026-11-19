// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import e "gowarp/core/extension"

func call(c *e.Context, op string, args ...any) (e.Result, error) {
	v, err := c.Call(op, args...)
	return e.Result{Value: v}, err
}
func cmd(op string, inputs []string, fn e.Handler) e.Block {
	return e.Block{Opcode: "archshyperclip_" + op, Kind: "command", Inputs: inputs, Handler: fn}
}
func New() e.Extension {
	x := e.Extension{ID: "archshyperclip", Events: map[string]e.Handler{"cancel": func(c *e.Context) (e.Result, error) { return call(c, "graphics.resetClipping") }}}
	x.Blocks = []e.Block{
		cmd("setMode", []string{"SW"}, func(c *e.Context) (e.Result, error) {
			v := c.String("SW")
			return call(c, "graphics.maskMode", v == "true" || v == "ON")
		}),
		cmd("setTarget", []string{"SW"}, func(c *e.Context) (e.Result, error) { return call(c, "graphics.maskTarget", c.String("SW")) }),
		{Opcode: "archshyperclip_getMode", Kind: "boolean", Handler: func(c *e.Context) (e.Result, error) { return call(c, "graphics.maskMode") }},
		{Opcode: "archshyperclip_getTarget", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) { return call(c, "graphics.maskTarget") }},
	}
	for _, key := range []string{"onOff", "spriteMenu"} {
		x.Blocks = append(x.Blocks, e.Block{Opcode: "archshyperclip_menu_" + key, Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field(key)}, nil }})
	}
	return x
}
