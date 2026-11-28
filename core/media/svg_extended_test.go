package media

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"runtime"
	"strings"
	"testing"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"

	"gowarp/core/project"
)

func embeddedImage(t *testing.T, format string, c color.NRGBA) string {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			im.SetNRGBA(x, y, c)
		}
	}
	var data bytes.Buffer
	var err error
	if format == "jpeg" {
		err = jpeg.Encode(&data, im, &jpeg.Options{Quality: 100})
	} else {
		err = png.Encode(&data, im)
	}
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(data.Bytes())
	return "data:image/" + format + ";base64," + encoded[:20] + " \n" + encoded[20:]
}

func TestSVGEmbeddedImageTransformAllocationsAndPixels(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 8), G: uint8(y * 10), B: 120, A: uint8(80 + (x+y)%176)})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	for _, tc := range []struct {
		transform string
		matrix    f64.Aff3
	}{
		{"matrix(3 0.3 -0.2 3 10 4)", f64.Aff3{3, -.2, 10, .3, 3, 4}},
		{"matrix(3 0 0 3 10 4)", f64.Aff3{3, 0, 10, 0, 3, 4}},
		{"matrix(1 0 0 1 10.25 4.75)", f64.Aff3{1, 0, 10.25, 0, 1, 4.75}},
		{"matrix(0.5 0 0 0.5 10 4)", f64.Aff3{.5, 0, 10, 0, .5, 4}},
		{"matrix(1000000 0 0 1000000 0 0)", f64.Aff3{1000000, 0, 0, 0, 1000000, 0}},
	} {
		for _, opacity := range []string{"1", "0.5"} {
			data := []byte(`<svg width="128" height="96"><image width="32" height="24" transform="` + tc.transform + `" opacity="` + opacity + `" href="` + uri + `"/></svg>`)
			var pixels *image.NRGBA
			allocs := testing.AllocsPerRun(2, func() {
				var err error
				pixels, _, err = rasterSVGWithFonts(data, 1, nil)
				if err != nil {
					t.Fatal(err)
				}
			})
			if opacity == "1" && allocs > 1000 {
				t.Errorf("opaque embedded image allocated %.0f objects", allocs)
			}
			alpha := uint8(255)
			if opacity == "0.5" {
				alpha = 128
			}
			wantRGBA := image.NewRGBA(pixels.Bounds())
			xdraw.BiLinear.Transform(wantRGBA, tc.matrix, src, src.Bounds(), draw.Over,
				&xdraw.Options{SrcMask: image.NewUniform(color.Alpha{A: alpha})})
			want := image.NewNRGBA(pixels.Bounds())
			draw.Draw(want, want.Bounds(), wantRGBA, image.Point{}, draw.Src)
			for i, value := range pixels.Pix {
				if delta := int(value) - int(want.Pix[i]); delta < -1 || delta > 1 {
					t.Fatalf("opacity=%s channel=%d: %d, want %d", opacity, i, value, want.Pix[i])
				}
			}
			t.Logf("opacity=%s: %.0f allocations", opacity, allocs)
		}
	}
}

func TestSVGUniformImageEnlargementMemory(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 128, 96))
	fill := color.NRGBA{R: 40, G: 100, B: 180, A: 255}
	draw.Draw(src, src.Bounds(), image.NewUniform(fill), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	data := []byte(`<svg width="512" height="384"><image width="512" height="384" href="data:image/png;base64,` + base64.StdEncoding.EncodeToString(encoded.Bytes()) + `"/></svg>`)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	pixels, _, err := rasterSVGWithFonts(data, 1, nil)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > uint64(len(pixels.Pix)*3) {
		t.Errorf("uniform SVG image allocated %d bytes for %d output bytes", allocated, len(pixels.Pix))
	}
	for y := 0; y < pixels.Bounds().Dy(); y++ {
		for x := 0; x < pixels.Bounds().Dx(); x++ {
			if pixels.NRGBAAt(x, y) != fill {
				t.Fatalf("uniform image changed at %d,%d", x, y)
			}
		}
	}
}

