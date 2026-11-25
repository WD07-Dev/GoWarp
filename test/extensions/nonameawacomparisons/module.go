package extension

import e "gowarp/core/extension"

func New() e.Extension {
	return e.Extension{ID: "nonameawacomparisons", Blocks: []e.Block{
		{Opcode: "nonameawacomparisons_equalOrGreater", Text: "[a] ≥ [b]", Kind: "boolean", Inputs: []string{"a", "b"}, BorrowContext: true,
			Handler: func(c *e.Context) (e.Result, error) {
				return e.Result{Value: e.Compare(c.Args["a"], c.Args["b"]) >= 0}, nil
			}},
		{Opcode: "nonameawacomparisons_equalOrLess", Text: "[a] ≤ [b]", Kind: "boolean", Inputs: []string{"a", "b"}, BorrowContext: true,
			Handler: func(c *e.Context) (e.Result, error) {
				return e.Result{Value: e.Compare(c.Args["a"], c.Args["b"]) <= 0}, nil
			}},
	}}
}
