// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
	"math/big"
	"strings"
)

func New() e.Extension {
	return e.Extension{ID: "hexconverter", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "hexconverter_decToHex", Kind: "reporter", Inputs: []string{"NUM"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: extensionHex(c.Number("NUM"), c.Field("CASE") == "대문자")}, nil
		}},
	}}
}
func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
func extensionHex(n float64, upper bool) string {
	if !finite(n) {
		return e.String(n)
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	whole, fraction := math.Modf(n)
	i, _ := new(big.Float).SetFloat64(whole).Int(nil)
	out := sign + i.Text(16)
	if fraction != 0 {
		out += "."
		for fraction != 0 {
			fraction *= 16
			digit := math.Floor(fraction)
			out += string("0123456789abcdef"[int(digit)])
			fraction -= digit
		}
	}
	if upper {
		out = strings.ToUpper(out)
	}
	return out
}
