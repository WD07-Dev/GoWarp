// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import e "gowarp/core/extension"

func New() e.Extension {
	return e.Extension{ID: "lmsAssets", Blocks: []e.Block{
		{Opcode: "lmsAssets_addCostume", Kind: "command", Inputs: []string{"URL", "NAME"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("assets.addCostume", c.String("URL"), c.String("NAME"))
			return e.Result{}, err
		}},
		{Opcode: "lmsAssets_deleteCostume", Kind: "command", Inputs: []string{"COSTUME"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("assets.deleteCostume", c.String("COSTUME"))
			return e.Result{}, err
		}},
	}}
}
