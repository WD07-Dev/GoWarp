package extension

import (
	e "gowarp/core/extension"
	"testing"
)

func TestJSONConversions(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"mergeObjects", map[string]any{"JSON1": `{"2":"c","0":"a","01":"skip","x":"skip"}`, "JSON2": `{"4":"ㄱ","1":"ㄴ"}`}, `{"0":"a","1":"c","2":"ㄴ","3":"ㄱ"}`},
		{"mergeObjects", map[string]any{"JSON1": `{"9007199254740991":1,"9007199254740992":2,"-1":3,"0":4}`, "JSON2": `{}`}, `{"0":4,"1":1}`},
		{"mergeObjects", map[string]any{"JSON1": `{"0":{"b":1,"2":"two","1":"one","a":2},"0":{"z":3,"a":4}}`, "JSON2": `{"0":[1e1000,9007199254740993,0.000001]}`}, `{"0":{"z":3,"a":4},"1":[null,9007199254740992,0.000001]}`},
		{"mergeObjects", map[string]any{"JSON1": `{"0":{"b":1,"2":"two","1":"one","a":2}}`, "JSON2": `{}`}, `{"0":{"1":"one","2":"two","b":1,"a":2}}`},
		{"mergeObjects", map[string]any{"JSON1": `[]`, "JSON2": `{}`}, `{}`},
		{"mergeObjects", map[string]any{"JSON1": `{"0":1} trailing`, "JSON2": `{}`}, `{}`},
		{"mergeObjects", map[string]any{"JSON1": `null`, "JSON2": `{}`}, `{}`},
		{"mergeObjects", map[string]any{"JSON1": `{}`, "JSON2": `{}`}, `{}`},
		{"arrayToObject", map[string]any{"ARRAY": `[0,1,2,3,4,5,6,7,8,9,10,11]`}, `{"0":0,"1":1,"2":2,"3":3,"4":4,"5":5,"6":6,"7":7,"8":8,"9":9,"10":10,"11":11}`},
		{"objectToArray", map[string]any{"JSON": `{"2":false,"4":{"a":1},"foo":5}`}, `[null,null,false,null,{"a":1}]`},
		{"objectToArray", map[string]any{"JSON": `{"01":1,"-1":2}`}, `[]`},
		{"appendValue", map[string]any{"JSON": `{"5":"a"}`, "VALUE": false}, `{"0":"a","1":"false"}`},
		{"appendValue", map[string]any{"JSON": `{}`, "VALUE": "<>&\u2028\\u003c\\u2028\n"}, "{\"0\":\"<>&\u2028\\\\u003c\\\\u2028\\n\"}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, block := range New().Blocks {
				if block.Opcode == "jsonarrayconverter_"+tt.name {
					result, err := block.Handler(&e.Context{Args: tt.args})
					if err != nil || result.Value != tt.want {
						t.Fatalf("got %q, %v; want %q", result.Value, err, tt.want)
					}
					return
				}
			}
			t.Fatal("missing block")
		})
	}
}
