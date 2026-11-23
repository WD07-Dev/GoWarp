package extension

import (
	"bytes"
	"context"
	"encoding/json"
	e "gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
	"testing"
)

func TestGlobalProcedureUsesCallerTargetAndMutableParameters(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[
	{"isStage":true,"name":"Stage","blocks":{}},
	{"name":"Library","variables":{"score":["score",50]},"extensionStorage":{"SPmbpCST":{"adjust %s":{"global":true}}},"blocks":{
	 "def":{"opcode":"procedures_definition","topLevel":true,"next":"set","inputs":{"custom_block":[1,"proto"]}},
	 "proto":{"opcode":"procedures_prototype","mutation":{"proccode":"adjust %s","argumentids":"[\"nID\"]","argumentnames":"[\"n\"]","argumentdefaults":"[0]","warp":"true"}},
	 "set":{"opcode":"SPmbpCST_setParam","next":"add","fields":{"PARAM":["n",null]},"inputs":{"VALUE":[1,[10,"007"]]}},
	 "add":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"arg"]}},
	 "arg":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["n",null]}}
	}},
	{"name":"Caller","variables":{"score":["score",0]},"blocks":{
	 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"call"},
	 "call":{"opcode":"procedures_call","mutation":{"proccode":"adjust %s","argumentids":"[\"nID\"]"},"inputs":{"nID":[1,[4,"3"]]}},
	 "set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"999"]]}}
	}}
	]}`), &p); err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(&p)
	r := vm.New(&p)
	if err := r.SetExtensions(e.Bundle{New}); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background(), 1, 30, true); err != nil {
		t.Fatal(err)
	}
	snapshot := r.Snapshot()
	if snapshot[1].Variables["score"].Value != float64(50) || snapshot[2].Variables["score"].Value != "007" {
		t.Fatalf("wrong target or parameter conversion: %+v", snapshot)
	}
	after, _ := json.Marshal(&p)
	if !bytes.Equal(original, after) {
		t.Fatal("expansion mutated the source project")
	}
	expanded, err := e.PrepareProject(&p, []*e.Extension{ptr(New())})
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.PrepareProject(&p, []*e.Extension{ptr(New())})
	if err != nil {
		t.Fatal(err)
	}
	one, _ := json.Marshal(expanded)
	two, _ := json.Marshal(again)
	if !bytes.Equal(one, two) {
		t.Fatal("global expansion is not deterministic")
	}
	if _, err = vm.GenerateNative(expanded); err != nil {
		t.Fatal(err)
	}
}

type parameterHost struct {
	name  string
	value any
}

func (h *parameterHost) Call(op string, args ...any) (any, error) {
	h.name, h.value = args[0].(string), args[1]
	return nil, nil
}
func TestSetParamReadsTheSavedField(t *testing.T) {
	h := &parameterHost{}
	for _, b := range New().Blocks {
		if b.Opcode == "SPmbpCST_setParam" {
			_, err := b.Handler(&e.Context{Host: h, Args: map[string]any{"VALUE": false, "PARAM": "wrong"}, Fields: map[string]string{"PARAM": "sound"}})
			if err != nil || h.name != "sound" || h.value != false {
				t.Fatalf("field/value mismatch: %+v, %v", h, err)
			}
		}
	}
}

func ptr(def e.Extension) *e.Extension { return &def }
