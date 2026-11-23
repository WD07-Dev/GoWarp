package ui

import (
	"testing"

	"golang.org/x/image/font"
)

func TestEmbeddedFontSupportsKorean(t *testing.T) {
	f := Font(16)
	defer f.Close()
	if font.MeasureString(f, "프로젝트 열기").Ceil() <= 0 {
		t.Fatal("no Korean text width")
	}
	if _, _, ok := f.GlyphBounds('한'); !ok {
		t.Fatal("missing Korean glyph")
	}
}
