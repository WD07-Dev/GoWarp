package extension

import "testing"

func TestNormalizedSplitHasNoFixedStageOrRounding(t *testing.T) {
	split := NormalizedSplit{TopX: .25, BottomX: .75, MoveX: .5, MoveY: -1}
	if got := split.Project(101, 60, 2); got != (SplitGeometry{25.25, 75.75, 1, -2}) {
		t.Fatalf("geometry = %+v", got)
	}
	if got := split.Project(200, 900, 1); got != (SplitGeometry{50, 150, .5, -1}) {
		t.Fatalf("resized geometry = %+v", got)
	}
}
