package desktop

import (
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"gowarp/app/i18n"
	"gowarp/app/ui"
	"gowarp/core/render"
	"gowarp/core/vm"
)

/*
The packaged player composes costumes on the GPU. The CPU renderer remains
responsible for sensing and deterministic headless output.
*/
func (g *game) drawPlayer(screen *ebiten.Image) {
	c := g.controller
	s := c.options.Settings
	stage := g.stageBounds(screen.Bounds().Dx(), screen.Bounds().Dy())
	width, height := render.DisplaySize(stage.Dx(), stage.Dy())
	if g.stageCanvas == nil || g.stageCanvas.Bounds().Size() != image.Pt(width, height) {
		if g.stageCanvas != nil {
			g.stageCanvas.Deallocate()
		}
		g.stageCanvas = ebiten.NewImage(width, height)
	}
	w, h := c.renderer.Size()
	scale := math.Min(float64(width)/float64(w), float64(height)/float64(h))
	face := g.scaledFace(scale, true)
	painter := newGPUOverlayPainter(g.stageCanvas, face, w, h, &g.texts)
	if g.failure != nil {
		g.stageCanvas.Fill(ui.Background)
		message := i18n.Tr("player.error", g.failure.Error())
		for i, line := range painter.wrap(message, w-32) {
			painter.text(16, 40+i*20, line, ui.Text)
		}
	} else {
		scene := c.displayScene()
		if err := g.gpu.Draw(g.stageCanvas, c.renderer, scene); err != nil {
			g.drawErr = err
			return
		}
		if err := c.assets.Err(); err != nil {
			g.drawErr = err
		}
		c.paintOverlays(painter, scene, 0, h)
	}
	screen.Fill(vm.ParseColor(s.Window.Background))
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(stage.Dx())/float64(width), float64(stage.Dy())/float64(height))
	op.GeoM.Translate(float64(stage.Min.X), float64(stage.Min.Y))
	screen.DrawImage(g.stageCanvas, op)
	if g.waitingStart && g.failure == nil {
		g.drawStartOverlay(screen, stage)
	}
	g.drawPlayerControls(screen, stage, 1)
}

func (g *game) drawStartOverlay(screen *ebiten.Image, stage image.Rectangle) {
	vector.DrawFilledRect(screen, float32(stage.Min.X), float32(stage.Min.Y), float32(stage.Dx()), float32(stage.Dy()), color.NRGBA{R: 128, G: 128, B: 128, A: 128}, false)
	x, y := float64(stage.Min.X+stage.Max.X)/2, float64(stage.Min.Y+stage.Max.Y)/2
	radius := math.Min(40*g.dpi(), float64(min(stage.Dx(), stage.Dy()))*.4)
	vector.DrawFilledCircle(screen, float32(x), float32(y), float32(radius), color.NRGBA{R: 255, G: 255, B: 255, A: 192}, true)
	vector.StrokeCircle(screen, float32(x), float32(y), float32(radius), float32(2*g.dpi()), color.White, true)
	// Trace the 17x18 flag in display pixels to keep curves sharp at any DPI.
	scale := radius * 1.1 / 18
	px := func(value float64) float32 { return float32(x + (value-8.5)*scale) }
	py := func(value float64) float32 { return float32(y + (value-9)*scale) }
	outline := color.RGBA{R: 69, G: 153, B: 61, A: 255}
	var pole vector.Path
	pole.MoveTo(px(.7), py(.7))
	pole.LineTo(px(.7), py(17.3))
	stroke := &vector.StrokeOptions{Width: float32(1.2 * scale), LineCap: vector.LineCapRound, LineJoin: vector.LineJoinRound}
	border := &vector.DrawPathOptions{AntiAlias: true}
	border.ColorScale.ScaleWithColor(outline)
	vector.StrokePath(screen, &pole, stroke, border)

	var flag vector.Path
	flag.MoveTo(px(1), py(2.5))
	flag.CubicTo(px(6), py(-1.5), px(8.5), py(7), px(16), py(2.5))
	flag.LineTo(px(16), py(12.5))
	flag.CubicTo(px(9.5), py(16), px(7.5), py(8), px(1), py(12))
	flag.Close()
	fill := &vector.DrawPathOptions{AntiAlias: true}
	fill.ColorScale.ScaleWithColor(color.RGBA{R: 76, G: 191, B: 86, A: 255})
	vector.FillPath(screen, &flag, nil, fill)
	vector.StrokePath(screen, &flag, stroke, border)
}
