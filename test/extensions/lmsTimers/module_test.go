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

func TestTimerCreationOrderingAndPredicate(t *testing.T) {
	h := &timerHost{}
	c := &e.Context{Host: h, State: map[string]any{}, Fields: map[string]string{"OP": ">"}}
	blocks := map[string]e.Block{}
	for _, b := range New().Blocks {
		blocks[b.Opcode] = b
	}
	run := func(op, name string, number float64) any {
		t.Helper()
		c.Args = map[string]any{"TIMER": name, "NUM": number}
		b, ok := blocks["lmsTimers_"+op]
		if !ok {
			t.Fatalf("missing %s", op)
		}
		r, err := b.Handler(c)
		if err != nil {
			t.Fatal(err)
		}
		return r.Value
	}
	if run("timerExists", "a", 0) != false {
		t.Fatal("nonexistent timer")
	}
	run("changeTimer", "a", 3)
	h.now = 2
	if run("valueOfTimer", "a", 0) != 5.0 {
		t.Fatal("change did not create running timer")
	}
	run("pauseTimer", "a", 0)
	run("changeTimer", "a", -2)
	h.now = 40
	if run("valueOfTimer", "a", 0) != 3.0 {
		t.Fatal("change resumed paused timer")
	}
	if run("whenTimerOp", "a", 2) != true || run("whenTimerOp", "a", 3) != false {
		t.Fatal("strict timer predicate")
	}
	for _, name := range []string{"b", "10", "2", "01"} {
		run("startResetTimer", name, 0)
	}
	if got := run("listExistingTimers", "", 0); got != "2,10,a,b,01" {
		t.Fatal(got)
	}
	run("removeTimer", "a", 0)
	run("setTimer", "a", 8)
	if got := run("listExistingTimers", "", 0); got != "2,10,b,01,a" {
		t.Fatal(got)
	}
	if blocks["lmsTimers_timerExists"].Kind != "boolean" || blocks["lmsTimers_listExistingTimers"].Text == "" {
		t.Fatal("monitor metadata")
	}
	run("removeTimers", "", 0)
	if run("listExistingTimers", "", 0) != "" {
		t.Fatal("clear")
	}
}
