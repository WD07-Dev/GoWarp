package vm

import (
	"context"
	"gowarp/core/extension"
	"strings"
	"testing"
)

func TestPredicateHatsKeepPerTargetLocalsAndRisingEdges(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"A","variables":{"v":["enabled",1]},"blocks":{"hat":{"opcode":"predicate_gate","topLevel":true,"fields":{"LABEL":["chosen",null]},"inputs":{"VALUE":[3,[12,"enabled","v"],[4,"0"]]}}}}]`)
	var contexts []*extension.Context
	factory := func() extension.Extension {
		return extension.Extension{ID: "predicate", Blocks: []extension.Block{{Opcode: "predicate_gate", Kind: "hat", Inputs: []string{"VALUE"}, Handler: func(c *extension.Context) (extension.Result, error) {
			c.Local["calls"] = extension.Number(c.Local["calls"]) + 1
			if c.Field("LABEL") != "chosen" {
				t.Error("predicate fields missing")
			}
			contexts = append(contexts, c)
			return extension.Result{Value: c.Bool("VALUE")}, nil
		}}}}
	}
	if err := r.SetExtensions(extension.Bundle{factory}); err != nil {
		t.Fatal(err)
	}
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	poll := func(mode string) any { return hostResult(t, c, "events.pollHats", "predicate_gate", mode) }
	drain := func() {
		t.Helper()
		if err := r.stepThreads(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if poll("edge") != 1.0 {
		t.Fatal("initial edge missing")
	}
	drain()
	if poll("edge") != 0.0 {
		t.Fatal("held predicate repeated")
	}
	r.targets[1].variables["v"].Value = 0.0
	if poll("edge") != 0.0 {
		t.Fatal("false predicate fired")
	}
	r.targets[1].variables["v"].Value = 1.0
	if poll("edge") != 1.0 {
		t.Fatal("new rising edge missed")
	}
	drain()
	if contexts[0].Args["VALUE"] != 1.0 || contexts[2].Args["VALUE"] != 0.0 {
		t.Fatal("subsequent polls overwrote captured argument maps")
	}
	r.createClone(r.targets[1])
	if poll("edge") != 1.0 {
		t.Fatal("clone inherited original edge state")
	}
	drain()
	var cloneContext *extension.Context
	for _, ctx := range contexts {
		host := ctx.Host.(*extensionContext)
		if host.th.target.state.CloneID != 0 {
			cloneContext = ctx
			break
		}
	}
	if cloneContext == nil || cloneContext.Local["calls"] != 1.0 {
		t.Fatal("clone shared hat Local state")
	}
	if poll("level") != 2.0 {
		t.Fatal("level mode did not start both predicates")
	}
	drain()
	r.GreenFlag()
	if poll("edge") != 1.0 {
		t.Fatal("green flag did not reset original edge state")
	}
}
func TestPredicateHatsSuspendReporterInputsWithoutRepeatingCompletedInputs(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage","blocks":{"hat":{"opcode":"predicate_gate","topLevel":true,"inputs":{"VALUE":[2,"report"]}},"report":{"opcode":"predicate_delayed"}}}]`)
	calls, predicates := 0, 0
	factory := func() extension.Extension {
		return extension.Extension{ID: "predicate", Blocks: []extension.Block{
			{Opcode: "predicate_gate", Kind: "hat", Inputs: []string{"VALUE"}, Handler: func(c *extension.Context) (extension.Result, error) {
				predicates++
				return extension.Result{Value: c.Bool("VALUE")}, nil
			}},
			{Opcode: "predicate_delayed", Kind: "reporter", Handler: func(c *extension.Context) (extension.Result, error) {
				calls++
				if !extension.Bool(c.Local["waited"]) {
					c.Local["waited"] = true
					return extension.Result{Again: true, YieldTick: true}, nil
				}
				return extension.Result{Value: true}, nil
			}},
		}}
	}
	if err := r.SetExtensions(extension.Bundle{factory}); err != nil {
		t.Fatal(err)
	}
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	if got := hostResult(t, c, "events.pollHats", "predicate_gate", "edge"); got != 0.0 || predicates != 0 {
		t.Fatal("predicate ran with incomplete input")
	}
	r.frame++
	if got := hostResult(t, c, "events.pollHats", "predicate_gate", "edge"); got != 1.0 || predicates != 1 || calls != 2 {
		t.Fatalf("suspended input state lost: %v %d %d", got, predicates, calls)
	}
}
func TestPredicateHatValidation(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage","blocks":{"hat":{"opcode":"predicate_gate","topLevel":true}}}]`)
	factory := func() extension.Extension {
		return extension.Extension{ID: "predicate", Blocks: []extension.Block{{Opcode: "predicate_gate", Kind: "hat", Handler: func(*extension.Context) (extension.Result, error) { return extension.Result{Yield: true}, nil }}}}
	}
	if err := r.SetExtensions(extension.Bundle{factory}); err != nil {
		t.Fatal(err)
	}
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	for _, args := range [][]any{{"predicate_gate"}, {"predicate_gate", "bad"}, {"missing", "edge"}} {
		if _, err := c.Call("events.pollHats", args...); err == nil {
			t.Fatalf("invalid call accepted: %v", args)
		}
	}
	if _, err := c.Call("events.pollHats", "predicate_gate", "edge"); err == nil || !strings.Contains(err.Error(), "cannot yield") {
		t.Fatalf("yielding predicate: %v", err)
	}
}
