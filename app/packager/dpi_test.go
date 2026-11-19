package packager

import (
	"image"
	"testing"
)

func TestHighDPILayoutUsesPhysicalPixelsAndLogicalHitAreas(t *testing.T) {
	a := &application{}
	w, h := a.layoutScale(840, 620, 2)
	defer a.face.Close()
	defer a.title.Close()
	if w != 1680 || h != 1240 || a.width != 840 || a.height != 620 {
		t.Fatalf("layout %gx%g; UI %dx%d", w, h, a.width, a.height)
	}
	if got := a.pixelRect(image.Rect(20, 30, 120, 64)); got != image.Rect(40, 60, 240, 128) {
		t.Fatal(got)
	}
	x, y := a.logicalPoint(100, 120)
	if x != 50 || y != 60 {
		t.Fatalf("click = %d,%d", x, y)
	}
}
