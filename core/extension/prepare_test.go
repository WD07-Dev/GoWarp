package extension

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gowarp/core/project"
)

func preparationProject(t *testing.T) *project.Project {
	t.Helper()
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage","blocks":{"z":{"opcode":"event_whenflagclicked","topLevel":true,"next":"move"},"a":{"opcode":"event_whenflagclicked","topLevel":true},"move":{"opcode":"motion_movesteps","inputs":{"STEPS":[1,[4,"10"]]}}}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	return &p
}

func TestPrepareProjectCopiesAndRunsInRegistrationOrder(t *testing.T) {
	p := preparationProject(t)
	before, _ := json.Marshal(p)
	defs := []*Extension{
		{ID: "first", Prepare: func(p *project.Project) error {
			p.Targets[0].Blocks["move"].Inputs["STEPS"] = []any{1, []any{4, "20"}}
			return nil
		}},
		{ID: "second", Prepare: func(p *project.Project) error {
			b := p.Targets[0].Blocks["move"]
			if b.Inputs["STEPS"][1].([]any)[1] != "20" {
				t.Fatal("transforms ran out of order")
			}
			b.Opcode = "motion_turnright"
			return nil
		}},
	}
	prepared, err := PrepareProject(p, defs)
	if err != nil {
		t.Fatal(err)
	}
	if prepared == p || prepared.Targets[0].Blocks["move"].Opcode != "motion_turnright" {
		t.Fatal("transformed graph missing")
	}
	if prepared.Targets[0].Blocks["move"].Inputs["STEPS"][0] != float64(1) {
		t.Fatal("generated inputs were not normalized to Scratch JSON numbers")
	}
	after, _ := json.Marshal(p)
	if string(before) != string(after) {
		t.Fatal("source mutated")
	}
	if strings.Join(prepared.Targets[0].ScriptOrder, ",") != strings.Join(p.Targets[0].ScriptOrder, ",") {
		t.Fatal("saved script order changed")
	}
	unchanged, err := PrepareProject(p, []*Extension{{ID: "ordinary"}})
	if err != nil || unchanged != p {
		t.Fatal("ordinary extensions should not copy the project")
	}
}

func TestPrepareProjectRejectsInvalidChangesWithoutMutatingSource(t *testing.T) {
	for name, change := range map[string]func(*project.Project) error{
		"error":                    func(p *project.Project) error { p.Targets[0].Blocks["move"].Next = "z"; return errors.New("failed") },
		"target":                   func(p *project.Project) error { p.Targets = nil; return nil },
		"metadata":                 func(p *project.Project) error { p.Targets[0].Name = "renamed"; return nil },
		"order":                    func(p *project.Project) error { p.Targets[0].ScriptOrder = nil; return nil },
		"nil block":                func(p *project.Project) error { p.Targets[0].Blocks["move"] = nil; return nil },
		"missing next":             func(p *project.Project) error { p.Targets[0].Blocks["move"].Next = "missing"; return nil },
		"deleted referenced block": func(p *project.Project) error { delete(p.Targets[0].Blocks, "move"); return nil },
		"missing input": func(p *project.Project) error {
			p.Targets[0].Blocks["move"].Inputs["STEPS"] = []any{2, "missing"}
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := preparationProject(t)
			before, _ := json.Marshal(p)
			if _, err := PrepareProject(p, []*Extension{{ID: "broken", Prepare: change}}); err == nil || !strings.Contains(err.Error(), "broken") {
				t.Fatalf("missing contextual error: %v", err)
			}
			after, _ := json.Marshal(p)
			if string(before) != string(after) {
				t.Fatal("failed preparation changed source")
			}
		})
	}
}

func TestPrepareProjectPreservesProgrammaticMetadata(t *testing.T) {
	for _, p := range []*project.Project{{}, {Targets: []*project.Target{{IsStage: true}}}} {
		before, _ := json.Marshal(p)
		prepared, err := PrepareProject(p, []*Extension{{ID: "noop", Prepare: func(*project.Project) error { return nil }}})
		if err != nil {
			t.Fatal(err)
		}
		after, _ := json.Marshal(prepared)
		if string(before) != string(after) {
			t.Fatalf("metadata changed: %s -> %s", before, after)
		}
	}
}
