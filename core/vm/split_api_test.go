package vm

import (
	"testing"

	"gowarp/core/extension"
)

func TestScreenSplitAPIUsesSuppliedGeometryAndCopiesSlice(t *testing.T) {
	r := fixture(t, "{}")
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	parts := []extension.ScreenSplit{extension.NormalizedSplit{TopX: .25, BottomX: .75, MoveX: .5, MoveY: -1}}
	if _, err := c.Call("graphics.screenSplits", parts, true); err != nil {
		t.Fatal(err)
	}
	parts[0] = extension.NormalizedSplit{}
	snapshot := r.View().Split
	if snapshot == nil || !snapshot.ShowOriginal {
		t.Fatal("split state was not published")
	}
	want := extension.SplitGeometry{TopX: 25.25, BottomX: 75.75, MoveX: 1, MoveY: -2}
	if got := snapshot.Effects[0].Project(101, 60, 2); got != want {
		t.Fatalf("host changed geometry or retained input slice: %+v", got)
	}
	exposed := r.SplitEffects()
	exposed[0] = extension.NormalizedSplit{}
	if got := r.SplitEffects()[0].Project(101, 60, 2); got != want {
		t.Fatalf("public slice exposed runtime storage: %+v", got)
	}
	if _, err := c.Call("graphics.screenSplits", nil, false); err != nil {
		t.Fatal(err)
	}
	if r.View().Split != nil {
		t.Fatal("clear retained split effects")
	}
	if got := snapshot.Effects[0].Project(101, 60, 2); got != want {
		t.Fatalf("clear changed a published frame: %+v", got)
	}
}

func TestScreenSplitInvalidInputKeepsPreviousFrame(t *testing.T) {
	r := fixture(t, "{}")
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	if _, err := c.Call("graphics.screenSplits", []extension.ScreenSplit{extension.NormalizedSplit{TopX: .5}}, true); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []any{"wrong", []extension.ScreenSplit{nil}} {
		if _, err := c.Call("graphics.screenSplits", invalid, false); err == nil {
			t.Fatalf("accepted %v", invalid)
		}
		if len(r.SplitEffects()) != 1 || !r.SplitShowsOriginal() {
			t.Fatal("invalid input changed live split state")
		}
	}
}
