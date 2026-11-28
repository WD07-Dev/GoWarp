package render

import (
	"container/list"
	"image"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"gowarp/core/media"
)

const (
	textFaceLimit  = 8
	textGlyphLimit = 1024
	textGlyphBytes = 512 << 10
)

type textFaceKey struct {
	font        *opentype.Font
	size        float64
	noAntialias bool
}

type textGlyphKey struct {
	char rune
	x, y fixed.Int26_6
}

type textGlyph struct {
	key     textGlyphKey
	rect    image.Rectangle
	mask    *image.Alpha
	advance fixed.Int26_6
	found   bool
}

type cachedTextFace struct {
	font.Face
	lastUsed uint64
	glyphs   map[textGlyphKey]*list.Element
	recent   list.List
	bytes    int
}

func (r *Renderer) textFace(size float64, family string) (font.Face, error) {
	key := textFaceKey{r.Assets.TextFont(family), size, r.view.NoAntialias}
	if face := r.textFaces[key]; face != nil {
		face.lastUsed = r.textSkinUse
		return face, nil
	}
	face, err := opentype.NewFace(key.font, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, err
	}
	if r.textFaces == nil {
		r.textFaces = make(map[textFaceKey]*cachedTextFace)
	}
	if len(r.textFaces) >= textFaceLimit {
		var oldest textFaceKey
		used := ^uint64(0)
		for k, f := range r.textFaces {
			if f.lastUsed < used {
				oldest, used = k, f.lastUsed
			}
		}
		r.textFaces[oldest].Close()
		delete(r.textFaces, oldest)
	}
	var source font.Face = face
	if key.noAntialias {
		source = media.WithoutFontAntialias(source)
	}
	cached := &cachedTextFace{Face: source, lastUsed: r.textSkinUse, glyphs: make(map[textGlyphKey]*list.Element)}
	r.textFaces[key] = cached
	return cached, nil
}

func (f *cachedTextFace) Glyph(dot fixed.Point26_6, char rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	// Keep all 64 subpixel positions; rounding them changes small text and outlines.
	offset := image.Pt(dot.X.Floor(), dot.Y.Floor())
	phase := fixed.Point26_6{X: dot.X & 63, Y: dot.Y & 63}
	key := textGlyphKey{char, phase.X, phase.Y}
	if entry := f.glyphs[key]; entry != nil {
		f.recent.MoveToFront(entry)
		glyph := entry.Value.(textGlyph)
		return glyph.rect.Add(offset), glyph.mask, image.Point{}, glyph.advance, glyph.found
	}
	rect, mask, point, advance, found := f.Face.Glyph(phase, char)
	bytes := rect.Dx() * rect.Dy()
	if bytes > textGlyphBytes {
		return rect.Add(offset), mask, point, advance, found
	}
	var pixels *image.Alpha
	if !rect.Empty() && mask != nil {
		// font.Face may overwrite its mask on the next Glyph call.
		pixels = image.NewAlpha(image.Rect(0, 0, rect.Dx(), rect.Dy()))
		draw.Draw(pixels, pixels.Bounds(), mask, point, draw.Src)
	}
	for len(f.glyphs) >= textGlyphLimit || f.bytes+bytes > textGlyphBytes {
		entry := f.recent.Back()
		glyph := entry.Value.(textGlyph)
		if glyph.mask != nil {
			f.bytes -= len(glyph.mask.Pix)
		}
		delete(f.glyphs, glyph.key)
		f.recent.Remove(entry)
	}
	glyph := textGlyph{key, rect, pixels, advance, found}
	f.glyphs[key] = f.recent.PushFront(glyph)
	if pixels != nil {
		f.bytes += len(pixels.Pix)
	}
	return rect.Add(offset), pixels, image.Point{}, advance, found
}
