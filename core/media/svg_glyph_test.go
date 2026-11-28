package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

func TestSVGGlyphMetricsAndBaseline(t *testing.T) {
	path, advance, err := testGlyphPath("A", 32, "sans-serif", "normal", "normal")
	if err != nil {
		t.Fatal(err)
	}
	if advance != 21.34375 || path == "" {
		t.Fatalf("path=%q advance=%v", path, advance)
	}
	negativeY := false
	fields := strings.Fields(path)
	for i := 0; i < len(fields); {
		command := fields[i]
		i++
		count := map[string]int{
			"M": 2,
			"L": 2,
			"Q": 4,
			"C": 6,
			"Z": 0,
		}[command]
		for j := 0; j < count; j++ {
			n, err := strconv.ParseFloat(fields[i+j], 64)
			if err != nil {
				t.Fatal(err)
			}
			if j%2 == 1 {
				if n > 0 {
					t.Fatalf("capital A extends below baseline: %v", n)
				}
				negativeY = negativeY || n < 0
			}
		}
		i += count
	}
	if !negativeY || !strings.Contains(path, "Z") {
		t.Fatalf("missing baseline-relative closed outline: %q", path)
	}
	spaced, spaceAdvance, err := testGlyphPath("A ", 32, "sans-serif", "normal", "normal")
	if err != nil || spaced != path || spaceAdvance != 30.234375 {
		t.Fatalf("space advance=%v err=%v", spaceAdvance, err)
	}
}

func TestSVGGlyphFontVariants(t *testing.T) {
	regular, _, _ := testGlyphPath("A", 32, "unknown-font", "normal", "normal")
	bold, advance, err := testGlyphPath("A", 32, "sans-serif", "bold", "normal")
	if err != nil || bold == regular || advance != 23.109375 {
		t.Fatalf("bold advance=%v err=%v", advance, err)
	}
	numeric, _, _ := testGlyphPath("A", 32, "sans-serif", "700", "normal")
	italic, _, _ := testGlyphPath("A", 32, "sans-serif", "normal", "italic")
	if numeric != bold || italic == regular {
		t.Fatal("font weight/style ignored")
	}
	for _, s := range []string{"W", "i"} {
		_, advance, err := testGlyphPath(s, 32, "monospace", "normal", "normal")
		if err != nil || advance != 19.203125 {
			t.Fatalf("mono %q advance=%v err=%v", s, advance, err)
		}
	}
}

func TestSVGGlyphRejectsUnsupportedAndExcessiveInput(t *testing.T) {
	for _, s := range []string{string([]byte{0xff}), strings.Repeat("A", 4097)} {
		if _, _, err := testGlyphPath(s, 16, "", "", ""); err == nil {
			t.Fatalf("accepted invalid text %q", s[:min(len(s), 20)])
		}
	}
	for _, size := range []float64{0, -1, 8193, math.Inf(1), math.NaN()} {
		if _, _, err := testGlyphPath("A", size, "", "", ""); err == nil {
			t.Fatalf("accepted size %v", size)
		}
	}
	path, advance, err := testGlyphPath("", 16, "", "", "")
	if err != nil || path != "" || advance != 0 {
		t.Fatalf("empty text=%q %v %v", path, advance, err)
	}
}

func TestSVGMissingGlyphUsesPlaceholder(t *testing.T) {
	path, advance, err := testGlyphPath("\U0010ffff", 32, "sans-serif", "", "")
	if err != nil || path == "" || advance <= 0 {
		t.Fatalf("missing glyph: path=%q advance=%g error=%v", path, advance, err)
	}
	img, _, err := rasterSVGWithFonts([]byte(`<svg width="160" height="60"><text x="4" y="44" font-size="36">A🎶한&#x10ffff;</text></svg>`), 1, nil)
	if err != nil || img == nil {
		t.Fatalf("mixed fallback text: %v", err)
	}
}

// Fixed metric assertions use bundled fonts, independent of the host desktop.
func testGlyphPath(text string, size float64, family, weight, style string) (string, float64, error) {
	return svgGlyphPathWithFont(text, size, family, weight, style, func(string) *sfnt.Font {
		variant := fontVariant(weight, style)
		if fontFamily(family) == 2 {
			variant |= 4
		}
		return bundledFont(variant)
	})
}

