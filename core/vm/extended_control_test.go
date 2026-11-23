package vm

import (
	"testing"

	"gowarp/core/project"
)

func TestForEachReevaluatesBoundAndBreaks(t *testing.T) {
	r := eventProject(t, `[{"name":"Stage","isStage":true,"variables":{"i":["i",0],"limit":["limit",5],"sum":["sum",0]},"blocks":{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"for"},
 "for":{"opcode":"control_for_each","next":"after","fields":{"VARIABLE":["i","i"]},"inputs":{"VALUE":[1,[12,"limit","limit"]],"SUBSTACK":[2,"add"]}},
 "add":{"opcode":"data_changevariableby","next":"shrink","fields":{"VARIABLE":["sum","sum"]},"inputs":{"VALUE":[1,[12,"i","i"]]}},
 "shrink":{"opcode":"data_setvariableto","fields":{"VARIABLE":["limit","limit"]},"inputs":{"VALUE":[1,[4,"2"]]}},
 "after":{"opcode":"control_repeat","next":"count","inputs":{"TIMES":[1,[4,"10"]],"SUBSTACK":[2,"inc"]}},
 "inc":{"opcode":"control_incr_counter","next":"break"},"break":{"opcode":"control_break"},
 "count":{"opcode":"data_changevariableby","fields":{"VARIABLE":["sum","sum"]},"inputs":{"VALUE":[2,"counter"]}},"counter":{"opcode":"control_get_counter"}
 }}]`)
	for _, turbo := range []bool{false, true} {
		// New preserves the source; each execution starts independently.
		sources := []*project.Target{}
		for _, target := range r.targets {
			sources = append(sources, target.source)
		}
		copy := New(&project.Project{Targets: sources})
		run(t, copy, 20, turbo)
		if !copy.Done() || number(copy.Snapshot()[0].Variables["sum"].Value) != 4 || number(copy.Snapshot()[0].Variables["i"].Value) != 2 {
			t.Fatalf("for/break result: %+v", copy.Snapshot())
		}
	}
}

func TestSwitchFallthroughDefaultAndCaseSensitivity(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  float64
	}{{"a", 1}, {"A", 100}, {"alias", 10}, {"b", 10}} {
		r := fixture(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"switch"},
 "switch":{"opcode":"control_switch","inputs":{"VALUE":[1,[10,"`+tc.value+`"]],"SUBSTACK":[2,"a"]}},
 "a":{"opcode":"control_case","next":"alias","inputs":{"VALUE":[1,[10,"a"]],"SUBSTACK":[2,"one"]}},
 "one":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
 "alias":{"opcode":"control_case_fallthrough","next":"b","inputs":{"VALUE":[1,[10,"alias"]]}},
 "b":{"opcode":"control_case","next":"default","inputs":{"VALUE":[1,[10,"b"]],"SUBSTACK":[2,"ten"]}},
 "ten":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"10"]]}},
 "default":{"opcode":"control_default","inputs":{"SUBSTACK":[2,"hundred"]}},
 "hundred":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"100"]]}}
 }`)
		run(t, r, 10, false)
		if number(r.Snapshot()[0].Variables["score"].Value) != tc.want {
			t.Fatalf("%q switch: %+v", tc.value, r.Snapshot())
		}
	}
}

func TestBreakFromConditionalLoop(t *testing.T) {
	r := fixture(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},"loop":{"opcode":"control_repeat_until","next":"set","inputs":{"CONDITION":[1,[10,"false"]],"SUBSTACK":[2,"break"]}},"break":{"opcode":"control_break"},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"42"]]}}}`)
	run(t, r, 2, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 42 {
		t.Fatal("break reentered conditional loop")
	}
}
