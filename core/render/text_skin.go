package render

import (
	"fmt"
	"image"
	"math"
	"strings"
	"sync/atomic"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"gowarp/core/media"
	"gowarp/core/vm"
)

type textSkinKey struct {
	Skin        vm.TextSkin
	Scale       float64
	NoAntialias bool
}

var textSkinSerial atomic.Uint64

func (r *Renderer) textCostume(t vm.DisplayTarget) *media.Costume {
	skin := t.State.Text.Defaults()
	scale := 1.0
	if skin.ScreenWidth {
		scale = math.Abs(t.State.Size * t.State.StretchX / 10000)
	}
	if !finite(scale) || scale <= 0 {
		scale = 1
	}
	key := textSkinKey{skin, scale, r.view.NoAntialias}
	if c := r.textSkins[key]; c != nil {
		return c
	}
	if len(r.textSkins) >= 128 || r.textSkinBytes >= 16<<20 {
		for _, c := range r.textSkins {
			delete(r.hulls, c)
		}
		r.textSkins = nil
		r.textSkinBytes = 0
	}
	if r.textSkins == nil {
		r.textSkins = make(map[textSkinKey]*media.Costume)
	}
	const density = 2
	fontSize := skin.FontSize
	face, err := opentype.NewFace(r.Assets.TextFont(skin.Font), &opentype.FaceOptions{
		Size: fontSize * density, DPI: 72, Hinting: font.HintingNone,
	})
	if err != nil {
		return nil
	}
	if r.view.NoAntialias {
		face = media.WithoutFontAntialias(face)
	}
	defer face.Close()
	wrapWidth := math.Max(1, math.Min(4096, skin.Width/scale))
	lines := wrapSkinText(face, skin.Text, wrapWidth*density)
	outline := skin.OutlineWidth
	width := wrapWidth + outline*2
	height := float64(len(lines))*skin.LineHeight/skin.LineHeightDivisor + skin.PaddingY*2 + outline*2
	if width*height*density*density > 16<<20 || height > 4096 {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, int(math.Ceil(width*density)), int(math.Ceil(height*density))))
	drawer := font.Drawer{Dst: img, Face: face}
	fill := image.NewUniform(vm.ParseColor(skin.Color))
	stroke := image.NewUniform(vm.ParseColor(skin.OutlineColor))
	for i, line := range lines {
		line = strings.TrimRight(line, " \t")
		w := float64(font.MeasureString(face, line)) / 64
		x := (wrapWidth*density-w)*skin.Align + outline*density
		y := (skin.PaddingY + float64(i)*skin.LineHeight/skin.LineHeightDivisor + fontSize + outline) * density
		// Canvas strokeText centers its stroke on the glyph outline.
		radius := outline * density / 2
		if radius > 0 {
			drawer.Src = stroke
			for angle := 0.0; angle < 2*math.Pi; angle += math.Pi / 8 {
				drawer.Dot = fixed.Point26_6{X: fixed.Int26_6((x + math.Cos(angle)*radius) * 64), Y: fixed.Int26_6((y + math.Sin(angle)*radius) * 64)}
				drawer.DrawString(line)
			}
		}
		drawer.Src = fill
		drawer.Dot = fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(y * 64)}
		drawer.DrawString(line)
	}
	c := &media.Costume{Image: img, Width: width, Height: height, CenterX: width / 2, CenterY: skin.AnchorY + outline}
	r.textSkins[key] = c
	c.CacheKey = fmt.Sprintf("text:%d", textSkinSerial.Add(1))
	r.textSkinBytes += len(img.Pix)
	return c
}

func wrapSkinText(face font.Face, text string, width float64) []string {
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		var line strings.Builder
		var advance fixed.Int26_6
		previous := rune(-1)
		for _, char := range paragraph {
			glyphAdvance, _ := face.GlyphAdvance(char)
			nextAdvance := advance
			if previous >= 0 {
				nextAdvance += face.Kern(previous, char)
			}
			nextAdvance += glyphAdvance
			if line.Len() != 0 && float64(nextAdvance)/64 > width {
				lines = append(lines, line.String())
				line.Reset()
				nextAdvance = glyphAdvance
			}
			line.WriteRune(char)
			advance = nextAdvance
			previous = char
		}
		lines = append(lines, line.String())
	}
	return lines
}
