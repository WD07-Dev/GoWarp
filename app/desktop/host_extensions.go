package desktop

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gowarp/core/vm"

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
	g.controller.vm.SetHostWindowProvider(g.sampleHostWindow)
}

func (g *game) sampleHostWindow() (vm.HostWindow, error) {
	monitor := ebiten.Monitor()
	if monitor == nil {
		return vm.HostWindow{}, fmt.Errorf("window monitor is unavailable")
	}
	w, h := monitor.Size()
	title := "GoWarp — " + filepath.Base(g.path)
	if s := g.options.Settings; s != nil {
		title = s.Player.Title
	}
	if g.windowReady {
		title = g.mainWindow.Title
	}
	focused := ebiten.IsFocused()
	if g.native != nil {
		focused = g.native.input.Focused
	}
	return vm.HostWindow{ScreenWidth: w, ScreenHeight: h, Focused: focused, Title: title}, nil
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
			if g.native != nil {
				_, err := g.nativeWindowCall("set", []any{"main", fmt.Sprintf("{\"fullscreen\":%t}", command.Value == "true")})
				if err != nil {
					return err
				}
				continue
			}
			g.projectFullscreen = command.Value == "true"
			g.mainWindow.Fullscreen = g.hostFullscreen || g.projectFullscreen
			ebiten.SetFullscreen(g.mainWindow.Fullscreen)
			g.windowFocusRead, g.windowFullscreenRead = false, false
		case "title":
			if g.native != nil {
				for i := range g.native.layout.Views {
					g.native.layout.Views[i].Title = command.Value
				}
				g.native.dirty = true
			} else {
				ebiten.SetWindowTitle(command.Value)
				g.mainWindow.Title = command.Value
			}
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
