package extension

import (
	e "gowarp/core/extension"
	"testing"
)

func TestDeltaruneJSONOperations(t *testing.T) {
	for _, tt := range []struct {
		op   string
		args map[string]any
		want any
	}{
		{"json_is_valid", map[string]any{"json": " [1,null] \n"}, true},
		{"json_is_valid", map[string]any{"json": "null"}, false},
		{"json_is_valid", map[string]any{"json": "\ufeff[1e1000]\u00a0"}, true},
		{"json_is", map[string]any{"json": "\ufeff[]", "types": "Array"}, false},
		{"json_is", map[string]any{"json": "[1e1000]", "types": "Array"}, true},
		{"json_is_valid", map[string]any{"json": "true"}, false},
		{"json_is_valid", map[string]any{"json": "{} trailing"}, false},
		{"json_is_valid", map[string]any{"json": 123}, false},
		{"json_is", map[string]any{"json": "{}", "types": "Object"}, true},
		{"json_is", map[string]any{"json": "[]", "types": "Object"}, false},
		{"json_is", map[string]any{"json": "[]", "types": "Array"}, true},
		{"json_is", map[string]any{"json": "null", "types": "Object"}, false},
		{"json_jlength", map[string]any{"json": `{"a":1,"b":2}`}, float64(2)},
		{"json_jlength", map[string]any{"json": `"😀"`}, float64(2)},
		{"json_jlength", map[string]any{"json": `null`}, " "},
		{"json_array_concat", map[string]any{"json": `[1,"a"]`, "json2": `[2,[3]]`}, `[1,"a",2,[3]]`},
		{"json_array_concat", map[string]any{"json": `[]`, "json2": `{"b":1,"a":2}`}, `[{"b":1,"a":2}]`},
		{"json_array_concat", map[string]any{"json": `[]`, "json2": `null`}, `[null]`},
		{"json_array_concat", map[string]any{"json": `"a"`, "json2": `[1,null,[2,3]]`}, `"a1,,2,3"`},
		{"json_array_concat", map[string]any{"json": `{}`, "json2": `[]`}, ""},
	} {
		t.Run(tt.op, func(t *testing.T) {
			for _, block := range New().Blocks {
				if block.Opcode == "skyhigh173JSON_"+tt.op {
					got, err := block.Handler(&e.Context{Args: tt.args})
					if err != nil || got.Value != tt.want {
						t.Errorf("got %#v,%v; want %#v", got.Value, err, tt.want)
					}
					return
				}
			}
			t.Fatal("missing block")
		})
	}
}
