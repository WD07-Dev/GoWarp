package extension

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	e "gowarp/core/extension"
)

type drawingHost struct {
	surfaces map[string]bool
	current  float64
	switches int
	draws    [][]any
}

type triangleBenchmarkHost struct{ drawingHost }

func (h *triangleBenchmarkHost) Call(op string, args ...any) (any, error) {
	if op == "graphics.surface.triangle" {
		return nil, nil
	}
	return h.drawingHost.Call(op, args...)
}

func BenchmarkPenTriangleTransport(b *testing.B) {
	for _, textured := range []bool{false, true} {
		name := "solid"
		var texture any
		if textured {
			name, texture = "textured", "texture"
		}
		b.Run(name, func(b *testing.B) {
			host := &triangleBenchmarkHost{drawingHost: drawingHost{surfaces: map[string]bool{"texture": true}}}
			c := &e.Context{Host: host, State: map[string]any{}, Args: map[string]any{"x1": -10.5, "y1": 10.5, "x2": 10.5, "y2": -10.5, "x3": 10.5, "y3": 10.5}}
			if _, err := drawTriangle(c, texture); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := drawTriangle(c, texture); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func (h *drawingHost) Call(op string, args ...any) (any, error) {
	switch op {
	case "graphics.penColor":
		return []float64{1, 0, 0, .5}, nil
	case "graphics.surface.exists":
		return h.surfaces[args[0].(string)], nil
	case "target.costumeIndex":
		if args[0] == "texture" {
			return float64(1), nil
		}
		return float64(-1), nil
	case "target.get":
		return h.current, nil
	case "target.set":
		h.current = args[1].(float64)
		h.switches++
	case "graphics.surface.triangle":
		h.draws = append(h.draws, args)
	default:
		return nil, fmt.Errorf("unexpected operation %q", op)
	}
	return nil, nil
}

func TestTriangleConvertsPenPlusRulesWithoutChangingInputs(t *testing.T) {
	host := &drawingHost{}
	row := e.TriangleVertex{X: -10, Y: 10, R: 1, G: 1, B: 1, A: 1, W: 1}
	rows := [3]e.TriangleVertex{row, row, row}
	if _, err := drawPenTriangle(host, "Scratch Stage", nil, rows, false); err != nil {
		t.Fatal(err)
	}
	draw := host.draws[0][1].(e.SurfaceTriangle)
	got := draw.Vertices[0]
	want := e.TriangleVertex{X: -10, Y: 10, R: 1, A: .5, Z: -1, W: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("converted vertex = %#v, want %#v", got, want)
	}
	if rows[0].G != 1 || rows[0].Z != 0 {
		t.Fatal("adapter mutated the caller's vertices")
	}
	if !draw.MultiplyTextureAlpha {
		t.Fatal("Pen+ texture alpha rule was lost")
	}
}

func TestTextureResolutionAndCostumeSelection(t *testing.T) {
	row := e.TriangleVertex{R: 1, G: 1, B: 1, A: 1, W: 1}
	rows := [3]e.TriangleVertex{row, row, row}
	for _, tc := range []struct {
		name, destination, texture string
		surfaces                   map[string]bool
		current                    float64
		want                       *e.SurfaceTexture
		switches                   int
	}{
		{"named first", "Scratch Stage", "texture", map[string]bool{"texture": true}, 0, &e.SurfaceTexture{Kind: "surface", Surface: "texture"}, 0},
		{"select costume", "Scratch Stage", "texture", nil, 0, &e.SurfaceTexture{Kind: "costume", Costume: 1}, 1},
		{"current costume", "Scratch Stage", "texture", nil, 1, &e.SurfaceTexture{Kind: "costume", Costume: 1}, 0},
		{"self feedback", "texture", "texture", map[string]bool{"texture": true}, 0, &e.SurfaceTexture{Kind: "costume", Costume: 1}, 1},
		{"empty name", "Scratch Stage", "", map[string]bool{"": true}, 0, &e.SurfaceTexture{Kind: "surface"}, 0},
		{"missing", "Scratch Stage", "missing", nil, 0, nil, 0},
		{"stage is not a texture", "buffer", "Scratch Stage", map[string]bool{"Scratch Stage": true}, 0, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := &drawingHost{surfaces: tc.surfaces, current: tc.current}
			if _, err := drawPenTriangle(host, tc.destination, tc.texture, rows, true); err != nil {
				t.Fatal(err)
			}
			if host.switches != tc.switches {
				t.Fatalf("costume switches = %d, want %d", host.switches, tc.switches)
			}
			if tc.want == nil {
				if len(host.draws) != 0 {
					t.Fatal("missing source produced a draw")
				}
				return
			}
			if len(host.draws) != 1 || !reflect.DeepEqual(host.draws[0][1].(e.SurfaceTriangle).Texture, *tc.want) {
				t.Fatalf("draws = %#v", host.draws)
			}
			if host.draws[0][1].(e.SurfaceTriangle).Vertices[0].G != 1 {
				t.Fatal("textured draw inherited the pen color")
			}
		})
	}
}

func TestInvalidDrawingDoesNotSwitchCostume(t *testing.T) {
	for _, tc := range []struct {
		destination string
		x, w        float64
	}{
		{"missing", 0, 1},
		{"Scratch Stage", math.Inf(1), 1},
		{"Scratch Stage", 0, 0},
		{"Scratch Stage", 0, math.NaN()},
		{"Scratch Stage", 0, -1},
	} {
		host := &drawingHost{}
		row := e.TriangleVertex{X: tc.x, R: 1, G: 1, B: 1, A: 1, W: tc.w}
		_, err := drawPenTriangle(host, tc.destination, "texture", [3]e.TriangleVertex{row, row, row}, false)
		if err != nil {
			t.Fatal(err)
		}
		if host.switches != 0 || len(host.draws) != 0 {
			t.Fatalf("%+v: invalid drawing changed the target", tc)
		}
	}
}

func TestTriangleNormalizesNaNBeforePenPlusConversions(t *testing.T) {
	host := &drawingHost{}
	row := e.TriangleVertex{X: math.NaN(), R: math.NaN(), G: 1, B: 1, A: 1, Z: math.NaN(), W: 1}
	if _, err := drawPenTriangle(host, stageSurface, nil, [3]e.TriangleVertex{row, row, row}, false); err != nil {
		t.Fatal(err)
	}
	got := host.draws[0][1].(e.SurfaceTriangle).Vertices[0]
	want := e.TriangleVertex{A: .5, Z: -1, W: 1}
	if got != want {
		t.Fatalf("converted vertex = %+v, want %+v", got, want)
	}
}
