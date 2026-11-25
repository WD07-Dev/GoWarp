package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"go/format"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	e "gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestPlaySoundToInVM(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"extensions":["lmsSoundExpanded"],"targets":[{"isStage":true,"name":"Stage","volume":37,"sounds":[{"name":"beep","rate":100,"sampleCount":100}],"blocks":{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"pitch"},
		"pitch":{"opcode":"sound_seteffectto","fields":{"EFFECT":["pitch",null]},"inputs":{"VALUE":[1,[4,"120"]]},"next":"play"},
		"play":{"opcode":"lmsSoundExpanded_playSoundTo","inputs":{"SOUND":[1,[10,"beep"]],"START":[1,[4,"0.25"]],"END":[1,[4,"0.75"]]}}
	}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	rt := vm.New(&p)
	rt.ConfigureAudio([][]float64{{1}})
	if err := rt.SetExtensions(e.Bundle{New}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Run(context.Background(), 2, 30, false); err != nil {
		t.Fatal(err)
	}
	commands := rt.DrainAudio()
	if len(commands) != 1 || commands[0].Kind != "play" || commands[0].Start != .25 || commands[0].End != .75 || commands[0].Volume != 37 || commands[0].Pitch != 120 {
		t.Fatalf("bounded extension playback = %+v", commands)
	}
}

func TestEmbeddedGoImplementationMatchesTestedModule(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	path := filepath.Join(dir, "..", "..", "..", "extensions", "examples", "lmsSoundExpanded.js")
	js, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source, err := e.ExtractGo(path, js)
	if err != nil {
		t.Fatal(err)
	}
	source, err = format.Source(source)
	if err != nil {
		t.Fatal(err)
	}
	tested, err := os.ReadFile(filepath.Join(dir, "module.go"))
	if err != nil {
		t.Fatal(err)
	}
	tested = bytes.TrimPrefix(tested, []byte("// Code generated from the extension Go comment for regression tests. DO NOT EDIT.\n"))
	if !bytes.Equal(source, tested) {
		t.Fatal("embedded Go differs from tested module")
	}
}
