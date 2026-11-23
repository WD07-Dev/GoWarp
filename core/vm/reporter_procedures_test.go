package vm

import (
	"context"
	"strings"
	"testing"
)

const reporterProcedureProject = `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},
 "set":{"opcode":"data_setvariableto","next":"after","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"call"]}},
 "call":{"opcode":"procedures_call","mutation":{"proccode":"square %s","argumentids":"[\"n\"]"},"inputs":{"n":[1,[4,"7"]]}},
 "after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
 "def":{"opcode":"procedures_definition","topLevel":true,"next":"return","inputs":{"custom_block":[1,"proto"]}},
 "proto":{"opcode":"procedures_prototype","mutation":{"proccode":"square %s","argumentids":"[\"n\"]","argumentnames":"[\"n\"]","argumentdefaults":"[0]","warp":"false"}},
 "return":{"opcode":"procedures_return","inputs":{"VALUE":[2,"multiply"]}},
 "multiply":{"opcode":"operator_multiply","inputs":{"NUM1":[2,"arg"],"NUM2":[2,"arg"]}},
 "arg":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["n",null]}}
}`

func TestReporterProcedureReturnsAndRestoresCaller(t *testing.T) {
	r := fixture(t, reporterProcedureProject)
	run(t, r, 1, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 50 {
		t.Fatalf("return/caller lost: %+v", r.Snapshot())
	}
}

func TestMissingReporterProcedureReturnsEmpty(t *testing.T) {
	blocks := strings.Replace(reporterProcedureProject, `"proccode":"square %s"`, `"proccode":"missing"`, 1)
	r := fixture(t, blocks)
	r.targets[0].source.Blocks["set"].Next = ""
	run(t, r, 1, false)
	if !r.Done() || r.Snapshot()[0].Variables["score"].Value != "" {
		t.Fatal("missing reporter did not return an empty string")
	}
}

func TestMissingReporterInsideProcedurePreservesCaller(t *testing.T) {
	blocks := strings.Replace(reporterProcedureProject,
		`"multiply":{"opcode":"operator_multiply","inputs":{"NUM1":[2,"arg"],"NUM2":[2,"arg"]}}`,
		`"multiply":{"opcode":"procedures_call","mutation":{"proccode":"missing"}}`, 1)
	r := fixture(t, blocks)
	run(t, r, 1, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 1 {
		t.Fatal("missing nested reporter lost its caller's return or continuation")
	}
}

func TestRepeatedReporterCallsKeepMetadataBounded(t *testing.T) {
	blocks := strings.Replace(reporterProcedureProject, `"next":"set"`, `"next":"repeat"`, 1)
	blocks = strings.Replace(blocks, `"set":`, `"repeat":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"100"]],"SUBSTACK":[2,"set"]}},"set":`, 1)
	r := fixture(t, blocks)
	run(t, r, 101, false)
	if !r.Done() || number(r.stage.variables["score"].Value) != 50 {
		t.Fatal("repeated reporter did not return to its caller")
	}
	if len(r.procedureCodeCache) > 1 || len(r.procedureArgumentCache) > 1 {
		t.Fatalf("one call site retained %d code entries and %d argument entries",
			len(r.procedureCodeCache), len(r.procedureArgumentCache))
	}
}

func TestReporterProcedureInstructionLimit(t *testing.T) {
	blocks := strings.Replace(reporterProcedureProject, `"return":{"opcode":"procedures_return","inputs":{"VALUE":[2,"multiply"]}}`, `"return":{"opcode":"control_forever"}`, 1)
	r := fixture(t, blocks)
	err := r.Run(context.Background(), 1, 30, false)
	if err == nil || !strings.Contains(err.Error(), "reporter procedure instruction limit") {
		t.Fatalf("unbounded reporter: %v", err)
	}
}

func TestReporterProcedureWaitResumesCaller(t *testing.T) {
	blocks := strings.Replace(reporterProcedureProject, `"next":"return","inputs":{"custom_block"`, `"next":"wait","inputs":{"custom_block"`, 1)
	blocks = strings.Replace(blocks, `"return":`, `"wait":{"opcode":"control_wait","next":"return","inputs":{"DURATION":[1,[4,"1"]]}},"return":`, 1)
	r := fixture(t, blocks)
	run(t, r, 30, false)
	if r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 0 {
		t.Fatal("reporter wait completed early")
	}
	run(t, r, 1, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 50 {
		t.Fatalf("reporter wait lost caller: %+v", r.Snapshot())
	}
}

func TestReporterSuspensionKeepsCompletedOperandsAndArguments(t *testing.T) {
	blocks := `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},
 "set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"sum"]}},
 "sum":{"opcode":"operator_add","inputs":{"NUM1":[2,"increment"],"NUM2":[2,"outer"]}},
 "increment":{"opcode":"procedures_call","mutation":{"proccode":"increment"}},
 "incDef":{"opcode":"procedures_definition","topLevel":true,"next":"inc","inputs":{"custom_block":[1,"incProto"]}},
 "incProto":{"opcode":"procedures_prototype","mutation":{"proccode":"increment","argumentids":"[]","argumentnames":"[]","argumentdefaults":"[]","warp":"false"}},
 "inc":{"opcode":"data_changevariableby","next":"incReturn","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
 "incReturn":{"opcode":"procedures_return","inputs":{"VALUE":[3,[12,"score","score"],[4,"0"]]}},
 "outer":{"opcode":"procedures_call","mutation":{"proccode":"delayed %s %s"},"inputs":{"a":[2,"increment"],"b":[2,"inner"]}},
 "outerDef":{"opcode":"procedures_definition","topLevel":true,"next":"outerWait","inputs":{"custom_block":[1,"outerProto"]}},
 "outerProto":{"opcode":"procedures_prototype","mutation":{"proccode":"delayed %s %s","argumentids":"[\"a\",\"b\"]","argumentnames":"[\"a\",\"b\"]","argumentdefaults":"[0,0]","warp":"false"}},
 "outerWait":{"opcode":"control_wait","next":"outerReturn","inputs":{"DURATION":[1,[4,"0"]]}},
 "outerReturn":{"opcode":"procedures_return","inputs":{"VALUE":[2,"product"]}},
 "product":{"opcode":"operator_multiply","inputs":{"NUM1":[2,"argA"],"NUM2":[2,"argB"]}},
 "argA":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["a",null]}},
 "argB":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["b",null]}},
 "inner":{"opcode":"procedures_call","mutation":{"proccode":"inner"}},
 "innerDef":{"opcode":"procedures_definition","topLevel":true,"next":"innerWait","inputs":{"custom_block":[1,"innerProto"]}},
 "innerProto":{"opcode":"procedures_prototype","mutation":{"proccode":"inner","argumentids":"[]","argumentnames":"[]","argumentdefaults":"[]","warp":"false"}},
 "innerWait":{"opcode":"control_wait","next":"innerReturn","inputs":{"DURATION":[1,[4,"0"]]}},
 "innerReturn":{"opcode":"procedures_return","inputs":{"VALUE":[1,[4,"10"]]}}
 }`
	r := fixture(t, blocks)
	run(t, r, 1, false)
	if got := number(r.Snapshot()[0].Variables["score"].Value); got != 2 {
		t.Fatalf("argument side effects: %v", got)
	}
	run(t, r, 1, false)
	if got := number(r.Snapshot()[0].Variables["score"].Value); got != 2 {
		t.Fatalf("replayed argument side effects: %v", got)
	}
	run(t, r, 1, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 21 {
		t.Fatalf("nested expression lost: %+v", r.Snapshot())
	}
}

func TestReporterProcedureWaitKinds(t *testing.T) {
	for _, tc := range []struct {
		name, wait, extra string
		frames            int
	}{
		{
			"broadcast",
			`"wait":{"opcode":"event_broadcastandwait","next":"return","inputs":{"BROADCAST_INPUT":[1,[11,"go","go"]]}}`,
			`,"receive":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"receiverWait","fields":{"BROADCAST_OPTION":["go","go"]}},"receiverWait":{"opcode":"control_wait","inputs":{"DURATION":[1,[4,"0"]]}}`,
			3,
		},
		{
			"ask",
			`"wait":{"opcode":"sensing_askandwait","next":"return","inputs":{"QUESTION":[1,[10,"Ready?"]]}}`,
			"",
			2,
		},
		{
			"sound",
			`"wait":{"opcode":"sound_playuntildone","next":"return","inputs":{"SOUND_MENU":[1,[10,"beep"]]}}`,
			"",
			5,
		},
		{
			"glide",
			`"wait":{"opcode":"motion_glidesecstoxy","next":"return","inputs":{"SECS":[1,[4,"0.1"]],"X":[1,[4,"30"]],"Y":[1,[4,"20"]]}}`,
			"",
			5,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blocks := strings.Replace(reporterProcedureProject, `"next":"return","inputs":{"custom_block"`, `"next":"wait","inputs":{"custom_block"`, 1)
			blocks = strings.Replace(blocks, `"return":`, tc.wait+tc.extra+`,"return":`, 1)
			r := soundRuntime(t, blocks)
			run(t, r, 1, false)
			if got := number(r.Snapshot()[0].Variables["score"].Value); got != 0 {
				t.Fatalf("completed before suspension: %v", got)
			}
			if tc.name == "ask" {
				if q := r.Question(); q == nil || q.Text != "Ready?" {
					t.Fatalf("missing reporter question: %+v", q)
				}
				if err := r.QueueEvents([]InputEvent{{
					Frame:  1,
					Kind:   "answer",
					Answer: "yes",
				}}); err != nil {
					t.Fatal(err)
				}
			}
			run(t, r, tc.frames, false)
			if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 50 {
				t.Fatalf("wait lost continuation: %+v", r.Snapshot())
			}
		})
	}
}

func TestReporterInEdgeHatThresholdCanSuspend(t *testing.T) {
	blocks := strings.Replace(reporterProcedureProject, `"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"}`, `"flag":{"opcode":"event_whengreaterthan","topLevel":true,"next":"after","fields":{"WHENGREATERTHANMENU":["loudness",null]},"inputs":{"VALUE":[2,"call"]}}`, 1)
	blocks = strings.Replace(blocks, `"next":"return","inputs":{"custom_block"`, `"next":"wait","inputs":{"custom_block"`, 1)
	blocks = strings.Replace(blocks, `"return":`, `"wait":{"opcode":"control_wait","next":"return","inputs":{"DURATION":[1,[4,"0"]]}},"return":`, 1)
	r := fixture(t, blocks)
	if err := r.QueueEvents([]InputEvent{{
		Frame: 0,
		Kind:  "loudness",
		Value: 100,
	}}); err != nil {
		t.Fatal(err)
	}
	run(t, r, 1, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 0 {
		t.Fatal("edge threshold finished early")
	}
	run(t, r, 1, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 1 {
		t.Fatal("edge threshold did not resume")
	}
	run(t, r, 4, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 1 {
		t.Fatal("edge threshold lost rising-edge state")
	}
}

func TestReporterNestedReturnWaitAndCallerWarpRestore(t *testing.T) {
	blocks := strings.Replace(reporterProcedureProject, `"VALUE":[2,"multiply"]`, `"VALUE":[2,"inner"]`, 1)
	blocks = strings.TrimSpace(blocks)
	blocks = strings.TrimSuffix(blocks, "}") + `,
 "inner":{"opcode":"procedures_call","mutation":{"proccode":"inner"}},
 "innerDef":{"opcode":"procedures_definition","topLevel":true,"next":"innerWait","inputs":{"custom_block":[1,"innerProto"]}},
 "innerProto":{"opcode":"procedures_prototype","mutation":{"proccode":"inner","argumentids":"[]","argumentnames":"[]","argumentdefaults":"[]","warp":"false"}},
 "innerWait":{"opcode":"control_wait","next":"innerReturn","inputs":{"DURATION":[1,[4,"0"]]}},
 "innerReturn":{"opcode":"procedures_return","inputs":{"VALUE":[1,[4,"9"]]}}
 }`
	blocks = strings.Replace(blocks, `"after":{"opcode":"data_changevariableby"`, `"after":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"2"]],"SUBSTACK":[2,"increment"]}},"increment":{"opcode":"data_changevariableby"`, 1)
	r := fixture(t, blocks)
	run(t, r, 1, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 0 {
		t.Fatal("nested reporter wait skipped")
	}
	stepOnce(t, r)
	if number(r.Snapshot()[0].Variables["score"].Value) != 10 {
		t.Fatal("nested return or caller warp lost")
	}
	run(t, r, 2, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 11 {
		t.Fatal("caller did not finish")
	}
}

func TestReporterStopAndFallthroughReturnEmpty(t *testing.T) {
	for _, body := range []string{
		`"return":{"opcode":"control_stop","fields":{"STOP_OPTION":["this script",null]}}`,
		`"return":{"opcode":"control_if","inputs":{"CONDITION":[1,[4,"0"]]}}`,
	} {
		blocks := strings.Replace(reporterProcedureProject, `"return":{"opcode":"procedures_return","inputs":{"VALUE":[2,"multiply"]}}`, body, 1)
		r := fixture(t, blocks)
		run(t, r, 1, false)
		if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 1 {
			t.Fatalf("empty reporter return failed: %+v", r.Snapshot())
		}
	}
}

func TestReporterInputCacheDoesNotEscapeStatement(t *testing.T) {
	blocks := strings.Replace(reporterProcedureProject, `"next":"set"`, `"next":"repeat"`, 1)
	blocks = strings.Replace(blocks, `"set":`, `"repeat":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"2"]],"SUBSTACK":[2,"set"]}},"set":`, 1)
	blocks = strings.Replace(blocks, `"n":[1,[4,"7"]]`, `"n":[3,[12,"score","score"],[4,"0"]]`, 1)
	r := fixture(t, blocks)
	run(t, r, 5, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 2 {
		t.Fatalf("input cache escaped statement: %+v", r.Snapshot())
	}
}

func TestReporterCallsShareBoundedSchedulerSlice(t *testing.T) {
	blocks := strings.Replace(reporterProcedureProject, `"next":"set"`, `"next":"loop"`, 1)
	blocks = strings.Replace(blocks, `"set":`, `"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"set"]}},"set":`, 1)
	blocks = strings.Replace(blocks, `"next":"return","inputs":{"custom_block"`, `"next":"work","inputs":{"custom_block"`, 1)
	blocks = strings.Replace(blocks, `"return":`, `"work":{"opcode":"control_repeat","next":"return","inputs":{"TIMES":[1,[4,"100"]],"SUBSTACK":[2,"count"]}},"count":{"opcode":"control_incr_counter"},"return":`, 1)
	r := fixture(t, blocks)
	run(t, r, 1, true)
	if r.counter < 100 || r.counter > 20000 {
		t.Fatalf("reporter calls monopolized scheduler: %v increments", r.counter)
	}
}

func TestSayForSecondsWaitsForReporterBeforeShowingBubble(t *testing.T) {
	blocks := strings.Replace(reporterProcedureProject, `"set":{"opcode":"data_setvariableto","next":"after","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"call"]}}`, `"set":{"opcode":"looks_sayforsecs","next":"after","inputs":{"MESSAGE":[1,[10,"Hello"]],"SECS":[2,"call"]}}`, 1)
	blocks = strings.Replace(blocks, `"next":"return","inputs":{"custom_block"`, `"next":"wait","inputs":{"custom_block"`, 1)
	blocks = strings.Replace(blocks, `"return":`, `"wait":{"opcode":"control_wait","next":"return","inputs":{"DURATION":[1,[4,"0"]]}},"return":`, 1)
	r := soundRuntime(t, blocks)
	run(t, r, 1, false)
	if r.Snapshot()[1].Bubble != "" {
		t.Fatal("bubble displayed before duration reporter completed")
	}
	run(t, r, 1, false)
	if r.Snapshot()[1].Bubble != "Hello" {
		t.Fatal("bubble not displayed after duration reporter returned")
	}
}
