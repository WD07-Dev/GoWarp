package extension

import (
	e "gowarp/core/extension"
	"testing"
)

func TestUtilitySemantics(t *testing.T) {
	cases := []struct {
		op   string
		a    map[string]any
		want any
	}{
		{"itemOfFromString", map[string]any{"INPUTA": 2.0, "INPUTB": "a,,c", "INPUTC": ","}, ""},
		{"itemOfFromString", map[string]any{"INPUTA": 1.5, "INPUTB": "a,b", "INPUTC": ","}, ""},
		{"lettersToOf", map[string]any{"STRING": "abcdef", "INPUTA": -2.0, "INPUTB": 5.0}, "de"},
		{"stringIfElse", map[string]any{"BOOLEAN": false, "INPUTA": "x", "INPUTB": 17.0}, 17.0},
		{"trueFalseBoolean", map[string]any{"TRUEFALSE": "true"}, true},
	}
	for _, tt := range cases {
		found := false
		for _, b := range New().Blocks {
			if b.Opcode == "lmsutilsblocks_"+tt.op {
				found = true
				r, err := b.Handler(&e.Context{Args: tt.a})
				if err != nil || r.Value != tt.want {
					t.Fatalf("%s: %#v %v", tt.op, r, err)
				}
			}
		}
		if !found {
			t.Fatalf("missing %s", tt.op)
		}
	}
}
