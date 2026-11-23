// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"strconv"
	"strings"
)

func New() e.Extension {
	return e.Extension{ID: "darkencolorext", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "darkencolorext_darkenColor", Kind: "reporter", Inputs: []string{"COLOR", "AMOUNT"}, Handler: func(c *e.Context) (e.Result, error) {
			h := strings.TrimSpace(strings.Replace(c.String("COLOR"), "#", "", 1))
			if len(h) == 3 {
				h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
			}
			v, err := strconv.ParseUint(h, 16, 24)
			if len(h) != 6 || err != nil {
				return e.Result{Value: "000000"}, nil
			}
			factor := 1 - e.Clamp(c.Number("AMOUNT"), 0, 100)/100
			return e.Result{Value: fmt.Sprintf("%02x%02x%02x", int(float64(v>>16)*factor), int(float64((v>>8)&255)*factor), int(float64(v&255)*factor))}, nil
		}},
	}}
}
