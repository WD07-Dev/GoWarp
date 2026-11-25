// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"strings"
)

func New() e.Extension {
	return e.Extension{ID: "fKey", Blocks: []e.Block{
		{Opcode: "fKey_isFKeyPressed", Kind: "boolean", Text: "[KEY] key pressed?", Inputs: []string{"KEY"}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("input.key", strings.ToUpper(c.String("KEY")))
			return e.Result{Value: v}, err
		}},
		{Opcode: "fKey_menu_fKeys", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field("fKeys")}, nil }},
	}}
}
