//go:build windows && amd64

package multiwindow

import (
	"fmt"

	"github.com/lxn/win"
)

func setWindowIcons(hwnd uintptr, icons [2]uintptr) {
	for kind, icon := range icons {
		win.SendMessage(win.HWND(hwnd), win.WM_SETICON, uintptr(kind), icon)
	}
}

func destroyIcons(icons [2]uintptr) {
	for _, icon := range icons {
		if icon != 0 {
			win.DestroyIcon(win.HICON(icon))
		}
	}
}

func (m *windowManager) copySourceIcons(hwnd uintptr) error {
	var source, copies [2]uintptr
	for kind, classIndex := range []uintptr{^uintptr(33), ^uintptr(13)} { // GCLP_HICONSM / GCLP_HICON
		source[kind] = win.SendMessage(win.HWND(hwnd), win.WM_GETICON, uintptr(kind), 0)
		if source[kind] == 0 {
			source[kind], _, _ = user32.NewProc("GetClassLongPtrW").Call(hwnd, classIndex)
		}
	}
	if source[0] == 0 {
		source[0] = source[1]
	}
	if source[1] == 0 {
		source[1] = source[0]
	}
	for kind, icon := range source {
		if icon == 0 {
			continue
		}
		copy, _, err := user32.NewProc("CopyIcon").Call(icon)
		if copy == 0 {
			destroyIcons(copies)
			return fmt.Errorf("copy source window icon: %w", err)
		}
		copies[kind] = copy
	}
	m.do(func() {
		old := m.icons
		m.icons = copies
		for _, w := range m.items {
			setWindowIcons(w.handle, copies)
		}
		for hwnd := range m.retired {
			setWindowIcons(hwnd, copies)
		}
		destroyIcons(old)
	})
	return nil
}
