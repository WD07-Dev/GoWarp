// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
)

func New() e.Extension {
	return e.Extension{ID: "truefantommath", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "truefantommath_not_equal_block", Kind: "reporter", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: e.Compare(c.Args["A"], c.Args["B"]) != 0}, nil
		}},
		{Opcode: "truefantommath_between_or_equal", Kind: "reporter", Inputs: []string{"A", "B", "C"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: e.Compare(c.Args["A"], c.Args["B"]) <= 0 && e.Compare(c.Args["B"], c.Args["C"]) <= 0}, nil
		}},
		{Opcode: "truefantommath_clamp_block", Kind: "reporter", Inputs: []string{"A", "B", "C"}, Handler: func(c *e.Context) (e.Result, error) {
			value := c.Args["A"]
			if e.Compare(value, c.Args["B"]) < 0 {
				value = c.Args["B"]
			} else if e.Compare(value, c.Args["C"]) > 0 {
				value = c.Args["C"]
			}
			return e.Result{Value: value}, nil
		}},
		{Opcode: "truefantommath_exponent_block", Kind: "reporter", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: math.Pow(c.Number("A"), c.Number("B"))}, nil
		}},
		{Opcode: "truefantommath_root_block", Kind: "reporter", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: math.Pow(c.Number("B"), 1/c.Number("A"))}, nil
		}},
		{Opcode: "truefantommath_trunc2_block", Kind: "reporter", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
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

		{Opcode: "truefantommath_less_or_equal_block", Kind: "reporter", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: e.Compare(c.Args["A"], c.Args["B"]) <= 0}, nil
		}},
		{Opcode: "truefantommath_more_or_equal_block", Kind: "reporter", Inputs: []string{"A", "B"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: e.Compare(c.Args["A"], c.Args["B"]) >= 0}, nil
		}},
	}}
}
