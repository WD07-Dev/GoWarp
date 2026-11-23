package vm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gowarp/core/project"
)

func BenchmarkRepeatedProcedureCall(b *testing.B) {
	var blocks map[string]*project.Block
	if err := json.Unmarshal([]byte(procedureProject), &blocks); err != nil {
		b.Fatal(err)
	}
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
	th := &thread{target: r.stage}
	call := blocks["call"]
	input := func(string) (any, error) {
		return float64(3), nil
	}
	if _, err := r.call(context.Background(), th, call, input, false); err != nil {
		b.Fatal(err)
	}
	th.stopScript()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.call(context.Background(), th, call, input, false); err != nil {
			b.Fatal(err)
		}
		th.stopScript()
	}
}

func TestFreshProcedureCallsReuseReleasedArguments(t *testing.T) {
	r := fixture(t, procedureProject)
	call := r.stage.source.Blocks["call"]
	ctx := context.Background()
	stack := make([]continuation, 0, 1)
	allocations := testing.AllocsPerRun(100, func() {
		th := &thread{target: r.stage, stack: stack}
		if _, err := r.call(ctx, th, call, nil, false); err != nil {
			t.Fatal(err)
		}
		if th.params["n"].boxed() != "3" {
			t.Fatalf("argument changed: %v", th.params)
		}
		th.stopScript()
	})
	if allocations > 1 {
		t.Fatalf("fresh script allocated %g objects; only its thread needs new storage", allocations)
	}
}

func TestArgumentReuseKeepsSuspendedCallsSeparate(t *testing.T) {
	blocks := strings.Replace(procedureProject, `"callN":[1,[4,"3"]]`, `"callN":[1,[12,"input","input"]]`, 1)
	r := fixture(t, blocks)
	call := r.stage.source.Blocks["call"]
	ctx := context.Background()
	enter := func(th *thread, value float64) {
		t.Helper()
		r.stage.variables["input"] = &Variable{Value: value}
		if _, err := r.call(ctx, th, call, nil, false); err != nil {
			t.Fatal(err)
		}
	}
	first, waiting := &thread{target: r.stage}, &thread{target: r.stage}
	enter(first, 2)
	enter(waiting, 5)
	returned := first.params
	first.stopScript()
	if len(returned) != 0 {
		t.Fatal("returned arguments still retain values")
	}
	third := &thread{target: r.stage}
	enter(third, 7)
	if waiting.params["n"].boxed() != float64(5) || third.params["n"].boxed() != float64(7) {
		t.Fatal("another script overwrote suspended arguments")
	}
	enter(third, 11)
	third.stopScript()
	if third.params["n"].boxed() != float64(7) {
		t.Fatal("nested return lost the caller's arguments")
	}
	third.stopScript()
	if waiting.params["n"].boxed() != float64(5) {
		t.Fatal("returning a nested call cleared another script's arguments")
	}
	waiting.stopScript()
}

func TestProcedureArgumentErrorAfterCloneDeletion(t *testing.T) {
	r := motionRuntime(t, procedureProject)
	source := r.targets[1]
	call := source.source.Blocks["call"]
	call.Inputs["callN"] = []any{float64(2), "external"}
	source.source.Blocks["external"] = &project.Block{Opcode: "custom_reporter"}
	r.createClone(source)
	clone := r.targets[len(r.targets)-1]
	th := &thread{target: clone}
	want := errors.New("argument failed")
	_, err := r.call(context.Background(), th, call, func(string) (any, error) {
		r.deleteClone(clone)
		return nil, want
	}, false)
	if !errors.Is(err, want) {
		t.Fatalf("deleted clone lost its argument error: %v", err)
	}
}

func BenchmarkFreshProcedureCall(b *testing.B) {
	var blocks project.Blocks
	if err := json.Unmarshal([]byte(procedureProject), &blocks); err != nil {
		b.Fatal(err)
	}
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
	call := blocks["call"]
	ctx := context.Background()
	stack := make([]continuation, 0, 1)
	b.ReportAllocs()
	for b.Loop() {
		th := &thread{target: r.stage, stack: stack}
		if _, err := r.call(ctx, th, call, nil, false); err != nil {
			b.Fatal(err)
		}
		th.stopScript()
	}
}

