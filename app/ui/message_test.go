package ui

import (
	"image"
	"strings"
	"testing"

	"golang.org/x/image/font"
)

func TestMessageUsesContentHeightAndPhysicalResolution(t *testing.T) {
	m := newMessage("GoWarp", "no supported packaged project: export again with GoWarp")
	width, height := m.Layout(0, 0)
	if width != 560 || height != 160 {
		t.Fatalf("short message window = %dx%d", width, height)
	}
	for _, scale := range []float64{1, 1.25, 2, 1} {
		w, h := m.layoutScale(scale)
		want := image.Rect(0, 0, int(560*scale), int(160*scale))
		if int(w) != want.Dx() || int(h) != want.Dy() || m.renderImage().Bounds() != want {
			t.Fatalf("message image was not rendered at scale %g", scale)
		}
	}
	long := newMessage("실행 오류", strings.Repeat("오류 내용이 길어지면 창 높이를 늘립니다.\n", 20))
	if long.height <= height || long.height > 384 || len(long.lines) != 12 {
		t.Fatalf("long message height=%d, lines=%d", long.height, len(long.lines))
	}
	if !strings.HasSuffix(long.lines[len(long.lines)-1], "…") {
		t.Fatal("truncated message has no ellipsis")
	}
}

func TestMessageWrapPreservesTextAndFitsWidth(t *testing.T) {
	face := Font(16)
	defer face.Close()
	for _, text := range []string{"한글메시지" + strings.Repeat("가", 100), strings.Repeat("a", 100), "첫째 줄\n둘째 줄"} {
		lines := wrapMessage(text, face, 512, 12)
		for _, line := range lines {
			if font.MeasureString(face, line).Ceil() > 512 {
				t.Fatalf("line exceeds available width: %q", line)
			}
		}
		if strings.Join(lines, "") != strings.ReplaceAll(text, "\n", "") {
			t.Fatal("wrapping lost message text")
		}
	}
}
