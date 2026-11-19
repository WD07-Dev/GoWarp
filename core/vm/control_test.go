package vm

import (
	"strings"
	"testing"
)

const untilProject = `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
 "loop":{"opcode":"control_repeat_until","next":"after","inputs":{"CONDITION":[2,"condition"],"SUBSTACK":[2,"add"]}},
 "condition":{"opcode":"operator_equals","inputs":{"OPERAND1":[3,[12,"score","score"],[4,"0"]],"OPERAND2":[1,[4,"3"]]}},
 "add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
 "after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"100"]]}}
}`

func TestConditionalLoops(t *testing.T) {
	for _, opcode := range []string{"control_repeat_until", "control_while"} {
		blocks := untilProject
		if opcode == "control_while" {
			blocks = strings.Replace(blocks, "control_repeat_until", opcode, 1)
			blocks = strings.Replace(blocks, "operator_equals", "operator_lt", 1)
		}
		r := fixture(t, blocks)
		stepOnce(t, r)
		if number(r.Snapshot()[0].Variables["score"].Value) != 1 {
			t.Fatal("loop did not yield")
		}
		run(t, r, 3, false)
		if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 103 {
			t.Fatalf("bad %s result", opcode)
		}
		r = fixture(t, blocks)
		run(t, r, 1, true)
		if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 103 {
			t.Fatal("turbo loop did not finish")
		}
	}
}

func TestWaitUntilAndEmptyLoop(t *testing.T) {
	for _, opcode := range []string{"control_wait_until", "control_repeat_until"} {
		blocks := `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait"},"wait":{"opcode":"` + opcode + `","next":"set","inputs":{"CONDITION":[2,"ready"]}},"ready":{"opcode":"operator_gt","inputs":{"OPERAND1":[2,"timer"],"OPERAND2":[1,[4,"0.05"]]}},"timer":{"opcode":"sensing_timer"},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"9"]]}}}`
		r := fixture(t, blocks)
		run(t, r, 2, true)
		if number(r.Snapshot()[0].Variables["score"].Value) != 0 {
			t.Fatal("completed early")
		}
		run(t, r, 1, true)
		if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 9 {
			t.Fatalf("%s did not resume", opcode)
		}
	}
}

func TestTimerReset(t *testing.T) {
	r := fixture(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait1"},
 "wait1":{"opcode":"control_wait","next":"reset","inputs":{"DURATION":[1,[4,"0.1"]]}},
 "reset":{"opcode":"sensing_resettimer","next":"wait2"},
 "wait2":{"opcode":"control_wait","next":"set","inputs":{"DURATION":[1,[4,"0.1"]]}},
 "set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"timer"]}},
 "timer":{"opcode":"sensing_timer"}}`)
	run(t, r, 7, true)
	if got := number(r.Snapshot()[0].Variables["score"].Value); got != 0.1 {
		t.Fatalf("timer=%v", got)
	}
}
