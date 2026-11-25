// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"encoding/json"
	e "gowarp/core/extension"
)

func New() e.Extension {
	return e.Extension{ID: "lmsAssets", Blocks: []e.Block{
		{Opcode: "lmsAssets_getAllSprites", Kind: "reporter", Text: "all sprites", Handler: func(c *e.Context) (e.Result, error) {
			value, err := c.Call("target.list")
			if err != nil {
				return e.Result{}, err
			}
			names := []string{}
			for _, item := range value.([]any) {
				target := item.(map[string]any)
				if !e.Bool(target["isStage"]) && e.Number(target["cloneId"]) == 0 {
					names = append(names, e.String(target["name"]))
				}
			}
			data, err := json.Marshal(names)
			return e.Result{Value: string(data)}, err
		}},
		{Opcode: "lmsAssets_getAllCostumes", Kind: "reporter", Text: "all costumes", Handler: func(c *e.Context) (e.Result, error) {
			value, err := c.Call("target.costumes")
			if err != nil {
				return e.Result{}, err
			}
			names := []string{}
			for _, item := range value.([]any) {
				names = append(names, e.String(item.(map[string]any)["name"]))
			}
			data, err := json.Marshal(names)
			return e.Result{Value: string(data)}, err
		}},
		{Opcode: "lmsAssets_getAllSounds", Kind: "reporter", Text: "all sounds", Handler: func(c *e.Context) (e.Result, error) {
			value, err := c.Call("target.sounds")
			if err != nil {
				return e.Result{}, err
			}
			data, err := json.Marshal(value)
			return e.Result{Value: string(data)}, err
		}},
		{Opcode: "lmsAssets_getLoadedExtensions", Kind: "reporter", Text: "loaded extensions", Handler: func(c *e.Context) (e.Result, error) {
			value, err := c.Call("runtime.extensions")
			if err != nil {
				return e.Result{}, err
			}
			data, err := json.Marshal(value)
			return e.Result{Value: string(data)}, err
		}},
		{Opcode: "lmsAssets_getSpriteName", Kind: "reporter", Text: "sprite name", Handler: func(c *e.Context) (e.Result, error) {
			value, err := c.Call("target.get", "name")
			return e.Result{Value: value}, err
		}},
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
