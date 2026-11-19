package media

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

var svgFonts = [8]struct {
	once sync.Once
	font *sfnt.Font
	err  error
}{}

/*
svgGlyphPath returns outlines in SVG coordinates relative to the text baseline.
Bundled Go fonts make the fallback independent of fonts installed on the host.
*/
func svgGlyphPath(text string, size float64, family, weight, style string) (string, float64, error) {
	return svgGlyphPathWithFont(text, size, family, weight, style, nil)
}

func svgGlyphPathWithFont(text string, size float64, family, weight, style string, resolve svgFontResolver) (string, float64, error) {
	if math.IsNaN(size) || math.IsInf(size, 0) || size <= 0 || size > 8192 {
		return "", 0, fmt.Errorf("SVG text font size must be positive and at most 8192")
	}
	if len(text) > 4096*4 || !utf8.ValidString(text) || utf8.RuneCountInString(text) > 4096 {
		return "", 0, fmt.Errorf("SVG text must be valid UTF-8 and at most 4096 characters")
	}
	var face *sfnt.Font
	if resolve != nil {
		face = resolve(family)
	}
	if face == nil {
		variant := 0
		weight = strings.ToLower(strings.TrimSpace(weight))
		numericWeight, _ := strconv.ParseFloat(weight, 64)
		if weight == "bold" || weight == "bolder" || numericWeight >= 600 {
			variant |= 1
		}
		style = strings.ToLower(strings.TrimSpace(style))
		if style == "italic" || strings.HasPrefix(style, "oblique") {
			variant |= 2
		}
		family = strings.ToLower(family)
		if strings.Contains(family, "mono") || strings.Contains(family, "courier") || strings.Contains(family, "consolas") {
			variant |= 4
		}
		cached := &svgFonts[variant]
		cached.once.Do(func() {
			data := [8][]byte{
				goregular.TTF,
				gobold.TTF,
				goitalic.TTF,
				gobolditalic.TTF,
				gomono.TTF,
				gomonobold.TTF,
				gomonoitalic.TTF,
				gomonobolditalic.TTF,
			}
			cached.font, cached.err = sfnt.Parse(data[variant])
		})
		if cached.err != nil {
			return "", 0, cached.err
		}
		face = cached.font
	}
	ppem := fixed.Int26_6(math.Max(1, math.Round(size*64)))
	var buffer sfnt.Buffer
	var path strings.Builder
	var advance float64
	var previous sfnt.GlyphIndex
	first := true
	for _, r := range text {
		index, err := face.GlyphIndex(&buffer, r)
		if err != nil {
			return "", 0, err
		}
		if index == 0 {
			return "", 0, fmt.Errorf("SVG text font has no glyph for %U", r)
		}
		if !first {
			kern, err := face.Kern(&buffer, previous, index, ppem, font.HintingNone)
			// GPOS fonts may omit this pair even when both glyphs exist.
			if err != nil && err != sfnt.ErrNotFound {
				return "", 0, err
			}
			advance += float64(kern) / 64
		}
		width, err := face.GlyphAdvance(&buffer, index, ppem, font.HintingNone)
		if err != nil {
			return "", 0, err
		}
		segments, err := face.LoadGlyph(&buffer, index, ppem, nil)
		if err != nil {
			return "", 0, err
		}
		open := false
		for _, segment := range segments {
			command, count := "", 0
			switch segment.Op {
			case sfnt.SegmentOpMoveTo:
				if open {
					path.WriteString("Z ")
				}
				open = true
				command, count = "M ", 1
			case sfnt.SegmentOpLineTo:
				command, count = "L ", 1
			case sfnt.SegmentOpQuadTo:
				command, count = "Q ", 2
			case sfnt.SegmentOpCubeTo:
				command, count = "C ", 3
			default:
				return "", 0, fmt.Errorf("unsupported SVG font outline operation")
			}
			// Each segment adds at most six short finite coordinates.
			if path.Len() > 16*1024*1024-256 {
				return "", 0, fmt.Errorf("SVG text outline exceeds 16 MiB")
			}
			path.WriteString(command)
			for _, point := range segment.Args[:count] {
				path.WriteString(strconv.FormatFloat(advance+float64(point.X)/64, 'f', -1, 64))
				path.WriteByte(' ')
				path.WriteString(strconv.FormatFloat(float64(point.Y)/64, 'f', -1, 64))
				path.WriteByte(' ')
			}
		}
		if open {
			path.WriteString("Z ")
		}
		advance += float64(width) / 64
		previous, first = index, false
	}
	return path.String(), advance, nil
}
