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
	x := e.Extension{ID: "OACShaded", Blocks: []e.Block{menu("OACShaded_menu_shadersAndStageALT", "shadersAndStageALT")}}
	for _, op := range []string{"setStageShaderAlt", "setStageShader"} {
		x.Blocks = append(x.Blocks, cmd("OACShaded_"+op, []string{"shader"}, func(c *e.Context) (e.Result, error) {
			return call(c, "graphics.shader.select", c.String("shader"))
		}))
	}
	x.Blocks = append(x.Blocks, e.Block{
		Opcode: "OACShaded_shaderCompiledForSprites", Kind: "boolean", Inputs: []string{"shader"},
		Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: false}, nil
		},
	})
	return x
}
