package native

import (
	"fmt"
	"syscall"
	"unsafe"
)

var windowUser32 = syscall.NewLazyDLL("user32.dll")

/*
Find the player window on its event thread, including hidden or unfocused startup. Update applies
styles before the first presentation.
*/
func SetWindowButtons(disableMinimize, disableMaximize bool) error {
	var handle uintptr
	var name [64]uint16
	callback := syscall.NewCallback(func(window, _ uintptr) uintptr {
		/*
			GLFW's hidden message window uses the same class. Only the player
			has WS_EX_APPWINDOW, including while hidden or in fullscreen.
		*/
		const exStyleIndex = ^uintptr(19) // GWL_EXSTYLE = -20
		exStyle, _, _ := windowUser32.NewProc("GetWindowLongW").Call(window, exStyleIndex)
		if exStyle&0x00040000 == 0 { // WS_EX_APPWINDOW
			return 1
		}
		windowUser32.NewProc("GetClassNameW").Call(window, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
		if syscall.UTF16ToString(name[:]) == "GLFW30" {
			handle = window
			return 0
		}
		return 1
	})
	thread, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentThreadId").Call()
	windowUser32.NewProc("EnumThreadWindows").Call(thread, callback, 0)
	if handle == 0 {
		return fmt.Errorf("player window not found")
	}
	// Window styles are 32-bit even on 64-bit Windows; no pointer-sized API needed.
	const styleIndex = ^uintptr(15) // GWL_STYLE = -16
	style, _, err := windowUser32.NewProc("GetWindowLongW").Call(handle, styleIndex)
	if style == 0 {
		return err
	}
	updated := style
	if disableMinimize {
		updated &^= 0x00020000 // WS_MINIMIZEBOX
	}
	if disableMaximize {
		updated &^= 0x00010000 // WS_MAXIMIZEBOX
	}
	if updated == style {
		return nil
	}
	previous, _, err := windowUser32.NewProc("SetWindowLongW").Call(handle, styleIndex, updated)
	if previous == 0 {
		return err
	}
	// Refresh the title bar without moving, resizing, activating, or raising it.
	ok, _, err := windowUser32.NewProc("SetWindowPos").Call(handle, 0, 0, 0, 0, 0, 0x37)
	if ok == 0 {
		return err
	}
	return nil
}