func TestRepeatedProcedureCallReadsCurrentCloneArguments(t *testing.T) {
	blocks := strings.Replace(procedureProject, `"callN":[1,[4,"3"]]`, `"callN":[1,[12,"input","input"]]`, 1)
	r := motionRuntime(t, blocks)
	original := r.targets[1]
	original.variables["input"] = &Variable{Name: "input", Value: float64(2)}
	// This clone exists before either target has prepared its procedures.
	r.createClone(original)
	clone := r.targets[len(r.targets)-1]
	clone.variables["input"] = &Variable{Name: "input", Value: float64(5)}
	for _, tc := range []struct {
		target *target
		value  float64
	}{{original, 2}, {clone, 5}, {original, 7}, {clone, 9}} {
		tc.target.variables["input"] = &Variable{Name: "input", Value: tc.value}
		th := &thread{target: tc.target}
		if _, err := r.call(context.Background(), th, tc.target.source.Blocks["call"], nil, false); err != nil {
			t.Fatal(err)
		}
		if got := th.params["n"].boxed(); got != tc.value {
			t.Fatalf("clone %d argument=%v, want %v", tc.target.state.CloneID, got, tc.value)
		}
		th.stopScript()
	}
}

const procedureProject = `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"call"},
 "call":{"opcode":"procedures_call","next":"after","mutation":{"proccode":"count %s","argumentids":"[\"callN\"]"},"inputs":{"callN":[1,[4,"3"]]}},
 "after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"100"]]}},
 "def":{"opcode":"procedures_definition","topLevel":true,"next":"repeat","inputs":{"custom_block":[1,"proto"]}},
 "proto":{"opcode":"procedures_prototype","mutation":{"proccode":"count %s","argumentids":"[\"nID\"]","argumentnames":"[\"n\"]","argumentdefaults":"[2]","warp":"false"}},
 "repeat":{"opcode":"control_repeat","inputs":{"TIMES":[2,"arg"],"SUBSTACK":[2,"add"]}},
 "arg":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["n",null]}},
 "add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"arg"]}}
}`

func TestProcedureArgumentsAndWarp(t *testing.T) {
	for _, tc := range []struct {
		passes int
		warp   bool
		want   float64
	}{{1, false, 3}, {4, false, 109}, {1, true, 109}} {
		blocks := procedureProject
		if tc.warp {
			blocks = strings.Replace(blocks, `"warp":"false"`, `"warp":"true"`, 1)
		}
		r := fixture(t, blocks)
		for pass := 0; pass < tc.passes; pass++ {
			stepOnce(t, r)
		}
		if got := number(r.Snapshot()[0].Variables["score"].Value); got != tc.want {
			t.Fatalf("warp=%v passes=%d: %v", tc.warp, tc.passes, got)
		}
	}
}

func TestInfiniteWarpYieldsAndCancels(t *testing.T) {
	blocks := strings.Replace(procedureProject, `"warp":"false"`, `"warp":"true"`, 1)
	blocks = strings.Replace(blocks, `"repeat":{"opcode":"control_repeat"`, `"repeat":{"opcode":"control_forever"`, 1)
	// Both a working body and an empty loop must respect the safety timeout.
	for _, empty := range []bool{false, true} {
		body := blocks
		if empty {
			body = strings.Replace(body, `"SUBSTACK":[2,"add"]`, `"SUBSTACK":[2,null]`, 1)
		}
		r := fixture(t, body)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := r.Run(ctx, 1, 30, false)
		cancel()
		if err != nil || r.Frames() != 1 || r.Done() {
			t.Fatalf("infinite warp did not yield (empty=%v): %v", empty, err)
		}
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
		err = r.Run(ctx, 1, 30, false)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("infinite warp did not cancel (empty=%v): %v", empty, err)
		}
	}
}

func TestLongWarpCallerStillWaits(t *testing.T) {
	blocks := strings.Replace(procedureProject, `"warp":"false"`, `"warp":"true"`, 1)
	blocks = strings.Replace(blocks, `"callN":[1,[4,"3"]]`, `"callN":[1,[4,"6000"]]`, 1)
	blocks = strings.Replace(blocks, `"call":{"opcode":"procedures_call","next":"after"`, `"call":{"opcode":"procedures_call","next":"wait"`, 1)
	blocks = strings.Replace(blocks, `"after":`, `"wait":{"opcode":"control_wait","next":"after","inputs":{"DURATION":[1,[4,"0"]]}},"after":`, 1)
	r := fixture(t, blocks)
	run(t, r, 1, false)
	if got := number(r.stage.variables["score"].Value); got != 36000000 || r.Done() {
		t.Fatalf("extended drawing slice skipped the explicit wait: score=%g", got)
	}
	run(t, r, 1, false)
	if got := number(r.stage.variables["score"].Value); got != 36000100 || !r.Done() {
		t.Fatalf("caller did not resume after the wait: score=%g", got)
	}
}

