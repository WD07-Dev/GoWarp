package vm

import (
	"context"
	"gowarp/core/extension"
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
