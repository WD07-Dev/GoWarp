// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

func New() e.Extension {
	return e.Extension{ID: "truefantommath", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "truefantommath_pi_block", Kind: "reporter", Text: "𝜋", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: math.Pi}, nil }},
		{Opcode: "truefantommath_not_equal_block", Kind: "boolean", Text: "[A] ≠ [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: e.Compare(c.Args["A"], c.Args["B"]) != 0}, nil
		}},
		{Opcode: "truefantommath_between_or_equal", Kind: "boolean", Text: "[A] ≤ [B] ≤ [C]", Inputs: []string{"A", "B", "C"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: e.Compare(c.Args["A"], c.Args["B"]) <= 0 && e.Compare(c.Args["B"], c.Args["C"]) <= 0}, nil
		}},
		{Opcode: "truefantommath_clamp_block", Kind: "reporter", Text: "clamp [A] between [B] and [C]", Inputs: []string{"A", "B", "C"}, Handler: func(c *e.Context) (e.Result, error) {
			value := c.Args["A"]
			if e.Compare(value, c.Args["B"]) < 0 {
				value = c.Args["B"]
			} else if e.Compare(value, c.Args["C"]) > 0 {
				value = c.Args["C"]
			}
			return e.Result{Value: value}, nil
		}},
		{Opcode: "truefantommath_exponent_block", Kind: "reporter", Text: "[A] ^ [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: math.Pow(c.Number("A"), c.Number("B"))}, nil
		}},
		{Opcode: "truefantommath_root_block", Kind: "reporter", Text: "[A] √ [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: math.Pow(c.Number("B"), 1/c.Number("A"))}, nil
		}},
		{Opcode: "truefantommath_trunc2_block", Kind: "reporter", Text: "trunc of [A] with [B] digits after dot", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			value, places := c.Number("A"), math.Floor(c.Number("B"))
			if places < 1 {
				return e.Result{Value: math.Trunc(value)}, nil
			}
			scale := math.Pow(10, places)
			if math.IsInf(scale, 1) {
				return e.Result{Value: value}, nil
			}
			return e.Result{Value: math.Trunc(value*scale) / scale}, nil
		}},

		{Opcode: "truefantommath_less_or_equal_block", Kind: "boolean", Text: "[A] ≤ [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: e.Compare(c.Args["A"], c.Args["B"]) <= 0}, nil
		}},
		{Opcode: "truefantommath_more_or_equal_block", Kind: "boolean", Text: "[A] ≥ [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: e.Compare(c.Args["A"], c.Args["B"]) >= 0}, nil
		}},
		{Opcode: "truefantommath_negative_block", Kind: "reporter", Text: "- [A]", Inputs: []string{"A"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: 0 - c.Number("A")}, nil
		}},
		{Opcode: "truefantommath_exactly_equal_block", Kind: "boolean", Text: "[A] ≡ [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: exactlyCompare(c.Args["A"], c.Args["B"]) == 0}, nil
		}},
		{Opcode: "truefantommath_not_exactly_equal_block", Kind: "boolean", Text: "[A] ≢ [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: exactlyCompare(c.Args["A"], c.Args["B"]) != 0}, nil
		}},
		{Opcode: "truefantommath_almost_equal_block", Kind: "boolean", Text: "[A] ≈ [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: math.Abs(e.Compare(c.Args["A"], c.Args["B"])) <= 0.5}, nil
		}},
		{Opcode: "truefantommath_not_almost_equal_block", Kind: "boolean", Text: "[A] ≉ [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: !(math.Abs(e.Compare(c.Args["A"], c.Args["B"])) <= 0.5)}, nil
		}},
		{Opcode: "truefantommath_between", Kind: "boolean", Text: "[A] < [B] < [C]", Inputs: []string{"A", "B", "C"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: e.Compare(c.Args["A"], c.Args["B"]) < 0 && e.Compare(c.Args["B"], c.Args["C"]) < 0}, nil
		}},
		{Opcode: "truefantommath_nand_block", Kind: "boolean", Text: "[A] nand [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: !(c.Bool("A") && c.Bool("B"))}, nil
		}},
		{Opcode: "truefantommath_nor_block", Kind: "boolean", Text: "[A] nor [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: !(c.Bool("A") || c.Bool("B"))}, nil
		}},
		{Opcode: "truefantommath_xor_block", Kind: "boolean", Text: "[A] xor [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: c.Bool("A") != c.Bool("B")}, nil
		}},
		{Opcode: "truefantommath_xnor_block", Kind: "boolean", Text: "[A] xnor [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: c.Bool("A") == c.Bool("B")}, nil
		}},
		{Opcode: "truefantommath_exactly_cont_block", Kind: "boolean", Text: "[A] exactly contains [B]?", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: strings.Contains(c.String("A"), c.String("B"))}, nil
		}},
		{Opcode: "truefantommath_scale_block", Kind: "reporter", Text: "map [A] from range [m1] - [M1] to range [m2] - [M2]", Inputs: []string{"A", "m1", "M1", "m2", "M2"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: (c.Number("A")-c.Number("m1"))*(c.Number("M2")-c.Number("m2"))/(c.Number("M1")-c.Number("m1")) + c.Number("m2")}, nil
		}},
		{Opcode: "truefantommath_log_with_base_block", Kind: "reporter", Text: "log of [A] with base [B]", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: math.Log(c.Number("A")) / math.Log(c.Number("B"))}, nil
		}},
		{Opcode: "truefantommath_is_multiple_of_block", Kind: "boolean", Text: "[A] is multiple of [B]?", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: math.Mod(c.Number("A"), c.Number("B")) == 0}, nil
		}},
		{Opcode: "truefantommath_is_number_block", Kind: "boolean", Text: "is number [A]?", Inputs: []string{"A"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: !math.IsNaN(nanNumber(c.Args["A"]))}, nil
		}},
		{Opcode: "truefantommath_is_int_block", Kind: "boolean", Text: "is int [A]?", Inputs: []string{"A"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: isTrueInt(c.Args["A"]) && !math.IsNaN(nanNumber(c.Args["A"]))}, nil
		}},
		{Opcode: "truefantommath_is_float_block", Kind: "boolean", Text: "is float [A]?", Inputs: []string{"A"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: !isTrueInt(c.Args["A"]) && !math.IsNaN(nanNumber(c.Args["A"]))}, nil
		}},
		{Opcode: "truefantommath_e_block", Kind: "reporter", Text: "𝘦", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: math.E}, nil
		}},
		{Opcode: "truefantommath_infinity_block", Kind: "reporter", Text: "∞", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: math.Inf(1)}, nil
		}},
		{Opcode: "truefantommath_trunc_block", Kind: "reporter", Text: "trunc of [A]", Inputs: []string{"A"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: math.Trunc(c.Number("A"))}, nil
		}},
		{Opcode: "truefantommath_sign_of", Kind: "reporter", Text: "sign of [A]", Inputs: []string{"A"}, Handler: func(c *e.Context) (e.Result, error) {
			n := c.Number("A")
			if n != 0 {
				n = math.Copysign(1, n)
			}
			return e.Result{Value: n}, nil
		}},
		{Opcode: "truefantommath_is_safe_number_block", Kind: "boolean", Text: "is safe number [A]?", Inputs: []string{"A"}, Handler: func(c *e.Context) (e.Result, error) {
			n := c.Number("A")
			return e.Result{Value: math.Abs(n) <= 9007199254740991 && n == math.Trunc(n)}, nil
		}},
		{Opcode: "truefantommath_true_math_op", Kind: "reporter", Text: "true [OPERATOR] [NUM]", Inputs: []string{"OPERATOR", "NUM"}, Handler: func(c *e.Context) (e.Result, error) {
			n := c.Number("NUM")
			switch strings.ToLower(c.String("OPERATOR")) {
			case "sin":
				n = math.Sin(n)
			case "cos":
				n = math.Cos(n)
			case "tan":
				n = math.Tan(n)
			case "asin":
				n = math.Asin(n)
			case "acos":
				n = math.Acos(n)
			case "atan":
				n = math.Atan(n)
			default:
				n = 0
			}
			return e.Result{Value: n}, nil
		}},
		{Opcode: "truefantommath_menu_OPERATOR", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field("OPERATOR")}, nil }},
	}}
}

