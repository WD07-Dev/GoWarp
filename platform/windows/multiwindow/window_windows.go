//go:build windows && amd64

package multiwindow

import (
	"fmt"
	"gowarp/core/window"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var user32 = windows.NewLazySystemDLL("user32.dll")

type nativeWindow struct {
	handle  uintptr
	view    window.View
	restore window.View // Last windowed geometry, owned by the window thread.
}
type windowManager struct {
	centerMain    bool
	stop          chan struct{}
	done          chan struct{}
	tasks         chan func()
	items         map[string]*nativeWindow
	retired       map[uintptr]bool
	events        window.Input
	closed        []string
	held          map[string]bool
	class         *uint16
	instance      uintptr
	icons         [2]uintptr // Owned ICON_SMALL / ICON_BIG copies; window thread only.
	wakeEvent     windows.Handle
	wakeWindow    atomic.Uintptr
	snapshotMu    sync.Mutex
	snapshot      window.Input
	positions     []window.View
	pendingClosed []string
}

func startWindows() (*windowManager, error) {
	m := &windowManager{
		stop: make(chan struct{}), done: make(chan struct{}), tasks: make(chan func(), 32),
		items: make(map[string]*nativeWindow), retired: make(map[uintptr]bool), held: make(map[string]bool),
	}
	ready := make(chan error, 1)
	go m.run(ready)
	if err := <-ready; err != nil {
		<-m.done
		return nil, err
	}
	return m, nil
}

func (m *windowManager) do(fn func()) {
	done := make(chan struct{})
	m.tasks <- func() { fn(); m.updateSnapshot(); close(done) }
	m.wake()
	<-done
}

func (m *windowManager) wake() {
	windows.SetEvent(m.wakeEvent)
	if hwnd := m.wakeWindow.Load(); hwnd != 0 {
		win.PostMessage(win.HWND(hwnd), win.WM_APP+1, 0, 0)
	}
}
func (m *windowManager) drain() {
	for {
		select {
		case task := <-m.tasks:
			task()
		default:
			return
		}
	}
}

func (m *windowManager) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(m.done)
	dpiContext := user32.NewProc("SetThreadDpiAwarenessContext")
	if err := dpiContext.Find(); err != nil {
		ready <- fmt.Errorf("native multiwindow requires per-monitor DPI support: %w", err)
		return
	}
	old, _, err := dpiContext.Call(^uintptr(3))
	if old == 0 {
		ready <- fmt.Errorf("SetThreadDpiAwarenessContext: %w", err)
		return
	}
	defer dpiContext.Call(old)
	m.wakeEvent, err = windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		ready <- err
		return
	}
	defer windows.CloseHandle(m.wakeEvent)
	m.instance = uintptr(win.GetModuleHandle(nil))
	m.class, _ = windows.UTF16PtrFromString(fmt.Sprintf("GoWarpViews-%d", windows.GetCurrentProcessId()))
	callback := syscall.NewCallback(m.wndProc)
	cursor := win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW))
	class := win.WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(win.WNDCLASSEX{})), LpfnWndProc: callback, HInstance: win.HINSTANCE(m.instance), LpszClassName: m.class, HCursor: cursor}
	atom, _, err := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		ready <- fmt.Errorf("RegisterClassExW: %w", err)
		return
	}
	defer user32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(m.class)), m.instance)
	defer func() { destroyIcons(m.icons) }()
	defer func() {
		for _, w := range m.items {
			win.DestroyWindow(win.HWND(w.handle))
		}
		for hwnd := range m.retired {
			win.DestroyWindow(win.HWND(hwnd))
		}
	}()
	command, _, err := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(m.class)), 0, 0, 0, 0, 0, 0, ^uintptr(2), 0, m.instance, 0)
	if command == 0 {
		ready <- fmt.Errorf("create window command queue: %w", err)
		return
	}
	m.wakeWindow.Store(command)
	defer win.DestroyWindow(win.HWND(command))
	ready <- nil
	for {
		select {
		case <-m.stop:
			return
		default:
		}
		m.drain()
		var msg win.MSG
		for {
			if !win.PeekMessage(&msg, 0, 0, 0, win.PM_REMOVE) {
				break
			}
			win.TranslateMessage(&msg)
			win.DispatchMessage(&msg)
		}
		m.updateSnapshot()
		user32.NewProc("MsgWaitForMultipleObjectsEx").Call(1, uintptr(unsafe.Pointer(&m.wakeEvent)), 0xffffffff, 0x04ff, 4)
	}
}

