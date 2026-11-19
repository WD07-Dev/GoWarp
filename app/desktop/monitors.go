package desktop

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"golang.org/x/image/font"

	"gowarp/core/vm"
)

var monitorOrange = color.RGBA{255, 140, 26, 255}

// drawMonitors uses stage coordinates and clips every panel to the stage.
func drawMonitors(dst *image.RGBA, face font.Face, monitors []vm.MonitorState, yOffset int) {
	widthStage, heightStage := dst.Bounds().Dx(), dst.Bounds().Dy()-yOffset
	if yOffset > 0 {
		heightStage -= AnswerHeight
	}
	drawMonitorsAt(newOverlayPainter(dst, face, dst.Bounds().Dx(), dst.Bounds().Dy()), monitors, yOffset, widthStage, heightStage)
}

func drawMonitorsAt(painter overlayPainter, monitors []vm.MonitorState, yOffset, widthStage, heightStage int) {
	viewport := image.Rect(0, yOffset, widthStage, yOffset+heightStage)
	if viewport.Empty() {
		return
	}
	stage := painter.clipped(viewport)
	panel := color.RGBA{238, 238, 238, 255}
	border := color.RGBA{180, 180, 180, 255}
	for _, m := range monitors {
		if !m.Visible || math.IsNaN(m.X) || math.IsNaN(m.Y) || m.X >= float64(widthStage) || m.Y >= float64(heightStage) || m.X < -float64(widthStage) || m.Y < -float64(heightStage) {
			continue
		}
		x, y := int(m.X), int(m.Y)+yOffset
		label, value := monitorText(m.Label), monitorText(m.Value)
		labelWidth := painter.measure(label)
		textWidth := painter.measure(value)
		valueWidth := max(40, textWidth+10)
		width, height := labelWidth+valueWidth+16, 22
		if m.List {
			width = int(mathClamp(m.Width, 100, float64(widthStage)))
			height = int(mathClamp(m.Height, 60, float64(heightStage)))
		} else if m.Mode == "large" {
			width = max(40, textWidth+8)
		}
		bounds := image.Rect(x, y, x+width, y+height).Intersect(viewport)
		if bounds.Empty() {
			continue
		}
		target := stage.clipped(bounds)
		if !m.List && m.Mode == "large" {
			target.fill(bounds, monitorOrange)
			target.text(x+(width-textWidth)/2, y+16, value, color.White)
			continue
		}
		box := image.Rect(x, y, x+width, y+height)
		target.fill(box, border)
		target.fill(box.Inset(1), panel)
		target.text(x+5, y+15, label, color.Black)
		if m.List {
			for i, item := range m.Items {
				row := y + 20 + i*16
				if row > y+height-16 {
					break
				}
				target.text(x+4, row+12, fmt.Sprintf("%d %s", i+1, monitorText(item)), color.Black)
			}
		} else {
			badge := image.Rect(x+labelWidth+10, y+3, x+labelWidth+10+valueWidth, y+19)
			target.fill(badge, monitorOrange)
			target.text(x+labelWidth+15, y+15, value, color.White)
		}
	}
}

func monitorText(s string) string {
	count := 0
	for i := range s {
		if count == 1024 {
			return s[:i] + "…"
		}
		count++
	}
	return s
}
