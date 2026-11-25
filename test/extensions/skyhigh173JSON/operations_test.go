package extension

import (
	e "gowarp/core/extension"
	"math"
	"reflect"
	"testing"
)

func TestArrayLengthRejectsExcessiveAllocation(t *testing.T) {
	for _, block := range New().Blocks {
		if block.Opcode != "skyhigh173JSON_json_array_setlen" {
			continue
		}
		_, err := block.Handler(&e.Context{Args: map[string]any{"json": "[]", "len": 1_000_001.0}})
		if err == nil {
			t.Fatal("oversized array did not report an error")
		}
		return
	}
	t.Fatal("missing array length block")
}

func TestAdditionalJSONOperations(t *testing.T) {
	for _, tc := range []struct {
		op   string
		args map[string]any
		want any
	}{
		{"json_get_all", map[string]any{"json": `{"b":null,"2":"two","a":[1]}`, "Stype": "keys"}, `["2","b","a"]`},
		{"json_get_all", map[string]any{"json": `{"b":null,"a":[1]}`, "Stype": "values"}, `["",[1]]`},
		{"json_get_all", map[string]any{"json": `{"b":null,"a":[1]}`, "Stype": "datas"}, `[["b",""],["a",[1]]]`},
		{"json_has_key", map[string]any{"json": `{"nil":null}`, "key": "nil"}, true},
		{"json_has_key", map[string]any{"json": `[]`, "key": "length"}, true},
		{"json_has_key", map[string]any{"json": `"text"`, "key": "0"}, false},
		{"json_equal", map[string]any{"json1": `{"a":1,"b":2}`, "json2": `{"b":2,"a":1}`, "equal": "="}, true},
		{"json_equal", map[string]any{"json1": `{"a":[]}`, "json2": `{"a":[]}`, "equal": "="}, false},
		{"json_equal", map[string]any{"json1": `[1]`, "json2": `["1"]`, "equal": "≠"}, true},
		{"json_delete", map[string]any{"json": `{"a":1,"b":2}`, "item": "a"}, `{"b":2}`},
		{"json_delete", map[string]any{"json": `[1,2]`, "item": "0"}, `[null,2]`},
		{"json_array_insert", map[string]any{"json": `[1,2,3]`, "pos": 0.0, "item": "x"}, `[1,2,"x",3]`},
		{"json_array_insert", map[string]any{"json": `[1]`, "pos": math.Inf(1), "item": `{}`}, `[1,{}]`},
		{"json_array_delete", map[string]any{"json": `[1,2,3]`, "item": 0.0}, `[1,2]`},
		{"json_array_remove_all", map[string]any{"json": `[1,"1",1]`, "item": 1.0}, `["1"]`},
		{"json_array_remove_all", map[string]any{"json": `[{},{}]`, "item": `{}`}, `[{},{}]`},
		{"json_array_fromto", map[string]any{"json": `[1,2,3,4]`, "item": -2.0, "item2": -1.0}, `[2,3]`},
		{"json_array_fromto", map[string]any{"json": `[1,2,3]`, "item": 3.0, "item2": 1.0}, `[]`},
		{"json_array_reverse", map[string]any{"json": `[1,"x",null]`}, `[null,"x",1]`},
		{"json_array_flat", map[string]any{"json": `[1,[2,[3]]]`, "depth": 1.0}, `[1,2,[3]]`},
		{"json_array_flat", map[string]any{"json": `[1,[2,[3]]]`, "depth": math.Inf(1)}, `[1,2,3]`},
		{"json_array_create", map[string]any{"text": "a::b:", "d": ":"}, `["a","","b",""]`},
		{"json_array_join", map[string]any{"json": `[1,null,[2,3],{}]`, "d": "|"}, `1||2,3|[object Object]`},
		{"json_array_filter", map[string]any{"json": `[{"a":1},{"b":2},{"a":null}]`, "key": "a"}, `[1,null,null]`},
		{"json_array_filter", map[string]any{"json": `[null]`, "key": "a"}, ""},
		{"json_array_setlen", map[string]any{"json": `[1]`, "len": 3.0}, `[1,null,null]`},
		{"json_array_setlen", map[string]any{"json": `[1]`, "len": -1.0}, ""},
		{"json_array_setlen", map[string]any{"json": `[1]`, "len": 1.5}, ""},
		{"json_array_setlen", map[string]any{"json": `{}`, "len": "3"}, `{"length":"3"}`},
		{"json_array_sort", map[string]any{"list": `["10",2,"A","b"]`, "order": "descending"}, `["b","A","10",2]`},
		{"json_array_analysis", map[string]any{"list": `[1,2,"3","invalid"]`, "analysis": "sum"}, 6.0},
		{"json_array_analysis", map[string]any{"list": `[1,2,3,4]`, "analysis": "average"}, 2.5},
		{"json_array_analysis", map[string]any{"list": `[4,2,3,1]`, "analysis": "median"}, 2.5},
		{"json_array_analysis", map[string]any{"list": `[2,1,2,1]`, "analysis": "mode"}, 2.0},
		{"json_array_analysis", map[string]any{"list": `[1,2,3]`, "analysis": "variance"}, 2.0 / 3},
		{"json_array_analysis", map[string]any{"list": `[]`, "analysis": "maximum"}, math.Inf(-1)},
		{"json_array_analysis", map[string]any{"list": `[]`, "analysis": "minimum"}, math.Inf(1)},
		{"json_array_analysis", map[string]any{"list": `{}`, "analysis": "sum"}, 0.0},
	} {
		t.Run(tc.op, func(t *testing.T) {
			got := call(t, tc.op, tc.args, nil).Value
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestJSONReporterMetadata(t *testing.T) {
	for _, b := range New().Blocks {
		if b.Opcode == "skyhigh173JSON_json_has_value" && b.Kind != "boolean" {
			t.Fatal("contains value is not boolean")
		}
		if b.Kind != "command" && len(b.Opcode) > len("skyhigh173JSON_menu_") && b.Opcode[:len("skyhigh173JSON_menu_")] != "skyhigh173JSON_menu_" && b.Text == "" {
			t.Fatalf("missing monitor label: %s", b.Opcode)
		}
	}
}