func TestSVGUniformImagePreservesAlphaAndClipping(t *testing.T) {
	for _, alpha := range []uint8{0, 80, 200, 255} {
		fill := color.NRGBA{R: 40, G: 100, B: 180, A: alpha}
		uri := embeddedImage(t, "png", fill)
		for _, opacity := range []string{"1", "0.5"} {
			data := []byte(`<svg width="8" height="6"><rect width="8" height="6" fill="red"/><image x="-2" y="1" width="6" height="6" opacity="` + opacity + `" href="` + uri + `"/></svg>`)
			got, _, err := rasterSVGWithFonts(data, 1, nil)
			if err != nil {
				t.Fatal(err)
			}
			src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
			draw.Draw(src, src.Bounds(), image.NewUniform(fill), image.Point{}, draw.Src)
			reference := image.NewRGBA(got.Bounds())
			draw.Draw(reference, reference.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
			mask := uint8(255)
			if opacity == "0.5" {
				mask = 128
			}
			xdraw.BiLinear.Transform(reference, f64.Aff3{3, 0, -2, 0, 3, 1}, src, src.Bounds(), draw.Over,
				&xdraw.Options{SrcMask: image.NewUniform(color.Alpha{A: mask})})
			want := image.NewNRGBA(got.Bounds())
			draw.Draw(want, want.Bounds(), reference, image.Point{}, draw.Src)
			for i, value := range got.Pix {
				if delta := int(value) - int(want.Pix[i]); delta < -1 || delta > 1 {
					t.Fatalf("alpha=%d opacity=%s channel=%d: %d, want %d", alpha, opacity, i, value, want.Pix[i])
				}
			}
		}
	}
}

func TestSVGEmbeddedImageLayeringTransformsOpacity(t *testing.T) {
	uri := embeddedImage(t, "png", color.NRGBA{B: 255, A: 255})
	data := `<svg width="20" height="12"><rect width="20" height="12" fill="#ff0000"/><g transform="translate(4 2) scale(2)" opacity="0.5"><image width="2" height="2" href="` + uri + `"/></g><path d="M6 2h2v4h-2z" fill="#00ff00"/></svg>`
	c, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if p := c.Image.NRGBAAt(1, 3); p.R != 255 || p.B != 0 {
		t.Fatalf("background %v", p)
	}
	if p := c.Image.NRGBAAt(4, 3); p.R < 126 || p.R > 129 || p.B < 126 || p.B > 129 || p.A != 255 {
		t.Fatalf("image transform/opacity %v", p)
	}
	if p := c.Image.NRGBAAt(6, 3); p.G != 255 || p.B != 0 {
		t.Fatalf("later path did not cover image: %v", p)
	}
}

func TestSVGImageGroupOpacityCompositesOnce(t *testing.T) {
	uri := embeddedImage(t, "png", color.NRGBA{B: 255, A: 255})
	data := `<svg width="8" height="8"><g opacity="0.5"><rect width="8" height="8" fill="red"/><image x="2" y="2" width="4" height="4" href="` + uri + `"/></g></svg>`
	c, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if p := c.Image.NRGBAAt(3, 3); p.B != 255 || p.R != 0 || p.A < 126 || p.A > 129 {
		t.Fatalf("group opacity applied per child: %v", p)
	}
}

func TestSVGEmbeddedJPEGAndViewBox(t *testing.T) {
	uri := embeddedImage(t, "jpeg", color.NRGBA{R: 255, A: 255})
	data := `<svg viewBox="10 20 8 8"><image x="10" y="20" width="8" height="8" href="` + uri + `" transform="matrix(1 0 0 1 0 0)"/></svg>`
	c, err := DecodeCostume(project.Costume{
		DataFormat:      "svg",
		RotationCenterX: 14,
		RotationCenterY: 24,
	}, []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if c.CenterX != 4 || c.CenterY != 4 || c.Image.NRGBAAt(3, 3).R < 250 {
		t.Fatalf("JPEG/viewBox: %+v", c)
	}
}

func TestSVGTextAndTspanRenderInOrder(t *testing.T) {
	data := `<svg width="100" height="45"><rect width="100" height="45" fill="#ff0000"/><g transform="translate(5 2)" fill="white"><text x="2" y="22" font-family="Sans Serif" font-size="20"><tspan>Hi</tspan><tspan dx="8" fill="#0000ff">A</tspan></text></g><rect x="0" y="35" width="100" height="10" fill="#00ff00"/></svg>`
	c, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	white, blue := 0, 0
	for y := 5; y < 28; y++ {
		for x := 5; x < 70; x++ {
			p := c.Image.NRGBAAt(x, y)
			if p.R > 200 && p.G > 200 && p.B > 200 {
				white++
			}
			if p.B > 200 && p.R < 50 {
				blue++
			}
		}
	}
	if white < 20 || blue < 10 {
		t.Fatalf("missing text/tspan colors: white=%d blue=%d", white, blue)
	}
	if p := c.Image.NRGBAAt(20, 40); p.G != 255 {
		t.Fatalf("later path layer %v", p)
	}
}

func TestSVGRejectsExternalAndUnsupportedImageData(t *testing.T) {
	for _, href := range []string{
		"https://example.invalid/a.png",
		"file:///tmp/a.png",
		"data:image/svg+xml;base64,PHN2Zy8+",
		"data:image/png;base64,not-base64",
	} {
		_, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(`<svg width="10" height="10"><image width="10" height="10" href="`+href+`"/></svg>`))
		if err == nil {
			t.Fatalf("accepted %q", href)
		}
	}
}

func TestMixedSVGProjectAssetsLoad(t *testing.T) {
	path, p := mixedMediaProject(t)
	lib, err := Load(path, p)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if len(lib.Costumes) != 3 || len(lib.Sounds) != 3 {
		t.Fatal("asset target count changed")
	}
	for i, target := range p.Targets {
		if len(lib.Costumes[i]) != len(target.Costumes) || len(lib.Sounds[i]) != len(target.Sounds) {
			t.Fatalf("%s: incomplete assets", target.Name)
		}
	}
	mixed := lib.Costumes[1][1].Image
	if mixed.NRGBAAt(1, 3).R != 255 || mixed.NRGBAAt(4, 3).B != 255 || mixed.NRGBAAt(6, 3).G != 255 {
		t.Fatal("loaded SVG lost embedded image transforms or vector ordering")
	}
	bitmap := lib.Costumes[1][0]
	if !bitmap.Bitmap || bitmap.Width != 2 || bitmap.Height != 2 || bitmap.Image.NRGBAAt(0, 0).B != 255 {
		t.Fatal("loaded bitmap geometry or pixels changed")
	}
	text := lib.Costumes[1][2].Image
	if bytes.Equal(text.Pix, make([]byte, len(text.Pix))) {
		t.Fatal("loaded SVG text is blank")
	}
	if lib.Costumes[1][3].Image.NRGBAAt(0, 0).A != 0 {
		t.Fatal("empty costume is not transparent")
	}
	for _, target := range []int{0, 1} {
		sound := lib.Sounds[target][0]
		if sound.Rate != 8000 || len(sound.Samples) != 8 || sound.Samples[2] <= 0 || sound.Samples[6] >= 0 {
			t.Fatal("loaded sound samples changed")
		}
	}
}

func TestSVGRejectsUnsupportedTextLayout(t *testing.T) {
	_, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(`<svg width="10" height="10"><text><textPath href="#x">bad</textPath></text></svg>`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "text") {
		t.Fatalf("unsupported text layout: %v", err)
	}
}

func TestSVGImageAspectRatioAndAffineRotation(t *testing.T) {
	uri := embeddedImage(t, "png", color.NRGBA{B: 255, A: 255})
	data := `<svg width="16" height="16"><image x="2" y="2" width="8" height="4" href="` + uri + `"/><image width="2" height="2" transform="translate(12 12) rotate(90) scale(2 1)" href="` + uri + `"/></svg>`
	c, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if c.Image.NRGBAAt(2, 3).A != 0 || c.Image.NRGBAAt(5, 3).B != 255 || c.Image.NRGBAAt(10, 13).B != 255 || c.Image.NRGBAAt(13, 13).A != 0 {
		t.Fatal("image aspect alignment or rotation was lost")
	}
}

func TestSVGRejectsExcessiveEmbeddedImageAndDepth(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 8193, 1))
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	inputs := []string{
		`<svg width="1" height="1"><image href="` + uri + `"/></svg>`,
		`<svg width="1" height="1">` + strings.Repeat(`<g>`, 129) + strings.Repeat(`</g>`, 129) + `</svg>`,
		`<svg width="1" height="1"><image transform="matrix(1e309 0 0 1 0 0)" href="` + uri + `"/></svg>`,
	}
	for _, data := range inputs {
		if _, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(data)); err == nil {
			t.Fatal("accepted excessive SVG resource")
		}
	}
}

