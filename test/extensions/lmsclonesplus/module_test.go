package extension

import (
	"fmt"
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
	case "variable.list":
		out := []any{}
		for id := range h.variables[args[0].(string)] {
			out = append(out, map[string]any{"id": id, "name": "display name is not an ID"})
		}
		return out, nil
	case "variable.get":
		return h.variables[args[1].(string)][args[0].(string)], nil
	case "target.touchingTarget":
		ref := args[0].(string)
		h.checked = append(h.checked, ref)
		return h.touching[ref], nil
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
	if len(h.checked) != 2 || h.checked[0] != "match" || h.checked[1] != "main" {
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
