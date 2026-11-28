package render

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"gowarp/core/media"
	"gowarp/core/vm"
)

type textLine struct {
	text string
	x, y float64
}

type TextLayout struct {
	Size     image.Point
	skin     vm.TextSkin
	lines    []textLine
	costume  media.Costume
	anchor   *media.Costume
	lastUsed uint64
	bytes    int
}

func (r *Renderer) displayText(t vm.DisplayTarget) (*TextLayout, *media.Costume) {
	skin := t.State.Text.Defaults()
	anchor := skin.AnchorY
	skin.AnchorY = 0
	scale := 1.0
	if skin.ScreenWidth {
		scale = math.Abs(t.State.Size * t.State.StretchX / 10000)
	}
	if !finite(scale) || scale <= 0 {
		scale = 1
	}
	key := textSkinKey{skin, scale, r.view.NoAntialias}
	r.textSkinUse++
	layout := r.textLayouts[key]
	if layout == nil {
		face, err := r.textFace(skin.FontSize*2, skin.Font)
		if err != nil {
			return nil, nil
		}
		wrapWidth := math.Max(1, math.Min(4096, skin.Width/scale))
		lines := wrapSkinText(face, skin.Text, wrapWidth*2)
		width := wrapWidth + skin.OutlineWidth*2
		height := float64(len(lines))*skin.LineHeight/skin.LineHeightDivisor + skin.PaddingY*2 + skin.OutlineWidth*2
		if !finite(width) || !finite(height) || width <= 0 || height <= 0 || width*height*4 > 16<<20 || height > 4096 {
			return nil, nil
		}
		layout = &TextLayout{
			Size: image.Pt(int(math.Ceil(width*2)), int(math.Ceil(height*2))),
			skin: skin, lines: make([]textLine, len(lines)),
			costume: media.Costume{Width: width, Height: height, CenterX: width / 2, CenterY: skin.OutlineWidth,
				CacheKey: fmt.Sprintf("text-display:%d", textSkinSerial.Add(1))},
			bytes: len(skin.Text) + len(lines)*32,
		}
		for i, line := range lines {
			line = strings.TrimRight(line, " \t")
			w := float64(font.MeasureString(face, line)) / 64
			layout.lines[i] = textLine{
				text: line,
				x:    (wrapWidth*2-w)*skin.Align + skin.OutlineWidth*2,
				y:    (skin.PaddingY + float64(i)*skin.LineHeight/skin.LineHeightDivisor + skin.FontSize + skin.OutlineWidth) * 2,
			}
		}
		const limit = 1 << 20
		if layout.bytes <= limit {
			if r.textLayouts == nil {
				r.textLayouts = make(map[textSkinKey]*TextLayout)
			}
			for len(r.textLayouts) >= 128 || r.textLayoutBytes+layout.bytes > limit {
				var oldest textSkinKey
				used := ^uint64(0)
				for k, entry := range r.textLayouts {
					if entry.lastUsed < used {
						oldest, used = k, entry.lastUsed
					}
				}
				r.textLayoutBytes -= r.textLayouts[oldest].bytes
				delete(r.textLayouts, oldest)
			}
			r.textLayouts[key] = layout
			r.textLayoutBytes += layout.bytes
		}
	}
	layout.lastUsed = r.textSkinUse
	if anchor == 0 {
		return layout, &layout.costume
	}
	centerY := layout.costume.CenterY + anchor
	if layout.anchor == nil || layout.anchor.CenterY != centerY {
		c := layout.costume
		c.CenterY = centerY
		layout.anchor = &c
	}
	return layout, layout.anchor
}

// DrawTextGlyphs visits masks in the same order as the CPU text rasterizer.
// The callback must consume the mask before returning; oversized masks may be reused.
func (r *Renderer) DrawTextGlyphs(layout *TextLayout, draw func(image.Image, image.Point, image.Rectangle, color.NRGBA)) {
	skin := layout.skin
	face, err := r.textFace(skin.FontSize*2, skin.Font)
	if err != nil {
		return
	}
	lineAt := func(line textLine, x, y float64, color color.NRGBA) {
		dot := fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(y * 64)}
		previous := rune(-1)
		for _, char := range line.text {
			if previous >= 0 {
				dot.X += face.Kern(previous, char)
			}
			rect, mask, point, advance, _ := face.Glyph(dot, char)
			if !rect.Empty() {
				draw(mask, point, rect, color)
			}
			dot.X += advance
			previous = char
		}
	}
	for _, line := range layout.lines {
		if radius := skin.OutlineWidth; radius > 0 {
			stroke := vm.ParseColor(skin.OutlineColor)
			for angle := 0.0; angle < 2*math.Pi; angle += math.Pi / 8 {
				lineAt(line, line.x+math.Cos(angle)*radius, line.y+math.Sin(angle)*radius, stroke)
			}
		}
		lineAt(line, line.x, line.y, vm.ParseColor(skin.Color))
	}
}
