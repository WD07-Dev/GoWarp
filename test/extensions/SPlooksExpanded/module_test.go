package extension

import (
	e "gowarp/core/extension"
	"reflect"
	"testing"
)

type tintHost struct {
	color  [3]float64
	target string
	calls  int
}

func (h *tintHost) Call(op string, args ...any) (any, error) {
	if op == "target.list" {
		return []any{map[string]any{"id": "original", "name": "Sprite", "cloneId": 0, "isStage": false}}, nil
	}
	h.calls++
	h.color = args[0].([3]float64)
	h.target = args[1].(string)
	return nil, nil
}
func TestTintParsingAndTargetSelection(t *testing.T) {
	for _, tt := range []struct {
		color, target string
		want          [3]float64
		ref           string
	}{
		{"#ffffff", "_myself_", [3]float64{1, 1, 1}, "_myself_"},
		{"#676767", "_stage_", [3]float64{103.0 / 255, 103.0 / 255, 103.0 / 255}, "_stage_"},
		{"00ff0080", "Sprite", [3]float64{0, 1, 0}, "original"},
	} {
		host := &tintHost{}
		_, err := tintSprite(&e.Context{Args: map[string]any{"COLOR": tt.color, "TARGET": tt.target}, Host: host})
		if err != nil || !reflect.DeepEqual(host.color, tt.want) || host.target != tt.ref {
			t.Errorf("%+v: got %+v, %v", tt, host, err)
		}
	}
	host := &tintHost{}
	_, err := tintSprite(&e.Context{Args: map[string]any{"COLOR": "#fff", "TARGET": "missing"}, Host: host})
	if err != nil || host.calls != 0 {
		t.Fatal("missing target was changed", host, err)
	}
	result, err := New().Blocks[1].Handler(&e.Context{Fields: map[string]string{"TARGETS": "_myself_"}})
	if err != nil || result.Value != "_myself_" {
		t.Fatal("target menu", result, err)
	}
}
