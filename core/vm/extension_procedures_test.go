package vm

import (
	"context"
	"encoding/json"
	"errors"
	"gowarp/core/extension"
	"gowarp/core/project"
	myblocks "gowarp/test/extensions/SPmbpCST"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGlobalProceduresMatchNativeCompilationAndKeepCallerBindings(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage","blocks":{}},
	{"name":"Library","variables":{"score":["score",50]},"extensionStorage":{"SPmbpCST":{"adjust":{"global":true},"mutable %s":{"global":true}}},"blocks":{
	"def":{"opcode":"procedures_definition","topLevel":true,"next":"move","inputs":{"custom_block":[1,"proto"]}},
	"proto":{"opcode":"procedures_prototype","mutation":{"proccode":"adjust","argumentids":"[]","argumentnames":"[]","argumentdefaults":"[]","warp":"true"}},
	"move":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"7"]]}},
 "mdef":{"opcode":"procedures_definition","topLevel":true,"next":"before","inputs":{"custom_block":[1,"mproto"]}},
 "mproto":{"opcode":"procedures_prototype","mutation":{"proccode":"mutable %s","argumentids":"[\"nID\"]","argumentnames":"[\"n\"]","argumentdefaults":"[0]","warp":"true"}},
 "before":{"opcode":"data_changevariableby","next":"mutate","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"arg"]}},
 "mutate":{"opcode":"library_set","next":"after"},
 "after":{"opcode":"data_changevariableby","next":"last","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"arg"]}},
 "last":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
 "arg":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["n",null]}}
	}}, {"name":"Caller","variables":{"score":["score",0]},"blocks":{
	"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"call"},
	"call":{"opcode":"procedures_call","next":"mcall","mutation":{"proccode":"adjust","argumentids":"[]"}},
 "mcall":{"opcode":"procedures_call","mutation":{"proccode":"mutable %s","argumentids":"[\"nID\"]"},"inputs":{"nID":[1,[4,"3"]]}}
	}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	expanded, err := extension.PrepareProject(&p, []*extension.Extension{{ID: "library", Prepare: myblocks.New().Prepare}})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateNative(expanded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), `nativeProcedures[2]["adjust"]`) {
		t.Fatalf("caller global missing from native generation: %s", generated)
	}
	if os.Getenv("GOWARP_GLOBAL_PARITY_CHILD") != "1" {
		root, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		for {
			if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
				break
			}
			parent := filepath.Dir(root)
			if parent == root {
				t.Fatal("project root missing")
			}
			root = parent
		}
		dir := t.TempDir()
		file := filepath.Join(dir, "generated.go")
		if err := os.WriteFile(file, generated, 0600); err != nil {
			t.Fatal(err)
		}
		overlay, _ := json.Marshal(map[string]any{"Replace": map[string]string{filepath.Join(root, "core", "vm", "native_project_generated.go"): file}})
		path := filepath.Join(dir, "overlay.json")
		if err := os.WriteFile(path, overlay, 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "test", "-overlay="+path, filepath.Join(root, "core", "vm"), "-run", "^TestGlobalProceduresMatchNativeCompilationAndKeepCallerBindings$", "-count=1")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOWARP_GLOBAL_PARITY_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("native global parity: %v\n%s", err, out)
		}
	}
	r := New(&p)
	if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "library", Prepare: myblocks.New().Prepare, Blocks: []extension.Block{{Opcode: "library_set", Kind: "command", Handler: func(c *extension.Context) (extension.Result, error) {
			_, err := c.Call("procedures.setParam", "n", float64(9))
			return extension.Result{}, err
		}}}}
	}}); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("GOWARP_GLOBAL_PARITY_CHILD") == "1" && !r.native {
		t.Fatal("runtime expansion does not match compiled project key")
	}
	run(t, r, 1, true)
	if number(r.targets[1].variables["score"].Value) != 50 || number(r.targets[2].variables["score"].Value) != 20 {
		t.Fatalf("global procedure did not use caller: %+v", r.Snapshot())
	}
}

func TestProcedureParameterMutationIsScopedToCurrentCall(t *testing.T) {
	r := fixture(t, `{}`)
	outer := procedureParameters{"n": {value: "outer"}}
	th := &thread{target: r.stage, params: procedureParameters{"n": {value: "inner"}}, stack: []continuation{{call: true, params: outer}}}
	host := &extensionContext{r: r, th: th}
	if _, err := host.Call("procedures.setParam", "n", false); err != nil {
		t.Fatal(err)
	}
	if th.params["n"].boxed() != false || outer["n"].boxed() != "outer" {
		t.Fatal("setParam changed caller parameters or coerced the value")
	}
	th.params = nil
	if _, err := host.Call("procedures.setParam", "new", "007"); err != nil {
		t.Fatal(err)
	}
	if th.params["new"].boxed() != "007" || outer["new"].boxed() != nil {
		t.Fatal("parameter creation leaked into caller")
	}
}

func TestProcedureUpdatesPrepareWritableBlockMaps(t *testing.T) {
	p := &project.Project{Targets: []*project.Target{
		{IsStage: true, Name: "Stage"},
		{Name: "Sprite", Blocks: project.Blocks{
			"call": {Opcode: "procedures_call", Mutation: map[string]json.RawMessage{
				"proccode":    json.RawMessage(`"before"`),
				"argumentids": json.RawMessage(`"[]"`),
			}},
		}},
	}}
	r := New(p)
	preparations := 0
	if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "tools", Prepare: func(p *extension.Project) error {
			preparations++
			if preparations == 1 {
				return nil
			}
			if p.Targets[0].Blocks == nil {
				return errors.New("procedure updates must provide writable block maps")
			}
			p.Targets[0].Blocks["value"] = &project.Block{Opcode: "operator_add"}
			return nil
		}}
	}}); err != nil {
		t.Fatal(err)
	}
	host := &extensionContext{r: r, th: &thread{target: r.stage}}
	_, err := host.Call("procedures.updateCalls", []any{map[string]any{
		"target": 1, "id": "call",
		"mutation": map[string]any{"proccode": "after", "argumentids": "[]"},
		"inputs":   map[string]any{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if preparations != 2 || r.project.Targets[0].Blocks["value"] == nil {
		t.Fatal("updated graph did not run preparation")
	}
	if p.Targets[0].Blocks != nil || string(p.Targets[1].Blocks["call"].Mutation["proccode"]) != `"before"` {
		t.Fatal("procedure update mutated the original project")
	}
}
