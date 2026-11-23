package desktop

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/app/host"
)

func (g *game) applyWindowButtons() error {
	s := g.options.Settings
	if s == nil || (!s.Window.DisableMinimize && !s.Window.DisableMaximize) {
		return nil
	}
	fullscreen := g.sampleFullscreen()
	if g.windowButtonsApplied && g.windowButtonsFullscreen == fullscreen {
		return nil
	}
	var err error
	ebiten.RunOnMainThread(func() {
		err = host.SetWindowButtons(s.Window.DisableMinimize, s.Window.DisableMaximize)
	})
	if err != nil {
		return fmt.Errorf("window buttons: %w", err)
	}
	g.windowButtonsApplied = true
	g.windowButtonsFullscreen = fullscreen
	return nil
}
