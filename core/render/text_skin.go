package render

import (
	"fmt"
	"image"
	"math"
	"strings"
	"sync/atomic"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"gowarp/core/media"
	"gowarp/core/vm"
)

type textSkinKey struct {
	Skin        vm.TextSkin
	Scale       float64
	NoAntialias bool
}

type cachedTextSkin struct {
	costume  *media.Costume
	lastUsed uint64
	raster   *textRaster
}

type textRaster struct {
	costume    media.Costume
	references int
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
	r.textSkinUse++
	if cached, ok := r.textSkins[key]; ok {
		cached.lastUsed = r.textSkinUse
		r.textSkins[key] = cached
		return cached.costume
	}
	rasterKey := key
	rasterKey.Skin.AnchorY = 0
	if raster := r.textRasters[rasterKey]; raster != nil {
		return r.cacheTextSkin(key, rasterKey, raster)
	}
	const density = 2
	fontSize := skin.FontSize
	face, err := r.textFace(fontSize*density, skin.Font)
	if err != nil {
		return nil
	}
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
	return r.cacheTextSkin(key, rasterKey, &textRaster{costume: media.Costume{
		Image: img, Width: width, Height: height, CenterX: width / 2, CenterY: outline,
		CacheKey: fmt.Sprintf("text:%d", textSkinSerial.Add(1)),
	}})
}

func (r *Renderer) cacheTextSkin(key, rasterKey textSkinKey, raster *textRaster) *media.Costume {
	if r.textSkins == nil {
		r.textSkins = make(map[textSkinKey]cachedTextSkin)
	}
	if r.textRasters == nil {
		r.textRasters = make(map[textSkinKey]*textRaster)
	}
	if raster.references == 0 {
		r.textRasters[rasterKey] = raster
		r.textSkinBytes += len(raster.costume.Image.Pix)
	}
	raster.references++
	for len(r.textSkins) > 0 && (len(r.textSkins) >= 128 || r.textSkinBytes > 16<<20) {
		var oldest textSkinKey
		lastUsed := ^uint64(0)
		for k, cached := range r.textSkins {
			if cached.lastUsed < lastUsed {
				oldest, lastUsed = k, cached.lastUsed
			}
		}
		removed := r.textSkins[oldest]
		removed.raster.references--
		if removed.raster.references == 0 {
			rasterKey := oldest
			rasterKey.Skin.AnchorY = 0
			delete(r.textRasters, rasterKey)
			r.textSkinBytes -= len(removed.costume.Image.Pix)
		}
		delete(r.hulls, removed.costume)
		delete(r.textSkins, oldest)
	}
	c := &raster.costume
	if key.Skin.AnchorY != 0 {
		variant := *c
		variant.CenterY += key.Skin.AnchorY
		c = &variant
	}
	r.textSkins[key] = cachedTextSkin{costume: c, lastUsed: r.textSkinUse, raster: raster}
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