func keyName(v uintptr) string {
	if v >= 'A' && v <= 'Z' {
		return strings.ToLower(string(rune(v)))
	}
	if v >= '0' && v <= '9' {
		return string(rune(v))
	}
	switch v {
	case 0x20:
		return "space"
	case 0x25:
		return "left arrow"
	case 0x26:
		return "up arrow"
	case 0x27:
		return "right arrow"
	case 0x28:
		return "down arrow"
	case 0x10:
		return "shift"
	case 0x11:
		return "control"
	case 0x12:
		return "alt"
	case 0x0d:
		return "enter"
	case 0x1b:
		return "escape"
	case 0x08:
		return "backspace"
	case 0x09:
		return "tab"
	case 0x2e:
		return "delete"
	}
	if v >= 0x70 && v <= 0x87 {
		return fmt.Sprintf("f%d", v-0x70+1)
	}
	return ""
}

func (m *windowManager) wndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	var current *nativeWindow
	for _, w := range m.items {
		if w.handle == hwnd {
			current = w
			break
		}
	}
	switch msg {
	case 3, 5: // WM_MOVE / WM_SIZE, including interactive modal movement.
		if current != nil {
			m.updateSnapshot()
		}
	case 0x2e0:
		if current != nil && !current.view.Fullscreen {
			v := current.view
			b, err := windowBounds(v, uint32(wparam&0xffff))
			if err == nil {
				var suggested win.RECT
				if err := windows.ReadProcessMemory(windows.CurrentProcess(), lparam, (*byte)(unsafe.Pointer(&suggested)), unsafe.Sizeof(suggested), nil); err == nil {
					win.SetWindowPos(win.HWND(hwnd), 0, suggested.Left, suggested.Top, b.Right-b.Left, b.Bottom-b.Top, win.SWP_NOACTIVATE|win.SWP_NOZORDER)
				}
			}
		}
		return 0
	case 0x8001:
		m.drain()
		m.updateSnapshot()
		return 0
	case 0x112: // WM_SYSCOMMAND; enforce restrictions for keyboard/system menus too.
		if current != nil {
			switch wparam & 0xfff0 {
			case 0xf020:
				if current.view.DisableMinimize {
					return 0
				}
			case 0xf030:
				if current.view.DisableMaximize || !current.view.Resizable || current.view.Fullscreen {
					return 0
				}
			case 0xf060:
				if current.view.DisableClose {
					return 0
				}
			}
		}
	case 0x10:
		if current != nil && !current.view.DisableClose {
			m.closed = append(m.closed, current.view.ID)
			win.ShowWindow(win.HWND(hwnd), win.SW_HIDE)
		}
		return 0
	case 0x14:
		return 1
	case 0x84:
		if current != nil && current.view.Passthrough {
			return ^uintptr(0)
		} // HTTRANSPARENT
	case 0x100, 0x104:
		if key := keyName(wparam); key != "" && !m.held[key] {
			m.held[key] = true
			m.events.Keys = append(m.events.Keys, key)
		}
	case 0x101, 0x105:
		if key := keyName(wparam); key != "" {
			delete(m.held, key)
			m.events.Released = append(m.events.Released, key)
		}
	case 0x102:
		if wparam >= 32 && wparam < 0xd800 || wparam > 0xdfff && wparam <= 0xffff {
			m.events.Text += string(rune(wparam))
		}
	case 0x08:
		for key := range m.held {
			m.events.Released = append(m.events.Released, key)
			delete(m.held, key)
		}
		m.events.Down = false
	case 0x201:
		m.events.Down = true
		m.events.Pressed = true
		win.SetCapture(win.HWND(hwnd))
	case 0x202:
		m.events.Down = false
		win.ReleaseCapture()
	case 0x20a:
		m.events.Wheel += float64(int16(wparam>>16)) / 120
	}
	return win.DefWindowProc(win.HWND(hwnd), msg, wparam, lparam)
}

