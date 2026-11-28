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
	now      float64
}

func (h *screenHost) Call(operation string, args ...any) (any, error) {
	if operation == "runtime.time" {
		return h.now, nil
	}
	if operation != "graphics.screenSplits" {
		return nil, fmt.Errorf("unexpected operation %s", operation)
	}
	h.parts, h.original = args[0].([]extension.ScreenSplit), args[1].(bool)
	return nil, nil
}

func TestSplitAnimationAndClear(t *testing.T) {
	host := &screenHost{}
	active := []split{{X: 12, Y: 34, Offset: 20, Start: 10, Duration: 1, Wait: .5,
		Direction: "horizontal", Easing: "sin", ReturnEasing: "sin", Returning: true}}
	c := &extension.Context{Host: host, State: map[string]any{"splits": active, "original": true}}
	ext := New()
	var previous extension.ScreenSplit
	for _, tc := range []struct{ now, offset float64 }{{10, 0}, {10.5, 20}, {11.25, 20}, {12, 0}, {12.5, 0}} {
		host.now = tc.now
		if _, err := ext.Events["tick"](c); err != nil {
			t.Fatal(err)
		}
		if len(host.parts) != 1 || !host.original {
			t.Fatalf("parts=%v original=%v", host.parts, host.original)
		}
		want := extension.SplitGeometry{TopX: 252, BottomX: 252, MoveX: tc.offset}
		if got := host.parts[0].Project(480, 360, 1); got != want {
			t.Fatalf("time %v: geometry=%+v want=%+v", tc.now, got, want)
		}
		if tc.now == 10.5 {
			previous = host.parts[0]
		}
	}
	active[0].X = 1000
	if got := previous.Project(480, 360, 1); got.TopX != 252 || got.MoveX != 20 {
		t.Fatalf("later updates changed previous geometry: %+v", got)
	}
	for _, block := range ext.Blocks {
		if block.Opcode == "edgetalesplit_clearSplits" {
			if _, err := block.Handler(c); err != nil {
				t.Fatal(err)
			}
			if len(host.parts) != 0 {
				t.Fatal("clear retained screen splits")
			}
			return
		}
	}
	t.Fatal("clear block is missing")
}
