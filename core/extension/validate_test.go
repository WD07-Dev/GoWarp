package extension

import (
	"strings"
	"testing"
)

func TestValidateDefinitionsAndProjectCoverage(t *testing.T) {
	handler := func(*Context) (Result, error) {
		t.Fatal("validation invoked a handler")
		return Result{}, nil
	}
	valid := Extension{ID: "fresh", Blocks: []Block{{Opcode: "fresh_value", Kind: "reporter", Handler: handler}}, Events: map[string]Handler{"load": handler, "flag": handler}}
	if err := Validate(Bundle{func() Extension { return valid }}, []string{"fresh"}, []string{"fresh_value", "pen_clear", "event_whenflagclicked"}); err != nil {
		t.Fatal(err)
	}
	for name, def := range map[string]Extension{
		"wrong ID":         {ID: "other"},
		"reserved ID":      {ID: "motion"},
		"invalid ID":       {ID: "fresh_bad"},
		"wrong prefix":     {ID: "fresh", Blocks: []Block{{Opcode: "other_value", Kind: "reporter", Handler: handler}}},
		"duplicate opcode": {ID: "fresh", Blocks: []Block{valid.Blocks[0], valid.Blocks[0]}},
		"missing handler":  {ID: "fresh", Blocks: []Block{{Opcode: "fresh_value", Kind: "reporter"}}},
		"invalid kind":     {ID: "fresh", Blocks: []Block{{Opcode: "fresh_value", Kind: "unknown", Handler: handler}}},
		"missing opcode":   {ID: "fresh"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := Validate(Bundle{func() Extension { return def }}, []string{"fresh"}, []string{"fresh_value"}); err == nil {
				t.Fatal("accepted invalid extension metadata")
			}
		})
	}
	if err := Validate(Bundle{func() Extension { return valid }}, []string{"fresh"}, []string{"undeclared_value"}); err == nil || !strings.Contains(err.Error(), "undeclared_value") {
		t.Fatalf("unknown opcode: %v", err)
	}
	if err := Validate(Bundle{func() Extension { return valid }, func() Extension { return valid }}, []string{"fresh", "fresh"}, nil); err == nil {
		t.Fatal("accepted duplicate IDs")
	}
}
