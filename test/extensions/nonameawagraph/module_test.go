package extension

import (
	e "gowarp/core/extension"
	"math"
	"testing"
)

func TestRayDirection(t *testing.T) {
	for _, tt := range []struct{ x, y, want float64 }{{0, 1, 0}, {1, 0, 90}, {0, -1, 180}, {-1, 0, -90}, {-1, -1, -135}, {0, 0, 0}} {
		args := map[string]any{"x1": "2", "y1": 3, "x2": 2 + tt.x, "y2": 3 + tt.y}
		got, err := New().Blocks[0].Handler(&e.Context{Args: args})
		if err != nil || math.Abs(got.Value.(float64)-tt.want) > 1e-10 {
			t.Errorf("(%v,%v): got %v, %v; want %v", tt.x, tt.y, got.Value, err, tt.want)
		}
	}
	// The older hidden opcode deliberately uses the positive 0..360 quadrant.
	got, err := New().Blocks[1].Handler(&e.Context{Args: map[string]any{"x1": 0, "y1": 0, "x2": -1, "y2": -1}})
	if err != nil || got.Value != float64(225) {
		t.Fatalf("legacy direction: %v, %v", got.Value, err)
	}
}
