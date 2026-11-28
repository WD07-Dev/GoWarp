package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"gowarp/core/media"
)

func TestTextFacePreservesPixels(t *testing.T) {
	for _, hard := range []bool{false, true} {
		r := New(&media.Library{})
		r.view.NoAntialias = hard
		cached, err := r.textFace(31.5, "Sans Serif")
		if err != nil {
			t.Fatal(err)
		}
		raw, err := opentype.NewFace(r.Assets.TextFont("Sans Serif"), &opentype.FaceOptions{Size: 31.5, DPI: 72, Hinting: font.HintingNone})
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		var reference font.Face = raw
		if hard {
			reference = media.WithoutFontAntialias(reference)
		}
		for phase := range 64 {
			dot := fixed.Point26_6{X: fixed.Int26_6(-64 + phase), Y: fixed.Int26_6(30*64 + phase)}
			for _, text := range []string{"AV To 한글 e\u0301", "0123456789", " \t\uffff"} {
				got, want := image.NewNRGBA(image.Rect(0, 0, 320, 50)), image.NewNRGBA(image.Rect(0, 0, 320, 50))
				draw.Draw(got, got.Bounds(), image.NewUniform(color.NRGBA{50, 70, 120, 100}), image.Point{}, draw.Src)
				copy(want.Pix, got.Pix)
				for i, face := range []font.Face{cached, reference} {
					dst := got
					if i == 1 {
						dst = want
					}
					d := font.Drawer{Dst: dst, Src: image.NewUniform(color.NRGBA{220, 80, 50, 170}), Face: face, Dot: dot}
					d.DrawString(text)
					d.Dot = dot.Add(fixed.Point26_6{X: 17, Y: -31})
					d.DrawString(text)
				}
				if !bytes.Equal(got.Pix, want.Pix) {
					t.Fatalf("changed pixels: noAA=%v phase=%d text=%q", hard, phase, text)
				}
			}
		}
	}
}

func TestTextFaceCacheOwnsMasksAndStaysBounded(t *testing.T) {
	r := New(&media.Library{})
	face, err := r.textFace(48, "Sans Serif")
	if err != nil {
		t.Fatal(err)
	}
	_, mask, _, _, _ := face.Glyph(fixed.P(0, 0), 'A')
	saved := bytes.Clone(mask.(*image.Alpha).Pix)
	for i := range 1600 {
		face.Glyph(fixed.Point26_6{X: fixed.Int26_6(i % 64), Y: fixed.Int26_6(i / 64)}, rune('A'+i%26))
	}
	if !bytes.Equal(saved, mask.(*image.Alpha).Pix) {
		t.Fatal("later glyphs overwrote retained mask")
	}
	cached := face.(*cachedTextFace)
	total := 0
	for _, entry := range cached.glyphs {
		if m := entry.Value.(textGlyph).mask; m != nil {
			total += len(m.Pix)
		}
	}
	if total != cached.bytes || total > textGlyphBytes || len(cached.glyphs) > textGlyphLimit {
		t.Fatalf("glyph cache: bytes=%d tracked=%d entries=%d", total, cached.bytes, len(cached.glyphs))
	}
	for i := range 20 {
		r.textSkinUse++
		if _, err := r.textFace(float64(20+i), "Sans Serif"); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.textFaces) > textFaceLimit {
		t.Fatal("unbounded font cache")
	}
}
