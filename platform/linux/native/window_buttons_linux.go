package native

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/ebitengine/purego"
)

type windowX11 struct {
	openDisplay     func(*byte) uintptr
	closeDisplay    func(uintptr) int32
	rootWindow      func(uintptr) uintptr
	internAtom      func(uintptr, string, int32) uintptr
	queryTree       func(uintptr, uintptr, *uintptr, *uintptr, **uintptr, *uint32) int32
	getProperty     func(uintptr, uintptr, uintptr, int, int, int32, uintptr, *uintptr, *int32, *uintptr, *uintptr, **uintptr) int32
	changeProperty  func(uintptr, uintptr, uintptr, uintptr, int32, int32, *uintptr, int32) int32
	free            func(unsafe.Pointer) int32
	sync            func(uintptr, int32) int32
	setErrorHandler func(uintptr) uintptr
}

/*
A window can disappear while walking the X11 tree. Ignore that BadWindow
response during discovery, restoring the application's handler before return.
*/
var windowX11ErrorHandler = purego.NewCallback(func(uintptr, uintptr) int32 {
	return 0
})

func SetWindowButtons(disableMinimize, disableMaximize bool) error {
	lib, err := purego.Dlopen("libX11.so.6", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return err
	}
	defer purego.Dlclose(lib)
	var x windowX11
	purego.RegisterLibFunc(&x.openDisplay, lib, "XOpenDisplay")
	purego.RegisterLibFunc(&x.closeDisplay, lib, "XCloseDisplay")
	purego.RegisterLibFunc(&x.rootWindow, lib, "XDefaultRootWindow")
	purego.RegisterLibFunc(&x.internAtom, lib, "XInternAtom")
	purego.RegisterLibFunc(&x.queryTree, lib, "XQueryTree")
	purego.RegisterLibFunc(&x.getProperty, lib, "XGetWindowProperty")
	purego.RegisterLibFunc(&x.changeProperty, lib, "XChangeProperty")
	purego.RegisterLibFunc(&x.free, lib, "XFree")
	purego.RegisterLibFunc(&x.sync, lib, "XSync")
	purego.RegisterLibFunc(&x.setErrorHandler, lib, "XSetErrorHandler")
	display := x.openDisplay(nil)
	if display == 0 {
		return fmt.Errorf("cannot open X11 display")
	}
	defer x.closeDisplay(display)
	previous := x.setErrorHandler(windowX11ErrorHandler)
	defer x.setErrorHandler(previous)
	pidAtom := x.internAtom(display, "_NET_WM_PID", 0)
	window := x.findWindow(display, x.rootWindow(display), pidAtom, 0)
	if window == 0 {
		return fmt.Errorf("player window not found")
	}
	atom := x.internAtom(display, "_MOTIF_WM_HINTS", 0)
	values := x.property(display, window, atom)
	var hints [5]uintptr
	copy(hints[:], values)
	// MWM_FUNC_ALL reverses the mask: other bits then mean exclusions.
	const allFunctions = uintptr(2 | 4 | 8 | 16 | 32)
	if hints[0]&1 == 0 {
		hints[1] = allFunctions
	} else if hints[1]&1 != 0 {
		hints[1] = allFunctions &^ hints[1]
	}
	hints[0] |= 1 // MWM_HINTS_FUNCTIONS; preserve decorations and other fields.
	if disableMinimize {
		hints[1] &^= 8
	}
	if disableMaximize {
		hints[1] &^= 16
	}
	// Xlib stores each 32-bit property item in a native unsigned long.
	x.changeProperty(display, window, atom, atom, 32, 0, &hints[0], 5)
	x.sync(display, 0)
	got := x.property(display, window, atom)
	if len(got) < 2 || got[0] != hints[0] || got[1] != hints[1] {
		return fmt.Errorf("cannot update window buttons")
	}
	return nil
}

func (x *windowX11) property(display, window, atom uintptr) []uintptr {
	var kind, count, remaining uintptr
	var format int32
	var data *uintptr
	status := x.getProperty(display, window, atom, 0, 5, 0, 0, &kind, &format, &count, &remaining, &data)
	if data != nil {
		defer x.free(unsafe.Pointer(data))
	}
	if status != 0 || format != 32 || count > 5 || data == nil {
		return nil
	}
	return append([]uintptr(nil), unsafe.Slice(data, int(count))...)
}

func (x *windowX11) findWindow(display, window, pidAtom uintptr, depth int) uintptr {
	pid := x.property(display, window, pidAtom)
	if len(pid) == 1 && pid[0] == uintptr(os.Getpid()) {
		return window
	}
	if depth >= 4 {
		return 0
	}
	var root, parent uintptr
	var children *uintptr
	var count uint32
	status := x.queryTree(display, window, &root, &parent, &children, &count)
	if children != nil {
		defer x.free(unsafe.Pointer(children))
	}
	if status == 0 || children == nil {
		return 0
	}
	for _, child := range unsafe.Slice(children, int(count)) {
		if found := x.findWindow(display, child, pidAtom, depth+1); found != 0 {
			return found
		}
	}
	return 0
}
