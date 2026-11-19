// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
)

func New() e.Extension {
	return e.Extension{ID: "koreandetector", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "koreandetector_isKorean", Kind: "reporter", Inputs: []string{"TEXT"}, Handler: func(c *e.Context) (e.Result, error) {
			for _, ch := range c.String("TEXT") {
				if ch >= 'ㄱ' && ch <= 'ㅣ' || ch >= '가' && ch <= '힣' {
					return e.Result{Value: true}, nil
				}
			}
			return e.Result{Value: false}, nil
		}},
	}}
}
