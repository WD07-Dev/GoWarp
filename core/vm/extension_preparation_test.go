package vm

import (
	"encoding/json"
	"strings"
	"testing"

	"gowarp/core/extension"
	"gowarp/core/project"
)

func TestPreparationRebuildsFlagScriptsInSavedOrder(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage","blocks":{
 "z":{"opcode":"event_whenflagclicked","topLevel":true,"next":"old"},
 "a":{"opcode":"event_whenflagclicked","topLevel":true,"next":"second"},
 "old":{"opcode":"example_mark","inputs":{"VALUE":[1,[10,"old"]]}},
 "second":{"opcode":"example_mark","inputs":{"VALUE":[1,[10,"second"]]}}
 }}]}`), &p); err != nil {
		t.Fatal(err)
	}
	var marks []string
	r := New(&p)
	if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "example", Prepare: func(p *project.Project) error {
			blocks := p.Targets[0].Blocks
			blocks["z"].Next = "replacement"
			blocks["replacement"] = &project.Block{Opcode: "example_mark", Inputs: map[string][]any{"VALUE": {1, []any{10, "replacement"}}}}
			blocks["newhat"] = &project.Block{Opcode: "event_whenflagclicked", TopLevel: true, Next: "last"}
			blocks["last"] = &project.Block{Opcode: "example_mark", Inputs: map[string][]any{"VALUE": {1, []any{10, "last"}}}}
			return nil
		}, Blocks: []extension.Block{{Opcode: "example_mark", Kind: "command", Inputs: []string{"VALUE"}, Handler: func(c *extension.Context) (extension.Result, error) {
			marks = append(marks, c.String("VALUE"))
			return extension.Result{}, nil
		}}}}
	}}); err != nil {
		t.Fatal(err)
	}
	run(t, r, 1, true)
	if strings.Join(marks, ",") != "replacement,second,last" {
		t.Fatal(marks)
	}
	if p.Targets[0].Blocks["z"].Next != "old" {
		t.Fatal("source changed")
	}
}

func TestPreparationRejectsUnregisteredBlocksBeforeInstallingGraph(t *testing.T) {
	p := &project.Project{Targets: []*project.Target{{IsStage: true, Blocks: project.Blocks{}}}}
	r := New(p)
	err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "example", Prepare: func(p *project.Project) error {
			p.Targets[0].Blocks["bad"] = &project.Block{Opcode: "missing_command"}
			return nil
		}}
	}})
	if err == nil || !strings.Contains(err.Error(), "missing_command") {
		t.Fatalf("expected unregistered block error, got %v", err)
	}
	if r.project != p || r.stage.source != p.Targets[0] || len(r.extensions) != 0 {
		t.Fatal("invalid graph was installed")
	}
}
