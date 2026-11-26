package media

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"

	"github.com/srwiley/rasterx"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type hardSVGScanner struct {
	*rasterx.ScannerGV
	mask *image.Alpha
}

func (lib *Library) SetAntialias(enabled bool) error {
	if lib.noAntialias == !enabled {
		return nil
	}
	cache := lib.cache
	if cache == nil {
		return fmt.Errorf("antialias mode requires a runtime asset library")
	}
	if cache.playback != nil || cache.display != nil {
		return fmt.Errorf("set antialias mode before preparing costume playback or display workers")
	}
	lib.noAntialias = !enabled
	for key, entry := range cache.entries {
		if strings.HasPrefix(strings.ToLower(key), "image:svg:") || strings.HasPrefix(key, "svg:") {
			cache.bytes -= entry.Value.(*cachedAsset).bytes
			cache.recent.Remove(entry)
			delete(cache.entries, key)
		}
	}
	return nil
}

func WithoutFontAntialias(face font.Face) font.Face {
	if _, ok := face.(*hardFontFace); ok {
		return face
	}
	return &hardFontFace{Face: face}
}

type hardFontFace struct{ font.Face }

func (f *hardFontFace) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	rect, mask, point, advance, ok := f.Face.Glyph(dot, r)
	if mask != nil {
		mask = hardCoverage{mask}
	}
	return rect, mask, point, advance, ok
}

type hardCoverage struct{ image.Image }

func (m hardCoverage) ColorModel() color.Model {
	return color.AlphaModel
}
func (m hardCoverage) At(x, y int) color.Color {
	_, _, _, a := m.Image.At(x, y).RGBA()
	if a >= 0x8000 {
		return color.Alpha{A: 255}
	}
	return color.Alpha{}
}

func (s *hardSVGScanner) Draw() {
	dst, src, offset := s.Dest, s.Source, s.Offset
	if s.mask == nil || s.mask.Bounds() != dst.Bounds() {
		s.mask = image.NewAlpha(dst.Bounds())
	}
	clear(s.mask.Pix)
	s.Dest, s.Source, s.Offset = s.mask, image.White, image.Point{}
	s.ScannerGV.Draw()
	s.Dest, s.Source, s.Offset = dst, src, offset
	for i, a := range s.mask.Pix {
		if a >= 128 {
			s.mask.Pix[i] = 255
		} else {
			s.mask.Pix[i] = 0
		}
	}
	switch src.(type) {
	case *rasterx.ColorFuncImage, *rasterx.ClipImage:
		src = struct{ image.Image }{src}
	}
	draw.DrawMask(dst, dst.Bounds(), src, offset, s.mask, s.mask.Bounds().Min, draw.Over)
}
