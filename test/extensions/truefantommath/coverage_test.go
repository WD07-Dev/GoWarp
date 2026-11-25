package extension

import (
	e "gowarp/core/extension"
	"math"
	"reflect"
	"testing"
)

// These expected values were captured from the preserved JavaScript extension.
// Tests use embedded cases and never load a reference directory or JS engine.
func TestDeclaredBlockSemantics(t *testing.T) {
	cases := []struct {
		Op   string
		Args map[string]any
		Want any
	}{
		{"truefantommath_negative_block", map[string]any{"A": float64(7)}, float64(-7)},
		{"truefantommath_exactly_equal_block", map[string]any{"A": "Hello", "B": "hello"}, false},
		{"truefantommath_exactly_equal_block", map[string]any{"A": "02", "B": float64(2)}, true},
		{"truefantommath_exactly_equal_block", map[string]any{"A": " ", "B": float64(0)}, false},
		{"truefantommath_not_exactly_equal_block", map[string]any{"A": "A", "B": "a"}, true},
		{"truefantommath_almost_equal_block", map[string]any{"A": float64(1), "B": float64(1.5)}, true},
		{"truefantommath_almost_equal_block", map[string]any{"A": float64(1), "B": float64(1.51)}, false},
		{"truefantommath_not_almost_equal_block", map[string]any{"A": "x", "B": "X"}, false},
		{"truefantommath_between", map[string]any{"A": float64(1), "B": float64(2), "C": float64(3)}, true},
		{"truefantommath_between", map[string]any{"A": float64(1), "B": float64(1), "C": float64(3)}, false},
		{"truefantommath_nand_block", map[string]any{"A": true, "B": "false"}, true},
		{"truefantommath_nor_block", map[string]any{"A": false, "B": float64(0)}, true},
		{"truefantommath_xor_block", map[string]any{"A": "true", "B": float64(0)}, true},
		{"truefantommath_xnor_block", map[string]any{"A": "0", "B": false}, true},
		{"truefantommath_exactly_cont_block", map[string]any{"A": "Abc", "B": "a"}, false},
		{"truefantommath_exactly_cont_block", map[string]any{"A": "Abc", "B": "A"}, true},
		{"truefantommath_sign_of", map[string]any{"A": float64(-9)}, float64(-1)},
		{"truefantommath_sign_of", map[string]any{"A": "not a number"}, float64(0)},
		{"truefantommath_scale_block", map[string]any{"A": float64(25), "m1": float64(0), "M1": float64(100), "m2": float64(-1), "M2": float64(1)}, float64(-0.5)},
		{"truefantommath_trunc_block", map[string]any{"A": float64(-1.9)}, float64(-1)},
		{"truefantommath_is_multiple_of_block", map[string]any{"A": float64(12), "B": float64(3)}, true},
		{"truefantommath_is_multiple_of_block", map[string]any{"A": float64(12), "B": float64(0)}, false},
		{"truefantommath_log_with_base_block", map[string]any{"A": float64(8), "B": float64(2)}, float64(3)},
		{"truefantommath_e_block", map[string]any{}, float64(2.718281828459045)},
		{"truefantommath_is_safe_number_block", map[string]any{"A": float64(9007199254740991)}, true},
		{"truefantommath_is_safe_number_block", map[string]any{"A": float64(9007199254740992)}, false},
		{"truefantommath_is_number_block", map[string]any{"A": "hello"}, false},
		{"truefantommath_is_number_block", map[string]any{"A": "0x123456789abcdef012345"}, true},
		{"truefantommath_is_number_block", map[string]any{"A": "  "}, true},
		{"truefantommath_is_number_block", map[string]any{"A": "Infinity"}, true},
		{"truefantommath_is_int_block", map[string]any{"A": "2.0"}, true},
		{"truefantommath_is_int_block", map[string]any{"A": "2.5"}, false},
		{"truefantommath_is_int_block", map[string]any{"A": "Infinity"}, true},
		{"truefantommath_is_float_block", map[string]any{"A": "2.5"}, true},
		{"truefantommath_is_float_block", map[string]any{"A": "hello"}, false},
		{"truefantommath_true_math_op", map[string]any{"OPERATOR": "SIN", "NUM": float64(0)}, float64(0)},
		{"truefantommath_true_math_op", map[string]any{"OPERATOR": "cos", "NUM": float64(0)}, float64(1)},
		{"truefantommath_true_math_op", map[string]any{"OPERATOR": "unknown", "NUM": float64(10)}, float64(0)},
	}
	handlers := map[string]e.Handler{}
	for _, b := range New().Blocks {
		handlers[b.Opcode] = b.Handler
	}
	for _, tc := range cases {
		t.Run(tc.Op, func(t *testing.T) {
			handler := handlers[tc.Op]
			if handler == nil {
				t.Fatalf("missing opcode %s", tc.Op)
			}
			got, err := handler(&e.Context{Args: tc.Args})
			equal := reflect.DeepEqual(got.Value, tc.Want)
			if a, ok := got.Value.(float64); ok {
				if b, ok := tc.Want.(float64); ok {
					equal = a == b || math.Abs(a-b) <= math.Abs(b)*1e-14
				}
			}
			if err != nil || !equal {
				t.Fatalf("args %#v: got %#v (%T), %v; want %#v (%T)", tc.Args, got.Value, got.Value, err, tc.Want, tc.Want)
			}
		})
	}
}
func TestDeclaredBlockMonitorMetadata(t *testing.T) {
	want := map[string]struct{ Kind, Text string }{
		"truefantommath_exponent_block":          {"reporter", "[A] ^ [B]"},
		"truefantommath_root_block":              {"reporter", "[A] √ [B]"},
		"truefantommath_negative_block":          {"reporter", "- [A]"},
		"truefantommath_more_or_equal_block":     {"boolean", "[A] ≥ [B]"},
		"truefantommath_less_or_equal_block":     {"boolean", "[A] ≤ [B]"},
		"truefantommath_not_equal_block":         {"boolean", "[A] ≠ [B]"},
		"truefantommath_exactly_equal_block":     {"boolean", "[A] ≡ [B]"},
		"truefantommath_not_exactly_equal_block": {"boolean", "[A] ≢ [B]"},
		"truefantommath_almost_equal_block":      {"boolean", "[A] ≈ [B]"},
		"truefantommath_not_almost_equal_block":  {"boolean", "[A] ≉ [B]"},
		"truefantommath_between_or_equal":        {"boolean", "[A] ≤ [B] ≤ [C]"},
		"truefantommath_between":                 {"boolean", "[A] < [B] < [C]"},
		"truefantommath_nand_block":              {"boolean", "[A] nand [B]"},
		"truefantommath_nor_block":               {"boolean", "[A] nor [B]"},
		"truefantommath_xor_block":               {"boolean", "[A] xor [B]"},
		"truefantommath_xnor_block":              {"boolean", "[A] xnor [B]"},
		"truefantommath_exactly_cont_block":      {"boolean", "[A] exactly contains [B]?"},
		"truefantommath_sign_of":                 {"reporter", "sign of [A]"},
		"truefantommath_clamp_block":             {"reporter", "clamp [A] between [B] and [C]"},
		"truefantommath_scale_block":             {"reporter", "map [A] from range [m1] - [M1] to range [m2] - [M2]"},
		"truefantommath_trunc2_block":            {"reporter", "trunc of [A] with [B] digits after dot"},
		"truefantommath_trunc_block":             {"reporter", "trunc of [A]"},
		"truefantommath_is_multiple_of_block":    {"boolean", "[A] is multiple of [B]?"},
		"truefantommath_log_with_base_block":     {"reporter", "log of [A] with base [B]"},
		"truefantommath_true_math_op":            {"reporter", "true [OPERATOR] [NUM]"},
		"truefantommath_pi_block":                {"reporter", "𝜋"},
		"truefantommath_e_block":                 {"reporter", "𝘦"},
		"truefantommath_infinity_block":          {"reporter", "∞"},
		"truefantommath_is_safe_number_block":    {"boolean", "is safe number [A]?"},
		"truefantommath_is_number_block":         {"boolean", "is number [A]?"},
		"truefantommath_is_int_block":            {"boolean", "is int [A]?"},
		"truefantommath_is_float_block":          {"boolean", "is float [A]?"},
	}
	for _, b := range New().Blocks {
		if spec, ok := want[b.Opcode]; ok {
			if b.Kind != spec.Kind || b.Text != spec.Text {
				t.Errorf("%s: kind %q text %q, want %q %q", b.Opcode, b.Kind, b.Text, spec.Kind, spec.Text)
			}
			delete(want, b.Opcode)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing declared blocks: %v", want)
	}
}
