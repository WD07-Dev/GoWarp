package extension

import (
	"testing"
)

func TestScratchValues(t *testing.T) {
	if Number("0x10") != 16 || Number("no number") != 0 || Bool("FALSE") || !Bool(" ") {
		t.Fatal("SDK coercion differs from Scratch")
	}
	if String(float64(1000000000000000000000)) != "1e+21" || Compare("A", "a") != 0 {
		t.Fatal("SDK string conversion or comparison differs from Scratch")
	}
	c := &Context{Args: map[string]any{"n": "12", "s": false}, Fields: map[string]string{"MODE": "on"}}
	if c.Number("n") != 12 || c.String("s") != "false" || c.Field("MODE") != "on" {
		t.Fatal("context accessor changed argument values")
	}
}

func TestNativeNumericValues(t *testing.T) {
	for _, value := range []any{int(7), int64(7), float32(7)} {
		if Number(value) != 7 || String(value) != "7" || !Bool(value) {
			t.Errorf("native number %T was not converted consistently", value)
		}
	}
}
