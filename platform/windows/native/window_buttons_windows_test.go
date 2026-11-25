package native

import "testing"

func TestOpacityPreservesMousePassthrough(t *testing.T) {
	const appWindow, transparent, layered = uintptr(0x00040000), uintptr(0x00000020), uintptr(0x00080000)
	for _, tt := range []struct {
		name    string
		style   uintptr
		opacity float64
		want    uintptr
	}{
		{"translucent", appWindow, 0.5, appWindow | layered},
		{"restore opaque", appWindow | layered, 1, appWindow},
		{"restore opaque passthrough", appWindow | transparent | layered, 1, appWindow | transparent | layered},
		{"passthrough translucency", appWindow | transparent | layered, 0.5, appWindow | transparent | layered},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := opacityExtendedStyle(tt.style, tt.opacity); got != tt.want {
				t.Fatalf("extended style = %#x; want %#x", got, tt.want)
			}
		})
	}
}