func TestSVGSystemColorGlyphs(t *testing.T) {
	for _, format := range []string{"COLR", "CBDT"} {
		t.Run(format, func(t *testing.T) {
			face, err := sfnt.Parse(goregular.TTF)
			if err != nil {
				t.Fatal(err)
			}
			var buffer sfnt.Buffer
			glyph, err := face.GlyphIndex(&buffer, 'A')
			if err != nil {
				t.Fatal(err)
			}
			var tables map[string][]byte
			if format == "COLR" {
				colr := make([]byte, 24)
				binary.BigEndian.PutUint16(colr[2:], 1)
				binary.BigEndian.PutUint32(colr[4:], 14)
				binary.BigEndian.PutUint32(colr[8:], 20)
				binary.BigEndian.PutUint16(colr[12:], 1)
				binary.BigEndian.PutUint16(colr[14:], uint16(glyph))
				binary.BigEndian.PutUint16(colr[18:], 1)
				binary.BigEndian.PutUint16(colr[20:], uint16(glyph))
				cpal := make([]byte, 18)
				binary.BigEndian.PutUint16(cpal[2:], 1)
				binary.BigEndian.PutUint16(cpal[4:], 1)
				binary.BigEndian.PutUint16(cpal[6:], 1)
				binary.BigEndian.PutUint32(cpal[8:], 14)
				copy(cpal[14:], []byte{0, 0, 255, 255})
				tables = map[string][]byte{"COLR": colr, "CPAL": cpal}
			} else {
				pixels := image.NewNRGBA(image.Rect(0, 0, 2, 2))
				for y := range 2 {
					for x := range 2 {
						pixels.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
					}
				}
				var encoded bytes.Buffer
				if err := png.Encode(&encoded, pixels); err != nil {
					t.Fatal(err)
				}
				cbdt := make([]byte, 13)
				binary.BigEndian.PutUint16(cbdt, 3)
				copy(cbdt[4:], []byte{2, 2, 0, 2, 2})
				binary.BigEndian.PutUint32(cbdt[9:], uint32(encoded.Len()))
				cbdt = append(cbdt, encoded.Bytes()...)
				cblc := make([]byte, 80)
				binary.BigEndian.PutUint16(cblc, 3)
				binary.BigEndian.PutUint32(cblc[4:], 1)
				binary.BigEndian.PutUint32(cblc[8:], 56)
				binary.BigEndian.PutUint32(cblc[12:], 24)
				binary.BigEndian.PutUint32(cblc[16:], 1)
				binary.BigEndian.PutUint16(cblc[48:], uint16(glyph))
				binary.BigEndian.PutUint16(cblc[50:], uint16(glyph))
				copy(cblc[52:], []byte{2, 2, 32, 1})
				binary.BigEndian.PutUint16(cblc[56:], uint16(glyph))
				binary.BigEndian.PutUint16(cblc[58:], uint16(glyph))
				binary.BigEndian.PutUint32(cblc[60:], 8)
				binary.BigEndian.PutUint16(cblc[64:], 1)
				binary.BigEndian.PutUint16(cblc[66:], 17)
				binary.BigEndian.PutUint32(cblc[68:], 4)
				binary.BigEndian.PutUint32(cblc[76:], uint32(len(cbdt)-4))
				tables = map[string][]byte{"CBLC": cblc, "CBDT": cbdt}
			}
			data := make([]byte, 12+16*len(tables))
			binary.BigEndian.PutUint16(data[4:], uint16(len(tables)))
			i := 0
			for tag, table := range tables {
				record := data[12+i*16:]
				copy(record, tag)
				binary.BigEndian.PutUint32(record[8:], uint32(len(data)))
				binary.BigEndian.PutUint32(record[12:], uint32(len(table)))
				data = append(data, table...)
				i++
			}
			f := &systemFont{face: face, data: data}
			systemFontFaces.Store(face, f)
			defer systemFontFaces.Delete(face)
			img, _, err := rasterSVGWithFonts([]byte(`<svg width="80" height="64"><text x="10" y="48" font-size="32" fill="blue">A</text></svg>`), 1, func(string) *sfnt.Font { return face })
			if err != nil {
				t.Fatal(err)
			}
			red := 0
			for i := 0; i < len(img.Pix); i += 4 {
				if img.Pix[i] > 200 && img.Pix[i+1] < 10 && img.Pix[i+2] < 10 && img.Pix[i+3] > 128 {
					red++
				}
			}
			if red < 20 {
				t.Fatalf("color glyph lost its palette/image: %d red pixels", red)
			}
			// Invalid table bounds must fall back to the ordinary outline, not panic.
			f.data = f.data[:12]
			img, _, err = rasterSVGWithFonts([]byte(`<svg width="80" height="64"><text x="10" y="48" font-size="32">A</text></svg>`), 1, func(string) *sfnt.Font { return face })
			if err != nil || img == nil {
				t.Fatalf("malformed color table fallback: %v", err)
			}
		})
	}
}

