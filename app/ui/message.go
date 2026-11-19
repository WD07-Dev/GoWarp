package ui

import (
	"image"
	"image/color"
	"math"
	"strings"
	"unicode/utf8"

	"gowarp/app/assets"
	"gowarp/app/i18n"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"golang.org/x/image/font"
)

const messageWidth = 560

const messagePadding = 24

const messageLineHeight = 24

const messageMaxLines = 12

type message struct {
	title      string
	lines      []string
	height     int
	scale      float64
	imageScale float64
	image      *ebiten.Image
}

func newMessage(title, text string) *message {
	face := Font(16)
	defer face.Close()
	width := messageWidth - 2*messagePadding
	lines := wrapMessage(text, face, width, messageMaxLines)
	return &message{
		title:  wrapMessage(title, face, width, 1)[0],
		lines:  lines,
		height: max(160, 96+len(lines)*messageLineHeight),
		scale:  1,
	}
}

func wrapMessage(text string, face font.Face, width, limit int) []string {
	var lines []string
	line := ""
	for _, r := range strings.ReplaceAll(text, "\r", "") {
		if r == '\n' {
			lines = append(lines, line)
			line = ""
		} else if line != "" && font.MeasureString(face, line+string(r)).Ceil() > width {
			if space := strings.LastIndexByte(line, ' '); space > 0 {
				lines = append(lines, line[:space])
				line = line[space+1:] + string(r)
			} else {
				lines = append(lines, line)
				line = string(r)
			}
		} else {
			line += string(r)
		}
		if len(lines) >= limit {
			last := lines[limit-1]
			for len(last) > 0 && font.MeasureString(face, last+"…").Ceil() > width {
				_, size := utf8.DecodeLastRuneInString(last)
				last = last[:len(last)-size]
			}
			lines[limit-1] = last + "…"
			return lines[:limit]
		}
	}
	return append(lines, line)
}

func (m *message) Update() error {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) || inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
		return ebiten.Termination
	}
	return nil
}

func (m *message) Draw(dst *ebiten.Image) {
	if m.image == nil || m.imageScale != m.scale {
		if m.image != nil {
			m.image.Deallocate()
		}
		m.image = ebiten.NewImageFromImage(m.renderImage())
		m.imageScale = m.scale
	}
	dst.DrawImage(m.image, nil)
}

func (m *message) Layout(int, int) (int, int) {
	return messageWidth, m.height
}

func (m *message) LayoutF(float64, float64) (float64, float64) {
	return m.layoutScale(ebiten.Monitor().DeviceScaleFactor())
}

func (m *message) layoutScale(scale float64) (float64, float64) {
	if scale <= 0 {
		scale = 1
	}
	m.scale = scale
	return math.Ceil(messageWidth * scale), math.Ceil(float64(m.height) * scale)
}

func (m *message) renderImage() *image.RGBA {
	width, height := m.layoutScale(m.scale)
	img := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	face := Font(16 * m.scale)
	defer face.Close()
	pixel := func(value int) int {
		return int(math.Round(float64(value) * m.scale))
	}
	border := max(1, pixel(1))
	Fill(img, img.Bounds(), color.RGBA{112, 112, 112, 255})
	Fill(img, img.Bounds().Inset(border), Background)
	Fill(img, image.Rect(border, border, img.Bounds().Max.X-border, pixel(4)), Accent)
	Fill(img, image.Rect(pixel(messagePadding), pixel(44), img.Bounds().Max.X-pixel(messagePadding), pixel(45)), color.RGBA{70, 70, 70, 255})
	Label(img, face, pixel(messagePadding), pixel(32), m.title, Accent)
	for i, line := range m.lines {
		Label(img, face, pixel(messagePadding), pixel(64+i*messageLineHeight), line, Text)
	}
	Label(img, face, pixel(messagePadding), pixel(m.height-20), i18n.Tr("action.close_hint"), Muted)
	return img
}

func ShowMessage(title, text string) error {
	m := newMessage(title, text)
	defer func() {
		if m.image != nil {
			m.image.Deallocate()
		}
	}()
	ebiten.SetWindowTitle(title)
	ebiten.SetWindowIcon(assets.DefaultWindowIcon())
	ebiten.SetWindowSize(messageWidth, m.height)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeDisabled)
	return ebiten.RunGame(m)
}
