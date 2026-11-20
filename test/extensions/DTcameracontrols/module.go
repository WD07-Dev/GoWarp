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
func New() e.Extension {

	x := e.Extension{ID: "DTcameracontrols", Events: map[string]e.Handler{"load": func(c *e.Context) (e.Result, error) {
		if _, err := c.Call("options.set", "fencing", false); err != nil {
			return e.Result{}, err
		}
		return call(c, "options.set", "interpolation", false)
	}}}
	for _, entry := range []struct{ name, key string }{{"X", "x"}, {"Y", "y"}, {"Zoom", "zoom"}, {"Direction", "direction"}, {"Col", "color"}} {
		entry := entry
		x.Blocks = append(x.Blocks, rep("DTcameracontrols_get"+entry.name, nil, func(c *e.Context) (e.Result, error) {
			return call(c, "camera.get", entry.key)
		}), cmd("DTcameracontrols_set"+entry.name, []string{"val"}, func(c *e.Context) (e.Result, error) {
			return call(c, "camera.set", entry.key, c.Args["val"])
		}))
		if entry.key != "color" {
			x.Blocks = append(x.Blocks, cmd("DTcameracontrols_change"+entry.name, []string{"val"}, func(c *e.Context) (e.Result, error) {
				v, err := c.Call("camera.get", entry.key)
				if err != nil {
					return e.Result{}, err
				}
				return call(c, "camera.set", entry.key, e.Number(v)+c.Number("val"))
			}))
		}
	}
	x.Blocks = append(x.Blocks, cmd("DTcameracontrols_setBoth", []string{"x", "y"}, func(c *e.Context) (e.Result, error) {
		if _, err := c.Call("camera.set", "x", c.Number("x")); err != nil {
			return e.Result{}, err
		}
		return call(c, "camera.set", "y", c.Number("y"))
	}))
	for _, op := range []string{"rotateCW", "rotateCCW"} {
		op := op
		x.Blocks = append(x.Blocks, cmd("DTcameracontrols_"+op, []string{"val"}, func(c *e.Context) (e.Result, error) {
			v, err := c.Call("camera.get", "direction")
			if err != nil {
				return e.Result{}, err
			}
			n := c.Number("val")
			if op == "rotateCCW" {
				n = -n
			}
			return call(c, "camera.set", "direction", e.Number(v)+n)
		}))
	}
	return x
}