func TestTextFontPrefersProjectThenSystem(t *testing.T) {
	custom := bundledFont(1)
	lib := &Library{fonts: map[string]*sfnt.Font{"Project": custom}}
	if lib.TextFont(`"Project", sans-serif`) != custom {
		t.Fatal("system font replaced project font")
	}
	want := bundledFont(0)
	if system := browserFont("sans-serif", "", ""); system != nil {
		want = system.face
	}
	if lib.TextFont("sans-serif") != want {
		t.Fatal("default text font did not use system sans-serif")
	}
}

func TestScratchSansSerifMetrics(t *testing.T) {
	// Chromium SVG measurement with Scratch's Noto Sans Medium at 40px, including kerning.
	for _, family := range []string{"Sans Serif", `"Sans Serif", sans-serif`, "'Sans Serif'"} {
		_, advance, err := svgGlyphPath("COMPILE JSON", 40, family, "normal", "normal")
		if err != nil || math.Abs(advance-281.453125) > 0.001 {
			t.Errorf("%s: advance=%g, want Noto Sans Medium width 281.453125; error=%v", family, advance, err)
		}
	}
	custom := bundledFont(1)
	lib := &Library{fonts: map[string]*sfnt.Font{"Sans Serif": custom}}
	if lib.TextFont("Sans Serif") != custom {
		t.Fatal("built-in Scratch font overrides project font")
	}
}

func TestScratchMarkerMetrics(t *testing.T) {
	// Knewave: advances 6303, OR kerning -15, units/em 1000.
	for _, family := range []string{"Marker", `"Marker", sans-serif`} {
		_, advance, err := svgGlyphPath("EDITOR MENU", 40, family, "normal", "normal")
		if err != nil || math.Abs(advance-251.52) > 0.15 {
			t.Fatalf("%s: advance=%g, want Knewave width 251.52; error=%v", family, advance, err)
		}
	}
}

func TestYaHeiFontFamily(t *testing.T) {
	for _, family := range []string{"Microsoft YaHei", `"微软雅黑", STXihei`, `"Microsoft YaHei", "微软雅黑", STXihei, "华文细黑"`} {
		if fontFamily(family) != 6 {
			t.Errorf("%s fell back to a different font family", family)
		}
	}
}

func TestSVGScaledTextStroke(t *testing.T) {
	for _, scale := range []float64{1, 2} {
		got, _, err := rasterSVGWithFonts([]byte(`<svg width="160" height="60"><g stroke="black" stroke-width="10" fill="white"><text transform="translate(5,45) scale(0.5)" font-family="Sans Serif" font-size="80">MENU</text></g></svg>`), scale, nil)
		if err != nil {
			t.Fatal(err)
		}
		want, _, err := rasterSVGWithFonts([]byte(`<svg width="160" height="60"><text x="5" y="45" stroke="black" stroke-width="5" fill="white" font-family="Sans Serif" font-size="40">MENU</text></svg>`), scale, nil)
		if err != nil {
			t.Fatal(err)
		}
		var total int
		for i, v := range got.Pix {
			total += int(math.Abs(float64(int(v) - int(want.Pix[i]))))
		}
		if float64(total)/float64(len(got.Pix)) > 1 {
			t.Fatalf("scale %g: scaled text stroke differs from equivalent geometry: mean error %g", scale, float64(total)/float64(len(got.Pix)))
		}
	}
}
