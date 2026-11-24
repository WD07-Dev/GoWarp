package desktop

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"reflect"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"gowarp/core/media"
	"gowarp/core/vm"
)

func writeText(dst draw.Image, face font.Face, x, y int, s string, c color.Color) {
	drawer := font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	drawer.DrawString(s)
}

func wrapText(face font.Face, s string, width int) []string {
	lines := []string{}
	line := ""
	count := 0
	for _, r := range s {
		if count == 1024 {
			return append(lines, line+"…")
		}
		count++
		if len(lines) >= 9 {
			return append(lines, "…")
		}
		if r == '\n' {
			lines = append(lines, line)
			line = ""
			continue
		}
		next := line + string(r)
		if line != "" && font.MeasureString(face, next).Ceil() > width {
			lines = append(lines, line)
			line = string(r)
		} else {
			line = next
		}
	}
	return append(lines, line)
}

func (c *controller) paint(face font.Face) *image.RGBA {
	w, h := c.renderer.Size()
	return c.paintAt(face, w, h)
}

/*
paintAt receives a font rasterized at min(width/logicalWidth,
height/logicalHeight) times the logical font size.
*/
func (c *controller) paintAt(face font.Face, width, height int) *image.RGBA {
	wStage, hStage := c.renderer.Size()
	top, bottom := ToolbarHeight, AnswerHeight
	if c.options.Settings != nil {
		top, bottom = 0, 0
	}
	windowHeight := hStage + top + bottom
	scene := c.displayScene()
	stage := c.paintStage(scene, width, height)
	frame := image.NewRGBA(image.Rect(0, 0, stage.Bounds().Dx(), stage.Bounds().Dy()+top+bottom))
	draw.Draw(frame, frame.Bounds(), image.NewUniform(color.RGBA{235, 238, 244, 255}), image.Point{}, draw.Src)
	draw.Draw(frame, image.Rect(0, top, frame.Bounds().Dx(), top+stage.Bounds().Dy()), stage, image.Point{}, draw.Src)
	painter := newOverlayPainter(frame, face, wStage, windowHeight)
	if c.options.Settings == nil {
		painter.fill(image.Rect(8, 7, 32, 29), color.RGBA{35, 175, 85, 255})
		painter.fill(image.Rect(42, 7, 66, 29), color.RGBA{210, 55, 55, 255})
		status := fmt.Sprintf("GoWarp  |  %g fps  |  F5: start", c.vm.FPS())
		if c.vm.Paused() {
			status = "Paused  |  F8: resume"
		}
		painter.text(80, 23, status, color.Black)
	}
	c.paintOverlays(painter, scene, top, windowHeight)
	return frame
}

// The returned scene is valid until the next displayScene call.
func (c *controller) displayScene() []vm.DisplayTarget {
	c.current = c.vm.SceneInto(c.current)
	scene := c.current
	if c.options.Settings != nil && c.vm.Options().Interpolation && c.previousView == c.vm.View() && !c.vm.Stopped() && !c.vm.Paused() {
		alpha := c.credit
		if c.cadenceNow != nil && !c.options.Replay && !c.cadenceTime.IsZero() {
			// Realtime ticks round at half a tick; draws may occur between updates.
			alpha += .5 + max(0, c.cadenceNow().Sub(c.cadenceTime).Seconds())*c.cadenceFPS
		}
		return c.interpolation.interpolate(c.previous, scene, alpha)
	}
	return scene
}

func (c *controller) paintOverlays(painter overlayPainter, scene []vm.DisplayTarget, top, windowHeight int) {
	if c.renderer.DisplayView().NoAntialias {
		painter.face = media.WithoutFontAntialias(painter.face)
	}
	wStage, hStage := c.renderer.Size()
	for _, t := range scene {
		if t.State.Bubble == "" || !t.State.Visible {
			continue
		}
		lines := painter.wrap(t.State.Bubble, 170)
		w, h := 190, len(lines)*18+18
		px, py := c.renderer.ToScreen(t.State.X, t.State.Y)
		x := int(mathClamp(px+12, 4, float64(wStage-w-4)))
		y := int(mathClamp(py-float64(h), 4, float64(hStage-h))) + top
		box := image.Rect(x, y, x+w, y+h)
		painter.fill(box, color.RGBA{90, 90, 100, 255})
		painter.fill(box.Inset(1), color.White)
		for i, line := range lines {
			if i == 0 && t.State.Think {
				line = "… " + line
			}
			painter.text(x+8, y+19+i*18, line, color.Black)
		}
	}
	drawMonitorPanels(painter, c.vm.MonitorWindow(c.monitors.offsets, max(1, (hStage-44)/22)), top, wStage, hStage, &c.monitors)
	if q := c.vm.Question(); q != nil {
		answerTop := top + hStage
		if c.options.Settings != nil {
			answerTop = max(0, hStage-AnswerHeight)
			painter.fill(image.Rect(0, answerTop, wStage, windowHeight), color.White)
		}
		prompt := q.Text
		count := 0
		for offset := range prompt {
			if count == 52 {
				prompt = prompt[:offset] + "…"
				break
			}
			count++
		}
		painter.text(8, answerTop+18, prompt, color.Black)
		answer := c.answer
		for painter.measure(answer) > wStage-30 && answer != "" {
			_, n := utf8.DecodeRuneInString(answer)
			answer = answer[n:]
		}
		painter.text(8, windowHeight-9, answer+"|", color.RGBA{25, 65, 140, 255})
	} else if c.options.Settings == nil {
		painter.text(8, windowHeight-18, "Green: start   Red: stop   Esc: close", color.RGBA{70, 75, 90, 255})
	}
}

func (c *controller) paintStage(scene []vm.DisplayTarget, width, height int) *image.RGBA {
	if c.options.Settings == nil {
		c.paintedStage = nil
		return c.renderer.Draw(scene)
	}
	if c.vm.Options().Interpolation {
		c.paintedStage = nil
		return c.renderer.DrawAt(scene, width, height)
	}
	view, frame := c.vm.View(), c.vm.Frames()
	if c.paintedStage == nil || c.paintedFrame != frame || c.paintedView != view || !reflect.DeepEqual(c.paintedScene, scene) || c.paintedSize != image.Pt(width, height) {
		c.paintedStage = c.renderer.DrawAt(scene, width, height)
		c.paintedSize = image.Pt(width, height)
		if cap(c.paintedScene) > max(64, 4*len(scene)) {
			c.paintedScene = nil
		}
		clear(c.paintedScene[min(len(scene), len(c.paintedScene)):])
		c.paintedScene = append(c.paintedScene[:0], scene...)
		c.paintedView = view
		c.paintedFrame = frame
	}
	/*
		paint copies these pixels before adding bubbles, monitors, and answers,
		so UI editing stays responsive between project execution frames.
	*/
	return c.paintedStage
}

func mathClamp(n, low, high float64) float64 {
	if n < low {
		return low
	}
	if n > high {
		return high
	}
	return n
}