func TestProcedureDefaultArgument(t *testing.T) {
	blocks := strings.Replace(procedureProject, `"callN":[1,[4,"3"]]`, "", 1)
	r := fixture(t, blocks)
	run(t, r, 10, false)
	if got := number(r.Snapshot()[0].Variables["score"].Value); got != 104 {
		t.Fatalf("got %v", got)
	}
}

const recursiveProject = `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"call"},
 "call":{"opcode":"procedures_call","mutation":{"proccode":"sum %s","argumentids":"[\"nID\"]"},"inputs":{"nID":[1,[4,"4"]]}},
 "def":{"opcode":"procedures_definition","topLevel":true,"next":"if","inputs":{"custom_block":[1,"proto"]}},
 "proto":{"opcode":"procedures_prototype","mutation":{"proccode":"sum %s","argumentids":"[\"nID\"]","argumentnames":"[\"n\"]","argumentdefaults":"[0]","warp":"false"}},
 "if":{"opcode":"control_if","inputs":{"CONDITION":[2,"positive"],"SUBSTACK":[2,"recur"]}},
 "positive":{"opcode":"operator_gt","inputs":{"OPERAND1":[2,"arg"],"OPERAND2":[1,[4,"0"]]}},
 "arg":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["n",null]}},
 "recur":{"opcode":"procedures_call","next":"add","mutation":{"proccode":"sum %s","argumentids":"[\"nID\"]"},"inputs":{"nID":[2,"minus"]}},
 "minus":{"opcode":"operator_subtract","inputs":{"NUM1":[2,"arg"],"NUM2":[1,[4,"1"]]}},
 "add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"arg"]}}
}`

func TestRecursiveArgumentsRestore(t *testing.T) {
	for _, warp := range []bool{false, true} {
		blocks := recursiveProject
		if warp {
			blocks = strings.Replace(blocks, `"warp":"false"`, `"warp":true`, 1)
		}
		r := fixture(t, blocks)
		stepOnce(t, r)
		if !warp && number(r.Snapshot()[0].Variables["score"].Value) != 0 {
			t.Fatal("recursive normal call did not yield")
		}
		run(t, r, 20, false)
		if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 10 {
			t.Fatalf("recursive result: %+v", r.Snapshot())
		}
	}
}

