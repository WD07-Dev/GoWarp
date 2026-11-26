package desktop

import (
	"fmt"
	"image"
	"time"

	"gowarp/core/render"
)

func (g *game) displayFPS() int {
	if g.options.Editor != nil && g.options.Editor.Embedded {
		return 60
	}
	if g.controller == nil || g.controller.options.Settings == nil || g.options.Replay || g.options.Screenshot != "" {
		return 0
	}
	return g.controller.options.Settings.Player.RenderFPS
}

func (g *game) logDisplaySize(size image.Point) {
	if g.displayLogged || g.options.Diagnostic == nil || g.controller == nil || g.controller.options.Settings == nil {
		return
	}
	g.displayLogged = true
	stage := g.stageBounds(size.X, size.Y)
	width, height := render.DisplaySize(stage.Dx(), stage.Dy())
	g.options.Diagnostic(fmt.Sprintf("display render_fps_limit=%d render_size=%dx%d stage_size=%dx%d", g.displayFPS(), width, height, stage.Dx(), stage.Dy()))
}

func (g *game) shouldPresent(now time.Time, size image.Point) bool {
	fps := g.displayFPS()
	if fps == 0 {
		g.nextPresentation = time.Time{}
		return true
	}
	interval := time.Second / time.Duration(fps)
	force := g.nextPresentation.IsZero() || size != g.presentationSize || g.focusDraws > 0 || g.limitReached || g.failure != nil || g.waitingStart != g.presentationWaiting
	if !force && now.Add(500*time.Microsecond).Before(g.nextPresentation) {
		return false
	}
	if force || now.Sub(g.nextPresentation) > interval {
		g.nextPresentation = now.Add(interval)
	} else {
		g.nextPresentation = g.nextPresentation.Add(interval)
	}
	g.presentationSize, g.presentationWaiting = size, g.waitingStart
	return true
}
