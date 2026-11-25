package desktop

import (
	"fmt"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/core/window"
)

func (g *game) initMainWindow() {
	w, h := ebiten.WindowSize()
	x, y := ebiten.WindowPosition()
	v := window.View{
		ID: "main", Title: "GoWarp — " + filepath.Base(g.path),
		X: x, Y: y, Width: w, Height: h, StageWidth: 480, StageHeight: 360,
		Border: ebiten.IsWindowDecorated(), Resizable: ebiten.WindowResizingMode() == ebiten.WindowResizingModeEnabled,
	}
	if s := g.options.Settings; s != nil {
		alpha := s.Platform.Opacity
		v.Title, v.Border, v.Resizable = s.Player.Title, s.Platform.Border, s.Platform.Resizable
		v.Topmost = s.Platform.Topmost
		v.DisableMinimize, v.DisableMaximize = s.Platform.DisableMinimize, s.Platform.DisableMaximize
		v.DisableClose, v.Opacity, v.Fullscreen = s.Platform.DisableClose, &alpha, s.Platform.Fullscreen
		v.StageWidth, v.StageHeight = float64(s.Width), float64(s.Height)
	}
	g.mainWindow, g.windowReady = v, true
}

// Ebitengine's single window uses monitor-relative device-independent pixels.
func (g *game) readMainWindow() window.View {
	if !g.windowReady {
		g.initMainWindow()
	}
	if !g.windowDirty {
		v := &g.mainWindow
		v.X, v.Y = ebiten.WindowPosition()
		v.Width, v.Height = ebiten.WindowSize()
		v.Fullscreen = ebiten.IsFullscreen()
	}
	return g.mainWindow
}

func (g *game) windowCall(op string, args []any) (any, error) {
	if g.native != nil {
		return g.nativeWindowCall(op, args)
	}
	if op == "create" {
		return nil, fmt.Errorf("creating additional windows requires Windows multiwindow mode")
	}
	current := g.readMainWindow()
	views, value, err := editWindows([]window.View{current}, op, args)
	if err != nil || op == "get" || op == "list" {
		return value, err
	}
	if len(views) == 0 {
		g.windowCloseRequested = true
		return nil, nil
	}
	v := views[0]
	if v.StageX != current.StageX || v.StageY != current.StageY ||
		v.StageWidth != current.StageWidth || v.StageHeight != current.StageHeight {
		return nil, fmt.Errorf("stage viewport changes require Windows multiwindow mode")
	}
	if v.Hidden != current.Hidden {
		return nil, fmt.Errorf("hiding windows requires Windows multiwindow mode")
	}
	g.mainWindow, g.windowDirty = v, true
	return value, nil
}

func (g *game) applyWindowChanges() error {
	if g.windowCloseRequested {
		return ebiten.Termination
	}
	if g.native != nil {
		return g.syncNativeViews()
	}
	if !g.windowDirty {
		return nil
	}
	v := g.mainWindow
	if ebiten.IsWindowDecorated() != v.Border {
		ebiten.SetWindowDecorated(v.Border)
	}
	resizing := ebiten.WindowResizingModeDisabled
	if v.Resizable {
		resizing = ebiten.WindowResizingModeEnabled
	}
	if ebiten.WindowResizingMode() != resizing {
		ebiten.SetWindowResizingMode(resizing)
	}
	x, y := ebiten.WindowPosition()
	if x != v.X || y != v.Y {
		ebiten.SetWindowPosition(v.X, v.Y)
	}
	w, h := ebiten.WindowSize()
	if w != v.Width || h != v.Height {
		ebiten.SetWindowSize(v.Width, v.Height)
	}
	ebiten.SetWindowTitle(v.Title)
	ebiten.SetWindowFloating(v.Topmost)
	ebiten.SetWindowMousePassthrough(v.Passthrough)
	if ebiten.IsFullscreen() != v.Fullscreen {
		g.hostFullscreen, g.projectFullscreen = v.Fullscreen, false
		ebiten.SetFullscreen(v.Fullscreen)
		g.windowFocusRead, g.windowFullscreenRead = false, false
	}
	g.windowDirty = false
	return g.applyWindowButtons()
}
