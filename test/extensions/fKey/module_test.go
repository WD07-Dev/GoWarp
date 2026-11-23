package extension

import (
	e "gowarp/core/extension"
	"testing"
)

type keyHost struct{ key string }

func (h *keyHost) Call(op string, a ...any) (any, error) {
	h.key = e.String(a[0])
	return h.key == "F2", nil
}
func TestFunctionKey(t *testing.T) {
	h := &keyHost{}
	for _, b := range New().Blocks {
		if b.Opcode == "fKey_isFKeyPressed" {
			r, err := b.Handler(&e.Context{Args: map[string]any{"KEY": "f2"}, Host: h})
			if err != nil || r.Value != true || h.key != "F2" {
				t.Fatalf("%#v %v", r, err)
			}
			return
		}
	}
	t.Fatal("missing key reporter")
}
