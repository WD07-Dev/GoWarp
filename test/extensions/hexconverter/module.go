// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
	"math/big"
	"strings"
	"unicode"
)

func New() e.Extension {
	return e.Extension{ID: "hexconverter", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "hexconverter_decToHex", Kind: "reporter", Text: "10진수 [NUM] 을(를) 16진수 [CASE] (으)로 바꾸기", Inputs: []string{"NUM"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: extensionHex(c.Number("NUM"), c.Field("CASE") == "대문자")}, nil
		}},
		{Opcode: "hexconverter_hexToDec", Kind: "reporter", Text: "16진수 [HEX] 을(를) 10진수로 바꾸기", Inputs: []string{"HEX"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: extensionParseHex(c.String("HEX"))}, nil
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

func extensionParseHex(text string) float64 {
	text = strings.TrimLeftFunc(text, func(r rune) bool { return r == '\ufeff' || (r != '\u0085' && unicode.IsSpace(r)) })
	sign := 1.0
	if strings.HasPrefix(text, "-") {
		sign = -1
		text = text[1:]
	} else if strings.HasPrefix(text, "+") {
		text = text[1:]
	}
	if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") {
		text = text[2:]
	}
	end := 0
	for end < len(text) && strings.ContainsRune("0123456789abcdefABCDEF", rune(text[end])) {
		end++
	}
	if end == 0 {
		return 0
	}
	n, _ := new(big.Int).SetString(text[:end], 16)
	value, _ := new(big.Float).SetInt(n).Float64()
	return math.Copysign(value, sign)
}