func TestWarpRestoresAfterReturnAndWaits(t *testing.T) {
	blocks := strings.Replace(procedureProject, `"warp":"false"`, `"warp":"true"`, 1)
	// Caller repeat still yields after returning from a warp procedure.
	blocks = strings.Replace(blocks, `"after":{"opcode":"data_changevariableby"`, `"after":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"2"]],"SUBSTACK":[2,"inc"]}},"inc":{"opcode":"data_changevariableby"`, 1)
	r := fixture(t, blocks)
	stepOnce(t, r)
	if got := number(r.Snapshot()[0].Variables["score"].Value); got != 109 {
		t.Fatalf("warp leaked: %v", got)
	}
	run(t, r, 2, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 209 {
		t.Fatal("caller failed to resume")
	}
	blocks = strings.Replace(procedureProject, `"warp":"false"`, `"warp":"true"`, 1)
	blocks = strings.Replace(blocks, `"next":"repeat"`, `"next":"wait"`, 1)
	blocks = strings.Replace(blocks, `"repeat":`, `"wait":{"opcode":"control_wait","next":"repeat","inputs":{"DURATION":[1,[4,"0.1"]]}},"repeat":`, 1)
	r = fixture(t, blocks)
	run(t, r, 3, true)
	if number(r.Snapshot()[0].Variables["score"].Value) != 0 {
		t.Fatal("warp wait skipped")
	}
	run(t, r, 1, true)
	if number(r.Snapshot()[0].Variables["score"].Value) != 109 {
		t.Fatal("warp wait lost continuation")
	}
}

func TestMalformedProcedures(t *testing.T) {
	for _, blocks := range []string{
		strings.Replace(procedureProject, `"argumentnames":"[\"n\"]"`, `"argumentnames":"broken"`, 1),
		strings.Replace(procedureProject, `"argumentdefaults":"[2]"`, `"argumentdefaults":"[{}]"`, 1),
		strings.Replace(procedureProject, `"argumentnames":"[\"n\"]"`, `"argumentnames":"[]"`, 1),
		strings.Replace(procedureProject, `"warp":"false"`, `"warp":"garbage"`, 1),
		strings.Replace(procedureProject, `"custom_block":[1,"proto"]`, `"custom_block":[1,"missing"]`, 1),
	} {
		r := fixture(t, blocks)
		if err := r.Run(context.Background(), 10, 30, true); err == nil {
			t.Fatal("malformed procedure accepted")
		}
	}
}

func TestMissingProcedureContinuesCaller(t *testing.T) {
	blocks := strings.Replace(procedureProject, `"proccode":"count %s"`, `"proccode":"missing"`, 1)
	r := fixture(t, blocks)
	run(t, r, 1, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 100 {
		t.Fatal("missing procedure did not continue at the following block")
	}
}

func TestRecursiveCallDepthIsBounded(t *testing.T) {
	blocks := strings.Replace(recursiveProject, `"next":"if"`, `"next":"recur"`, 1)
	r := fixture(t, blocks)
	if err := r.Run(context.Background(), 1, 30, true); err == nil || !strings.Contains(err.Error(), "nesting limit") {
		t.Fatalf("expected bounded recursion: %v", err)
	}
}

func TestProcedureArgumentsAreThreadLocal(t *testing.T) {
	blocks := strings.Replace(procedureProject, `"flag":`, `"secondFlag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"secondCall"},"secondCall":{"opcode":"procedures_call","mutation":{"proccode":"count %s","argumentids":"[\"callN\"]"},"inputs":{"callN":[1,[4,"2"]]}},"flag":`, 1)
	r := fixture(t, blocks)
	run(t, r, 10, false)
	if got := number(r.Snapshot()[0].Variables["score"].Value); got != 113 {
		t.Fatalf("threads shared arguments: %v", got)
	}
}

func TestBooleanArgumentAndStopScript(t *testing.T) {
	blocks := `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"call"},
 "call":{"opcode":"procedures_call","next":"after","mutation":{"proccode":"check %b","argumentids":"[\"b\"]"},"inputs":{"b":[2,"eq"]}},
 "eq":{"opcode":"operator_equals","inputs":{"OPERAND1":[1,[4,"1"]],"OPERAND2":[1,[4,"1"]]}},
 "def":{"opcode":"procedures_definition","topLevel":true,"next":"branch","inputs":{"custom_block":[1,"proto"]}},
 "proto":{"opcode":"procedures_prototype","mutation":{"proccode":"check %b","argumentids":"[\"b\"]","argumentnames":"[\"ok\"]","argumentdefaults":"[false]","warp":"false"}},
 "branch":{"opcode":"control_if","inputs":{"CONDITION":[2,"arg"],"SUBSTACK":[2,"stop"]}},
 "arg":{"opcode":"argument_reporter_boolean","fields":{"VALUE":["ok",null]}},
 "stop":{"opcode":"control_stop","fields":{"STOP_OPTION":["this script",null]}},
 "after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"100"]]}}
 }`
	r := fixture(t, blocks)
	run(t, r, 10, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 100 {
		t.Fatal("stop this script did not return to caller")
	}
	blocks = strings.Replace(blocks, `"b":[2,"eq"]`, "", 1)
	r = fixture(t, blocks)
	run(t, r, 10, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 100 {
		t.Fatal("false boolean default did not return")
	}
}

func TestStopInNestedProcedureRestoresCaller(t *testing.T) {
	blocks := strings.Replace(procedureProject, `"add":{"opcode":"data_changevariableby"`, `"add":{"opcode":"procedures_call","next":"addValue","mutation":{"proccode":"early %s","argumentids":"[\"innerID\"]"},"inputs":{"innerID":[1,[4,"99"]]}},"addValue":{"opcode":"data_changevariableby"`, 1)
	blocks = strings.Replace(blocks, `"flag":`, `"innerDef":{"opcode":"procedures_definition","topLevel":true,"next":"innerLoop","inputs":{"custom_block":[1,"innerProto"]}},"innerProto":{"opcode":"procedures_prototype","mutation":{"proccode":"early %s","argumentids":"[\"innerID\"]","argumentnames":"[\"n\"]","argumentdefaults":"[0]","warp":"true"}},"innerLoop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"innerStop"]}},"innerStop":{"opcode":"control_stop","fields":{"STOP_OPTION":["this script",null]}},"flag":`, 1)
	r := fixture(t, blocks)
	stepOnce(t, r)
	if number(r.Snapshot()[0].Variables["score"].Value) != 3 {
		t.Fatal("nested return did not restore argument or normal loop yield")
	}
	run(t, r, 3, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 109 {
		t.Fatal("nested return failed to continue outer function")
	}
}

func TestStopOutsideProcedureEndsThread(t *testing.T) {
	r := fixture(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"stop"},"stop":{"opcode":"control_stop","next":"bad","fields":{"STOP_OPTION":["this script",null]}},"bad":{"opcode":"unsupported"}}`)
	run(t, r, 1, false)
	if !r.Done() {
		t.Fatal("top-level stop did not end thread")
	}
}

func TestProcedureArgumentsKeepLiteralTypesAndLiveValues(t *testing.T) {
	r := fixture(t, `{
  "call":{"opcode":"procedures_call","mutation":{"proccode":"sample %s %s %s","argumentids":"[\"constant\",\"live\",\"missing\"]"},"inputs":{"constant":[3,null,[10,"007"]],"live":[1,[12,"score","score"]]}},
  "def":{"opcode":"procedures_definition","topLevel":true,"inputs":{"custom_block":[1,"proto"]}},
  "proto":{"opcode":"procedures_prototype","mutation":{"proccode":"sample %s %s %s","argumentids":"[\"a\",\"b\",\"c\"]","argumentnames":"[\"a\",\"b\",\"c\"]","argumentdefaults":"[0,0,false]","warp":"true"}}
 }`)
	th := &thread{target: r.stage, expression: newExpression("call")}
	b := r.stage.source.Blocks["call"]
	for _, value := range []any{float64(12), "updated", false} {
		*r.stage.variables["score"] = Variable{Value: value}
		th.expression.reset("call")
		budget := stepsPerFrame
		input := func(name string) (any, error) { return r.input(context.Background(), th, b, name, 0, &budget) }
		if _, err := r.call(context.Background(), th, b, input, false); err != nil {
			t.Fatal(err)
		}
		if th.params["a"].boxed() != "007" || th.params["b"].boxed() != value || th.params["c"].boxed() != false {
			t.Fatalf("arguments changed type or reused old state: %v", th.params)
		}
		th.stopScript()
	}
}

func BenchmarkProcedureCoordinateArguments(b *testing.B) {
	var blocks map[string]*project.Block
	if err := json.Unmarshal([]byte(procedureProject), &blocks); err != nil {
		b.Fatal(err)
	}
	ids := []string{"coordinate-start-x", "coordinate-start-y", "coordinate-end-x", "coordinate-end-y", "rectangle-left", "rectangle-top", "rectangle-right", "rectangle-bottom"}
	names := []string{"x1", "y1", "x2", "y2", "left", "top", "right", "bottom"}
	encode := func(value any) json.RawMessage {
		raw, err := json.Marshal(value)
		if err != nil {
			b.Fatal(err)
		}
		return raw
	}
	idsJSON, _ := json.Marshal(ids)
	namesJSON, _ := json.Marshal(names)
	blocks["call"].Mutation["argumentids"] = encode(string(idsJSON))
	blocks["proto"].Mutation["argumentids"] = encode(string(idsJSON))
	blocks["proto"].Mutation["argumentnames"] = encode(string(namesJSON))
	blocks["proto"].Mutation["argumentdefaults"] = encode("[0,0,0,0,0,0,0,0]")
	blocks["call"].Inputs = make(map[string][]any)
	for _, id := range ids {
		blocks["call"].Inputs[id] = []any{float64(1), []any{float64(4), "120"}}
	}
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
	th := &thread{target: r.stage, expression: newExpression("call")}
	block := blocks["call"]
	input := func(name string) (any, error) {
		budget := stepsPerFrame
		return r.input(context.Background(), th, block, name, 0, &budget)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		th.expression.reset("call")
		if _, err := r.call(context.Background(), th, block, input, false); err != nil {
			b.Fatal(err)
		}
		if th.params["bottom"].boxed() != "120" {
			b.Fatal("coordinate changed")
		}
		th.stopScript()
	}
}

func TestProcedureRepeatedInputIDKeepsOneRandomValue(t *testing.T) {
	r := fixture(t, `{
  "call":{"opcode":"procedures_call","mutation":{"proccode":"pair %s %s","argumentids":"[\"same\",\"same\"]"},"inputs":{"same":[2,"random"]}},
  "random":{"opcode":"data_itemoflist","fields":{"LIST":["values","values"]},"inputs":{"INDEX":[1,[10,"random"]]}},
  "def":{"opcode":"procedures_definition","topLevel":true,"inputs":{"custom_block":[1,"proto"]}},
  "proto":{"opcode":"procedures_prototype","mutation":{"proccode":"pair %s %s","argumentids":"[\"a\",\"b\"]","argumentnames":"[\"a\",\"b\"]","argumentdefaults":"[0,0]","warp":"true"}}
 }`)
	items := make([]any, 100)
	for i := range items {
		items[i] = float64(i)
	}
	r.stage.state.Lists["values"] = List{Items: items}
	th := &thread{target: r.stage, expression: newExpression("call")}
	block := r.stage.source.Blocks["call"]
	for i := 0; i < 20; i++ {
		th.expression.reset("call")
		budget := stepsPerFrame
		input := func(name string) (any, error) { return r.input(context.Background(), th, block, name, 0, &budget) }
		if _, err := r.call(context.Background(), th, block, input, false); err != nil {
			t.Fatal(err)
		}
		if th.params["a"].boxed() != th.params["b"].boxed() {
			t.Fatalf("same input evaluated twice: %v", th.params)
		}
		th.stopScript()
	}
}

func TestProcedureCoordinateArgumentsPreserveShortCircuit(t *testing.T) {
	r := fixture(t, `{
  "call":{"opcode":"procedures_call","mutation":{"proccode":"point %s %s","argumentids":"[\"point\",\"guard\"]"},"inputs":{"point":[2,"point"],"guard":[2,"guard"]}},
  "point":{"opcode":"data_itemoflist","fields":{"LIST":["values","values"]},"inputs":{"INDEX":[1,[12,"score","score"]]}},
  "guard":{"opcode":"operator_and","inputs":{"OPERAND1":[1,[12,"gate","gate"]],"OPERAND2":[1,[12,"missing","missing"]]}},
  "def":{"opcode":"procedures_definition","topLevel":true,"inputs":{"custom_block":[1,"proto"]}},
  "proto":{"opcode":"procedures_prototype","mutation":{"proccode":"point %s %s","argumentids":"[\"a\",\"b\"]","argumentnames":"[\"a\",\"b\"]","argumentdefaults":"[0,0]","warp":"true"}}
 }`)
	r.stage.state.Lists["values"] = List{Items: []any{"point", float64(7)}}
	r.stage.variables["gate"] = &Variable{Value: false}
	th := &thread{target: r.stage, expression: newExpression("call")}
	block := r.stage.source.Blocks["call"]
	call := func() error {
		th.expression.reset("call")
		budget := stepsPerFrame
		input := func(name string) (any, error) { return r.input(context.Background(), th, block, name, 0, &budget) }
		_, err := r.call(context.Background(), th, block, input, false)
		return err
	}
	for _, tc := range []struct {
		index float64
		want  any
	}{{2, float64(7)}, {1, "point"}} {
		*r.stage.variables["score"] = Variable{Value: tc.index}
		if err := call(); err != nil {
			t.Fatal(err)
		}
		if th.params["a"].boxed() != tc.want || th.params["b"].boxed() != false {
			t.Fatalf("live coordinate or short circuit lost: %v", th.params)
		}
		th.stopScript()
	}
	r.stage.variables["gate"] = &Variable{Value: true}
	if err := call(); err == nil {
		t.Fatal("unknown variable in a visited branch was ignored")
	}
	if th.params != nil || len(th.stack) != 0 {
		t.Fatal("failed argument evaluation entered the procedure")
	}
}

func parameterValues(params procedureParameters) map[string]any {
	if params == nil {
		return nil
	}
	values := make(map[string]any, len(params))
	for name, value := range params {
		values[name] = value.boxed()
	}
	return values
}
