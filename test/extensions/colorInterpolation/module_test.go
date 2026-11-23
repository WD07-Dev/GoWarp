package extension

import (
	e "gowarp/core/extension"
	"testing"
)

func TestInterpolateColor(t *testing.T) {
	for _, tc := range []struct {
		name, first, second string
		percent             any
		want                string
	}{
		{"midpoint", "#ff0000", "#0000ff", 50, "#800080"},
		{"lower clamp", "#123456", "#abcdef", -20, "#123456"},
		{"upper clamp", "#123456", "#abcdef", 120, "#abcdef"},
		{"short hex", "#F00", "00F", 25, "#bf0040"},
		{"descending half rounds up", "#ffffff", "#000000", 50, "#808080"},
		{"number coercion", "#000000", "#ffffff", "20", "#333333"},
		{"invalid percentage", "#123456", "#ffffff", "invalid", "#123456"},
		{"invalid color", "invalid", "#ffffff", 0, "#000000"},
		{"hex prefix parsing", "ff0000tail", "#000000", 0, "#ff0000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ext := New()
			if ext.ID != "colorInterpolation" {
				t.Fatalf("extension ID = %q", ext.ID)
			}
			for _, b := range ext.Blocks {
				if b.Opcode != "colorInterpolation_interpolateColor" {
					continue
				}
				got, err := b.Handler(&e.Context{Args: map[string]any{"COLOR1": tc.first, "COLOR2": tc.second, "N": tc.percent}})
				if err != nil || got.Value != tc.want {
					t.Fatalf("got %v, %v; want %s", got.Value, err, tc.want)
				}
				return
			}
			t.Fatal("missing interpolation block")
		})
	}
}
