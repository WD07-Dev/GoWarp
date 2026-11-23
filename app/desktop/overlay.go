package desktop

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font"
)

/*
overlayPainter maps logical stage coordinates into the drawing surface.
*/
type overlayPainter struct {
	dst               *image.RGBA
	gpu               *ebiten.Image
	texts             *gpuTextCache
	face              font.Face
	sx, sy, textScale float64
}

func newOverlayPainter(dst *image.RGBA, face font.Face, width, height int) overlayPainter {
	sx, sy := float64(dst.Bounds().Dx())/float64(width), float64(dst.Bounds().Dy())/float64(height)
	return overlayPainter{dst: dst, face: face, sx: sx, sy: sy, textScale: math.Min(sx, sy)}
}

func newGPUOverlayPainter(dst *ebiten.Image, face font.Face, width, height int, texts *gpuTextCache) overlayPainter {
	sx, sy := float64(dst.Bounds().Dx())/float64(width), float64(dst.Bounds().Dy())/float64(height)
	return overlayPainter{gpu: dst, texts: texts, face: face, sx: sx, sy: sy, textScale: math.Min(sx, sy)}
}

func (p overlayPainter) clipped(r image.Rectangle) overlayPainter {
	if p.gpu != nil {
		p.gpu = p.gpu.SubImage(p.rect(r).Intersect(p.gpu.Bounds())).(*ebiten.Image)
	} else {
		p.dst = p.dst.SubImage(p.rect(r).Intersect(p.dst.Bounds())).(*image.RGBA)
	}
	return p
}

func (p overlayPainter) rect(r image.Rectangle) image.Rectangle {
	return image.Rect(
		int(math.Round(float64(r.Min.X)*p.sx)),
		int(math.Round(float64(r.Min.Y)*p.sy)),
		int(math.Round(float64(r.Max.X)*p.sx)),
		int(math.Round(float64(r.Max.Y)*p.sy)),
	)
}

func (p overlayPainter) fill(r image.Rectangle, c color.Color) {
	if p.gpu != nil {
		p.gpu.SubImage(p.rect(r).Intersect(p.gpu.Bounds())).(*ebiten.Image).Fill(c)
		return
	}
	draw.Draw(p.dst, p.rect(r), image.NewUniform(c), image.Point{}, draw.Src)
}

func (p overlayPainter) text(x, y int, s string, c color.Color) {
	px, py := int(math.Round(float64(x)*p.sx)), int(math.Round(float64(y)*p.sy))
	if p.gpu != nil {
		p.texts.draw(p.gpu, p.face, px, py, p.sx/p.textScale, p.sy/p.textScale, s, c)
		return
	}
	writeText(p.dst, p.face, px, py, s, c)
}

func (p overlayPainter) measure(s string) int {
	return int(math.Ceil(float64(font.MeasureString(p.face, s)) / 64 / p.textScale))
}

func (p overlayPainter) wrap(s string, width int) []string {
	return wrapText(p.face, s, int(math.Round(float64(width)*p.textScale)))
}
