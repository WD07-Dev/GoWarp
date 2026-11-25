package extension

import (
	e "gowarp/core/extension"
	"math"
	"reflect"
	"testing"
)

// These expected values were captured from the preserved JavaScript extension.
// Tests use embedded cases and never load a reference directory or JS engine.
func TestDeclaredBlockSemantics(t *testing.T) {
	cases := []struct {
		Op   string
		Args map[string]any
		Want any
	}{
		{"hexconverter_hexToDec", map[string]any{"HEX": "D78C"}, float64(55180)},
		{"hexconverter_hexToDec", map[string]any{"HEX": "  -0xFz "}, float64(-15)},
		{"hexconverter_hexToDec", map[string]any{"HEX": "+fftail"}, float64(255)},
		{"hexconverter_hexToDec", map[string]any{"HEX": "not hex"}, float64(0)},
		{"hexconverter_hexToDec", map[string]any{"HEX": "0x"}, float64(0)},
		{"hexconverter_hexToDec", map[string]any{"HEX": "1.8"}, float64(1)},
		{"hexconverter_hexToDec", map[string]any{"HEX": "1e3"}, float64(483)},
		{"hexconverter_hexToDec", map[string]any{"HEX": "\ufeffFF"}, float64(255)},
		{"hexconverter_hexToDec", map[string]any{"HEX": "FF"}, float64(0)},
		{"hexconverter_hexToDec", map[string]any{"HEX": "-0"}, float64(0)},
		{"hexconverter_hexToDec", map[string]any{"HEX": "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"}, float64(1.157920892373162e+77)},
	}
	handlers := map[string]e.Handler{}
	for _, b := range New().Blocks {
		handlers[b.Opcode] = b.Handler
	}
	for _, tc := range cases {
		t.Run(tc.Op, func(t *testing.T) {
			handler := handlers[tc.Op]
			if handler == nil {
				t.Fatalf("missing opcode %s", tc.Op)
			}
			got, err := handler(&e.Context{Args: tc.Args})
			equal := reflect.DeepEqual(got.Value, tc.Want)
			if a, ok := got.Value.(float64); ok {
				if b, ok := tc.Want.(float64); ok {
					equal = a == b || math.Abs(a-b) <= math.Abs(b)*1e-14
				}
			}
			if err != nil || !equal {
				t.Fatalf("args %#v: got %#v (%T), %v; want %#v (%T)", tc.Args, got.Value, got.Value, err, tc.Want, tc.Want)
			}
		})
	}
}
func TestDeclaredBlockMonitorMetadata(t *testing.T) {
	want := map[string]struct{ Kind, Text string }{
		"hexconverter_decToHex": {"reporter", "10진수 [NUM] 을(를) 16진수 [CASE] (으)로 바꾸기"},
		"hexconverter_hexToDec": {"reporter", "16진수 [HEX] 을(를) 10진수로 바꾸기"},
	}
	for _, b := range New().Blocks {
		if spec, ok := want[b.Opcode]; ok {
			if b.Kind != spec.Kind || b.Text != spec.Text {
				t.Errorf("%s: kind %q text %q, want %q %q", b.Opcode, b.Kind, b.Text, spec.Kind, spec.Text)
			}
			delete(want, b.Opcode)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing declared blocks: %v", want)
	}
}
