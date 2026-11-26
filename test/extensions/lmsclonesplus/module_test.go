package extension

import (
	"fmt"
	"sort"
	"testing"

	e "gowarp/core/extension"
)

type cloneHost struct {
	targets   []any
	variables map[string]map[string]any
	touching  map[string]bool
	checked   []string
}

func (h *cloneHost) Call(op string, args ...any) (any, error) {
	switch op {
	case "target.get":
		if args[0] == "source" {
			return 1, nil
		}
	case "target.list":
		return append([]any{}, h.targets...), nil
	case "variable.exists", "variable.get":
		id, ref := args[0].(string), args[1].(string)
		value, found := h.variables[ref][id]
		if !found {
			value, found = h.variables["_stage_"][id]
		}
		if op == "variable.exists" {
			return found, nil
		}
		return value, nil
	case "target.query":
		q := args[0].(e.TargetQuery)
		if q.Source != "_myself_" {
			return nil, fmt.Errorf("unexpected query family %q", q.Source)
		}
		targets := append([]any(nil), h.targets...)
		sort.SliceStable(targets, func(i, j int) bool {
			return e.Number(targets[i].(map[string]any)["cloneId"]) < e.Number(targets[j].(map[string]any)["cloneId"])
		})
		var ids []string
		for _, item := range targets {
			target := item.(map[string]any)
			if e.Number(target["source"]) != 1 || (q.ClonesOnly && e.Number(target["cloneId"]) == 0) {
				continue
			}
			ref := target["id"].(string)
			value, found := h.variables[ref][q.Variable]
			if !found {
				value, found = h.variables["_stage_"][q.Variable]
			}
			if q.Variable != "" && (!found || e.Compare(value, q.Value) != 0) {
				continue
			}
			ids = append(ids, ref)
			if q.First {
				break
			}
		}
		return ids, nil
	case "target.touchingTargets":
		ids := args[0].([]string)
		h.checked = append(h.checked, ids...)
		for _, id := range ids {
			if h.touching[id] {
				return true, nil
			}
		}
		return false, nil
	}
	return nil, fmt.Errorf("unexpected operation %s %v", op, args)
}

func TestTouchingCloneOnlyQueriesMatchingFamilyTargets(t *testing.T) {
	h := &cloneHost{
		targets: []any{cloneRecord("other", 2, 1), cloneRecord("wrong", 1, 2), cloneRecord("match", 1, 3), cloneRecord("main", 1, 0)},
		variables: map[string]map[string]any{
			"other": {"filter": 2}, "wrong": {"filter": 3}, "match": {"filter": "02"}, "main": {"filter": 2},
		},
		touching: map[string]bool{"other": true, "wrong": true, "main": true},
	}
	c := &e.Context{Host: h, Fields: map[string]string{"INPUTA": "filter"}, Args: map[string]any{"INPUTB": 2}}
	result, err := touchingCloneWithVar(c)
	if err != nil || result.Value != true {
		t.Fatal(result, err)
	}
	if len(h.checked) != 2 || h.checked[0] != "main" || h.checked[1] != "match" {
		t.Fatal("wrong collision candidates", h.checked)
	}
	h.checked = nil
	h.touching["main"] = false
	result, err = touchingCloneWithVar(c)
	if err != nil || result.Value != false {
		t.Fatal(result, err)
	}
}

func cloneRecord(id string, source, cloneID int) any {
	return map[string]any{"id": id, "source": source, "cloneId": cloneID, "name": "Sprite", "isStage": false}
}

func TestGetVariableOfCloneUsesFirstMatchingFamilyClone(t *testing.T) {
	h := &cloneHost{
		targets: []any{cloneRecord("other", 2, 1), cloneRecord("later", 1, 3), cloneRecord("main", 1, 0), cloneRecord("first", 1, 2)},
		variables: map[string]map[string]any{
			"other": {"filter": "2", "value": "wrong family"},
			"main":  {"filter": "2", "value": "original"},
			"later": {"filter": 2, "value": "later clone"},
			"first": {"filter": "02", "value": "first clone"},
		},
	}
	var block e.Block
	for _, b := range New().Blocks {
		if b.Opcode == "lmsclonesplus_getVariableOfClone" {
			block = b
		}
	}
	if block.Handler == nil || len(block.Inputs) != 1 || block.Inputs[0] != "INPUTC" {
		t.Fatal("variable dropdowns must be fields", block)
	}
	c := &e.Context{Host: h, Fields: map[string]string{"INPUTA": "value", "INPUTB": "filter"}, Args: map[string]any{"INPUTC": 2, "INPUTA": "wrong", "INPUTB": "wrong"}}
	result, err := block.Handler(c)
	if err != nil || result.Value != "first clone" {
		t.Fatal(result, err)
	}
	c.Args["INPUTC"] = "absent"
	result, err = block.Handler(c)
	if err != nil || result.Value != "" {
		t.Fatal(result, err)
	}
}

func TestGetVariableOfCloneStageFallbackAndMissingVariables(t *testing.T) {
	h := &cloneHost{targets: []any{cloneRecord("clone", 1, 1)}, variables: map[string]map[string]any{
		"clone":   {"filter": "hello"},
		"_stage_": {"value": "global value"},
	}}
	c := &e.Context{Host: h, Fields: map[string]string{"INPUTA": "value", "INPUTB": "filter"}, Args: map[string]any{"INPUTC": "HELLO"}}
	result, err := getVariableOfClone(c)
	if err != nil || result.Value != "global value" {
		t.Fatal(result, err)
	}
	c.Fields["INPUTB"] = "missing"
	result, err = getVariableOfClone(c)
	if err != nil || result.Value != "" {
		t.Fatal(result, err)
	}
}
