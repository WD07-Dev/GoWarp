package extension

import (
	"encoding/json"
	"fmt"
	e "gowarp/core/extension"
	"math"
	"reflect"
	"testing"
)

type timedKeysHost struct {
	keys  []string
	now   float64
	polls int
}

func (h *timedKeysHost) Call(op string, args ...any) (any, error) {
	switch op {
	case "input.keys":
		return append([]string{}, h.keys...), nil
	case "runtime.monotonicTime":
		return h.now, nil
	case "events.pollHats":
		if !reflect.DeepEqual(args, []any{"enderKeysPlusV2_eventKeysPressed", "level"}) {
			return nil, fmt.Errorf("changed hat polling: %v", args)
		}
		h.polls++
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected operation %s", op)
}
func TestKeyReporterTransitionsAndTiming(t *testing.T) {
	ext := New()
	host := &timedKeysHost{now: 10}
	state := map[string]any{}
	ctx := &e.Context{Host: host, State: state, Args: map[string]any{}, Fields: map[string]string{}}
	run := func(op string, want any) {
		t.Helper()
		result, err := handler(t, op)(ctx)
		equal := reflect.DeepEqual(result.Value, want)
		if a, ok := result.Value.(float64); ok {
			if b, ok := want.(float64); ok {
				equal = math.Abs(a-b) < 1e-10
			}
		}
		if err != nil || !equal {
			t.Fatalf("%s (%v / %v), now %g keys %v: got %#v, %v; want %#v", op, ctx.Args, ctx.Fields, host.now, host.keys, result.Value, err, want)
		}
	}
	tick := func() {
		t.Helper()
		if _, err := ext.Events["tick"](ctx); err != nil {
			t.Fatal(err)
		}
	}
	tick()
	run("lastKeyPressed", "")
	run("currentKeyPressed", "None")
	run("currentKeysPressed", "[]")
	host.keys = []string{"shiftleft"}
	tick()
	run("currentKeyPressed", "left shift")
	run("lastKeyPressed", "left shift")
	run("currentKeysPressed", `["left shift"]`)
	ctx.Args["key"] = "left shift"
	run("timeKeyPressed", float64(0))
	run("isKeyHit", false)
	host.now = 10.025
	run("timeKeyPressed", float64(.025))
	run("isKeyHit", true)
	ctx.Args["key"] = "#shift"
	run("timeKeyPressed", float64(.025))
	host.keys = []string{"shiftleft", "a"}
	host.now = 10.05
	tick()
	host.now = 10.1
	run("lastKeyPressed", "a")
	run("currentKeyPressed", "a")
	ctx.Args["key"] = "left shift"
	run("timeKeyPressed", float64(.1))
	run("isKeyHit", false)
	ctx.Args["key"] = "a"
	run("timeKeyPressed", float64(.05))
	run("isKeyHit", true)
	ctx.Args["keys"] = `"left shift","a"`
	ctx.Fields["ordered"] = "together & in order"
	run("isKeysPressed", true)
	run("isKeysHit", true)
	ctx.Fields["mode"] = "together & in order"
	run("timeKeysPressed", float64(.05))
	ctx.Args["keys"] = `"a","left shift"`
	run("isKeysPressed", false)
	run("timeKeysPressed", float64(0))
	// The original hit block calls timeKeysPressed without its mode argument.
	ctx.Fields["mode"] = ""
	run("isKeysHit", true)
	ctx.Fields["mode"] = "individually"
	result, err := handler(t, "timeKeysPressed")(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var times []float64
	if err = json.Unmarshal([]byte(result.Value.(string)), &times); err != nil || len(times) != 2 || math.Abs(times[0]-.05) > 1e-10 || math.Abs(times[1]-.1) > 1e-10 {
		t.Fatalf("individual times: %v %v", result.Value, err)
	}
	host.keys = []string{"shiftleft"}
	host.now = 11
	tick()
	run("lastKeyPressed", "a")
	run("currentKeyPressed", "left shift")
	ctx.Args["key"] = "a"
	run("timeKeyPressed", float64(0))
	run("isKeyHit", false)
	host.keys = nil
	tick()
	run("lastKeyPressed", "a")
	run("currentKeysPressed", "[]")
	host.keys = []string{"a"}
	host.now = 12
	tick()
	run("timeKeyPressed", float64(0))
	host.now = 12.02
	run("timeKeyPressed", float64(.02))
	run("isKeyHit", true)
	if host.polls != 6 {
		t.Fatalf("poll calls = %d, want 6", host.polls)
	}
}
func TestTimingStartsAtTickBeforeFirstReporter(t *testing.T) {
	ext := New()
	host := &timedKeysHost{keys: []string{"f2", "numpad3"}, now: 1}
	ctx := &e.Context{Host: host, State: map[string]any{}, Args: map[string]any{"key": "F2"}}
	if _, err := ext.Events["tick"](ctx); err != nil {
		t.Fatal(err)
	}
	host.now = 4
	got, err := handler(t, "timeKeyPressed")(ctx)
	if err != nil || got.Value != float64(3) {
		t.Fatalf("duration = %v, %v", got.Value, err)
	}
	got, err = handler(t, "currentKeyPressed")(ctx)
	if err != nil || got.Value != "numpad: 3" {
		t.Fatalf("mapped current key = %v, %v", got.Value, err)
	}
	// Repeated reporter reads must not reset timestamps or consume the hit state.
	host.now = 4.05
	ctx.Args["key"] = "numpad: 3"
	got, err = handler(t, "timeKeyPressed")(ctx)
	if err != nil || math.Abs(got.Value.(float64)-3.05) > 1e-10 {
		t.Fatalf("duration after repeated reads = %v, %v", got.Value, err)
	}
}
func TestKeyReporterMetadata(t *testing.T) {
	wanted := map[string]string{"isKeysPressed": "boolean", "isKeysHit": "boolean", "isKeyHit": "boolean", "lastKeyPressed": "reporter", "currentKeysPressed": "reporter", "currentKeyPressed": "reporter", "timeKeysPressed": "reporter", "timeKeyPressed": "reporter", "isKeyPressed": "boolean"}
	for _, block := range New().Blocks {
		op := block.Opcode[len("enderKeysPlusV2_"):]
		if kind, ok := wanted[op]; ok {
			if block.Kind != kind || block.Text == "" {
				t.Errorf("%s has kind %q text %q", op, block.Kind, block.Text)
			}
			delete(wanted, op)
		}
	}
	if len(wanted) > 0 {
		t.Fatalf("missing reporters: %v", wanted)
	}
}
