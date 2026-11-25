package extension

import (
	e "gowarp/core/extension"
	"math"
	"testing"
)

func TestComparisonsPreserveScratchValues(t *testing.T) {
	for _, tc := range []struct {
		a, b          any
		greater, less bool
	}{
		{10, 2, true, false}, {"10", "2", true, false}, {"A", "a", true, true},
		{"z", "B", true, false}, {"", 0, false, true}, {" ", 0, false, true},
		{true, 1, true, true}, {math.Inf(1), math.Inf(1), true, true},
		{3, 3, true, true},
	} {
		for _, op := range []string{"equalOrGreater", "equalOrLess"} {
			found := false
			for _, block := range New().Blocks {
				if block.Opcode != "nonameawacomparisons_"+op {
					continue
				}
				found = true
				result, err := block.Handler(&e.Context{Args: map[string]any{"a": tc.a, "b": tc.b}})
				want := tc.greater
				if op == "equalOrLess" {
					want = tc.less
				}
				if err != nil || result.Value != want {
					t.Fatalf("%s(%v,%v)=%v, %v; want %v", op, tc.a, tc.b, result.Value, err, want)
				}
			}
			if !found {
				t.Fatalf("missing block %s", op)
			}
		}
	}
}
