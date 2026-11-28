package media

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"gowarp/app/assets"

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

const (
	bundledMono      = 4
	bundledSansSerif = 8
	bundledMarker    = 9
)

var svgFonts = [...]struct {
	data []byte
	once sync.Once
	font *sfnt.Font
}{
	{data: goregular.TTF},
	{data: gobold.TTF},
	{data: goitalic.TTF},
	{data: gobolditalic.TTF},
	bundledMono: {data: gomono.TTF},
	{data: gomonobold.TTF},
	{data: gomonoitalic.TTF},
	{data: gomonobolditalic.TTF},
	bundledSansSerif: {data: assets.NotoSansMedium},
	bundledMarker:    {data: assets.Knewave},
}

func bundledFont(variant int) *sfnt.Font {
	cached := &svgFonts[variant]
	cached.once.Do(func() {
		cached.font, _ = sfnt.Parse(cached.data)
	})
	return cached.font
}

func defaultTextFont(family, weight, style string) *sfnt.Font {
	name, _, _ := strings.Cut(family, ",")
	switch strings.ToLower(strings.Trim(strings.TrimSpace(name), "\"'")) {
	case "sans serif":
		return bundledFont(bundledSansSerif)
	case "marker":
		return bundledFont(bundledMarker)
	}

	if f := browserFont(family, weight, style); f != nil {
		return f.face
	}
	v := fontVariant(weight, style)
	if fontFamily(family) == fontMono {
		v |= bundledMono
	}
	return bundledFont(v)
}

func svgGlyphPath(text string, size float64, family, weight, style string) (string, float64, error) {
	return svgGlyphPathWithFont(text, size, family, weight, style, nil)
}

func svgGlyphPathWithFont(text string, size float64, family, weight, style string, resolve svgFontResolver) (string, float64, error) {
	nodes, advance, err := svgGlyphNodes(text, size, family, weight, style, resolve)
	var path strings.Builder
	for _, n := range nodes {
		path.WriteString(n.attr("d"))
	}
	return path.String(), advance, err
}

type renderedGlyph struct {
	face     *sfnt.Font
	index    sfnt.GlyphIndex
	segments sfnt.Segments
	color    []*svgNode
	width    fixed.Int26_6
}

func readGlyph(face *sfnt.Font, r rune, ppem fixed.Int26_6, b *sfnt.Buffer) (renderedGlyph, bool) {
	index, err := face.GlyphIndex(b, r)
	if err != nil || index == 0 {
		return renderedGlyph{}, false
	}
	width, err := face.GlyphAdvance(b, index, ppem, font.HintingNone)
	if err != nil {
		return renderedGlyph{}, false
	}
	g := renderedGlyph{face: face, index: index, width: width}
	if v, ok := systemFontFaces.Load(face); ok {
		g.color = v.(*systemFont).colorGlyph(index, float64(ppem)/64)
		if len(g.color) > 0 {
			return g, true
		}
	}
	g.segments, err = face.LoadGlyph(b, index, ppem, nil)
	if err != nil || (len(g.segments) == 0 && !unicode.IsSpace(r)) {
		return renderedGlyph{}, false
	}
	return g, true
}

func findGlyph(primary *sfnt.Font, r rune, ppem fixed.Int26_6, b *sfnt.Buffer) (renderedGlyph, bool) {
	if g, ok := readGlyph(primary, r, ppem, b); ok {
		return g, true
	}
	for _, family := range []int{fontEmoji, fontCJK, fontSymbols, fontSans} {
		f := loadSystemFont(family, 0)
		if f == nil || f.face == primary {
			continue
		}
		if g, ok := readGlyph(f.face, r, ppem, b); ok {
			return g, true
		}
	}
	if f := bundledFont(0); f != primary {
		return readGlyph(f, r, ppem, b)
	}
	return renderedGlyph{}, false
}

func svgGlyphNodes(text string, size float64, family, weight, style string, resolve svgFontResolver) ([]*svgNode, float64, error) {
	if math.IsNaN(size) || math.IsInf(size, 0) || size <= 0 || size > 8192 {
		return nil, 0, fmt.Errorf("SVG text font size must be positive and at most 8192")
	}
	if len(text) > 4096*4 || !utf8.ValidString(text) || utf8.RuneCountInString(text) > 4096 {
		return nil, 0, fmt.Errorf("SVG text must be valid UTF-8 and at most 4096 characters")
	}
	var primary *sfnt.Font
	if resolve != nil {
		primary = resolve(family)
	}
	if primary == nil {
		primary = defaultTextFont(family, weight, style)
	}
	ppem := fixed.Int26_6(math.Max(1, math.Round(size*64)))
	var b, kernBuffer sfnt.Buffer
	var nodes []*svgNode
	var paths strings.Builder
	var previous renderedGlyph
	var advance float64
	bytes := 0
	flush := func() {
		if paths.Len() > 0 {
			nodes = append(nodes, svgElement("path", "d", paths.String()))
			paths.Reset()
		}
	}
	for _, r := range text {
		if r == 0xfe0e || r == 0xfe0f || r == 0x200d {
			continue
		}
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		g, found := findGlyph(primary, r, ppem, &b)
		if !found {
			x, y, w, h, s := advance+size*.1, -size*.75, size*.5, size*.75, size*.06
			p := fmt.Sprintf("M%g %g h%g v%g h%g Z M%g %g v%g h%g v%g Z ", x, y, w, h, -w, x+s, y+s, h-2*s, w-2*s, -h+2*s)
			paths.WriteString(p)
			bytes += len(p)
			advance += size * .7
			previous = renderedGlyph{}
			continue
		}
		if previous.face == g.face {
			kern, err := g.face.Kern(&kernBuffer, previous.index, g.index, ppem, font.HintingNone)
			if err != nil && err != sfnt.ErrNotFound {
				return nil, 0, err
			}
			advance += float64(kern) / 64
		}
		if len(g.color) > 0 {
			flush()
			group := svgElement("g", "transform", fmt.Sprintf("translate(%g,0)", advance))
			group.children = g.color
			nodes = append(nodes, group)
			for _, n := range g.color {
				for _, a := range n.start.Attr {
					bytes += len(a.Value)
				}
			}
		} else {
			p, err := glyphOutline(g.segments, advance)
			if err != nil {
				return nil, 0, err
			}
			paths.WriteString(p)
			bytes += len(p)
		}
		if bytes > 16<<20 {
			return nil, 0, fmt.Errorf("SVG text outlines and images exceed 16 MiB")
		}
		advance += float64(g.width) / 64
		previous = g
	}
	flush()
	return nodes, advance, nil
}

func glyphOutline(segments sfnt.Segments, offset float64) (string, error) {
	var path strings.Builder
	open := false
	for _, s := range segments {
		command, count := "", 0
		switch s.Op {
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
			return "", fmt.Errorf("unsupported SVG font outline operation")
		}
		if path.Len() > 16*1024*1024-256 {
			return "", fmt.Errorf("SVG text outline exceeds 16 MiB")
		}
		path.WriteString(command)
		for _, p := range s.Args[:count] {
			path.WriteString(strconv.FormatFloat(offset+float64(p.X)/64, 'f', -1, 64))
			path.WriteByte(' ')
			path.WriteString(strconv.FormatFloat(float64(p.Y)/64, 'f', -1, 64))
			path.WriteByte(' ')
		}
	}
	if open {
		path.WriteString("Z ")
	}
	return path.String(), nil
}
