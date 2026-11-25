//go:build windows && amd64

package multiwindow

import (
	"image"
	"testing"
	"unsafe"

	"github.com/lxn/win"
	"gowarp/core/window"
)

func TestWindowControlStyles(t *testing.T) {
	for _, tt := range []struct {
		name string
		view window.View
		want uintptr
	}{
		{"fixed bordered defaults", window.View{Border: true}, 0x00ca0000},
		{"resizable", window.View{Border: true, Resizable: true}, 0x00cf0000},
		{"disabled buttons", window.View{Border: true, Resizable: true, DisableMinimize: true, DisableMaximize: true}, 0x00cc0000},
		{"fullscreen ignores border and resize", window.View{Border: true, Resizable: true, Fullscreen: true}, 0x80000000},
		{"borderless", window.View{}, 0x80000000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			style, _ := styles(tt.view)
			if style != tt.want {
				t.Fatalf("style = %#x; want %#x", style, tt.want)
			}
		})
	}
}

func TestDisabledCloseDoesNotRetireWindow(t *testing.T) {
	m := &windowManager{items: map[string]*nativeWindow{
		"main": {handle: 1, view: window.View{ID: "main", DisableClose: true}},
	}}
	for _, message := range []struct {
		id      uint32
		command uintptr
	}{{0x10, 0}, {0x112, 0xf060}} {
		if result := m.wndProc(1, message.id, message.command, 0); result != 0 {
			t.Fatalf("close result = %d", result)
		}
	}
	if len(m.closed) != 0 {
		t.Fatalf("disabled close generated events: %v", m.closed)
	}
}

func TestResizeRepackingWaitsForPublication(t *testing.T) {
	v := window.View{ID: "main", Width: 320, Height: 240, StageWidth: 480, StageHeight: 360}
	initial, err := window.Pack([]window.View{v})
	if err != nil {
		t.Fatal(err)
	}
	old := &frame{layout: initial, handles: []uintptr{1}}
	v.Width, v.Height = 640, 480
	b := &Backend{manager: &windowManager{positions: []window.View{v}}, views: initial.Views, frame: old}
	b.Publish(true)
	if _, err := b.Poll(); err != nil {
		t.Fatal(err)
	}
	if b.frame == old || b.frame.layout.Tiles[0].Dx() != 640 || b.Views()[0].Height != 480 {
		t.Fatal("client resize did not update atlas dimensions")
	}
	if b.bridge.frame.Load() != old || old.layout.Tiles[0].Dx() != 320 {
		t.Fatal("Poll changed the frame being presented")
	}
	b.Publish(true)
	if b.bridge.frame.Load() != b.frame {
		t.Fatal("new frame was not published")
	}
}

func TestMainWindowStartsCenteredOnlyOnce(t *testing.T) {
	for _, border := range []bool{false, true} {
		name := "borderless"
		if border {
			name = "bordered"
		}
		t.Run(name, func(t *testing.T) {
			m, err := startWindows()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.close)
			m.centerMain = true
			main := window.View{ID: "main", X: 100, Y: 100, Width: 320, Height: 240, StageWidth: 480, StageHeight: 360, Border: border, Hidden: true}
			other := main
			other.ID, other.X, other.Y = "other", 40, 60
			_, actual, err := m.setViews([]window.View{main, other})
			if err != nil {
				t.Fatal(err)
			}
			m.do(func() {
				w := m.items["main"]
				work, err := monitorBounds(w.view, w, true)
				if err != nil {
					t.Error(err)
					return
				}
				var bounds win.RECT
				ok, _, err := user32.NewProc("GetWindowRect").Call(w.handle, uintptr(unsafe.Pointer(&bounds)))
				if ok == 0 {
					t.Error(err)
					return
				}
				wantX := work.Left + max(0, (work.Right-work.Left-(bounds.Right-bounds.Left))/2)
				wantY := work.Top + max(0, (work.Bottom-work.Top-(bounds.Bottom-bounds.Top))/2)
				if bounds.Left != wantX || bounds.Top != wantY {
					t.Errorf("border %t: got %+v, work %+v", border, bounds, work)
				}
			})
			if actual[1].X != other.X || actual[1].Y != other.Y {
				t.Fatal("additional window position changed")
			}
			actual[0].X, actual[0].Y = 70, 90
			_, moved, err := m.setViews(actual)
			if err != nil {
				t.Fatal(err)
			}
			if moved[0].X != 70 || moved[0].Y != 90 {
				t.Fatal("explicit movement was recentered")
			}
		})
	}
}

func TestPollUpdatesWindowStateWithoutRepacking(t *testing.T) {
	v := window.View{ID: "main", Width: 320, Height: 240, StageWidth: 480, StageHeight: 360}
	layout, err := window.Pack([]window.View{v})
	if err != nil {
		t.Fatal(err)
	}
	old := &frame{layout: layout, handles: []uintptr{1}}
	b := &Backend{manager: &windowManager{}, views: layout.Views, frame: old}
	for _, state := range []window.State{{Minimized: true}, {Maximized: true}, {Focused: true}, {}} {
		v.State = state
		b.manager.positions = []window.View{v}
		if _, err := b.Poll(); err != nil {
			t.Fatal(err)
		}
		if b.Views()[0].State != state || b.frame != old || old.layout.Views[0].State != (window.State{}) {
			t.Fatal("state poll lost state or mutated the published frame")
		}
	}
}

func TestNativeGroupCenterUsesBordersAndWorkArea(t *testing.T) {
	m, err := startWindows()
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	views := []window.View{
		{ID: "main", X: 60, Y: 60, Width: 100, Height: 100, StageWidth: 100, StageHeight: 100, Border: true},
		{ID: "other", X: 200, Y: 60, Width: 100, Height: 100, StageWidth: 100, StageHeight: 100, Border: true},
	}
	_, views, err = m.setViews(views)
	if err != nil {
		t.Fatal(err)
	}
	monitor, err := m.monitor("main")
	if err != nil {
		t.Fatal(err)
	}
	if monitor.Width < 1 || monitor.WorkWidth < 1 || monitor.WorkHeight < 1 {
		t.Fatal(monitor)
	}
	gap := views[1].X - views[0].X
	moved, err := m.center(views, "main", true)
	if err != nil {
		t.Fatal(err)
	}
	if moved[1].X-moved[0].X != gap {
		t.Fatal("group spacing changed")
	}
	m.do(func() {
		var group image.Rectangle
		for _, id := range []string{"main", "other"} {
			w := m.items[id]
			var b win.RECT
			if !win.GetWindowRect(win.HWND(w.handle), &b) {
				t.Error("window rect unavailable")
				return
			}
			group = group.Union(image.Rect(int(b.Left), int(b.Top), int(b.Right), int(b.Bottom)))
		}
		if group.Min.X != monitor.WorkX+max(0, (monitor.WorkWidth-group.Dx())/2) || group.Min.Y != monitor.WorkY+max(0, (monitor.WorkHeight-group.Dy())/2) {
			t.Errorf("group %v not centered on %+v", group, monitor)
		}
	})
	if _, err := m.monitor("missing"); err == nil {
		t.Fatal("unknown monitor window accepted")
	}
}