func styles(v window.View) (uintptr, uintptr) {
	style := uintptr(win.WS_POPUP)
	if v.Border && !v.Fullscreen {
		style = win.WS_CAPTION | win.WS_SYSMENU
		if !v.DisableMinimize {
			style |= win.WS_MINIMIZEBOX
		}
		if v.Resizable {
			style |= win.WS_THICKFRAME
			if !v.DisableMaximize {
				style |= win.WS_MAXIMIZEBOX
			}
		}
	}
	ex := uintptr(0x00200000 | win.WS_EX_APPWINDOW) // WS_EX_NOREDIRECTIONBITMAP
	if v.Passthrough {
		ex |= win.WS_EX_NOACTIVATE | win.WS_EX_LAYERED | win.WS_EX_TRANSPARENT
	}
	return style, ex
}

func windowBounds(v window.View, dpi uint32) (win.RECT, error) {
	if dpi == 0 {
		dpi = 96
	}
	style, ex := styles(v)
	bounds := win.RECT{Right: int32(v.Width), Bottom: int32(v.Height)}
	ok, _, err := user32.NewProc("AdjustWindowRectExForDpi").Call(uintptr(unsafe.Pointer(&bounds)), style, 0, ex, uintptr(dpi))
	if ok == 0 {
		return bounds, fmt.Errorf("AdjustWindowRectExForDpi: %w", err)
	}
	return bounds, nil
}

// monitorBounds uses physical pixels on the per-monitor DPI-aware window thread.
func monitorBounds(v window.View, w *nativeWindow, workArea bool) (win.RECT, error) {
	var monitor uintptr
	if w != nil {
		monitor = uintptr(win.MonitorFromWindow(win.HWND(w.handle), win.MONITOR_DEFAULTTONEAREST))
	} else {
		bounds := win.RECT{Left: int32(v.X), Top: int32(v.Y), Right: int32(v.X + v.Width), Bottom: int32(v.Y + v.Height)}
		monitor, _, _ = user32.NewProc("MonitorFromRect").Call(uintptr(unsafe.Pointer(&bounds)), 2)
	}
	info := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
	ok, _, err := user32.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return win.RECT{}, fmt.Errorf("GetMonitorInfoW: %w", err)
	}
	if workArea {
		return info.RcWork, nil
	}
	return info.RcMonitor, nil
}

