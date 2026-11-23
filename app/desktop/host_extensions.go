package desktop

import (
	"fmt"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/pkg/browser"
)

func (g *game) sampleHostFrame() {
	now := time.Now()
	elapsed := 0.0
	g.updateBudget = time.Second / 60
	if !g.lastHostFrame.IsZero() {
		interval := now.Sub(g.lastHostFrame)
		elapsed = float64(interval) / float64(time.Millisecond)
		// Allow missed presentation time, capped at two nominal frames.
		g.updateBudget = min(time.Second/30, max(time.Second/60, interval))
	}
	g.updateTime = 0
	g.frameTicks = 0
	g.updateBudgetUntil = now.Add(g.updateBudget)
	g.lastHostFrame = now
	g.controller.vm.SetHostFrame(g.sampleFullscreen(), elapsed)
}

func (g *game) applyHostCommands() error {
	select {
	case err := <-g.urlErrors:
		return err
	default:
	}
	for _, command := range g.controller.vm.DrainHostCommands() {
		switch command.Kind {
		case "fullscreen":
			g.projectFullscreen = command.Value == "true"
			ebiten.SetFullscreen(g.hostFullscreen || g.projectFullscreen)
			g.windowFocusRead, g.windowFullscreenRead = false, false
		case "title":
			ebiten.SetWindowTitle(command.Value)
		case "stop-all":
			if s := g.options.Settings; s != nil && s.Player.CloseOnStop {
				if g.options.Diagnostic != nil {
					g.options.Diagnostic("project requested stop all; closing window")
				}
				return ebiten.Termination
			}
		case "close":
			if g.options.Diagnostic != nil {
				g.options.Diagnostic("project requested window close")
			}
			return ebiten.Termination
		case "open-url":
			if err := g.openURL(command.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *game) openURL(url string) error {
	if strings.ContainsRune(url, 0) {
		return fmt.Errorf("URL contains a NUL character")
	}
	if g.urlErrors == nil {
		g.urlErrors = make(chan error, 1)
	}
	urlErrors := g.urlErrors
	// The launcher may wait for a browser process; keep it off the game loop.
	go func() {
		if err := browser.OpenURL(url); err != nil {
			select {
			case urlErrors <- fmt.Errorf("open URL: %w", err):
			default:
			}
		}
	}()
	return nil
}
