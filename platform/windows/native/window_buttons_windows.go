package native

import (
	"fmt"
	"math"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var windowUser32 = windows.NewLazySystemDLL("user32.dll")

// Allocate this callback once: Windows callback trampolines cannot be freed.
var findPlayerWindow = syscall.NewCallback(func(window, context uintptr) uintptr {
	exStyle := win.GetWindowLong(win.HWND(window), win.GWL_EXSTYLE)
	if exStyle&win.WS_EX_APPWINDOW == 0 { // WS_EX_APPWINDOW excludes GLFW's message window.
		return 1
	}
	var name [64]uint16
	win.GetClassName(win.HWND(window), &name[0], len(name))
	if syscall.UTF16ToString(name[:]) == "GLFW30" {
		*(*uintptr)(unsafe.Pointer(context)) = window
		return 0
	}
	return 1
})

// SetWindowProperties runs on the GLFW event thread, including hidden startup.
// The caller handles close requests; disabling SC_CLOSE also greys the title button.
func SetWindowProperties(disableMinimize, disableMaximize, disableClose bool, opacity float64) error {
	if math.IsNaN(opacity) || math.IsInf(opacity, 0) || opacity < 0 || opacity > 1 {
		return fmt.Errorf("window opacity must be between 0 and 1")
	}
	var handle uintptr
	thread := windows.GetCurrentThreadId()
	windowUser32.NewProc("EnumThreadWindows").Call(uintptr(thread), findPlayerWindow, uintptr(unsafe.Pointer(&handle)))
	runtime.KeepAlive(&handle)
	if handle == 0 {
		return fmt.Errorf("player window not found")
	}
	const styleIndex = ^uintptr(15)
	style, _, err := windowUser32.NewProc("GetWindowLongW").Call(handle, styleIndex)
	if style == 0 {
		return err
	}
	updated := style &^ uintptr(win.WS_MINIMIZEBOX|win.WS_MAXIMIZEBOX)
	if !disableMinimize {
		updated |= win.WS_MINIMIZEBOX
	}
	if !disableMaximize && style&win.WS_THICKFRAME != 0 {
		updated |= win.WS_MAXIMIZEBOX
	}
	if updated != style {
		previous, _, err := windowUser32.NewProc("SetWindowLongW").Call(handle, styleIndex, updated)
		if previous == 0 {
			return err
		}
		ok, _, err := windowUser32.NewProc("SetWindowPos").Call(handle, 0, 0, 0, 0, 0, 0x37)
		if ok == 0 {
			return err
		}
	}
	menu := win.GetSystemMenu(win.HWND(handle), false)
	if menu != 0 {
		flags := uint32(win.MF_ENABLED)
		if disableClose {
			flags = win.MF_GRAYED
		}
		win.EnableMenuItem(menu, win.SC_CLOSE, flags)
		win.DrawMenuBar(win.HWND(handle))
	}
	const exStyleIndex = ^uintptr(19)
	exStyle := uintptr(uint32(win.GetWindowLong(win.HWND(handle), win.GWL_EXSTYLE)))
	layered := opacityExtendedStyle(exStyle, opacity)
	if layered != exStyle {
		// A zero previous extended style is valid. Clear LastError to distinguish it.
		win.SetLastError(0)
		previous, _, err := windowUser32.NewProc("SetWindowLongW").Call(handle, exStyleIndex, layered)
		if previous == 0 && err != syscall.Errno(0) {
			return err
		}
	}
	if layered&win.WS_EX_LAYERED != 0 {
		ok, _, err := windowUser32.NewProc("SetLayeredWindowAttributes").Call(handle, 0, uintptr(math.Round(opacity*255)), 2)
		if ok == 0 {
			return err
		}
	}
	return nil
}

// GLFW mouse passthrough requires both TRANSPARENT and LAYERED. Retain that
// combination at full opacity, while removing opacity-only layering otherwise.
func opacityExtendedStyle(style uintptr, opacity float64) uintptr {
	if opacity < 1 || style&win.WS_EX_TRANSPARENT != 0 {
		return style | win.WS_EX_LAYERED
	}
	return style &^ win.WS_EX_LAYERED
}
