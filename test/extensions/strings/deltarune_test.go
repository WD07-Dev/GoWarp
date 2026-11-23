package extension

import (
	e "gowarp/core/extension"
	"math"
	"testing"
)

func TestIdenticalPreservesTypeAndCase(t *testing.T) {
	for _, tt := range []struct {
		a, b any
		want bool
	}{{"A", "a", false}, {"한글", "한글", true}, {"1", 1, false}, {1, float64(1), true}, {true, "true", false}, {true, true, true}, {"01", "1", false}, {math.NaN(), math.NaN(), false}, {0, math.Copysign(0, -1), true}} {
		for _, block := range New().Blocks {
			if block.Opcode == "strings_identical" {
				got, err := block.Handler(&e.Context{Args: map[string]any{"OPERAND1": tt.a, "OPERAND2": tt.b}})
				if err != nil || got.Value != tt.want {
					t.Errorf("%#v === %#v: %v,%v; want %v", tt.a, tt.b, got, err, tt.want)
				}
			}
		}
	}
}
func TestUnicodeFromWrapsUTF16CodeUnit(t *testing.T) {
	for _, tt := range []struct {
		number any
		want   string
	}{{65, "A"}, {65536 + 65, "A"}, {-1, "\uffff"}, {65.9, "A"}, {math.Inf(1), "\x00"}, {"44032", "가"}} {
		for _, block := range New().Blocks {
			if block.Opcode == "strings_unicodefrom" {
				got, err := block.Handler(&e.Context{Args: map[string]any{"NUM": tt.number}})
				if err != nil || got.Value != tt.want {
					t.Errorf("%v: %q,%v; want %q", tt.number, got.Value, err, tt.want)
				}
			}
		}
	}
}
