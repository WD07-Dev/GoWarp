package extension

import (
	e "gowarp/core/extension"
	"math"
	"testing"
)

func TestPiBlock(t *testing.T) {
	for _, block := range New().Blocks {
		if block.Opcode == "truefantommath_pi_block" {
			result, err := block.Handler(&e.Context{})
			if err != nil || result.Value != math.Pi {
				t.Fatal(result, err)
			}
			return
		}
	}
	t.Fatal("missing pi block")
}
