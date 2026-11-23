package extension

import (
	"context"
	"encoding/json"
	e "gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
	myblocks "gowarp/test/extensions/SPmbpCST"
	"strings"
	"testing"
)

func TestSyncRenamedCallPreservesInputAndRefreshesExecutableCode(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[
	{"isStage":true,"name":"Stage","variables":{"score":["score",0]},"blocks":{}},
	{"name":"Library","extensionStorage":{"SPmbpCST":{"renamed %s %s":{"global":true}},"unrelated":[1,2]},"blocks":{
	 "def":{"opcode":"procedures_definition","topLevel":true,"next":"add","inputs":{"custom_block":[1,"proto"]}},
	 "proto":{"opcode":"procedures_prototype","mutation":{"proccode":"renamed %s %s","argumentids":"[\"stable\",\"added\"]","argumentnames":"[\"n\",\"extra\"]","argumentdefaults":"[0,4]","warp":"true"}},
	 "add":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"sum"]}},
	 "sum":{"opcode":"operator_add","inputs":{"NUM1":[2,"n"],"NUM2":[2,"extra"]}},
	 "n":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["n",null]}},
	 "extra":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["extra",null]}}
	}},
	{"name":"Caller","blocks":{
	 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"sync"},
	 "sync":{"opcode":"globalprocsync_syncAll","next":"call"},
	 "call":{"opcode":"procedures_call","next":"status","mutation":{"proccode":"old %s","argumentids":"[\"stable\"]","warp":"false"},"inputs":{"stable":[1,[4,"3"]]}},
	 "status":{"opcode":"globalprocsync_syncAll"}
	}}
	]}`), &p); err != nil {
		t.Fatal(err)
	}
	r := vm.New(&p)
	var state map[string]any
	if err := r.SetExtensions(e.Bundle{myblocks.New, func() e.Extension {
		def := New()
		def.Blocks = append(def.Blocks, e.Block{Opcode: "globalprocsync_capture", Kind: "command", Handler: func(c *e.Context) (e.Result, error) { state = c.State; return e.Result{}, nil }})
		def.Events = map[string]e.Handler{"tick": func(c *e.Context) (e.Result, error) { state = c.State; return e.Result{}, nil }}
		return def
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background(), 2, 30, true); err != nil {
		t.Fatal(err)
	}
	if got := e.Number(r.Snapshot()[0].Variables["score"].Value); got != 7 {
		t.Fatalf("updated call result = %v, status=%v", got, state)
	}
	if !strings.Contains(e.String(state["lastResult"]), "0개 호출 갱신") {
		t.Fatalf("second sync not idempotent: %v", state)
	}
	var code string
	_ = json.Unmarshal(p.Targets[2].Blocks["call"].Mutation["proccode"], &code)
	if code != "old %s" {
		t.Fatal("runtime authoring mutation leaked into source project")
	}
}

func TestChooseProtectsLocalProceduresAndAmbiguousMatches(t *testing.T) {
	call := &block{Mutation: map[string]any{"proccode": "old %s", "argumentids": "[\"stable\"]"}}
	prototypes := []prototype{{code: "first %s", ids: []string{"stable"}}, {code: "second %s", ids: []string{"stable"}}}
	if choose(call, prototypes, nil) != nil {
		t.Fatal("ambiguous rename selected an arbitrary definition")
	}
	if choose(call, prototypes[:1], map[string]bool{"old %s": true}) != nil {
		t.Fatal("local procedure was replaced")
	}
	if choose(call, []prototype{{code: "first %s", ids: []string{"different"}}}, nil) != nil {
		t.Fatal("deleted argument was lost")
	}
}
