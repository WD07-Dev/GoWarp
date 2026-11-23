package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"testing"
)

type keysHost struct{ keys []string }

func (h *keysHost) Call(op string, args ...any) (any, error) {
	if op == "input.keys" {
		return append([]string{}, h.keys...), nil
	}
	return nil, fmt.Errorf("unexpected %s", op)
}
func handler(t *testing.T, op string) e.Handler {
	t.Helper()
	for _, b := range New().Blocks {
		if b.Opcode == "enderKeysPlusV2_"+op {
			return b.Handler
		}
	}
	t.Fatal(op)
	return nil
}
func TestKeysKeepCaseAndMapModifierNames(t *testing.T) {
	h := &keysHost{[]string{"X", "shiftleft", "controlright", "f2"}}
	c := &e.Context{Host: h, Args: map[string]any{}}
	run := handler(t, "isKeyPressed")
	for key, want := range map[string]bool{"X": true, "x": false, "left shift": true, "right shift": false, "right control": true, "F2": true, "f2": false, "#x": true, "#shift": true, "#numbers": false, "any": true} {
		c.Args["key"] = key
		r, err := run(c)
		if err != nil || r.Value != want {
			t.Fatalf("%s=%v, %v", key, r.Value, err)
		}
	}
	h.keys = nil
	c.Args["key"] = "any"
	r, err := run(c)
	if err != nil || r.Value != false {
		t.Fatal(r, err)
	}
}
func TestKeyChordsAndTriggerTransitions(t *testing.T) {
	for _, trigger := range []string{"when", "while", "after"} {
		t.Run(trigger, func(t *testing.T) {
			h := &keysHost{}
			c := &e.Context{Host: h, Local: map[string]any{}, Args: map[string]any{"keys": `"1","2","3"`}, Fields: map[string]string{"trigger": trigger, "ordered": "together & ignore order"}}
			run := handler(t, "eventKeysPressed")
			states := [][]string{nil, {"1"}, {"1", "3", "2"}, {"1", "3", "2"}, {"1", "3", "2", "x"}, {"1", "3", "x"}, nil}
			wanted := map[string][]bool{"when": {false, false, true, false, true, false, false}, "while": {false, false, true, true, true, false, false}, "after": {false, false, false, false, false, true, false}}
			for i, keys := range states {
				h.keys = keys
				r, err := run(c)
				if err != nil || r.Value != wanted[trigger][i] {
					t.Fatalf("step %d: %v, %v", i, r.Value, err)
				}
			}
		})
	}
	if keysPressed([]string{"2", "1"}, parseKeys(`"1","2"`), true) {
		t.Fatal("unordered chord matched ordered mode")
	}
	if !keysPressed([]string{"x", "1", "2"}, parseKeys(`["1","2"]`), true) {
		t.Fatal("unrelated key broke ordered mode")
	}
	if keysPressed([]string{"1"}, parseKeys(`not json`), false) {
		t.Fatal("invalid key JSON matched")
	}
}
