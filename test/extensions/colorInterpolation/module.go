package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"math"
	"strconv"
	"strings"
)

func New() e.Extension {
	return e.Extension{ID: "colorInterpolation", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "colorInterpolation_interpolateColor", Kind: "reporter", Inputs: []string{"COLOR1", "COLOR2", "N"}, Handler: func(c *e.Context) (e.Result, error) {
			first, second := hexToRGB(c.String("COLOR1")), hexToRGB(c.String("COLOR2"))
			n := e.Clamp(c.Number("N"), 0, 100) / 100
			channel := func(shift uint) int {
				a, b := float64((first>>shift)&255), float64((second>>shift)&255)
				return int(math.Round(a + (b-a)*n))
			}
			return e.Result{Value: fmt.Sprintf("#%02x%02x%02x", channel(16), channel(8), channel(0))}, nil
		}},
	}}
}

func hexToRGB(hex string) uint32 {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	// JavaScript parseInt accepts whitespace, signs, and a valid hex prefix.
	hex = strings.TrimSpace(hex)
	sign := 1.0
	if strings.HasPrefix(hex, "-") {
		sign = -1
		hex = hex[1:]
	} else {
		hex = strings.TrimPrefix(hex, "+")
	}
	if strings.HasPrefix(strings.ToLower(hex), "0x") {
		hex = hex[2:]
	}
	end := 0
	for end < len(hex) && strings.ContainsRune("0123456789abcdefABCDEF", rune(hex[end])) {
		end++
	}
	if end == 0 {
		return 0
	}
	value, _ := strconv.ParseFloat("0x"+hex[:end]+"p0", 64)
	if math.IsInf(value, 0) {
		return 0
	}
	value = math.Mod(sign*value, 1<<32)
	if value < 0 {
		value += 1 << 32
	}
	return uint32(value)
}
