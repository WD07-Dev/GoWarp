package vm

import (
	"context"
	"fmt"
	"gowarp/core/extension"
	"gowarp/core/project"
	clonesplus "gowarp/test/extensions/lmsclonesplus"
	"testing"
)

func TestCloneVariablePredicateAndDeletion(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"A","variables":{"tag":["tag","ready"]},"blocks":{"hat":{"opcode":"lmsclonesplus_whenCloneStartsWithVar","topLevel":true,"fields":{"INPUTA":["tag",null]},"inputs":{"INPUTB":[1,[10,"READY"]]}}}},{"name":"B","variables":{"tag":["tag","ready"]}}]`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	if hostResult(t, c, "events.pollHats", "lmsclonesplus_whenCloneStartsWithVar", "edge") != 0.0 {
		t.Fatal("original fired clone hat")
	}
	first := hostResult(t, c, "target.clone").(string)
	second := hostResult(t, c, "target.clone").(string)
	hostResult(t, c, "variable.set", "tag", "waiting", second)
	if hostResult(t, c, "events.pollHats", "lmsclonesplus_whenCloneStartsWithVar", "edge") != 1.0 {
		t.Fatal("clone predicate did not match exact variable with Scratch comparison")
	}
	if err := r.stepThreads(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hostResult(t, c, "events.pollHats", "lmsclonesplus_whenCloneStartsWithVar", "edge") != 0.0 {
		t.Fatal("clone hat retriggered while true")
	}
	hostResult(t, c, "variable.set", "tag", "ready", second)
	if hostResult(t, c, "events.pollHats", "lmsclonesplus_whenCloneStartsWithVar", "edge") != 1.0 {
		t.Fatal("changed clone variable did not produce edge")
	}
	other := hostResult(t, c, "target.clone", "B").(string)
	var deletion extension.Handler
	for _, b := range clonesplus.New().Blocks {
		if b.Opcode == "lmsclonesplus_deleteCloneWithVar" {
			deletion = b.Handler
		}
	}
	if _, err := deletion(&extension.Context{Host: c, Fields: map[string]string{"INPUTA": "tag"}, Args: map[string]any{"INPUTB": "READY"}}); err != nil {
		t.Fatal(err)
	}
	if r.cloneCount != 1 || hostResult(t, c, "target.get", "name", other) != "B" {
		t.Fatal("delete by variable affected another sprite")
	}
	if _, err := c.Call("target.get", "name", first); err == nil {
		t.Fatal("matching clone survived")
	}
}

func BenchmarkCloneVariableLookup(b *testing.B) {
	for _, count := range []int{32, 128, 512} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Name: "Stage"}, {Name: "Sprite"}}})
			r.options.CloneLimit = count + 1
			original := r.targets[1]
			for i := range 64 {
				original.variables[fmt.Sprintf("v%d", i)] = &Variable{Name: "value", Value: float64(i)}
			}
			original.variables["tag"] = &Variable{Name: "tag", Value: "other"}
			for range count {
				r.createClone(original)
			}
			r.targets[len(r.targets)-1].variables["tag"].Value = "needle"
			var handler extension.Handler
			for _, block := range clonesplus.New().Blocks {
				if block.Opcode == "lmsclonesplus_getVariableOfClone" {
					handler = block.Handler
				}
			}
			c := &extension.Context{Host: &extensionContext{r: r, th: &thread{target: original}},
				Fields: map[string]string{"INPUTA": "v63", "INPUTB": "tag"}, Args: map[string]any{"INPUTC": "needle"}}
			b.ReportAllocs()
			for b.Loop() {
				got, err := handler(c)
				if err != nil || got.Value != 63.0 {
					b.Fatalf("lookup = %v, %v", got.Value, err)
				}
			}
		})
	}
}

func TestCloneVariableLookupOrderAndScope(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage","variables":{"global":["global",5]}},{"name":"A","variables":{"tag":["tag","ready"],"value":["value",1]}},{"name":"B","variables":{"tag":["tag","ready"],"value":["value",99]}}]`)
	host := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	first := hostResult(t, host, "target.clone").(string)
	second := hostResult(t, host, "target.clone").(string)
	hostResult(t, host, "variable.set", "value", 2.0, second)
	hostResult(t, host, "target.clone", "B")
	var handler extension.Handler
	for _, block := range clonesplus.New().Blocks {
		if block.Opcode == "lmsclonesplus_getVariableOfClone" {
			handler = block.Handler
		}
	}
	c := &extension.Context{Host: host, Fields: map[string]string{"INPUTA": "value", "INPUTB": "tag"}, Args: map[string]any{"INPUTC": "READY"}}
	check := func(want any) {
		t.Helper()
		got, err := handler(c)
		if err != nil || got.Value != want {
			t.Fatalf("lookup = %v, %v; want %v", got.Value, err, want)
		}
	}
	check(1.0)
	c.Fields["INPUTA"] = "global"
	check(5.0)
	c.Fields["INPUTA"] = "missing"
	check("")
	c.Fields["INPUTA"] = "value"
	hostResult(t, host, "variable.set", "value", nil, first)
	check(nil)
	hostResult(t, host, "target.delete", first)
	check(2.0)
	c.Fields["INPUTB"] = "missing"
	c.Args["INPUTC"] = ""
	check("")
}
