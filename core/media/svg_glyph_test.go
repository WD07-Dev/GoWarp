package media

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestSVGGlyphMetricsAndBaseline(t *testing.T) {
	path, advance, err := svgGlyphPath("A", 32, "sans-serif", "normal", "normal")
	if err != nil {
		t.Fatal(err)
	}
	/*
		Independent bundled Go Regular hmtx metrics: A=1366, space=569,
		unitsPerEm=2048. At 32px these are exact 26.6 coordinates.
	*/
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
	spaced, spaceAdvance, err := svgGlyphPath("A ", 32, "sans-serif", "normal", "normal")
	if err != nil || spaced != path || spaceAdvance != 30.234375 {
		t.Fatalf("space advance=%v err=%v", spaceAdvance, err)
	}
}

func TestSVGGlyphFontVariants(t *testing.T) {
	regular, _, _ := svgGlyphPath("A", 32, "unknown-font", "normal", "normal")
	bold, advance, err := svgGlyphPath("A", 32, "sans-serif", "bold", "normal")
	if err != nil || bold == regular || advance != 23.109375 {
		t.Fatalf("bold advance=%v err=%v", advance, err)
	}
	numeric, _, _ := svgGlyphPath("A", 32, "sans-serif", "700", "normal")
	italic, _, _ := svgGlyphPath("A", 32, "sans-serif", "normal", "italic")
	if numeric != bold || italic == regular {
		t.Fatal("font weight/style ignored")
	}
	for _, s := range []string{"W", "i"} {
		_, advance, err := svgGlyphPath(s, 32, "monospace", "normal", "normal")
		if err != nil || advance != 19.203125 {
			t.Fatalf("mono %q advance=%v err=%v", s, advance, err)
		}
	}
}

func TestSVGGlyphRejectsUnsupportedAndExcessiveInput(t *testing.T) {
	for _, s := range []string{"\U0010ffff", string([]byte{0xff}), strings.Repeat("A", 4097)} {
		if _, _, err := svgGlyphPath(s, 16, "", "", ""); err == nil {
			t.Fatalf("accepted invalid text %q", s[:min(len(s), 20)])
		}
	}
	for _, size := range []float64{0, -1, 8193, math.Inf(1), math.NaN()} {
		if _, _, err := svgGlyphPath("A", size, "", "", ""); err == nil {
			t.Fatalf("accepted size %v", size)
		}
	}
	path, advance, err := svgGlyphPath("", 16, "", "", "")
	if err != nil || path != "" || advance != 0 {
		t.Fatalf("empty text=%q %v %v", path, advance, err)
	}
}
