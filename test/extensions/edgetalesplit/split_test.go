package extension

import (
	"fmt"
	"testing"

	"gowarp/core/extension"
)

func TestProjectPreservesSplitCoordinatesAndRounding(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		part                 part
		width, height, scale float64
		want                 extension.SplitGeometry
	}{
		{"vertical", part{direction: "vertical", x: 12, y: 34, offset: 20}, 480, 360, 1, extension.SplitGeometry{TopX: 252, BottomX: 252, MoveX: 0, MoveY: 20}},
		{"widescreen", part{direction: "both", x: 120, y: 90, offset: 2.25}, 960, 540, 2, extension.SplitGeometry{TopX: 720, BottomX: 720, MoveX: 5, MoveY: 5}},
		{"positive angle", part{angle: 45, offset: 20, direction: "horizontal"}, 480, 360, 1, extension.SplitGeometry{TopX: 420, BottomX: 60, MoveX: -14, MoveY: 14}},
		{"negative angle", part{angle: -45, offset: 20, direction: "vertical"}, 480, 360, 1, extension.SplitGeometry{TopX: 60, BottomX: 420, MoveX: 14, MoveY: 14}},
		{"shifted high DPI", part{x: 120, y: 90, angle: 45, offset: 20}, 480, 360, 1.5, extension.SplitGeometry{TopX: 450, BottomX: 90, MoveX: -21, MoveY: 21}},
		{"negative half pixel", part{direction: "horizontal", offset: -1.5}, 480, 360, 1, extension.SplitGeometry{TopX: 240, BottomX: 240, MoveX: -2, MoveY: 0}},
		{"unknown direction", part{direction: "other", offset: 20}, 480, 360, 1, extension.SplitGeometry{TopX: 240, BottomX: 240, MoveX: 0, MoveY: 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.part.Project(tc.width, tc.height, tc.scale); got != tc.want {
				t.Fatalf("geometry = %+v, want %+v", got, tc.want)
			}
		})
	}
}

type screenHost struct {
	parts    []extension.ScreenSplit
	original bool
}

func (h *screenHost) Call(operation string, args ...any) (any, error) {
	if operation != "graphics.screenSplits" {
		return nil, fmt.Errorf("unexpected operation %s", operation)
	}
	h.parts, h.original = args[0].([]extension.ScreenSplit), args[1].(bool)
	return nil, nil
}

func TestAdapterCopiesInputAndKeepsUnusedFieldsHarmless(t *testing.T) {
	host := &screenHost{}
	row := map[string]any{"x": 12, "offset": 20, "angle": 0, "direction": "horizontal", "width": 5}
	if _, err := setSplits(host, []any{[]any{row, "ignored"}, true, "unused color"}); err != nil {
		t.Fatal(err)
	}
	row["x"], row["offset"] = 1000, 2000
	if len(host.parts) != 1 || !host.original {
		t.Fatalf("parts=%v original=%v", host.parts, host.original)
	}
	if got := host.parts[0].Project(480, 360, 1); got != (extension.SplitGeometry{TopX: 252, BottomX: 252, MoveX: 20, MoveY: 0}) {
		t.Fatalf("input mutation changed geometry: %+v", got)
	}
	if _, err := setSplits(host, []any{"not an array"}); err == nil {
		t.Fatal("accepted invalid parts")
	}
	if len(host.parts) != 1 {
		t.Fatal("invalid call cleared the previous state")
	}
	if _, err := setSplits(host, nil); err != nil || len(host.parts) != 0 || host.original {
		t.Fatalf("clear: parts=%v original=%v error=%v", host.parts, host.original, err)
	}
}