// The extension intentionally preserves NaN instead of Scratch's numeric zero cast.
func nanNumber(v any) float64 {
	if s, ok := v.(string); ok {
		s = strings.TrimFunc(s, func(r rune) bool { return r == '\ufeff' || (r != '\u0085' && unicode.IsSpace(r)) })
		if s == "" {
			return 0
		}
		if s == "Infinity" || s == "+Infinity" {
			return math.Inf(1)
		}
		if s == "-Infinity" {
			return math.Inf(-1)
		}
		if len(s) > 2 && s[0] == '0' {
			base := 0
			switch s[1] {
			case 'x', 'X':
				base = 16
			case 'b', 'B':
				base = 2
			case 'o', 'O':
				base = 8
			}
			if base != 0 {
				if n, ok := new(big.Int).SetString(s[2:], base); ok && !strings.ContainsAny(s[2:], "_+-") {
					f, _ := new(big.Float).SetInt(n).Float64()
					return f
				}
				return math.NaN()
			}
		}
		for _, r := range s {
			if !strings.ContainsRune("0123456789+-.eE", r) {
				return math.NaN()
			}
		}
		n, err := strconv.ParseFloat(s, 64)
		if err == nil || math.IsInf(n, 0) {
			return n
		}
		return math.NaN()
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case bool:
		if n {
			return 1
		}
		return 0
	case nil:
		return 0
	}
	return math.NaN()
}
func isTrueInt(v any) bool {
	switch v.(type) {
	case float64, float32, int, int64, bool, string:
		n := nanNumber(v)
		return math.IsNaN(n) || n == math.Floor(n)
	}
	return false
}
func exactlyCompare(a, b any) float64 {
	x, y := nanNumber(a), nanNumber(b)
	nonzero := func(v any) bool { s, ok := v.(string); return ok && !strings.ContainsAny(s, "0\t") }
	if x == 0 && nonzero(a) {
		x = math.NaN()
	} else if y == 0 && nonzero(b) {
		y = math.NaN()
	}
	if !math.IsNaN(x) && !math.IsNaN(y) {
		if x == y {
			return 0
		}
		return x - y
	}
	// JavaScript lexicographically compares UTF-16 units, including astral characters.
	left, right := utf16.Encode([]rune(e.String(a))), utf16.Encode([]rune(e.String(b)))
	for i := 0; i < len(left) && i < len(right); i++ {
		if left[i] < right[i] {
			return -1
		}
		if left[i] > right[i] {
			return 1
		}
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return 0
}
