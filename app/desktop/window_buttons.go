package desktop

import (
	"fmt"
	"github.com/hajimehoshi/ebiten/v2"
	"gowarp/app/host"
)

type windowButtonState struct {
	disableMinimize, disableMaximize, disableClose, fullscreen, border, resizable bool
	opacity                                                                       float64
}

func (g *game) applyWindowButtons() error {
	if g.native != nil {
		return nil
	}
	if !g.windowReady {
		g.initMainWindow()
	}
	v := g.mainWindow
	state := windowButtonState{v.DisableMinimize, v.DisableMaximize, v.DisableClose, g.sampleFullscreen(), v.Border, v.Resizable, v.Alpha()}
	if g.windowButtonsApplied && g.windowButtonsState == state {
		return nil
	}
	if !g.windowButtonsApplied && !v.DisableMinimize && !v.DisableMaximize && !v.DisableClose && v.Alpha() == 1 && g.windowButtonsState == (windowButtonState{}) {
		return nil
	}
	var err error
	ebiten.RunOnMainThread(func() {
		err = host.SetWindowProperties(v.DisableMinimize, v.DisableMaximize || !v.Resizable, v.DisableClose, v.Alpha())
	})
	if err != nil {
		return fmt.Errorf("window properties: %w", err)
	}
	g.windowButtonsApplied, g.windowButtonsState = true, state
	return nil
}
