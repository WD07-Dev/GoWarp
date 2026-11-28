package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"gowarp/core/media"
	"gowarp/core/vm"
)

func TestDisplayTextDoesNotRasterize(t *testing.T) {
	r := New(&media.Library{Costumes: [][]*media.Costume{{{Width: 1, Height: 1}}}})
	skin := vm.TextSkin{Text: "HUD", Width: 480, FontSize: 24, Visible: true, OutlineWidth: 2}
	target := vm.DisplayTarget{State: vm.TargetState{
		Text: &skin, X: 10000, Size: 100, StretchX: 100, StretchY: 100, Direction: 90,
	}}
	sprite, ok := r.DisplaySprite(target, 480, 360)
	if !ok {
		t.Fatal("text geometry missing")
	}
	if sprite.Costume.Image != nil || r.textSkinBytes != 0 {
		t.Fatal("display geometry rasterized text before visibility was checked")
	}
	cpu, ok := r.transform(target)
	if !ok || cpu.image == nil {
		t.Fatal("CPU sensing must still obtain text pixels")
	}
}

func TestDisplayTextGlyphsMatchCPUPixels(t *testing.T) {
	r := New(&media.Library{})
	for _, noAA := range []bool{false, true} {
		r.view.NoAntialias = noAA
		for _, outline := range []float64{0, 1.25, 2} {
			skin := vm.TextSkin{Text: "AV 한글 e\u0301\n01234  ", Font: "Sans Serif", Width: 83.25,
				FontSize: 18.25, LineHeight: 23, PaddingY: 2.125, AnchorY: 7,
				OutlineWidth: outline, OutlineColor: "#3185b6", Color: "#eb946c", Align: .5, Visible: true}
			target := vm.DisplayTarget{State: vm.TargetState{Text: &skin, Size: 100, StretchX: 100}}
			layout, costume := r.displayText(target)
			want := r.textCostume(target)
			got := image.NewNRGBA(image.Rectangle{Max: layout.Size})
			r.DrawTextGlyphs(layout, func(mask image.Image, point image.Point, rect image.Rectangle, ink color.NRGBA) {
				draw.DrawMask(got, rect, image.NewUniform(ink), image.Point{}, mask, point, draw.Over)
			})
			if !bytes.Equal(got.Pix, want.Image.Pix) || costume.CenterY != want.CenterY || costume.Width != want.Width {
				t.Fatalf("layout/glyphs differ: noAA=%v outline=%g", noAA, outline)
			}
		}
	}
}

func TestDisplayTextLayoutCacheBounded(t *testing.T) {
	r := New(&media.Library{})
	skin := vm.TextSkin{Width: 480, FontSize: 24}
	target := vm.DisplayTarget{State: vm.TargetState{Text: &skin}}
	for i := range 300 {
		skin.Text = fmt.Sprint(i)
		r.displayText(target)
	}
	total := 0
	for _, layout := range r.textLayouts {
		total += layout.bytes
	}
	if len(r.textLayouts) > 128 || total != r.textLayoutBytes || total > 1<<20 {
		t.Fatal("layout cache exceeded its budget")
	}
	if len(r.textSkins) != 0 || r.textSkinBytes != 0 {
		t.Fatal("layout cache created CPU pixel buffers")
	}
}

func BenchmarkDisplayTextChangingLabel(b *testing.B) {
	r := New(&media.Library{Costumes: [][]*media.Costume{{{Width: 1, Height: 1}}}})
	skin := vm.TextSkin{Width: 480, FontSize: 24, Visible: true, OutlineWidth: 2}
	target := vm.DisplayTarget{State: vm.TargetState{
		Text: &skin, Size: 100, StretchX: 100, StretchY: 100, Direction: 90,
	}}
	n := 0
	b.ReportAllocs()
	for b.Loop() {
		n++
		skin.Text = fmt.Sprint(n)
		r.DisplaySprite(target, 480, 360)
	}
}
