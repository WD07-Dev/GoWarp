package window

import (
	"image"
	"testing"
)

func TestCenterGroupKeepsSpacingAndUsesOuterBorders(t *testing.T) {
	views := []View{
		{ID: "main", X: 60, Y: 60, Width: 400, Height: 300, StageWidth: 240, StageHeight: 180, Topmost: true},
		{ID: "right", X: 480, Y: 60, Width: 400, Height: 300, StageX: 240, StageWidth: 240, StageHeight: 180},
		{ID: "bottom", X: 60, Y: 400, Width: 400, Height: 300, StageY: 180, StageWidth: 240, StageHeight: 180},
		{ID: "hidden", X: 9999, Y: 9999, Width: 10, Height: 10, StageWidth: 10, StageHeight: 10, Hidden: true},
	}
	outer := []image.Rectangle{image.Rect(60, 60, 476, 399), image.Rect(480, 60, 896, 399), image.Rect(60, 400, 476, 739), image.Rect(9999, 9999, 10009, 10009)}
	work := image.Rect(-1920, 40, 0, 1080)
	got, err := Center(views, outer, work, "main", true)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []image.Point{image.Pt(-1378, 220), image.Pt(-958, 220), image.Pt(-1378, 560), image.Pt(9999, 9999)} {
		v := got[i]
		if v.X != want.X || v.Y != want.Y {
			t.Fatalf("view %d=%d,%d want %v", i, v.X, v.Y, want)
		}
		v.X, v.Y = views[i].X, views[i].Y
		if v != views[i] {
			t.Fatal("centering changed non-position properties")
		}
	}
	if views[0].X != 60 || views[0].Y != 60 {
		t.Fatal("mutated caller's views")
	}
	got, err = Center(views, outer, work, "right", false)
	if err != nil || got[1].X != -1168 || got[1].Y != 390 || got[0] != views[0] {
		t.Fatalf("individual: %v %v", got, err)
	}
	got, err = Center(views, outer, image.Rect(20, 40, 320, 240), "main", true)
	if err != nil || got[0].X != 20 || got[0].Y != 40 || got[1].X != 440 {
		t.Fatalf("oversized: %v %v", got, err)
	}
	for _, state := range []State{{Minimized: true}, {Maximized: true}} {
		views[0].State = state
		if _, err := Center(views, outer, work, "main", false); err == nil {
			t.Fatal("moved minimized/maximized window")
		}
	}
	views[0].State = State{}
	views[0].Fullscreen = true
	if _, err := Center(views, outer, work, "main", false); err == nil {
		t.Fatal("moved fullscreen window")
	}
	for _, id := range []string{"", "missing"} {
		if _, err := Center(views, outer, work, id, true); err == nil {
			t.Fatal("unknown anchor accepted")
		}
	}
	if _, err := Center(views, nil, work, "main", true); err == nil {
		t.Fatal("missing outer rectangles accepted")
	}
}
