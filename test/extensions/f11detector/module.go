// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
)

func New() e.Extension {
	return e.Extension{ID: "f11detector", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "f11detector_isBrowserFullscreen", Kind: "boolean", Text: "F11 전체화면인가요?", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("host.fullscreen")
			return e.Result{Value: v}, err
		}},
	}}
}
