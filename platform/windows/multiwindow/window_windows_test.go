//go:build windows && amd64

package multiwindow

import (
	"errors"
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
		{"fixed bordered defaults", window.View{Frame: window.Bordered}, 0x00ca0000},
		{"resizable", window.View{Frame: window.Bordered, Resizable: true}, 0x00cf0000},
		{"disabled buttons", window.View{Frame: window.Bordered, Resizable: true, DisableMinimize: true, DisableMaximize: true}, 0x00cc0000},
		{"fullscreen ignores border and resize", window.View{Frame: window.Bordered, Resizable: true, Fullscreen: true}, 0x80000000},
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
			frame := window.Borderless
			if border {
				frame = window.Bordered
			}
			main := window.View{ID: "main", X: 100, Y: 100, Width: 320, Height: 240, StageWidth: 480, StageHeight: 360, Frame: frame, Hidden: true}
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
		{ID: "main", X: 60, Y: 60, Width: 100, Height: 100, StageWidth: 100, StageHeight: 100, Frame: window.Bordered},
		{ID: "other", X: 200, Y: 60, Width: 100, Height: 100, StageWidth: 100, StageHeight: 100, Frame: window.Bordered},
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

func TestUserMovementLockKeepsScriptPositioning(t *testing.T) {
	m, err := startWindows()
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	v := window.View{ID: "main", X: 100, Y: 100, Width: 320, Height: 240, StageWidth: 320, StageHeight: 240, Frame: window.BorderedLocked, Resizable: true}
	_, views, err := m.setViews([]window.View{v})
	if err != nil {
		t.Fatal(err)
	}
	m.do(func() {
		w := m.items["main"]
		hwnd := win.HWND(w.handle)
		menu := win.GetSystemMenu(hwnd, false)
		flags, _, _ := user32.NewProc("GetMenuState").Call(uintptr(menu), win.SC_MOVE, win.MF_BYCOMMAND)
		if flags&win.MF_GRAYED == 0 {
			t.Error("Move menu remained enabled")
		}
		var rect win.RECT
		win.GetWindowRect(hwnd, &rect)
		found := false
		for y := rect.Top; y < rect.Top+80; y++ {
			point := uintptr(uint32(uint16(rect.Left+100)) | uint32(uint16(y))<<16)
			if win.DefWindowProc(hwnd, win.WM_NCHITTEST, 0, point) == win.HTCAPTION {
				found = true
				if m.wndProc(w.handle, win.WM_NCHITTEST, 0, point) != win.HTBORDER {
					t.Error("title bar still allows dragging")
				}
				m.dragging = w.handle
				if m.wndProc(w.handle, win.WM_NCHITTEST, 0, point) != win.HTCAPTION {
					t.Error("project-initiated drag did not allow native title-bar handling")
				}
				m.dragging = w.handle + 1
				if m.wndProc(w.handle, win.WM_NCHITTEST, 0, point) != win.HTBORDER {
					t.Error("another window's drag unlocked this title bar")
				}
				m.dragging = 0
				if m.wndProc(w.handle, win.WM_NCHITTEST, 0, point) != win.HTBORDER {
					t.Error("drag completion did not restore the lock")
				}
				break
			}
		}
		if !found {
			t.Error("could not locate title bar for hit test")
		}
		// The keyboard Move command must return without entering a modal move loop.
		m.wndProc(w.handle, win.WM_SYSCOMMAND, win.SC_MOVE, 0)
	})
	views[0].X, views[0].Y = 180, 140
	_, views, err = m.setViews(views)
	if err != nil {
		t.Fatal(err)
	}
	if views[0].X != 180 || views[0].Y != 140 || views[0].Frame != window.BorderedLocked {
		t.Fatal("script move failed or unlocked the window", views)
	}
	views, err = m.center(views, "main", false)
	if err != nil || views[0].Frame != window.BorderedLocked {
		t.Fatal("centering failed or unlocked the window", err)
	}
	views[0].Frame = window.Bordered
	if _, _, err = m.setViews(views); err != nil {
		t.Fatal(err)
	}
	m.do(func() {
		hwnd := win.HWND(m.items["main"].handle)
		flags, _, _ := user32.NewProc("GetMenuState").Call(uintptr(win.GetSystemMenu(hwnd, false)), win.SC_MOVE, win.MF_BYCOMMAND)
		if flags&win.MF_GRAYED != 0 {
			t.Error("Move menu was not restored")
		}
	})
}

func TestLayoutTransitionWaitsForPreparedFrame(t *testing.T) {
	m, err := startWindows()
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	v := window.View{ID: "main", X: 80, Y: 80, Width: 320, Height: 240, StageWidth: 320, StageHeight: 240}
	handles, _, err := m.setViews([]window.View{v})
	if err != nil {
		t.Fatal(err)
	}
	if win.IsWindowVisible(win.HWND(handles[0])) {
		t.Fatal("new window became visible before its first GPU frame")
	}
	if in, _, _ := m.poll(); !in.Visible {
		t.Fatal("first-frame preparation would be skipped")
	}
	revision := m.revision.Load()
	if ok, err := m.presented(revision, func() error { return nil }); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if !win.IsWindowVisible(win.HWND(handles[0])) {
		t.Fatal("prepared window not revealed")
	}
	v.Width, v.StageWidth = 160, 160
	second := v
	second.ID, second.X = "second", 240
	handles, _, err = m.setViews([]window.View{v, second})
	if err != nil {
		t.Fatal(err)
	}
	var size win.RECT
	win.GetClientRect(win.HWND(handles[0]), &size)
	if size.Right != 320 || win.IsWindowVisible(win.HWND(handles[1])) {
		t.Fatal("old coverage disappeared before split frame was ready", size)
	}
	if ok, err := m.presented(revision, func() error { return nil }); err != nil || ok {
		t.Fatal("stale frame revealed newer layout", ok, err)
	}
	flushes := 0
	flushSplit := func() error {
		flushes++
		win.GetClientRect(win.HWND(handles[0]), &size)
		if size.Right != 320 || !win.IsWindowVisible(win.HWND(handles[1])) {
			t.Error("old coverage removed before replacement reached the desktop", size)
		}
		return nil
	}
	waitErr := errors.New("desktop presentation failed")
	if ok, err := m.presented(m.revision.Load(), func() error {
		flushSplit()
		return waitErr
	}); ok || !errors.Is(err, waitErr) {
		t.Fatal("presentation failure discarded old coverage", ok, err)
	}
	win.GetClientRect(win.HWND(handles[0]), &size)
	if size.Right != 320 {
		t.Fatal("failed presentation shrank the old window", size)
	}
	if ok, err := m.presented(m.revision.Load(), flushSplit); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if flushes != 2 {
		t.Fatal("split did not wait for desktop presentation", flushes)
	}
	win.GetClientRect(win.HWND(handles[0]), &size)
	if size.Right != 160 || !win.IsWindowVisible(win.HWND(handles[1])) {
		t.Fatal("prepared split was not applied", size)
	}
	v.Width, v.StageWidth = 320, 320
	if _, _, err = m.setViews([]window.View{v}); err != nil {
		t.Fatal(err)
	}
	if !win.IsWindowVisible(win.HWND(handles[1])) {
		t.Fatal("second window vanished before merged frame was ready")
	}
	flushes = 0
	if ok, err := m.presented(m.revision.Load(), func() error {
		flushes++
		win.GetClientRect(win.HWND(handles[0]), &size)
		if size.Right != 320 || !win.IsWindowVisible(win.HWND(handles[1])) {
			t.Error("retired window disappeared before expanded main reached the desktop", size)
		}
		return nil
	}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if flushes != 1 {
		t.Fatal("merge did not wait for desktop presentation", flushes)
	}
	win.GetClientRect(win.HWND(handles[0]), &size)
	if size.Right != 320 || win.IsWindowVisible(win.HWND(handles[1])) {
		t.Fatal("merged window did not replace the second window", size)
	}
	v.X += 20
	if _, _, err = m.setViews([]window.View{v}); err != nil {
		t.Fatal(err)
	}
	if ok, err := m.presented(m.revision.Load(), func() error {
		t.Error("position-only update waited for desktop presentation")
		return nil
	}); err != nil || !ok {
		t.Fatal(ok, err)
	}
}

func TestDragRequestRespectsWindowState(t *testing.T) {
	for _, v := range []window.View{
		{Hidden: true}, {Fullscreen: true}, {Passthrough: true},
	} {
		m := &windowManager{items: map[string]*nativeWindow{"main": {handle: 1, ready: true, view: v}}}
		if got := m.wndProc(1, win.WM_APP+2, 0, 0); got != 0 || m.dragging != 0 {
			t.Fatalf("drag started for disabled window: %+v", v)
		}
	}
	m := &windowManager{items: map[string]*nativeWindow{"main": {handle: 1}}}
	if got := m.wndProc(1, win.WM_APP+2, 0, 0); got != 0 || m.dragging != 0 {
		t.Fatal("unpresented window started dragging")
	}
	m.items["main"].ready = true
	m.dragging = 1
	if got := m.wndProc(1, win.WM_APP+2, 0, 0); got != 0 || m.dragging != 1 {
		t.Fatal("nested drag changed active drag")
	}
}

func TestFrameChangeKeepsClientOrigin(t *testing.T) {
	for _, hidden := range []bool{true, false} {
		name := "visible"
		if hidden {
			name = "hidden"
		}
		t.Run(name, func(t *testing.T) {
			m, err := startWindows()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.close)
			v := window.View{ID: "main", X: 500, Y: 300, Width: 400, Height: 300,
				StageWidth: 480, StageHeight: 360, Frame: window.Borderless, Hidden: hidden}
			_, views, err := m.setViews([]window.View{v})
			if err != nil {
				t.Fatal(err)
			}
			present := func() {
				t.Helper()
				if ok, err := m.presented(m.revision.Load(), func() error { return nil }); err != nil || !ok {
					t.Fatal("cannot apply prepared bounds", ok, err)
				}
			}
			present()
			origin := func() win.POINT {
				t.Helper()
				var point win.POINT
				m.do(func() {
					if !win.ClientToScreen(win.HWND(m.items["main"].handle), &point) {
						t.Error("cannot read client origin")
					}
				})
				return point
			}
			want := origin()
			for _, frame := range []string{window.Bordered, window.BorderedLocked, window.Borderless, window.Bordered, window.Borderless} {
				v = views[0]
				v.Frame, v.KeepClientX, v.KeepClientY = frame, true, true
				v.Width, v.Height = 800, 600
				_, views, err = m.setViews([]window.View{v})
				if err != nil {
					t.Fatal(err)
				}
				present()
				if got := origin(); got != want {
					t.Fatalf("%s moved client origin: %+v, want %+v", frame, got, want)
				}
				if views[0].KeepClientX || views[0].KeepClientY {
					t.Fatal("frame adjustment leaked into snapshot")
				}
				_, views, err = m.setViews(views)
				if err != nil {
					t.Fatal(err)
				}
				if got := origin(); got != want {
					t.Fatal("repeated sync shifted the client origin")
				}
			}
			v = views[0]
			v.Frame, v.X, v.Y = window.Bordered, 500, 300
			_, views, err = m.setViews([]window.View{v})
			if err != nil {
				t.Fatal(err)
			}
			if views[0].X != 500 || views[0].Y != 300 {
				t.Fatal("explicit outer position was adjusted", views[0])
			}

		})
	}
}

func TestPollUnchangedWindowsDoesNotAllocate(t *testing.T) {
	views := []window.View{
		{ID: "main", Width: 320, Height: 240, StageWidth: 480, StageHeight: 360},
		{ID: "other", Width: 320, Height: 240, StageWidth: 480, StageHeight: 360},
	}
	layout, err := window.Pack(views)
	if err != nil {
		t.Fatal(err)
	}
	old := &frame{layout: layout, handles: []uintptr{1, 2}}
	b := &Backend{manager: &windowManager{positions: views}, views: layout.Views, frame: old}
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := b.Poll(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("unchanged windows allocated %g times", allocs)
	}
	views[1].X = 100
	views[1].State.Focused = true
	if _, err := b.Poll(); err != nil {
		t.Fatal(err)
	}
	if b.views[1].X != 100 || !b.views[1].State.Focused || b.frame != old {
		t.Fatal("window movement or focus was not retained")
	}
	if old.layout.Views[1].X != 0 || old.layout.Views[1].State.Focused {
		t.Fatal("poll mutated the published frame")
	}
}
