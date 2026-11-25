package vm

import (
	"fmt"
	"testing"
)

func TestHostWindowSnapshotsAreLazyAndTitleChangesImmediate(t *testing.T) {
	r := fixture(t, `{}`)
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	if _, err := c.Call("host.window", "screenWidth"); err == nil {
		t.Fatal("headless window silently fabricated")
	}
	calls := 0
	provider := func() (HostWindow, error) {
		calls++
		return HostWindow{ScreenWidth: 1920, ScreenHeight: 1080, Focused: true, Title: "initial"}, nil
	}
	r.SetHostWindowProvider(provider)
	if calls != 0 {
		t.Fatal("eager window query")
	}
	for key, want := range map[string]any{"screenWidth": 1920.0, "screenHeight": 1080.0, "focused": true, "title": "initial"} {
		if got, err := c.Call("host.window", key); err != nil || got != want {
			t.Fatalf("%s=%v, %v", key, got, err)
		}
	}
	if calls != 1 {
		t.Fatal("snapshot repeated", calls)
	}
	if _, err := c.Call("host.command", "title", "new title"); err != nil {
		t.Fatal(err)
	}
	if got, err := c.Call("host.window", "title"); err != nil || got != "new title" {
		t.Fatal(got, err)
	}
	r.SetHostWindowProvider(provider)
	if calls != 1 {
		t.Fatal("eager next frame query")
	}
	if _, err := c.Call("host.window", "screenWidth"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("next frame kept stale geometry")
	}
	if got, err := c.Call("host.window", "title"); err != nil || got != "new title" {
		t.Fatal("title reverted", got, err)
	}
	if _, err := c.Call("host.window", "outerWidth"); err == nil {
		t.Fatal("unsupported geometry")
	}
	r.SetHostWindowProvider(func() (HostWindow, error) { return HostWindow{}, fmt.Errorf("monitor unavailable") })
	if _, err := c.Call("host.window", "focused"); err == nil {
		t.Fatal("snapshot error suppressed")
	}
}
