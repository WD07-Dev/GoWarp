package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"gowarp/core/project"
)

func assertHardOpacity(t *testing.T, pixels *image.NRGBA, opacity uint8) {
	t.Helper()
	if pixels == nil {
		t.Fatal("missing raster")
	}
	visible := false
	for i := 3; i < len(pixels.Pix); i += 4 {
		a := pixels.Pix[i]
		if a != 0 && a != opacity {
			t.Fatalf("coverage changed intended opacity: alpha=%d, want 0 or %d", a, opacity)
		}
		visible = visible || a != 0
	}
	if !visible {
		t.Fatal("raster lost all visible pixels")
	}
}

func TestRuntimeWithoutAntialiasPreservesOpacityAndPreparedPixels(t *testing.T) {
	data := []byte(`<svg width="32" height="32"><path d="M2.2 1.4L29.4 27.7L4.5 29.1Z" fill="red" opacity="0.5"/></svg>`)
	lib := scaledRuntimeFixture(t, context.Background(), data, project.Costume{MD5Ext: "shape.svg", DataFormat: "SVG"})
	c := lib.Costumes[0][0]
	original := append([]byte(nil), lib.CostumeImage(c).Pix...)
	if err := lib.SetAntialias(true); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, lib.CostumeImage(c).Pix) {
		t.Fatal("enabled antialiasing changed existing pixels")
	}
	if err := lib.SetAntialias(false); err != nil {
		t.Fatal(err)
	}
	if err := lib.PrepareCostumePlayback(nil); err != nil {
		t.Fatal(err)
	}
	assertHardOpacity(t, lib.CostumeImage(c), 127)
	assertHardOpacity(t, lib.CostumeImageAtScale(c, 4), 127)
	source := lib.cache.costumes[c]
	prepared := lib.cache.playback[source.key]
	if prepared == nil {
		t.Fatal("missing prepared SVG")
	}
	loaded, err := prepared.load()
	if err != nil {
		t.Fatal(err)
	}
	assertHardOpacity(t, loaded.asset(source.key).image, 127)
	if err := lib.SetAntialias(true); err == nil {
		t.Fatal("allowed changing mode after playback preparation")
	}
}

func TestDisplayWithoutAntialiasUsesWorkerMode(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`<svg width="24" height="24"><path d="M1.2 2.1L22.3 19.8L3.8 22.3Z" fill="blue" opacity="0.5"/></svg>`)
	if err := os.WriteFile(filepath.Join(dir, "shape.svg"), data, 0600); err != nil {
		t.Fatal(err)
	}
	p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{{MD5Ext: "shape.svg", DataFormat: "svg"}}}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if err := lib.SetAntialias(false); err != nil {
		t.Fatal(err)
	}
	if err := lib.EnableDisplayPreparation(dir, p); err != nil {
		t.Fatal(err)
	}
	c := lib.Costumes[0][0]
	lib.DisplayCostumeImageKey(c, 4, nil)
	select {
	case result := <-lib.cache.display.results:
		if result.err != nil {
			t.Fatal(result.err)
		}
		assertHardOpacity(t, result.pixels, 127)
	case <-time.After(5 * time.Second):
		t.Fatal("SVG display worker stalled")
	}
	if err := lib.SetAntialias(true); err == nil {
		t.Fatal("allowed changing active worker mode")
	}
}

func TestSVGWithoutAntialiasMixedClipAndText(t *testing.T) {
	uri := embeddedImage(t, "png", color.NRGBA{B: 255, A: 128})
	data := []byte(`<svg width="48" height="48"><defs><clipPath id="clip"><path d="M1.2 1.5L24.3 22.7L2.1 25.2Z"/></clipPath></defs><image width="28" height="28" href="` + uri + `" clip-path="url(#clip)"/><text x="3.3" y="45" font-size="19" fill="red" fill-opacity="0.5">Aa</text></svg>`)
	lib := scaledRuntimeFixture(t, context.Background(), data, project.Costume{MD5Ext: "mixed.svg", DataFormat: "svg"})
	if err := lib.SetAntialias(false); err != nil {
		t.Fatal(err)
	}
	pixels := lib.CostumeImage(lib.Costumes[0][0])
	blue, red := false, false
	for i := 0; i < len(pixels.Pix); i += 4 {
		a := pixels.Pix[i+3]
		if a != 0 && a != 127 && a != 128 {
			t.Fatalf("mixed SVG coverage remained: %d", a)
		}
		blue = blue || a == 128 && pixels.Pix[i+2] > 0
		red = red || a == 127 && pixels.Pix[i] > 0
	}
	if !blue || !red {
		t.Fatal("lost embedded image or text")
	}
}

func TestWithoutFontAntialiasPreservesMetricsAndColorOpacity(t *testing.T) {
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 19, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		t.Fatal(err)
	}
	hard := WithoutFontAntialias(face)
	defer hard.Close()
	if face.Metrics() != hard.Metrics() || font.MeasureString(face, "AV j") != font.MeasureString(hard, "AV j") {
		t.Fatal("font metrics changed")
	}
	dot := fixed.P(2, 22)
	dr, _, _, advance, ok := face.Glyph(dot, 'A')
	got, mask, mp, hardAdvance, hardOK := hard.Glyph(dot, 'A')
	if dr != got || advance != hardAdvance || ok != hardOK {
		t.Fatal("glyph positioning changed")
	}
	for y := 0; y < dr.Dy(); y++ {
		for x := 0; x < dr.Dx(); x++ {
			_, _, _, a := mask.At(mp.X+x, mp.Y+y).RGBA()
			if a != 0 && a != 65535 {
				t.Fatalf("font retained partial coverage %d", a)
			}
		}
	}
	pixels := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	drawer := font.Drawer{Dst: pixels, Src: image.NewUniform(color.NRGBA{R: 255, A: 91}), Face: hard, Dot: dot}
	drawer.DrawString("A")
	assertHardOpacity(t, pixels, 91)
}

func TestSVGWithoutAntialiasPreservesGradients(t *testing.T) {
	for _, gradient := range []string{
		`<linearGradient id="g" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#008cff" stop-opacity=".5"/><stop offset="1" stop-color="#008cff" stop-opacity="0"/></linearGradient>`,
		`<radialGradient id="g"><stop offset="0" stop-color="#008cff" stop-opacity=".5"/><stop offset="1" stop-color="#008cff" stop-opacity="0"/></radialGradient>`,
	} {
		data := []byte(`<svg width="32" height="32"><defs>` + gradient + `</defs><path d="M2.2 2.4H29.3V29.1H2.2Z" fill="url(#g)"/></svg>`)
		smooth, _, err := rasterSVGWithAntialias(data, 1, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		hard, _, err := rasterSVGWithAntialias(data, 1, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		var minAlpha, maxAlpha uint8 = 255, 0
		for y := 5; y < 27; y++ {
			for x := 5; x < 27; x++ {
				want, got := smooth.NRGBAAt(x, y), hard.NRGBAAt(x, y)
				if got != want {
					t.Fatalf("gradient color/opacity changed at %d,%d: hard=%v smooth=%v", x, y, got, want)
				}
				minAlpha, maxAlpha = min(minAlpha, got.A), max(maxAlpha, got.A)
				if got.A != 0 && (got.R != 0 || got.B < 240) {
					t.Fatalf("blue gradient replaced by another color: %v", got)
				}
			}
		}
		if maxAlpha == 0 || maxAlpha > 128 || minAlpha == maxAlpha {
			t.Fatalf("lost gradient transparency: alpha=%d..%d", minAlpha, maxAlpha)
		}
	}
}
