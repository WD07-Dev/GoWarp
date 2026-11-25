package extension

import (
	e "gowarp/core/extension"
	"testing"
)

type urlHost struct {
	starts  int
	pending bool
}

func (h *urlHost) Call(op string, args ...any) (any, error) {
	if op == "audio.url" {
		h.starts++
		return "voice", nil
	}
	return h.pending, nil
}
func TestURLWaitUsesOnePlayback(t *testing.T) {
	h := &urlHost{pending: true}
	c := &e.Context{Host: h, Args: map[string]any{"path": "https://example.test/sound.wav"}, Local: map[string]any{}}
	var handler e.Handler
	for _, b := range New().Blocks {
		if b.Opcode == "notSound_playUntilDone" {
			handler = b.Handler
		}
	}
	if handler == nil {
		t.Fatal("missing until done block")
	}
	for range 2 {
		r, err := handler(c)
		if err != nil || !r.Yield || !r.Again {
			t.Fatalf("wait=%+v %v", r, err)
		}
	}
	h.pending = false
	r, err := handler(c)
	if err != nil || r.Again || h.starts != 1 {
		t.Fatalf("completion=%+v starts=%d err=%v", r, h.starts, err)
	}
}