func (m *windowManager) setViews(views []window.View) ([]uintptr, []window.View, error) {
	var result error
	handles := make([]uintptr, len(views))
	actual := append([]window.View(nil), views...)
	m.do(func() {
		defer func() { m.centerMain = false }()
		// Resolve fullscreen and restore geometry before mutating any HWND. Atlas
		// validation must also apply to actual monitor sizes, not requested sizes.
		for i, v := range actual {
			w := m.items[v.ID]
			if v.Fullscreen {
				bounds, err := monitorBounds(v, w, false)
				if err != nil {
					result = err
					return
				}
				v.X, v.Y = int(bounds.Left), int(bounds.Top)
				v.Width, v.Height = int(bounds.Right-bounds.Left), int(bounds.Bottom-bounds.Top)
			} else if w != nil && w.view.Fullscreen {
				if v.X == w.view.X {
					v.X = w.restore.X
				}
				if v.Y == w.view.Y {
					v.Y = w.restore.Y
				}
				if v.Width == w.view.Width {
					v.Width = w.restore.Width
				}
				if v.Height == w.view.Height {
					v.Height = w.restore.Height
				}
			}
			actual[i] = v
		}
		if _, err := window.Pack(actual); err != nil {
			result = err
			return
		}
		active := make(map[string]bool, len(actual))
		for i, v := range actual {
			active[v.ID] = true
			w := m.items[v.ID]
			created := w == nil
			title, err := windows.UTF16PtrFromString(v.Title)
			if err != nil {
				result = err
				return
			}
			style, ex := styles(v)
			bounds, err := windowBounds(v, 96)
			if err != nil {
				result = err
				return
			}
			if created {
				hwnd, _, err := user32.NewProc("CreateWindowExW").Call(ex, uintptr(unsafe.Pointer(m.class)), uintptr(unsafe.Pointer(title)), style,
					uintptr(v.X), uintptr(v.Y), uintptr(bounds.Right-bounds.Left), uintptr(bounds.Bottom-bounds.Top), 0, 0, m.instance, 0)
				if hwnd == 0 {
					result = fmt.Errorf("CreateWindowExW: %w", err)
					return
				}
				w = &nativeWindow{handle: hwnd, view: v, restore: views[i]}
				m.items[v.ID] = w
				setWindowIcons(hwnd, m.icons)
			}
			old := w.view
			if !created && !old.Fullscreen && v.Fullscreen {
				w.restore = old
			}
			oldStyle, oldEx := styles(old)
			styleChanged := oldStyle != style || oldEx != ex
			// Store policy before SetWindowPos dispatches synchronous messages.
			w.view = v
			if styleChanged {
				// Keep visibility/minimized/maximized state bits owned by Windows.
				current := win.GetWindowLongPtr(win.HWND(w.handle), win.GWL_STYLE)
				style |= current & (win.WS_VISIBLE | win.WS_MINIMIZE | win.WS_MAXIMIZE)
				win.SetWindowLongPtr(win.HWND(w.handle), win.GWL_STYLE, style)
				win.SetWindowLongPtr(win.HWND(w.handle), win.GWL_EXSTYLE, ex)
			}
			if old.Title != v.Title {
				user32.NewProc("SetWindowTextW").Call(w.handle, uintptr(unsafe.Pointer(title)))
			}
			if created || old.DisableClose != v.DisableClose || styleChanged {
				menu := win.GetSystemMenu(win.HWND(w.handle), false)
				if menu != 0 {
					flags := uint32(win.MF_ENABLED)
					if v.DisableClose {
						flags = win.MF_GRAYED
					}
					win.EnableMenuItem(menu, win.SC_CLOSE, flags)
					win.DrawMenuBar(win.HWND(w.handle))
				}
			}
			if v.Passthrough && (created || !old.Passthrough) {
				ok, _, err := user32.NewProc("SetLayeredWindowAttributes").Call(w.handle, 0, 255, 2)
				if ok == 0 {
					result = fmt.Errorf("SetLayeredWindowAttributes: %w", err)
					return
				}
			}
			if created || old.X != v.X || old.Y != v.Y || old.Width != v.Width || old.Height != v.Height || styleChanged || old.Topmost != v.Topmost {
				z := ^uintptr(1)
				if v.Topmost {
					z = ^uintptr(0)
				}
				flags := uintptr(0x0010) // NOACTIVATE
				if !created && old.Topmost == v.Topmost {
					flags |= 0x0004
				}
				if !created && old.Width == v.Width && old.Height == v.Height && !styleChanged {
					flags |= 0x0001
				}
				if styleChanged {
					flags |= 0x0020
				}
				dpi := win.GetDpiForWindow(win.HWND(w.handle))
				bounds, err = windowBounds(v, dpi)
				if err != nil {
					result = err
					return
				}
				if created && m.centerMain && v.ID == "main" && !v.Fullscreen {
					work, err := monitorBounds(v, w, true)
					if err != nil {
						result = err
						return
					}
					v.X = int(work.Left + max(0, (work.Right-work.Left-(bounds.Right-bounds.Left))/2))
					v.Y = int(work.Top + max(0, (work.Bottom-work.Top-(bounds.Bottom-bounds.Top))/2))
					w.view, w.restore = v, v
				}
				ok, _, err := user32.NewProc("SetWindowPos").Call(w.handle, z, uintptr(v.X), uintptr(v.Y), uintptr(bounds.Right-bounds.Left), uintptr(bounds.Bottom-bounds.Top), flags)
				if ok == 0 {
					result = fmt.Errorf("SetWindowPos: %w", err)
					return
				}
			}
			// Do not restore a minimized window unless visibility explicitly changes.
			show := int32(win.SW_SHOWNOACTIVATE)
			if v.Hidden {
				show = win.SW_HIDE
			}
			if created || old.Hidden != v.Hidden {
				win.ShowWindow(win.HWND(w.handle), show)
			}
			if !win.IsWindowVisible(win.HWND(w.handle)) && !v.Hidden {
				win.ShowWindow(win.HWND(w.handle), win.SW_SHOWNOACTIVATE)
			}
			if v.Hidden {
				win.ShowWindow(win.HWND(w.handle), win.SW_HIDE)
			}
			m.updateGeometry(w)
			actual[i] = w.view
			handles[i] = w.handle
		}
		for id, w := range m.items {
			if !active[id] {
				win.ShowWindow(win.HWND(w.handle), win.SW_HIDE)
				m.retired[w.handle] = true
				delete(m.items, id)
			}
		}
	})
	return handles, actual, result
}

