package extension

import (
	e "gowarp/core/extension"
	"math"
	"reflect"
	"testing"
)

func TestAdditionalProjectOperations(t *testing.T) {
	for _, tc := range []struct {
		op   string
		args map[string]any
		want any
	}{
		{"truefantommath_not_equal_block", map[string]any{"A": "2", "B": float64(2)}, false},
		{"truefantommath_clamp_block", map[string]any{"A": float64(7), "B": "10", "C": float64(20)}, "10"},
		{"truefantommath_between_or_equal", map[string]any{"A": float64(1), "B": float64(2), "C": float64(3)}, true},
		{"truefantommath_between_or_equal", map[string]any{"A": float64(1), "B": float64(4), "C": float64(3)}, false},
		{"truefantommath_root_block", map[string]any{"A": float64(3), "B": float64(27)}, float64(3)},
		{"truefantommath_exponent_block", map[string]any{"A": float64(2), "B": float64(4)}, float64(16)},
		{"truefantommath_trunc2_block", map[string]any{"A": float64(-1.239), "B": float64(2)}, float64(-1.23)},
	} {
		t.Run(tc.op, func(t *testing.T) {
			for _, b := range New().Blocks {
				if b.Opcode == tc.op {
					got, err := b.Handler(&e.Context{Args: tc.args, State: map[string]any{}})
					equal := reflect.DeepEqual(got.Value, tc.want)
					if a, ok := got.Value.(float64); ok {
						if b, ok := tc.want.(float64); ok {
							equal = math.Abs(a-b) < 1e-12
						}
					}
					if err != nil || !equal {
						t.Fatalf("got %v (%T), %v; want %v (%T)", got.Value, got.Value, err, tc.want, tc.want)
					}
					return
				}
			}
			t.Fatalf("missing opcode %s", tc.op)
		})
	}
}
