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

	x := e.Extension{ID: "theshovelcanvaseffects"}
	for _, key := range []string{"EFFECTMENU", "EFFECTGETMENU", "RENDERMODE", "borderTypes"} {
		x.Blocks = append(x.Blocks, menu("theshovelcanvaseffects_menu_"+key, key))
	}
	x.Blocks = append(x.Blocks, rep("theshovelcanvaseffects_geteffect", []string{"EFFECT"}, func(c *e.Context) (e.Result, error) {
		key := c.String("EFFECT")
		if key == "resize rendering mode" {
			v, err := c.Call("canvas.get", "nearest")
			s := "default"
			if e.Bool(v) {
				s = "pixelated"
			}
			return e.Result{Value: s}, err
		}
		if !validEffect(key) && key != "border style" && key != "border color" && key != "background" {
			return e.Result{Value: ""}, nil
		}
		return call(c, "canvas.get", key)
	}))
	for _, op := range []string{"seteffect", "changeEffect"} {
		op := op
		x.Blocks = append(x.Blocks, cmd("theshovelcanvaseffects_"+op, []string{"EFFECT", "NUMBER"}, func(c *e.Context) (e.Result, error) {
			if !validEffect(c.String("EFFECT")) {
				return e.Result{}, nil
			}
			keys := []string{c.String("EFFECT")}
			if keys[0] == "scale" {
				keys = []string{"scale X", "scale Y"}
			}
			for _, key := range keys {
				v := c.Number("NUMBER")
				if op == "changeEffect" {
					old, err := c.Call("canvas.get", key)
					if err != nil {
						return e.Result{}, err
					}
					v += e.Number(old)
				}
				if _, err := c.Call("canvas.set", key, v); err != nil {
					return e.Result{}, err
				}
			}
			return e.Result{}, nil
		}))
	}
	x.Blocks = append(x.Blocks, cmd("theshovelcanvaseffects_cleareffects", nil, func(c *e.Context) (e.Result, error) {
		for _, key := range []string{"blur", "contrast", "saturation", "color shift", "brightness", "invert", "sepia", "transparency", "scale X", "scale Y", "skew X", "skew Y", "offset X", "offset Y", "rotation", "border radius", "border width"} {
			n := float64(0)
			if key == "contrast" || key == "saturation" || key == "brightness" || key == "scale X" || key == "scale Y" {
				n = 100
			}
			if _, err := c.Call("canvas.set", key, n); err != nil {
				return e.Result{}, err
			}
		}
		for k, v := range map[string]any{"nearest": false, "border style": "solid", "border color": "#000000", "background": "transparent"} {
			if _, err := c.Call("canvas.set", k, v); err != nil {
				return e.Result{}, err
			}
		}
		return e.Result{}, nil
	}),
		cmd("theshovelcanvaseffects_setrendermode", []string{"EFFECT"}, func(c *e.Context) (e.Result, error) {
			return call(c, "canvas.set", "nearest", c.String("EFFECT") == "pixelated")
		}),
		cmd("theshovelcanvaseffects_renderscale", []string{"X", "Y"}, func(c *e.Context) (e.Result, error) {
			if _, err := c.Call("canvas.set", "renderWidth", c.Number("X")); err != nil {
				return e.Result{}, err
			}
			return call(c, "canvas.set", "renderHeight", c.Number("Y"))
		}),
		cmd("theshovelcanvaseffects_setBorder", []string{"WIDTH", "STYLE", "COLOR1", "COLOR2"}, func(c *e.Context) (e.Result, error) {
			for k, v := range map[string]any{"border width": c.Number("WIDTH"), "border style": c.String("STYLE"), "border color": c.String("COLOR1"), "background": c.String("COLOR2")} {
				if _, err := c.Call("canvas.set", k, v); err != nil {
					return e.Result{}, err
				}
			}
			return e.Result{}, nil
		}))
	return x
}

func validEffect(key string) bool {
	switch key {
	case "blur", "contrast", "saturation", "color shift", "brightness", "invert", "sepia", "transparency", "scale", "scale X", "scale Y", "skew X", "skew Y", "offset X", "offset Y", "rotation", "border radius", "border width":
		return true
	}
	return false
}