// updateGeometry is called only on the window thread. Snapshots hand immutable
// copies to the backend, which republishes resized tiles with the next frame.
func (m *windowManager) updateGeometry(w *nativeWindow) {
	if win.IsIconic(win.HWND(w.handle)) {
		return
	}
	var bounds win.RECT
	if win.GetWindowRect(win.HWND(w.handle), &bounds) {
		w.view.X, w.view.Y = int(bounds.Left), int(bounds.Top)
	}
	if win.GetClientRect(win.HWND(w.handle), &bounds) && bounds.Right > 0 && bounds.Bottom > 0 {
		w.view.Width, w.view.Height = int(bounds.Right), int(bounds.Bottom)
	}
}

func (m *windowManager) collect(active map[uintptr]*output) {
	// Called only after GPU resources have been released. Copy keys before
	// posting to the window thread; it must never inspect the GPU-owned map.
	keep := make(map[uintptr]bool, len(active))
	for hwnd := range active {
		keep[hwnd] = true
	}
	select {
	case m.tasks <- func() {
		for hwnd := range m.retired {
			if !keep[hwnd] {
				win.DestroyWindow(win.HWND(hwnd))
				delete(m.retired, hwnd)
			}
		}
	}:
	default:
	}
	m.wake()
}

func (m *windowManager) updateSnapshot() {
	in := m.events
	in.Focused = false
	in.Visible = false
	in.Window = ""
	foreground := uintptr(win.GetForegroundWindow())
	var point win.POINT
	win.GetCursorPos(&point)
	views := make([]window.View, 0, len(m.items))
	for _, w := range m.items {
		m.updateGeometry(w)
		v := w.view
		in.Visible = in.Visible || (win.IsWindowVisible(win.HWND(w.handle)) && !win.IsIconic(win.HWND(w.handle)))
		views = append(views, v)
		if foreground == w.handle {
			in.Focused = true
			in.Window = v.ID
			client := point
			win.ScreenToClient(win.HWND(w.handle), &client)
			in.X, in.Y = v.StagePoint(float64(client.X), float64(client.Y))
		}
	}
	m.snapshotMu.Lock()
	in.Keys = append(m.snapshot.Keys, in.Keys...)
	in.Released = append(m.snapshot.Released, in.Released...)
	in.Text = m.snapshot.Text + in.Text
	in.Wheel += m.snapshot.Wheel
	in.Pressed = in.Pressed || m.snapshot.Pressed
	m.snapshot = in
	m.positions = views
	m.pendingClosed = append(m.pendingClosed, m.closed...)
	m.snapshotMu.Unlock()
	m.events.Keys = nil
	m.events.Released = nil
	m.events.Text = ""
	m.events.Wheel = 0
	m.events.Pressed = false
	m.closed = nil
}

func (m *windowManager) poll() (window.Input, []string, []window.View) {
	m.snapshotMu.Lock()
	defer m.snapshotMu.Unlock()
	in := m.snapshot
	m.snapshot.Keys = nil
	m.snapshot.Released = nil
	m.snapshot.Text = ""
	m.snapshot.Wheel = 0
	m.snapshot.Pressed = false
	closed := m.pendingClosed
	m.pendingClosed = nil
	return in, closed, m.positions
}

func (m *windowManager) close() {
	m.tasks <- func() {
		for _, w := range m.items {
			win.PostMessage(win.HWND(w.handle), win.WM_CANCELMODE, 0, 0)
		}
	}
	close(m.stop)
	m.wake()
	<-m.done
}

func concealSource(titleText string) (uintptr, error) {
	title, _ := windows.UTF16PtrFromString(titleText)
	class, _ := windows.UTF16PtrFromString("GLFW30")
	hwnd := uintptr(win.FindWindow(class, title))
	if hwnd == 0 {
		return 0, fmt.Errorf("Ebitengine source HWND not found")
	}
	var pid uint32
	win.GetWindowThreadProcessId(win.HWND(hwnd), &pid)
	if pid != windows.GetCurrentProcessId() {
		return 0, fmt.Errorf("source HWND belongs to another process")
	}
	gdi := windows.NewLazySystemDLL("gdi32.dll")
	region, _, err := gdi.NewProc("CreateRectRgn").Call(0, 0, 0, 0)
	if region == 0 {
		return 0, fmt.Errorf("CreateRectRgn: %w", err)
	}
	ok, _, err := user32.NewProc("SetWindowRgn").Call(hwnd, region, 1)
	if ok == 0 {
		gdi.NewProc("DeleteObject").Call(region)
		return 0, fmt.Errorf("SetWindowRgn: %w", err)
	}
	return hwnd, nil
}
