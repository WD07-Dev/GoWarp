package extension

import (
	"errors"
	e "gowarp/core/extension"
	"math"
	"reflect"
	"testing"
)

func invoke(t *testing.T, name string, args map[string]any, host e.Host) e.Result {
	t.Helper()
	for _, block := range New().Blocks {
		if block.Opcode == "pivotmove_"+name {
			r, err := block.Handler(&e.Context{Args: args, Host: host})
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
	}
	t.Fatal("missing block", name)
	return e.Result{}
}

func TestPivotGeometry(t *testing.T) {
	for _, tt := range []struct{ angle, x, y float64 }{{90, 90, 180}, {0, 120, 190}, {180, 80, 210}, {270, 110, 220}} {
		args := map[string]any{"X": 100, "Y": 200, "PX": 10, "PY": 20, "ANGLE": tt.angle}
		x := invoke(t, "finalX", args, nil).Value.(float64)
		y := invoke(t, "finalY", args, nil).Value.(float64)
		if math.Abs(x-tt.x) > 1e-10 || math.Abs(y-tt.y) > 1e-10 {
			t.Errorf("angle %v: (%v,%v), want (%v,%v)", tt.angle, x, y, tt.x, tt.y)
		}
	}
}

func TestVelocityAndPoints(t *testing.T) {
	for _, tt := range []struct{ x, y, direction, speed float64 }{{0, 0, 0, 0}, {0, 1, 0, 1}, {1, 0, 90, 1}, {0, -1, 180, 1}, {-1, 0, -90, 1}, {3, 4, 36.86989764584402, 5}, {math.Copysign(0, -1), math.Copysign(0, -1), 0, 0}} {
		for _, entry := range []struct {
			name string
			args map[string]any
			want float64
		}{
			{"directionFromVelocity", map[string]any{"VX": tt.x, "VY": tt.y}, tt.direction},
			{"speedFromVelocity", map[string]any{"VX": tt.x, "VY": tt.y}, tt.speed},
			{"directionBetweenPoints", map[string]any{"SX": 10, "SY": 20, "EX": 10 + tt.x, "EY": 20 + tt.y}, tt.direction},
			{"distanceBetweenPoints", map[string]any{"SX": 10, "SY": 20, "EX": 10 + tt.x, "EY": 20 + tt.y}, tt.speed},
		} {
			got := invoke(t, entry.name, entry.args, nil).Value.(float64)
			if math.Abs(got-entry.want) > 1e-10 {
				t.Errorf("%s: got %v, want %v", entry.name, got, entry.want)
			}
		}
	}
}

type hostCall struct {
	operation string
	args      []any
}
type recordingHost struct {
	stage bool
	calls []hostCall
	fail  string
}

func (h *recordingHost) Call(op string, args ...any) (any, error) {
	h.calls = append(h.calls, hostCall{op, args})
	if op == h.fail {
		return nil, errors.New("host failed")
	}
	if op == "target.get" {
		return h.stage, nil
	}
	return nil, nil
}

func TestPivotMoveUsesAtomicPosition(t *testing.T) {
	args := map[string]any{"X": 100, "Y": 200, "PX": 10, "PY": 20, "ANGLE": 90}
	host := &recordingHost{}
	invoke(t, "movePivotTo", args, host)
	want := []hostCall{{"target.get", []any{"isStage"}}, {"target.set", []any{"direction", float64(90)}}, {"target.setXY", []any{float64(90), float64(180)}}}
	if !reflect.DeepEqual(host.calls, want) {
		t.Fatalf("calls = %#v; want %#v", host.calls, want)
	}
	host = &recordingHost{stage: true}
	invoke(t, "movePivotTo", args, host)
	if len(host.calls) != 1 {
		t.Fatal("stage was moved")
	}
	for _, operation := range []string{"target.get", "target.set", "target.setXY"} {
		host = &recordingHost{fail: operation}
		_, err := New().Blocks[0].Handler(&e.Context{Args: args, Host: host})
		if err == nil {
			t.Errorf("%s failure was ignored", operation)
		}
	}
}
