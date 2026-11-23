package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"testing"
)

type timerHost struct{ now float64 }

func (h *timerHost) Call(op string, args ...any) (any, error) {
	if op != "runtime.monotonicTime" {
		return nil, fmt.Errorf("unexpected host call %s", op)
	}
	return h.now, nil
}

func TestResumeTimerRetainsPausedValue(t *testing.T) {
	host := &timerHost{}
	context := &e.Context{Args: map[string]any{"TIMER": "music"}, State: map[string]any{}, Host: host}
	run := func(op string) any {
		t.Helper()
		for _, block := range New().Blocks {
			if block.Opcode == "lmsTimers_"+op {
				result, err := block.Handler(context)
				if err != nil {
					t.Fatal(err)
				}
				return result.Value
			}
		}
		t.Fatalf("missing %s", op)
		return nil
	}
	run("startResetTimer")
	host.now = 2
	run("pauseTimer")
	host.now = 20
	run("resumeTimer")
	host.now = 21
	if got := run("valueOfTimer"); got != 3.0 {
		t.Fatalf("resumed timer = %v; want 3", got)
	}
	run("resumeTimer")
	host.now = 22
	if got := run("valueOfTimer"); got != 4.0 {
		t.Fatalf("running timer reset by resume: %v", got)
	}
}
