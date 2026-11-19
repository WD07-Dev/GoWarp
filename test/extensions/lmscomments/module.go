package extension

import e "gowarp/core/extension"

func New() e.Extension {
	return e.Extension{ID: "lmscomments", Blocks: []e.Block{
		{Opcode: "lmscomments_commentCommand", Kind: "command", Inputs: []string{"COMMENT"}, Handler: func(c *e.Context) (e.Result, error) { return e.Result{}, nil }},
		{Opcode: "lmscomments_commentC", Kind: "branch", Inputs: []string{"COMMENT"}, Handler: func(c *e.Context) (e.Result, error) { return e.Result{Branch: "SUBSTACK"}, nil }},
	}}
}
