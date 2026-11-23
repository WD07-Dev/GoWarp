// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
	"math/big"
	"strings"
)

func New() e.Extension {
	return e.Extension{ID: "sipctime", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "sipctime_formatTime", Kind: "reporter", Inputs: []string{"VALUE", "ROUND"}, Handler: func(c *e.Context) (e.Result, error) {
			value := c.Number("VALUE")
			seconds := extensionFixed(math.Mod(value, 60), 3)
			width := 6
			if c.String("ROUND") == "rounded" {
				seconds = e.String(math.Floor(math.Mod(value, 60) + .5))
				width = 2
			}
			return e.Result{Value: extensionPad(e.String(math.Floor(value/3600)), 2) + ":" + extensionPad(e.String(math.Floor(math.Mod(value/60, 60))), 2) + ":" + extensionPad(seconds, width)}, nil
		}},
		{Opcode: "sipctime_menu_TimeFormat", Kind: "reporter", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: c.Field("TimeFormat")}, nil
		}},
	}}
}
func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
func extensionFixed(n float64, digits int) string {
	if !finite(n) || math.Abs(n) >= 1e21 {
		return e.String(n)
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	value := new(big.Rat).SetFloat64(n)
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)
	numerator := new(big.Int).Mul(value.Num(), scale)
	whole, remainder := new(big.Int), new(big.Int)
	whole.QuoRem(numerator, value.Denom(), remainder)
	if remainder.Lsh(remainder, 1).Cmp(value.Denom()) >= 0 {
		whole.Add(whole, big.NewInt(1))
	}
	s := whole.String()
	if digits > 0 {
		s = extensionPad(s, digits+1)
		s = s[:len(s)-digits] + "." + s[len(s)-digits:]
	}
	return sign + s
}
func extensionPad(s string, width int) string {
	if len(s) < width {
		return strings.Repeat("0", width-len(s)) + s
	}
	return s
}
