package render

import (
	"image"
	"math"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"gowarp/core/media"
	"gowarp/core/vm"
)

type countedTextFace struct {
	font.Face
	advances int
}

func (f *countedTextFace) GlyphAdvance(char rune) (fixed.Int26_6, bool) {
	f.advances++
	return f.Face.GlyphAdvance(char)
}

func TestWrapSkinTextMeasuresEachCharacterOnce(t *testing.T) {
	face := &countedTextFace{Face: basicfont.Face7x13}
	text := strings.Repeat("AV text ", 100)
	got := wrapSkinText(face, text, 4096)
	if len(got) != 2 {
		t.Fatalf("wrapped into %d lines, want 2", len(got))
	}
	if face.advances > utf8.RuneCountInString(text) {
		t.Fatalf("%d glyph measurements for %d characters", face.advances, utf8.RuneCountInString(text))
	}
}

func TestWrapSkinTextPreservesKerningAndLineBreaks(t *testing.T) {
	lib := &media.Library{}
	face, err := opentype.NewFace(lib.TextFont("Sans Serif"), &opentype.FaceOptions{Size: 48, DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	reference := func(text string, width float64) []string {
		var lines []string
		for _, paragraph := range strings.Split(text, "\n") {
			line := ""
			for _, char := range paragraph {
				next := line + string(char)
				if line != "" && float64(font.MeasureString(face, next))/64 > width {
					lines = append(lines, line)
					line = string(char)
				} else {
					line = next
				}
			}
			lines = append(lines, line)
		}
		return lines
	}
	for _, text := range []string{"", "\nAV\n\nTo\n", "AVATAR To Wa", "한글과 English 😀 e\u0301", " A\tB  ", "bad\xffinput"} {
		boundary := float64(font.MeasureString(face, "AV")) / 64
		for _, width := range []float64{-1, 0, 1, 32, boundary, boundary - 1.0/64, 4096, math.Inf(1), math.NaN()} {
			got, want := wrapSkinText(face, text, width), reference(text, width)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("text=%q width=%g: got %q want %q", text, width, got, want)
			}
		}
	}
}

func BenchmarkWrapSkinText(b *testing.B) {
	lib := &media.Library{}
	face, err := opentype.NewFace(lib.TextFont("Sans Serif"), &opentype.FaceOptions{Size: 48, DPI: 72})
	if err != nil {
		b.Fatal(err)
	}
	defer face.Close()
	text := strings.Repeat("AV To dialogue 한글 text ", 30)
	b.ReportAllocs()
	for b.Loop() {
		wrapSkinText(face, text, 4096)
	}
}

func TestTextSkinReplacesCostumeAndUsesBaselineOrigin(t *testing.T) {
	lib := &media.Library{Costumes: [][]*media.Costume{{{Image: image.NewNRGBA(image.Rect(0, 0, 1, 1)), Width: 1, Height: 1}}}}
	r := New(lib)
	target := vm.DisplayTarget{State: vm.TargetState{
		Size: 100, StretchX: 100, StretchY: 100, Direction: 90, Visible: true,
		Text: &vm.TextSkin{Text: "TEST", Color: "#ff0000", Font: "Sans Serif", Width: 480, Visible: true,
			FontSize: 24, LineHeight: 192, LineHeightDivisor: 7, PaddingY: 24.0 / 7, AnchorY: 24.0*.9 + 24.0/7, Align: .5, ScreenWidth: true},
	}}
	sprite, ok := r.DisplaySprite(target, 480, 360)
	if !ok || sprite.Costume.Width != 480 || sprite.Costume.CenterY < 24 || sprite.Costume.CenterY > 26 {
		t.Fatalf("text layout or baseline differs: %+v", sprite.Costume)
	}
	found := false
	for i := 0; i < len(sprite.Costume.Image.Pix); i += 4 {
		pixel := sprite.Costume.Image.Pix[i : i+4]
		if pixel[3] > 0 {
			found = true
			if pixel[0] != 255 || pixel[1] != 0 || pixel[2] != 0 {
				t.Fatal("text color was not used")
			}
		}
	}
	if !found {
		t.Fatal("text skin is blank")
	}
	target.State.Text = nil
	restored, ok := r.DisplaySprite(target, 480, 360)
	if !ok || restored.Costume.Width != 1 {
		t.Fatal("original costume was not restored")
	}
}

func TestTextSkinUsesCallerLayoutAndWidthUnits(t *testing.T) {
	r := New(&media.Library{})
	target := vm.DisplayTarget{State: vm.TargetState{
		Size: 200, StretchX: 100,
		Text: &vm.TextSkin{Text: "A\nB", Color: "#ff0000", Width: 100, OutlineWidth: 2,
			FontSize: 20, LineHeight: 30, PaddingY: 4, AnchorY: 7, Align: 0},
	}}
	costume := r.textCostume(target)
	if costume == nil || costume.Width != 104 || costume.Height != 72 || costume.CenterY != 9 {
		t.Fatalf("caller layout was not used: %+v", costume)
	}
	first := costume
	skin := *target.State.Text
	skin.ScreenWidth = true
	target.State.Text = &skin
	costume = r.textCostume(target)
	if costume == first || costume.Width != 54 {
		t.Fatalf("screen width should compensate for 200%% sprite scale: %+v", costume)
	}
}
