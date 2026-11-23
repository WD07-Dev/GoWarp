package extension

import (
	"context"
	"encoding/json"
	"fmt"
	e "gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
	"math"
	"testing"
)

func waitRuntime(t *testing.T, body string, warp bool) *vm.Runtime {
	t.Helper()
	var p project.Project
	text := fmt.Sprintf(`{"targets":[{"isStage":true,"name":"Stage","variables":{"score":["score",0]},"blocks":{
	"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"call"},
	"call":{"opcode":"procedures_call","mutation":{"proccode":"wait","argumentids":"[]"}},
	"def":{"opcode":"procedures_definition","topLevel":true,"next":"wait","inputs":{"custom_block":[1,"proto"]}},
	"proto":{"opcode":"procedures_prototype","mutation":{"proccode":"wait","argumentids":"[]","argumentnames":"[]","argumentdefaults":"[]","warp":%v}},
	"done":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"100"]]}},
	"inc":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
	"condition":{"opcode":"operator_gt","inputs":{"OPERAND1":[3,[12,"score","score"],[4,"0"]],"OPERAND2":[1,[4,"1"]]}},%s
	}}]}`, warp, body)
	if err := json.Unmarshal([]byte(text), &p); err != nil {
		t.Fatal(err)
	}
	r := vm.New(&p)
	if err := r.SetExtensions(e.Bundle{New}); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestConditionReevaluatesAndExecutesBranchOncePerTick(t *testing.T) {
	for _, warp := range []bool{false, true} {
		for _, turbo := range []bool{false, true} {
			r := waitRuntime(t, `"wait":{"opcode":"waitplus_waitUntilConditionAsync","next":"done","inputs":{"M":[2,"condition"],"SUBSTACK":[2,"inc"]}}`, warp)
			if err := r.Run(context.Background(), 3, 30, turbo); err != nil {
				t.Fatal(err)
			}
			if got := r.Snapshot()[0].Variables["score"].Value; e.Number(got) != 102 {
				t.Fatalf("warp=%v turbo=%v score=%v", warp, turbo, got)
			}
		}
	}
}

func TestTickDeadlineRoundsUpAndDoesNotUseFPS(t *testing.T) {
	r := waitRuntime(t, `"wait":{"opcode":"waitplus_waitTicksAsync","next":"done","inputs":{"N":[1,[4,"1.2"]]}}`, true)
	if err := r.Run(context.Background(), 2, 120, true); err != nil {
		t.Fatal(err)
	}
	if e.Number(r.Snapshot()[0].Variables["score"].Value) != 0 {
		t.Fatal("wait completed before two runtime ticks")
	}
	if err := r.Run(context.Background(), 1, 120, true); err != nil {
		t.Fatal(err)
	}
	if e.Number(r.Snapshot()[0].Variables["score"].Value) != 100 {
		t.Fatal("wait did not complete on its runtime tick")
	}
}

func TestPositiveRejectsNonfiniteAndNegative(t *testing.T) {
	for _, value := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if positive(value) != 0 {
			t.Fatalf("positive(%v) != 0", value)
		}
	}
}
