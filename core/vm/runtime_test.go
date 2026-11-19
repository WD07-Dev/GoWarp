package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gowarp/core/project"
)

func fixture(t *testing.T, blocks string) *Runtime {
	t.Helper()
	var p project.Project
	data := `{"targets":[{"isStage":true,"name":"Stage","variables":{"score":["score",0]},"blocks":` + blocks + `}]}`
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatal(err)
	}
	r := New(&p)
	installTestExtensions(t, r)
	return r
}

func run(t *testing.T, r *Runtime, frames int, turbo bool) {
	t.Helper()
	if err := r.Run(context.Background(), frames, 30, turbo); err != nil {
		t.Fatal(err)
	}
}

const repeatProject = `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"repeat"},
 "repeat":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"3"]],"SUBSTACK":[2,"add"]},"next":"branch"},
 "add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"2"]]}},
 "branch":{"opcode":"control_if_else","inputs":{"CONDITION":[2,"eq"],"SUBSTACK":[2,"success"],"SUBSTACK2":[2,"fail"]}},
 "eq":{"opcode":"operator_equals","inputs":{"OPERAND1":[3,[12,"score","score"],[4,"0"]],"OPERAND2":[1,[4,"6"]]}},
 "success":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[3,"sum",[4,"0"]]}},
 "sum":{"opcode":"operator_add","inputs":{"NUM1":[1,[4,"40"]],"NUM2":[1,[4,"2"]]}},
 "fail":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"-1"]]}}
}`

func TestRepeatBranchesAndTurbo(t *testing.T) {
	for _, tc := range []struct {
		frames int
		turbo  bool
		want   float64
	}{{1, false, 42}, {3, false, 42}, {4, false, 42}, {1, true, 42}} {
		r := fixture(t, repeatProject)
		run(t, r, tc.frames, tc.turbo)
		if got := number(r.Snapshot()[0].Variables["score"].Value); got != tc.want {
			t.Fatalf("frames=%d turbo=%v: got %v want %v", tc.frames, tc.turbo, got, tc.want)
		}
	}
}

func TestWaitYieldsInTurbo(t *testing.T) {
	blocks := `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait"},"wait":{"opcode":"control_wait","next":"set","inputs":{"DURATION":[1,[4,"0.1"]]}},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"7"]]}}}`
	r := fixture(t, blocks)
	run(t, r, 3, true)
	if number(r.Snapshot()[0].Variables["score"].Value) != 0 {
		t.Fatal("wait finished early")
	}
	run(t, r, 1, true)
	if number(r.Snapshot()[0].Variables["score"].Value) != 7 {
		t.Fatal("wait did not resume")
	}
}

func TestUnsupportedAndBrokenBlocks(t *testing.T) {
	for _, blocks := range []string{
		`{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"bad"},"bad":{"opcode":"pen_clear"}}`,
		`{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"missing"}}`,
		`{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"cycle"]}},"cycle":{"opcode":"operator_add","inputs":{"NUM1":[2,"cycle"],"NUM2":[1,[4,"1"]]}}}`,
	} {
		r := fixture(t, blocks)
		if err := r.Run(context.Background(), 1, 30, false); err == nil {
			t.Fatal("expected explicit execution error")
		}
	}
}

func TestForeverAndCancellationAreBounded(t *testing.T) {
	r := fixture(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,null]}}}`)
	run(t, r, 2, true)
	if r.Done() {
		t.Fatal("forever unexpectedly completed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Run(ctx, 1, 30, true); err != context.Canceled {
		t.Fatalf("got %v", err)
	}
}

func TestGlobalAndLocalVariableIDs(t *testing.T) {
	var p project.Project
	data := `{"targets":[{"isStage":true,"name":"Stage","variables":{"global":["same",1]},"blocks":{}},{"name":"Sprite","variables":{"local":["same",10]},"blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"global"},"global":{"opcode":"data_changevariableby","next":"local","fields":{"VARIABLE":["same","global"]},"inputs":{"VALUE":[1,[4,"2"]]}},"local":{"opcode":"data_changevariableby","fields":{"VARIABLE":["same","local"]},"inputs":{"VALUE":[1,[4,"4"]]}}}}]}`
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatal(err)
	}
	r := New(&p)
	run(t, r, 1, false)
	s := r.Snapshot()
	if number(s[0].Variables["global"].Value) != 3 || number(s[1].Variables["local"].Value) != 14 {
		t.Fatalf("incorrect scope: %+v", s)
	}
	if p.Targets[0].Variables["global"][1] != float64(1) {
		t.Fatal("mutated source project")
	}
}

func TestUnknownVariableIsError(t *testing.T) {
	r := fixture(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["missing","missing"]},"inputs":{"VALUE":[1,[4,"1"]]}}}`)
	if err := r.Run(context.Background(), 1, 30, false); err == nil || !strings.Contains(err.Error(), "variable") {
		t.Fatalf("got %v", err)
	}
}

// Shared reporter references can expand exponentially even with shallow nesting.
func TestReporterWorkIsBounded(t *testing.T) {
	r := fixture(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"expr24"]}}}`)
	blocks := r.targets[0].source.Blocks
	for i := 0; i <= 24; i++ {
		input := []any{float64(1), []any{float64(4), "1"}}
		if i > 0 {
			input = []any{float64(2), fmt.Sprintf("expr%d", i-1)}
		}
		blocks[fmt.Sprintf("expr%d", i)] = &project.Block{Opcode: "operator_add", Inputs: map[string][]any{"NUM1": input, "NUM2": input}}
	}
	if err := r.Run(context.Background(), 1, 30, false); err == nil || !strings.Contains(err.Error(), "evaluation limit") {
		t.Fatalf("expected evaluation limit, got %v", err)
	}
}

func TestRejectNonScalarLiterals(t *testing.T) {
	for _, payload := range []string{`{"bad":1}`, `[1,2]`, `null`} {
		blocks := `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,` + payload + `]]}}}`
		r := fixture(t, blocks)
		if err := r.Run(context.Background(), 1, 30, false); err == nil {
			t.Fatalf("accepted malformed literal %s", payload)
		}
	}
}
