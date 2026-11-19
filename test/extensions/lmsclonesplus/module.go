// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
)

func New() e.Extension {
	return e.Extension{ID: "lmsclonesplus", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "lmsclonesplus_isClone", Kind: "reporter", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("target.get", "cloneId")
			return e.Result{Value: e.Number(v) != 0}, err
		}},
		{Opcode: "lmsclonesplus_deleteClonesInSprite", Kind: "command", Inputs: []string{"INPUT"}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("target.list")
			if err != nil {
				return e.Result{}, err
			}
			targets := v.([]any)
			source := float64(-1)
			for _, item := range targets {
				candidate := item.(map[string]any)
				if e.String(candidate["name"]) == c.String("INPUT") && e.Number(candidate["cloneId"]) == 0 && !e.Bool(candidate["isStage"]) {
					source = e.Number(candidate["source"])
					break
				}
			}
			for i := len(targets) - 1; i >= 0; i-- {
				t := targets[i].(map[string]any)
				if e.Number(t["source"]) == source && e.Number(t["cloneId"]) != 0 {
					if _, err := c.Call("target.delete", t["id"]); err != nil {
						return e.Result{}, err
					}
				}
			}
			return e.Result{}, nil
		}},
		{Opcode: "lmsclonesplus_menu_spriteMenu", Kind: "reporter", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: c.Field("spriteMenu")}, nil
		}},
	}}
}
