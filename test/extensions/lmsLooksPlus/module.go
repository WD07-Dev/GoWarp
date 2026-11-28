// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
)

func New() e.Extension {
	return e.Extension{ID: "lmsLooksPlus", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "lmsLooksPlus_hideSprite", Kind: "command", Inputs: []string{"TARGET"}, Handler: func(c *e.Context) (e.Result, error) {
			target, err := sprite(c)
			if err != nil || target == nil {
				return e.Result{}, err
			}
			_, err = c.Call("target.set", "visible", false, target)
			return e.Result{}, err
		}},
		{Opcode: "lmsLooksPlus_menu_spriteMenu", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: c.Field("spriteMenu")}, nil
		}},
		{Opcode: "lmsLooksPlus_snapshotStage", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("assets.snapshot")
			return e.Result{Value: v}, err
		}},
		{Opcode: "lmsLooksPlus_spriteLayerNumber", Kind: "reporter", Inputs: []string{"TARGET"}, Handler: func(c *e.Context) (e.Result, error) {
			target, err := sprite(c)
			if err != nil {
				return e.Result{}, err
			}
			if target == nil {
				return e.Result{Value: float64(0)}, nil
			}
			v, err := c.Call("target.get", "layer", target)
			return e.Result{Value: v}, err
		}},
		{Opcode: "lmsLooksPlus_setLayerTo", Kind: "command", Inputs: []string{"TARGET", "LAYER"}, Handler: func(c *e.Context) (e.Result, error) {
			target, err := sprite(c)
			if err != nil {
				return e.Result{}, err
			}
			if target == nil || !finite(c.Number("LAYER")) {
				return e.Result{}, nil
			}
			_, err = c.Call("target.set", "layer", c.Number("LAYER"), target)
			return e.Result{}, err
		}},

		{Opcode: "lmsLooksPlus_costumeAttribute", Kind: "reporter", Inputs: []string{"COSTUME"}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("target.costumes")
			if err != nil {
				return e.Result{}, err
			}
			costumes := v.([]any)
			index := -1
			if n, ok := c.Args["COSTUME"].(float64); ok {
				if len(costumes) > 0 {
					n = math.Floor(n - 1 + .5)
					if !finite(n) {
						n = 0
					}
					length := float64(len(costumes))
					index = int(math.Mod(math.Mod(n, length)+length, length))
				}
			} else {
				for i, v := range costumes {
					if e.String(v.(map[string]any)["name"]) == c.String("COSTUME") {
						index = i
						break
					}
				}
			}
			if index < 0 {
				return e.Result{Value: float64(0)}, nil
			}
			attribute := c.Field("ATTRIBUTE")
			switch attribute {
			case "width", "height":
				v, err := c.Call("target.costumeSize", float64(index))
				if err != nil {
					return e.Result{}, err
				}
				size := v.([]float64)
				if attribute == "height" {
					return e.Result{Value: math.Ceil(size[1])}, nil
				}
				return e.Result{Value: math.Ceil(size[0])}, nil
			case "rotationCenterX", "rotationCenterY", "format":
				return e.Result{Value: costumes[index].(map[string]any)[attribute]}, nil
			}
			return e.Result{Value: ""}, nil
		}},
	}}
}
func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func sprite(c *e.Context) (any, error) {
	name, err := c.Call("target.get", "name")
	if err != nil {
		return nil, err
	}
	if e.String(name) == c.String("TARGET") {
		return "_myself_", nil
	}
	list, err := c.Call("target.list")
	if err != nil {
		return nil, err
	}
	for _, item := range list.([]any) {
		t := item.(map[string]any)
		if !e.Bool(t["isStage"]) && e.Number(t["cloneId"]) == 0 && e.String(t["name"]) == c.String("TARGET") {
			return t["id"], nil
		}
	}
	return nil, nil
}
