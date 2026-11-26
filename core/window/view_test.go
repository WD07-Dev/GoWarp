package window

import (
	"image"
	"strconv"
	"testing"
)

func TestDesktopPositionDoesNotAffectAtlas(t *testing.T) {
	views := []View{
		{ID: "left", X: -2000, Y: 30, Width: 480, Height: 360, StageWidth: 480, StageHeight: 360},
		{ID: "right", X: 10000, Y: 200, Width: 480, Height: 360, StageX: 480, StageWidth: 480, StageHeight: 360},
	}
	layout, err := Pack(views)
	if err != nil {
		t.Fatal(err)
	}
	if layout.Size != image.Pt(960, 360) {
		t.Fatalf("atlas: %v", layout.Size)
	}
	if layout.Tiles[1] != image.Rect(480, 0, 960, 360) {
		t.Fatal(layout.Tiles)
	}
	x, y := views[1].StagePoint(240, 180)
	if x != 720 || y != 180 {
		t.Fatalf("stage input: %v %v", x, y)
	}
	views[1].X = -4000
	moved, err := Pack(views)
	if err != nil || moved.Size != layout.Size || moved.Tiles[1] != layout.Tiles[1] {
		t.Fatal(moved, err)
	}
}

func TestInvalidViews(t *testing.T) {
	valid := View{ID: "main", Width: 480, Height: 360, StageWidth: 480, StageHeight: 360}
	for _, change := range []func(*View){
		func(v *View) { v.Width = 0 },
		func(v *View) { v.Height = 4097 },
		func(v *View) { v.ID = "" },
		func(v *View) { v.StageWidth = 0 },
		func(v *View) { n := 1.1; v.Opacity = &n },
	} {
		v := valid
		change(&v)
		if _, err := Pack([]View{v}); err == nil {
			t.Fatalf("accepted %+v", v)
		}
	}
	if _, err := Pack([]View{valid, valid}); err == nil {
		t.Fatal("accepted duplicate ID")
	}
}

func BenchmarkPack(b *testing.B) {
	for _, count := range []int{1, 4, 16} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			views := make([]View, count)
			for i := range views {
				views[i] = View{ID: strconv.Itoa(i), Width: 480, Height: 360, StageWidth: 480, StageHeight: 360}
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Pack(views); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
