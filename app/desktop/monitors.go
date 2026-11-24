package desktop

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"unicode/utf8"

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
	drawMonitorPanels(painter, monitors, yOffset, widthStage, heightStage, nil)
}

func drawMonitorPanels(painter overlayPainter, monitors []vm.MonitorState, yOffset, widthStage, heightStage int, ui *monitorUI) {
	viewport := image.Rect(0, yOffset, widthStage, yOffset+heightStage)
	if ui != nil {
		ui.hits = ui.hits[:0]
	}
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
			w, h := m.Width, m.Height
			if w <= 0 || math.IsNaN(w) {
				w = 120
			}
			if h <= 0 || math.IsNaN(h) {
				h = 200
			}
			width = int(mathClamp(w, 100, float64(widthStage)))
			height = int(mathClamp(h, 60, float64(heightStage)))
		} else if m.Mode == "large" {
			width = max(40, textWidth+8)
		} else if m.Mode == "slider" {
			height = 42
		}
		box := image.Rect(x, y, x+width, y+height)
		bounds := box.Intersect(viewport)
		if bounds.Empty() {
			continue
		}
		target := stage.clipped(bounds)
		addHit := func(kind string, rect image.Rectangle, item int) {
			if ui == nil {
				return
			}
			if m.ReadOnly && kind == "value" {
				kind = "drag"
			}
			value := ""
			if m.List && item >= m.Offset && item-m.Offset < len(m.Items) {
				value = m.Items[item-m.Offset]
			}
			ui.hits = append(ui.hits, monitorHit{index: m.Index, item: item, kind: kind, value: value,
				size: image.Pt(width, height), list: m.List,
				rect: rect.Intersect(bounds).Sub(image.Pt(0, yOffset)),
				x:    m.X, y: m.Y, low: m.SliderMin, high: m.SliderMax, count: m.Count,
				rows: max(1, (height-44)/22), offset: m.Offset})
		}
		addHit("panel", box, -1)
		if !m.List && m.Mode == "large" {
			target.fill(bounds, monitorOrange)
			target.text(x+(width-textWidth)/2, y+16, value, color.White)
			addHit("value", box, -1)
			paintMonitorEditor(target, ui, m.Index, -1, box)
			continue
		}
		target.fill(box, border)
		target.fill(box.Inset(1), panel)
		target.clipped(image.Rect(x+1, y+1, x+width-1, y+21)).text(x+5, y+15, label, color.Black)
		addHit("drag", image.Rect(x, y, x+width, y+22), -1)
		if m.List {
			rows := max(0, (height-44)/22)
			for i, item := range m.Items {
				if i >= rows {
					break
				}
				row := y + 22 + i*22
				number := fmt.Sprint(m.Offset + i + 1)
				indexWidth := max(20, painter.measure(number)+6)
				indexWidth = min(indexWidth, width-50)
				target.text(x+3, row+16, number, color.RGBA{90, 90, 90, 255})
				cell := image.Rect(x+indexWidth, row+1, x+width-24, row+21)
				target.fill(cell, monitorOrange)
				target.clipped(cell).text(cell.Min.X+4, row+16, monitorText(item), color.White)
				addHit("value", cell, m.Offset+i)
				paintMonitorEditor(target, ui, m.Index, m.Offset+i, cell)
				target.text(x+width-22, row+16, "×", color.RGBA{90, 90, 90, 255})
				addHit("delete", image.Rect(x+width-24, row, x+width-8, row+22), m.Offset+i)
			}
			footer := y + height - 22
			target.fill(image.Rect(x+1, footer, x+width-1, y+height-1), color.White)
			target.text(x+5, footer+16, "+", color.Black)
			addHit("add", image.Rect(x+1, footer, x+24, y+height), -1)
			target.clipped(image.Rect(x+24, footer, x+width-12, y+height)).text(x+26, footer+16, fmt.Sprintf("length: %d", m.Count), color.Black)
			for i := 0; i < 3; i++ {
				target.fill(image.Rect(x+width-4-i*3, y+height-4, x+width-3-i*3, y+height-3), border)
				target.fill(image.Rect(x+width-4, y+height-4-i*3, x+width-3, y+height-3-i*3), border)
			}
			addHit("resize", image.Rect(x+width-12, y+height-12, x+width, y+height), -1)
			if m.Count == 0 {
				target.text(x+6, y+38, "(empty)", color.RGBA{90, 90, 90, 255})
			}
			if m.Count > rows && rows > 0 {
				track := image.Rect(x+width-7, y+22, x+width-1, footer)
				target.fill(track, border)
				thumb := max(6, track.Dy()*rows/m.Count)
				pos := (track.Dy() - thumb) * min(m.Offset, m.Count-rows) / (m.Count - rows)
				target.fill(image.Rect(track.Min.X, track.Min.Y+pos, track.Max.X, track.Min.Y+pos+thumb), color.RGBA{100, 100, 100, 255})
				addHit("scroll", track, -1)
			}
		} else {
			badge := image.Rect(x+labelWidth+10, y+3, x+labelWidth+10+valueWidth, y+19)
			target.fill(badge, monitorOrange)
			target.text(badge.Min.X+5, y+15, value, color.White)
			addHit("value", badge, -1)
			paintMonitorEditor(target, ui, m.Index, -1, badge)
			if m.Mode == "slider" {
				track := image.Rect(x+8, y+27, x+width-8, y+37)
				target.fill(image.Rect(track.Min.X, y+31, track.Max.X, y+34), border)
				fraction := 0.0
				if m.SliderMax > m.SliderMin {
					v, _ := strconv.ParseFloat(m.Value, 64)
					fraction = (v - m.SliderMin) / (m.SliderMax - m.SliderMin)
				}
				if math.IsNaN(fraction) {
					fraction = 0
				}
				pos := track.Min.X + int(mathClamp(fraction, 0, 1)*float64(track.Dx()-1))
				target.fill(image.Rect(pos-3, y+27, pos+4, y+38), monitorOrange)
				addHit("slider", track, -1)
			}
		}
	}
}

func paintMonitorEditor(p overlayPainter, ui *monitorUI, index, item int, rect image.Rectangle) {
	if ui == nil || ui.edit == nil || ui.edit.index != index || ui.edit.item != item {
		return
	}
	e := ui.edit
	p = p.clipped(rect)
	p.fill(rect, color.RGBA{70, 115, 180, 255})
	s := monitorText(e.text)
	if !e.selected {
		s += "|"
	}
	for p.measure(s) > rect.Dx()-8 && s != "" {
		_, size := utf8.DecodeRuneInString(s)
		s = s[size:]
	}
	p.text(rect.Min.X+4, rect.Min.Y+14, s, color.White)
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
