//go:build windows && amd64

package multiwindow

import (
	"testing"

	"gowarp/core/window"
)

func TestPreparedSourceIconsReachExistingAndNewWindows(t *testing.T) {
	m, err := startWindows()
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	v := window.View{ID: "existing", Width: 64, Height: 64, StageWidth: 480, StageHeight: 360, Frame: window.Bordered, Hidden: true}
	if _, _, err := m.setViews([]window.View{v}); err != nil {
		t.Fatal(err)
	}

	// Use an explicit source icon unrelated to this executable's resources.
	shared, _, err := user32.NewProc("LoadIconW").Call(0, 32515) // IDI_WARNING
	if shared == 0 {
		t.Fatal(err)
	}
	source, _, err := user32.NewProc("CopyIcon").Call(shared)
	if source == 0 {
		t.Fatal(err)
	}
	defer func() {
		if source != 0 {
			user32.NewProc("DestroyIcon").Call(source)
		}
	}()
	sourceWindow := m.wakeWindow.Load()
	m.invoke(func() {
		for kind := uintptr(0); kind < 2; kind++ {
			user32.NewProc("SendMessageW").Call(sourceWindow, 0x80, kind, source)
		}
	})
	b := &Backend{manager: m, prepared: true, sourceWindow: sourceWindow}
	if err := b.Prepare(); err != nil {
		t.Fatal(err)
	}
	var owned [2]uintptr
	m.invoke(func() { owned = m.icons })
	for _, icon := range owned {
		if icon == 0 || icon == source {
			t.Fatalf("native icon %x must be an independent copy of %x", icon, source)
		}
	}
	// The native copies must survive the source's icon destruction.
	m.invoke(func() {
		for kind := uintptr(0); kind < 2; kind++ {
			user32.NewProc("SendMessageW").Call(sourceWindow, 0x80, kind, 0)
		}
	})
	user32.NewProc("DestroyIcon").Call(source)
	source = 0
	v2 := v
	v2.ID = "new"
	handles, _, err := m.setViews([]window.View{v, v2})
	if err != nil {
		t.Fatal(err)
	}
	for _, hwnd := range handles {
		for kind, want := range owned {
			got, _, _ := user32.NewProc("SendMessageW").Call(hwnd, 0x7f, uintptr(kind), 0)
			if got != want {
				t.Fatalf("window %x icon %d = %x; want %x", hwnd, kind, got, want)
			}
			copy, _, err := user32.NewProc("CopyIcon").Call(got)
			if copy == 0 {
				t.Fatalf("native icon was invalidated with its source: %v", err)
			}
			user32.NewProc("DestroyIcon").Call(copy)
		}
	}
}
