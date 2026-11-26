// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"sort"
)

func New() e.Extension {
	return e.Extension{ID: "lmsclonesplus", State: map[string]any{}, Events: map[string]e.Handler{"tick": func(c *e.Context) (e.Result, error) {
		_, err := c.Call("events.pollHats", "lmsclonesplus_whenCloneStartsWithVar", "edge")
		return e.Result{}, err
	}}, Blocks: []e.Block{
		{Opcode: "lmsclonesplus_getVariableOfClone", Kind: "reporter", Inputs: []string{"INPUTC"}, Handler: getVariableOfClone},
		{Opcode: "lmsclonesplus_touchingCloneWithVar", Kind: "boolean", Inputs: []string{"INPUTB"}, Handler: touchingCloneWithVar},
		{Opcode: "lmsclonesplus_cloneCount", Kind: "reporter", Text: "clone count", Handler: func(c *e.Context) (e.Result, error) {
			value, err := c.Call("target.list")
			if err != nil {
				return e.Result{}, err
			}
			count := 0.0
			for _, item := range value.([]any) {
				if e.Number(item.(map[string]any)["cloneId"]) != 0 {
					count++
				}
			}
			return e.Result{Value: count}, nil
		}},
		{Opcode: "lmsclonesplus_whenCloneStartsWithVar", Kind: "hat", Inputs: []string{"INPUTB"}, Handler: func(c *e.Context) (e.Result, error) {
			id, err := c.Call("target.get", "cloneId")
			if err != nil || e.Number(id) == 0 {
				return e.Result{Value: false}, err
			}
			value, found, err := variableValue(c, c.Field("INPUTA"), "_myself_")
			return e.Result{Value: found && e.Compare(value, c.Args["INPUTB"]) == 0}, err
		}},
		{Opcode: "lmsclonesplus_deleteCloneWithVar", Kind: "command", Inputs: []string{"INPUTB"}, Handler: func(c *e.Context) (e.Result, error) {
			source, err := c.Call("target.get", "source")
			if err != nil {
				return e.Result{}, err
			}
			records, err := c.Call("target.list")
			if err != nil {
				return e.Result{}, err
			}
			targets := records.([]any)
			sort.SliceStable(targets, func(i, j int) bool {
				return e.Number(targets[i].(map[string]any)["cloneId"]) > e.Number(targets[j].(map[string]any)["cloneId"])
			})
			for _, record := range targets {
				target := record.(map[string]any)
				if e.Number(target["source"]) != e.Number(source) || e.Number(target["cloneId"]) == 0 {
					continue
				}
				value, found, err := variableValue(c, c.Field("INPUTA"), e.String(target["id"]))
				if err != nil {
					return e.Result{}, err
				}
				if found && e.Compare(value, c.Args["INPUTB"]) == 0 {
					if _, err := c.Call("target.delete", target["id"]); err != nil {
						return e.Result{}, err
					}
				}
			}
			return e.Result{}, nil
		}},
		{Opcode: "lmsclonesplus_isClone", Kind: "boolean", Text: "is clone?", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
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

func touchingCloneWithVar(c *e.Context) (e.Result, error) {
	targets, err := c.Call("target.query", e.TargetQuery{
		Source: "_myself_", Variable: c.Field("INPUTA"), Value: c.Args["INPUTB"],
	})
	if err != nil {
		return e.Result{}, err
	}
	ids := targets.([]string)
	if len(ids) == 0 {
		return e.Result{Value: false}, nil
	}
	value, err := c.Call("target.touchingTargets", ids)
	return e.Result{Value: value}, err
}

func getVariableOfClone(c *e.Context) (e.Result, error) {
	targets, err := c.Call("target.query", e.TargetQuery{
		Source: "_myself_", ClonesOnly: true, Variable: c.Field("INPUTB"), Value: c.Args["INPUTC"], First: true,
	})
	if err != nil {
		return e.Result{}, err
	}
	ids := targets.([]string)
	if len(ids) == 0 {
		return e.Result{Value: ""}, nil
	}
	value, found, err := variableValue(c, c.Field("INPUTA"), ids[0])
	if !found {
		value = ""
	}
	return e.Result{Value: value}, err
}

func variableValue(c *e.Context, id, ref string) (any, bool, error) {
	found, err := c.Call("variable.exists", id, ref)
	if err != nil || !e.Bool(found) {
		return nil, false, err
	}
	value, err := c.Call("variable.get", id, ref)
	return value, true, err
}
