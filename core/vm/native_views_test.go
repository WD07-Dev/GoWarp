package vm

import "testing"

func TestNativeWindowsRequireDesktopHost(t *testing.T) {
	r := fixture(t, `{}`)
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	if _, err := c.Call("window.list"); err == nil {
		t.Fatal("headless window operation accepted")
	}
	r.SetWindowHandler(func(op string, args []any) (any, error) {
		if op != "move" || len(args) != 3 || args[0] != "main" {
			t.Fatal(op, args)
		}
		return "moved", nil
	})
	v, err := c.Call("window.move", "main", 100.0, 200.0)
	if err != nil || v != "moved" {
		t.Fatal(v, err)
	}
}