func TestSVGTextAnchorAcrossTspan(t *testing.T) {
	decode := func(text string) *Costume {
		t.Helper()
		c, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(`<svg width="100" height="40"><text x="50" y="25" font-size="20" text-anchor="middle">`+text+`</text></svg>`))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	plain, span := decode("Hi"), decode("<tspan>Hi</tspan>")
	if !bytes.Equal(plain.Image.Pix, span.Image.Pix) {
		t.Fatal("inherited text anchor applied twice to tspan")
	}
}

func TestSVGImageInheritsRootVisibilityAndCanOverride(t *testing.T) {
	uri := embeddedImage(t, "png", color.NRGBA{B: 255, A: 255})
	for _, visible := range []bool{false, true} {
		attr := ""
		if visible {
			attr = ` visibility="visible"`
		}
		data := `<svg width="8" height="8" visibility="hidden"><image width="8" height="8"` + attr + ` href="` + uri + `"/></svg>`
		c, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(data))
		if err != nil {
			t.Fatal(err)
		}
		if got := c.Image.NRGBAAt(4, 4).A != 0; got != visible {
			t.Fatalf("visible=%v got=%v", visible, got)
		}
	}
}

func TestSVGRejectsExcessiveComposedTransform(t *testing.T) {
	uri := embeddedImage(t, "png", color.NRGBA{A: 255})
	for _, content := range []string{
		`<g transform="scale(1e12)"><image transform="scale(1e12)" href="` + uri + `"/></g>`,
		`<image width="1e308" height="1e308" href="` + uri + `"/>`,
	} {
		if _, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(`<svg width="8" height="8">`+content+`</svg>`)); err == nil {
			t.Fatal("accepted excessive composed transform")
		}
	}
}

