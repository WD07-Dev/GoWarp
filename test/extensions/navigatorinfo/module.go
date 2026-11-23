// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
)

func New() e.Extension {
	return e.Extension{ID: "navigatorinfo", Blocks: []e.Block{
		{Opcode: "navigatorinfo_getMemory", Kind: "reporter", Handler: func(*e.Context) (e.Result, error) {
			return e.Result{Value: "Unsupported"}, nil
		}},
	}}
}
