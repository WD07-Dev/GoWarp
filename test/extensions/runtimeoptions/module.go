// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
)

var optionKeys = map[string]string{
	"turbo mode":         "turbo",
	"interpolation":      "interpolation",
	"remove fencing":     "fencing",
	"remove misc limits": "miscLimits",
	"high quality pen":   "highQualityPen",
}

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
		v, ok := c.Fields[key]
		if !ok {
			v = c.Fields["menu"]
		}
		return e.Result{Value: v}, nil
	})
}
func New() e.Extension {

	x := e.Extension{ID: "runtimeoptions", Blocks: []e.Block{cmd("runtimeoptions_greenFlag", nil, func(c *e.Context) (e.Result, error) {
		return call(c, "runtime.greenFlag")
	}),
		cmd("runtimeoptions_setDimensions", []string{"width", "height"}, func(c *e.Context) (e.Result, error) {
			return call(c, "camera.dimensions", c.Number("width"), c.Number("height"))
		}),
		rep("runtimeoptions_getDimension", []string{"dimension"}, func(c *e.Context) (e.Result, error) {
			key := c.String("dimension")
			if key != "width" && key != "height" {
				return e.Result{Value: float64(0)}, nil
			}
			return call(c, "camera.get", key)
		})}}
	for _, key := range []string{"thing", "enabled", "dimension"} {
		x.Blocks = append(x.Blocks, menu("runtimeoptions_menu_"+key, key))
	}
	for _, entry := range []struct{ op, key, input string }{{"Framerate", "framerate", "fps"}, {"CloneLimit", "cloneLimit", "limit"}, {"Username", "username", "username"}} {
		entry := entry
		x.Blocks = append(x.Blocks, rep("runtimeoptions_get"+entry.op, nil, func(c *e.Context) (e.Result, error) {
			return call(c, "options.get", entry.key)
		}), cmd("runtimeoptions_set"+entry.op, []string{entry.input}, func(c *e.Context) (e.Result, error) {
			return call(c, "options.set", entry.key, c.Args[entry.input])
		}))
	}
	for _, set := range []bool{false, true} {
		set := set
		op, kind := "getEnabled", "reporter"
		inputs := []string{"thing"}
		if set {
			op, kind = "setEnabled", "command"
			inputs = append(inputs, "enabled")
		}
		x.Blocks = append(x.Blocks, e.Block{Opcode: "runtimeoptions_" + op, Kind: kind, Inputs: inputs, Handler: func(c *e.Context) (e.Result, error) {
			name := c.String("thing")
			key := optionKeys[name]
			if key == "" {
				return e.Result{Value: false}, nil
			}
			invert := name == "remove fencing" || name == "remove misc limits"
			if set {
				v := c.Bool("enabled")
				if invert {
					v = !v
				}
				return call(c, "options.set", key, v)
			}
			v, err := c.Call("options.get", key)
			enabled := e.Bool(v)
			if invert {
				enabled = !enabled
			}
			return e.Result{Value: enabled}, err
		}})
	}
	return x
}