func TestSVGEmptyScratchCostume(t *testing.T) {
	for _, content := range []string{"", `<g fill="none"/>`} {
		c, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(`<svg width="0" height="0">`+content+`</svg>`))
		if err != nil {
			t.Fatal(err)
		}
		if c.Width != 0 || c.Height != 0 || c.Image.Bounds().Dx() != 1 || c.Image.NRGBAAt(0, 0).A != 0 {
			t.Fatal("empty SVG geometry")
		}
	}
	if _, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(`<svg width="0" height="0"><rect width="4" height="4"/></svg>`)); err == nil {
		t.Fatal("nonempty zero-size SVG accepted")
	}
}

func TestSVGMixedGroupSharesUniformTransform(t *testing.T) {
	uri := embeddedImage(t, "png", color.NRGBA{B: 255, A: 255})
	data := `<svg width="12" height="12"><g transform="translate(2 2) scale(2)"><image width="2" height="2" href="` + uri + `"/><rect y="2" width="2" height="2" fill="#00ff00"/></g></svg>`
	c, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if c.Image.NRGBAAt(3, 3).B != 255 || c.Image.NRGBAAt(3, 7).G != 255 {
		t.Fatal("image and vector do not share group transform")
	}
}

func TestSVGVectorUniformScale(t *testing.T) {
	c, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(`<svg width="10" height="10"><g transform="translate(2 2) scale(2)"><rect width="2" height="2" fill="#00ff00"/></g></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Image.NRGBAAt(3, 5).G != 255 || c.Image.NRGBAAt(3, 7).A != 0 {
		t.Fatal("single-argument vector scale is not uniform")
	}
}

func TestSVGEmbeddedImageClipPath(t *testing.T) {
	uri := embeddedImage(t, "png", color.NRGBA{B: 255, A: 255})
	for i, content := range []string{
		`<image width="8" height="8" href="` + uri + `" clip-path="url(#clip)"/>`,
		`<g clip-path="url(#clip)" opacity="0.5"><rect width="8" height="8" fill="red"/><image width="8" height="8" href="` + uri + `"/></g>`,
	} {
		data := `<svg viewBox="10 20 12 12"><defs><clipPath id="clip"><path fill="none" d="M2 2h4v4h-4z"/></clipPath></defs><g transform="translate(11 21)">` + content + `</g></svg>`
		c, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(data))
		if err != nil {
			t.Fatal(err)
		}
		if pixel := c.Image.NRGBAAt(4, 4); pixel.B != 255 || pixel.R != 0 || pixel.A != []uint8{255, 128}[i] {
			t.Fatalf("clip interior missing: %v", pixel)
		}
		for _, point := range []image.Point{{2, 4}, {7, 4}, {4, 2}, {4, 7}} {
			if pixel := c.Image.NRGBAAt(point.X, point.Y); pixel.A != 0 {
				t.Fatalf("pixel outside clip at %v: %v", point, pixel)
			}
		}
	}
}
