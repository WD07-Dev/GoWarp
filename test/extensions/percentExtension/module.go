// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"math"
	"math/big"
	"strings"
)

func New() e.Extension {
	return e.Extension{ID: "percentExtension", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "percentExtension_convertToPercent", Kind: "reporter", Inputs: []string{"VALUE", "DECIMALS"}, Handler: func(c *e.Context) (e.Result, error) {
			digits := math.Trunc(math.Max(0, c.Number("DECIMALS")))
			if !finite(digits) || digits > 100 {
				return e.Result{}, fmt.Errorf("percent decimal places must be between 0 and 100")
			}
			return e.Result{Value: extensionFixed(c.Number("VALUE")*100, int(digits)) + "%"}, nil
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
