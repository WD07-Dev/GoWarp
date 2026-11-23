package desktop

import (
	"fmt"
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/app/config"
	"gowarp/app/ui"
	"gowarp/core/vm"
)

// TurboWarp reserves 48 logical pixels above the stage for player controls.
const playerControlsHeight = 48

func playerWindowSize(s *config.Settings) (int, int) {
	width, height := s.DisplaySize()
	if s.Player.Controls {
		height += playerControlsHeight
	}
	return width, height
}

func (g *game) stageBounds(width, height int) image.Rectangle {
	w, h := g.controller.renderer.Size()
	top := 0
	if g.controller.options.Settings.Player.Controls {
		top = min(g.pixels(playerControlsHeight), max(0, height-1))
	}
	available := max(1, height-top)
	drawWidth, drawHeight := max(1, width), available
	if g.controller.options.Settings.Window.Fit != "stretch" {
		scale := math.Min(float64(drawWidth)/float64(w), float64(available)/float64(h))
		if g.controller.options.Settings.Window.Fit == "integer" && g.sampleFullscreen() && scale >= 1 {
			scale = math.Floor(scale)
		}
		drawWidth = max(1, int(math.Round(float64(w)*scale)))
		drawHeight = max(1, int(math.Round(float64(h)*scale)))
	}
	x := (width - drawWidth) / 2
	y := top + (available-drawHeight)/2
	return image.Rect(x, y, x+drawWidth, y+drawHeight)
}

func (g *game) toolbarPosition(x, y int) (int, bool) {
	s := g.controller.options.Settings
	if s == nil {
		return x, y >= 0 && y < ToolbarHeight
	}
	if !s.Player.Controls {
		return 0, false
	}
	stage := g.stageBounds(g.outsideWidth, g.outsideHeight)
	bar := image.Rect(stage.Min.X, stage.Min.Y-g.pixels(playerControlsHeight), stage.Max.X, stage.Min.Y)
	return int(math.Floor(float64(x-stage.Min.X) / g.dpi())), image.Pt(x, y).In(bar)
}

func (g *game) drawPlayerControls(screen *ebiten.Image, stage image.Rectangle) {
	s := g.controller.options.Settings
	if !s.Player.Controls {
		return
	}
	face := g.scaledFace(g.dpi(), false)
	bounds := image.Rect(0, 0, stage.Dx(), g.pixels(playerControlsHeight))
	if g.controlsCanvas == nil || g.controlsCanvas.Bounds() != bounds {
		if g.controlsCanvas != nil {
			g.controlsCanvas.Deallocate()
		}
		g.controlsCanvas = ebiten.NewImage(bounds.Dx(), bounds.Dy())
	}
	g.controlsCanvas.Fill(ui.Background)
	painter := overlayPainter{
		gpu: g.controlsCanvas, texts: &g.texts, face: face,
		sx: g.dpi(), sy: g.dpi(), textScale: g.dpi(),
	}
	painter.fill(image.Rect(8, 12, 32, 36), vm.ParseColor(s.Player.Accent))
	painter.text(13, 29, "▶", ui.Text)
	painter.fill(image.Rect(42, 12, 66, 36), ui.Field)
	painter.text(47, 29, "■", ui.Text)
	painter.text(80, 29, "⛶", ui.Text)
	painter.text(112, 29, fmt.Sprintf("%g FPS", g.controller.vm.FPS()), ui.Muted)
	options := &ebiten.DrawImageOptions{}
	options.GeoM.Translate(float64(stage.Min.X), float64(stage.Min.Y-g.pixels(playerControlsHeight)))
	screen.DrawImage(g.controlsCanvas, options)
}
