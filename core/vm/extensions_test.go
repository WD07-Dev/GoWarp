package vm

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"gowarp/core/extension"
	"gowarp/core/project"
)

func TestDisposeContinuesAfterExtensionError(t *testing.T) {
	r := fixture(t, "{}")
	failure := errors.New("cleanup failed")
	disposed := false
	if err := r.SetExtensions(extension.Bundle{
		func() extension.Extension {
			return extension.Extension{ID: "first", Events: map[string]extension.Handler{
				"dispose": func(*extension.Context) (extension.Result, error) {
					return extension.Result{}, failure
				},
			}}
		},
		func() extension.Extension {
			return extension.Extension{ID: "second", Events: map[string]extension.Handler{
				"dispose": func(*extension.Context) (extension.Result, error) {
					disposed = true
					return extension.Result{}, nil
				},
			}}
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.CloseExtensions(); !errors.Is(err, failure) {
		t.Fatalf("dispose error = %v", err)
	}
	if !disposed {
		t.Fatal("another extension's error skipped resource cleanup")
	}
}

func TestExtensionFoldedInputHonorsCancellation(t *testing.T) {
	r, th, block := constantExtensionRuntime(t)
	block.Inputs["B"] = []any{float64(2), "sum"}
	r.stage.source.Blocks = map[string]*project.Block{
		"sum": {Opcode: "operator_add", Inputs: literalInputs(map[string]any{"NUM1": "2", "NUM2": "3"})},
	}
	called := false
	r.extensionBlocks[block.Opcode].block.Handler = func(*extension.Context) (extension.Result, error) {
		called = true
		return extension.Result{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget := stepsPerFrame
	_, _, err := r.invokeExtension(ctx, th, block, func(name string) (any, error) {
		return r.input(ctx, th, block, name, 0, &budget)
	}, false, 0, &budget)
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled input reached handler: called=%v error=%v", called, err)
	}
}

func BenchmarkExtensionLookupForBuiltin(b *testing.B) {
	r := New(&project.Project{})
	if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		def := extension.Extension{ID: "example"}
		for _, name := range []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine"} {
			def.Blocks = append(def.Blocks, extension.Block{Opcode: "example_" + name, Kind: "reporter",
				Handler: func(*extension.Context) (extension.Result, error) {
					return extension.Result{}, nil
				}})
		}
		return def
	}}); err != nil {
		b.Fatal(err)
	}
	block := &project.Block{Opcode: "operator_add"}
	b.ReportAllocs()

	for b.Loop() {
		if _, handled, err := r.invokeExtension(context.Background(), nil, block, nil, true, 0, nil); handled || err != nil {
			b.Fatal("builtin was handled as an extension")
		}
	}
}

func TestNativeExtensionDispatchAndYieldTick(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"call"},
		"call":{"opcode":"example_wait","next":"set","inputs":{"VALUE":[1,[4,"7"]]}},
		"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"value"]}},
		"value":{"opcode":"example_value"}
	}`)
	calls := 0
	err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "example", Blocks: []extension.Block{
			{Opcode: "example_wait", Kind: "command", Inputs: []string{"VALUE"}, Handler: func(c *extension.Context) (extension.Result, error) {
				calls++
				if c.Local["waiting"] == nil {
					c.Local["waiting"] = true
					c.State["value"] = c.Number("VALUE")
					return extension.Result{Again: true, YieldTick: true}, nil
				}
				return extension.Result{}, nil
			}},
			{Opcode: "example_value", Kind: "reporter", Handler: func(c *extension.Context) (extension.Result, error) {
				return extension.Result{Value: c.State["value"]}, nil
			}},
		}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	run(t, r, 1, false)
	if calls != 1 || r.stage.variables["score"].Value != float64(0) {
		t.Fatal("extension wait did not yield")
	}
	run(t, r, 1, false)
	if calls != 2 || r.stage.variables["score"].Value != float64(7) {
		t.Fatal("extension did not resume with retained state")
	}
}

func TestNativeExtensionCooperativeYieldResumesInSameTick(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"call"},
		"call":{"opcode":"example_wait","next":"inc"},
		"inc":{"opcode":"control_incr_counter"}
	}`)
	calls := 0
	if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "example", Blocks: []extension.Block{{
			Opcode: "example_wait", Kind: "command", Handler: func(c *extension.Context) (extension.Result, error) {
				calls++
				if c.Local["started"] == nil {
					c.Local["started"] = true
					return extension.Result{Again: true, Yield: true}, nil
				}
				return extension.Result{}, nil
			},
		}}}
	}}); err != nil {
		t.Fatal(err)
	}
	run(t, r, 1, false)
	if calls != 2 || r.counter != 1 {
		t.Fatalf("ordinary yield delayed a ready script: calls=%d counter=%g", calls, r.counter)
	}
}

func TestNativeExtensionWarpHonorsYieldKind(t *testing.T) {
	for _, waitTick := range []bool{false, true} {
		r := fixture(t, `{
			"a":{"opcode":"event_whenflagclicked","topLevel":true,"next":"call"},
			"call":{"opcode":"example_wait","next":"inc"},
			"inc":{"opcode":"control_incr_counter"}
		}`)
		r.threads[0].warp = true
		calls := 0
		if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
			return extension.Extension{ID: "example", Blocks: []extension.Block{{
				Opcode: "example_wait", Kind: "command", Handler: func(c *extension.Context) (extension.Result, error) {
					calls++
					if c.Local["started"] == nil {
						c.Local["started"] = true
						// Redraw stops a new scheduler pass, but cannot interrupt warp.
						r.redrawRequested = true
						return extension.Result{Again: true, Yield: true, YieldTick: waitTick}, nil
					}
					return extension.Result{}, nil
				},
			}}}
		}}); err != nil {
			t.Fatal(err)
		}
		run(t, r, 1, false)
		if waitTick {
			if calls != 1 || r.counter != 0 {
				t.Fatal("warp bypassed an explicit tick wait")
			}
			run(t, r, 1, false)
		}
		if calls != 2 || r.counter != 1 {
			t.Fatalf("warp did not resume: waitTick=%v calls=%d counter=%g", waitTick, calls, r.counter)
		}
	}
}

func TestNativeExtensionBranchYieldWaitsUntilNextTick(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"branch"},
		"branch":{"opcode":"example_branch","inputs":{"SUBSTACK":[2,"inc"]}},
		"inc":{"opcode":"control_incr_counter"}
	}`)
	if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "example", Blocks: []extension.Block{{
			Opcode: "example_branch", Kind: "branch",
			Handler: func(*extension.Context) (extension.Result, error) {
				return extension.Result{Branch: "SUBSTACK", YieldTick: true}, nil
			},
		}}}
	}}); err != nil {
		t.Fatal(err)
	}
	run(t, r, 1, false)
	if r.counter != 0 {
		t.Fatal("yielding branch resumed in the same tick")
	}
	run(t, r, 1, false)
	if r.counter != 1 || !r.Done() {
		t.Fatal("yielding branch did not resume on the next tick")
	}
}

func TestNativeExtensionReporterWaitPreservesEarlierInputs(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},
		"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"add"]}},
		"add":{"opcode":"operator_add","inputs":{"NUM1":[2,"first"],"NUM2":[2,"second"]}},
		"first":{"opcode":"example_first"},"second":{"opcode":"example_second"}
	}`)
	calls := 0
	err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "example", Blocks: []extension.Block{
			{Opcode: "example_first", Kind: "reporter", Handler: func(c *extension.Context) (extension.Result, error) {
				calls++
				return extension.Result{Value: float64(5)}, nil
			}},
			{Opcode: "example_second", Kind: "reporter", Handler: func(c *extension.Context) (extension.Result, error) {
				if c.Local["waiting"] == nil {
					c.Local["waiting"] = true
					return extension.Result{Again: true, YieldTick: true}, nil
				}
				return extension.Result{Value: float64(6)}, nil
			}},
		}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background(), 2, 60, false); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || r.stage.variables["score"].Value != float64(11) {
		t.Fatalf("reporter wait replayed inputs: calls=%d score=%v", calls, r.stage.variables["score"])
	}
}

func TestNativeExtensionReporterTickYieldKeepsCompletedValue(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},
		"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"reporter"]}},
		"reporter":{"opcode":"example_value"}
	}`)
	calls := 0
	if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "example", Blocks: []extension.Block{{
			Opcode: "example_value", Kind: "reporter", Handler: func(*extension.Context) (extension.Result, error) {
				calls++
				return extension.Result{Value: float64(7), YieldTick: true}, nil
			},
		}}}
	}}); err != nil {
		t.Fatal(err)
	}
	run(t, r, 1, true)
	if calls != 1 || r.stage.variables["score"].Value != float64(0) {
		t.Fatal("turbo bypassed the reporter's explicit tick wait")
	}
	run(t, r, 1, true)
	if calls != 1 || r.stage.variables["score"].Value != float64(7) {
		t.Fatal("completed reporter was called again or lost its value")
	}
}

func TestNativeExtensionDuplicateID(t *testing.T) {
	r := fixture(t, "{}")
	factory := func() extension.Extension { return extension.Extension{ID: "duplicate"} }
	if err := r.SetExtensions(extension.Bundle{factory, factory}); err == nil {
		t.Fatal("duplicate extension ID accepted")
	}
}

func TestNativeExtensionCloneDeletedCanReadIdentity(t *testing.T) {
	r := eventProject(t, cloneProject)
	var deletedID any
	calls := 0
	err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "cleanup", Events: map[string]extension.Handler{
			"clone-deleted": func(c *extension.Context) (extension.Result, error) {
				calls++
				var err error
				deletedID, err = c.Call("target.get", "id")
				if err != nil {
					return extension.Result{}, err
				}
				value, err := c.Call("variable.get", "local", deletedID)
				if err != nil {
					return extension.Result{}, err
				}
				if value != float64(109) {
					t.Errorf("deleted clone variable = %v", value)
				}
				if _, err := c.Call("target.set", "x", 10, deletedID); err == nil {
					t.Error("deleted clone remained writable")
				}
				if _, err := c.Call("target.delete"); err == nil {
					t.Error("deleted clone remained deletable")
				}
				return extension.Result{}, nil
			},
		}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	run(t, r, 1, false)
	clone := r.targets[2]
	wantID := logicTargetID(clone)
	r.deleteClone(clone)
	if r.extensionErr != nil {
		t.Fatal(r.extensionErr)
	}
	if calls != 1 || deletedID != wantID {
		t.Fatalf("cleanup calls=%d, target=%v, want %s", calls, deletedID, wantID)
	}
	host := &extensionContext{r: r, th: &thread{target: r.stage}}
	if _, err := host.Call("target.get", "id", deletedID); err == nil {
		t.Fatal("deleted target handle remains valid outside callback")
	}
}

func TestNativeExtensionBranchAndStateIsolation(t *testing.T) {
	program := `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"branch"},
		"branch":{"opcode":"example_repeat","inputs":{"SUBSTACK":[2,"inc"]}},
		"inc":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
	}`
	bundle := extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "example", Blocks: []extension.Block{{
			Opcode: "example_repeat", Kind: "branch",
			Handler: func(c *extension.Context) (extension.Result, error) {
				count := extension.Number(c.Local["count"])
				if count == 3 {
					return extension.Result{}, nil
				}
				c.Local["count"] = count + 1
				c.State["visits"] = extension.Number(c.State["visits"]) + 1
				return extension.Result{Branch: "SUBSTACK", Again: true}, nil
			},
		}}}
	}}
	a, b := fixture(t, program), fixture(t, program)
	for _, r := range []*Runtime{a, b} {
		if err := r.SetExtensions(bundle); err != nil {
			t.Fatal(err)
		}
	}
	run(t, a, 5, false)
	if a.stage.variables["score"].Value != float64(3) {
		t.Fatal("branch did not repeat exactly three times")
	}
	if b.extensions[0].State["visits"] != nil {
		t.Fatal("extension state leaked into another runtime")
	}
	a.GreenFlag()
	run(t, a, 5, false)
	if a.stage.variables["score"].Value != float64(6) {
		t.Fatal("branch invocation state survived restart")
	}
}

func TestBuiltinExtensionNamespacesDoNotHideSimilarIDs(t *testing.T) {
	for _, id := range []string{"motion", "looks", "sound", "event", "control", "sensing", "operator", "data", "procedures", "argument", "pen"} {
		t.Run(id, func(t *testing.T) {
			r := New(&project.Project{})
			if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
				return extension.Extension{ID: id}
			}}); err == nil {
				t.Fatal("reserved namespace was accepted")
			}
			custom := id + "Extra"
			if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
				return extension.Extension{ID: custom, Blocks: []extension.Block{{Opcode: custom + "_value", Kind: "reporter", Handler: func(*extension.Context) (extension.Result, error) {
					return extension.Result{Value: 42.0}, nil
				}}}}
			}}); err != nil {
				t.Fatal(err)
			}
			if _, handled, err := r.invokeExtension(context.Background(), nil, &project.Block{Opcode: id + "_value"}, nil, true, 0, nil); handled || err != nil {
				t.Fatalf("builtin reached extension dispatch: %v", err)
			}
			budget := stepsPerFrame
			value, handled, err := r.invokeExtension(context.Background(), &thread{}, &project.Block{Opcode: custom + "_value"}, nil, true, 0, &budget)
			if err != nil || !handled || value.Value != 42.0 {
				t.Fatalf("similar custom ID lost: %v, %v, %v", value, handled, err)
			}
		})
	}
}

func constantExtensionRuntime(t testing.TB) (*Runtime, *thread, *project.Block) {
	t.Helper()
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true}}})
	if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "example", Blocks: []extension.Block{{
			Opcode: "example_values", Kind: "command", Inputs: []string{"A", "B", "C"},
			Handler: func(c *extension.Context) (extension.Result, error) {
				return extension.Result{Value: c.Args["A"]}, nil
			},
		}}}
	}}); err != nil {
		t.Fatal(err)
	}
	block := &project.Block{Opcode: "example_values", Inputs: literalInputs(map[string]any{
		"A": "1", "B": "2", "C": "3",
	}), Fields: map[string][]any{"MODE": {"fixed"}}}
	return r, &thread{target: r.stage, expression: newExpression("call")}, block
}

func listSearchExtension(t testing.TB) (*Runtime, *thread, *project.Block) {
	r, th := listSearchRuntime("data_itemnumoflist")
	if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "example", Blocks: []extension.Block{{
			Opcode: "example_search", Kind: "reporter", Inputs: []string{"INDEX"}, BorrowContext: true,
			Handler: func(c *extension.Context) (extension.Result, error) {
				return extension.Result{Value: c.Args["INDEX"]}, nil
			},
		}}}
	}}); err != nil {
		t.Fatal(err)
	}
	block := &project.Block{Opcode: "example_search", Inputs: map[string][]any{"INDEX": {float64(2), "search"}}}
	r.stage.source.Blocks["call"] = block
	return r, th, block
}

func TestExtensionListSearchInputsAvoidContinuations(t *testing.T) {
	r, th, block := listSearchExtension(t)
	reads := 0
	for _, tc := range []struct {
		needle string
		index  float64
	}{{"02", 2}, {"RED", 3}, {"missing", 0}} {
		th.params["needle"] = calculationValue{value: tc.needle}
		th.expression.reset("call")
		budget := stepsPerFrame
		got, _, err := r.invokeExtension(context.Background(), th, block, func(name string) (any, error) {
			reads++
			return r.input(context.Background(), th, block, name, 0, &budget)
		}, true, 0, &budget)
		if err != nil || got.Value != tc.index || budget != stepsPerFrame-2 {
			t.Fatalf("result=%v err=%v", got, err)
		}
	}
	if reads != 0 || th.expression.root.children != nil {
		t.Fatalf("synchronous list inputs used continuation evaluation: %d reads", reads)
	}
}

func TestExtensionListSearchInputLimits(t *testing.T) {
	for _, budgetLimit := range []int{2, 3} {
		r, th, _ := listSearchExtension(t)
		budget := budgetLimit
		value, err := r.eval(context.Background(), th, "call", 0, &budget)
		if budgetLimit == 2 {
			if err == nil || !strings.Contains(err.Error(), "evaluation limit") {
				t.Fatalf("limit bypassed: %v", err)
			}
		} else if err != nil || value != float64(2) || budget != 0 {
			t.Fatalf("value=%v budget=%d err=%v", value, budget, err)
		}
	}
}

func TestExtensionListSearchInputSurvivesYield(t *testing.T) {
	r, th, block := listSearchExtension(t)
	registered := r.extensionBlocks[block.Opcode]
	registered.block.BorrowContext = false
	registered.block.Handler = func(c *extension.Context) (extension.Result, error) {
		if c.Local["waiting"] == nil {
			c.Local["waiting"] = true
			return extension.Result{Again: true, YieldTick: true}, nil
		}
		return extension.Result{Value: c.Args["INDEX"]}, nil
	}
	budget := stepsPerFrame
	_, err := r.eval(context.Background(), th, "call", 0, &budget)
	if !errors.Is(err, errExtensionYield) {
		t.Fatalf("expected suspended reporter, got %v", err)
	}
	list := r.stage.state.Lists["items"]
	list.replace(1, "removed")
	r.stage.state.Lists["items"] = list
	r.frame++
	budget = stepsPerFrame
	value, err := r.eval(context.Background(), th, "call", 0, &budget)
	if err != nil || value != float64(2) {
		t.Fatalf("suspended reporter reread its list input: value=%v err=%v", value, err)
	}
}

func BenchmarkExtensionListSearchInputs(b *testing.B) {
	r, th, _ := listSearchExtension(b)
	b.ReportAllocs()
	for b.Loop() {
		th.expression.reset("test")
		budget := stepsPerFrame
		got, err := r.eval(context.Background(), th, "call", 0, &budget)
		if err != nil || got != float64(2) || budget != stepsPerFrame-3 {
			b.Fatalf("result=%v budget=%d err=%v", got, budget, err)
		}
	}
}

func TestExtensionConstantInputsPreserveIndependentContexts(t *testing.T) {
	r, th, block := constantExtensionRuntime(t)
	var saved []*extension.Context
	r.extensionBlocks[block.Opcode].block.Handler = func(c *extension.Context) (extension.Result, error) {
		if c.Args["A"] != "1" || c.Field("MODE") != "fixed" || len(c.Local) != 0 {
			t.Fatal("an earlier invocation changed a later context")
		}
		saved = append(saved, c)
		c.Args["A"], c.Fields["MODE"], c.Local["saved"] = "changed", "changed", true
		return extension.Result{}, nil
	}
	reads := 0
	for range 3 {
		th.expression.reset("call")
		budget := stepsPerFrame
		_, _, err := r.invokeExtension(context.Background(), th, block, func(name string) (any, error) {
			reads++
			return r.input(context.Background(), th, block, name, 0, &budget)
		}, false, 0, &budget)
		if err != nil {
			t.Fatal(err)
		}
	}
	if reads != 0 {
		t.Fatalf("immutable literals went through input evaluation %d times", reads)
	}
	if saved[0] == saved[1] || saved[1] == saved[2] {
		t.Fatal("extension contexts were reused while retained by user code")
	}
}

func BenchmarkExtensionConstantInputs(b *testing.B) {
	r, th, block := constantExtensionRuntime(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		th.expression.reset("call")
		budget := stepsPerFrame
		result, handled, err := r.invokeExtension(context.Background(), th, block, func(name string) (any, error) {
			return r.input(ctx, th, block, name, 0, &budget)
		}, false, 0, &budget)
		if err != nil || !handled || result.Value != "1" {
			b.Fatalf("result=%v, handled=%t, error=%v", result, handled, err)
		}
	}
}

func TestExtensionUnfinishedInputsDoNotAllocateContext(t *testing.T) {
	for _, failure := range []error{errExtensionYield, context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			r, th, block := constantExtensionRuntime(t)
			block.Inputs["C"] = []any{float64(2), "pending"}
			r.extensionBlocks[block.Opcode].block.Handler = func(*extension.Context) (extension.Result, error) {
				t.Fatal("handler ran before its inputs completed")
				return extension.Result{}, nil
			}
			input := func(name string) (any, error) {
				if name != "C" {
					t.Fatalf("unexpected input %s", name)
				}
				return nil, failure
			}
			allocs := testing.AllocsPerRun(100, func() {
				budget := stepsPerFrame
				_, handled, err := r.invokeExtension(context.Background(), th, block, input, false, 0, &budget)
				if !handled || !errors.Is(err, failure) {
					t.Fatalf("unfinished input: handled=%v, error=%v", handled, err)
				}
			})
			if allocs != 0 {
				t.Fatalf("unfinished inputs allocated %g objects before a handler could run", allocs)
			}
		})
	}
}

func BenchmarkExtensionUnfinishedInputs(b *testing.B) {
	r, th, block := constantExtensionRuntime(b)
	block.Inputs["C"] = []any{float64(2), "pending"}
	input := func(string) (any, error) {
		return nil, errExtensionYield
	}
	b.ReportAllocs()
	for b.Loop() {
		budget := stepsPerFrame
		_, _, err := r.invokeExtension(context.Background(), th, block, input, false, 0, &budget)
		if !errors.Is(err, errExtensionYield) {
			b.Fatal(err)
		}
	}
}

func BenchmarkExtensionNestedInputs(b *testing.B) {
	r, th, block := constantExtensionRuntime(b)
	r.extensionBlocks["example_value"] = &registeredBlock{
		owner: r.extensionBlocks[block.Opcode].owner,
		block: extension.Block{
			Opcode: "example_value", Kind: "reporter",
			Handler: func(*extension.Context) (extension.Result, error) {
				return extension.Result{Value: "1"}, nil
			},
		},
	}
	r.stage.source.Blocks = project.Blocks{
		"value": {Opcode: "example_value"},
	}
	for _, name := range []string{"A", "B", "C"} {
		block.Inputs[name] = []any{float64(2), "value"}
	}
	b.ReportAllocs()
	for b.Loop() {
		th.expression.reset("call")
		budget := stepsPerFrame
		result, handled, err := r.invokeExtension(context.Background(), th, block, func(name string) (any, error) {
			return r.input(context.Background(), th, block, name, 0, &budget)
		}, false, 0, &budget)
		if err != nil || !handled || result.Value != "1" {
			b.Fatalf("nested inputs: result=%v handled=%v error=%v", result, handled, err)
		}
	}
}

func TestExtensionEvaluatedInputsRemainIndependent(t *testing.T) {
	for _, count := range []int{1, 15, 16, 17, 40} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			r, th, block := constantExtensionRuntime(t)
			registered := r.extensionBlocks[block.Opcode]
			registered.block.Inputs = nil
			for i := range count {
				name := strconv.Itoa(i)
				registered.block.Inputs = append(registered.block.Inputs, name)
				block.Inputs[name] = []any{float64(2), "dynamic"}
			}
			// Duplicate names are evaluated in order; the last value wins.
			registered.block.Inputs = append(registered.block.Inputs, "0")
			var saved []*extension.Context
			registered.block.Handler = func(c *extension.Context) (extension.Result, error) {
				saved = append(saved, c)
				return extension.Result{}, nil
			}
			reads := 0
			input := func(string) (any, error) {
				reads++
				return float64(reads), nil
			}
			for range 2 {
				budget := stepsPerFrame
				if _, _, err := r.invokeExtension(context.Background(), th, block, input, false, 0, &budget); err != nil {
					t.Fatal(err)
				}
			}
			if reads != 2*(count+1) || len(saved) != 2 {
				t.Fatalf("input reads=%d, invocations=%d", reads, len(saved))
			}
			for call, c := range saved {
				if len(c.Args) != count || c.Args["0"] != float64((call+1)*(count+1)) {
					t.Fatalf("invocation %d lost its arguments: %v", call, c.Args)
				}
				for i := 1; i < count; i++ {
					if got, want := c.Args[strconv.Itoa(i)], float64(call*(count+1)+i+1); got != want {
						t.Fatalf("invocation %d argument %d: got %v, want %v", call, i, got, want)
					}
				}
			}
		})
	}
}

func TestExtensionPreparedInputsReadLiveValues(t *testing.T) {
	r, th, block := constantExtensionRuntime(t)
	block.Inputs["B"] = []any{float64(1), []any{float64(12), "value", "value"}}
	block.Inputs["C"] = []any{float64(3), nil, []any{float64(10), "shadow"}}
	r.extensionBlocks[block.Opcode].block.Handler = func(c *extension.Context) (extension.Result, error) {
		if c.Args["A"] != "1" || c.Args["C"] != "shadow" {
			t.Fatal("literal or fallback shadow changed")
		}
		return extension.Result{Value: c.Args["B"]}, nil
	}
	for _, value := range []any{float64(7), "changed", false} {
		r.stage.variables["value"] = &Variable{Value: value}
		th.expression.reset("call")
		budget := stepsPerFrame
		result, _, err := r.invokeExtension(context.Background(), th, block, func(name string) (any, error) {
			return r.input(context.Background(), th, block, name, 0, &budget)
		}, false, 0, &budget)
		if err != nil || result.Value != value {
			t.Fatalf("live input = %v, want %v; error = %v", result.Value, value, err)
		}
	}
}

func BenchmarkExtensionDynamicInputs(b *testing.B) {
	r, th, block := constantExtensionRuntime(b)
	for _, name := range []string{"A", "B", "C"} {
		block.Inputs[name] = []any{float64(1), []any{float64(12), name, name}}
		r.stage.variables[name] = &Variable{Value: "1"}
	}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		th.expression.reset("call")
		budget := stepsPerFrame
		result, handled, err := r.invokeExtension(context.Background(), th, block, func(name string) (any, error) {
			return r.input(ctx, th, block, name, 0, &budget)
		}, false, 0, &budget)
		if err != nil || !handled || result.Value != "1" {
			b.Fatalf("result=%v, handled=%t, error=%v", result, handled, err)
		}
	}
}

func dynamicExtensionReporter(t testing.TB) (*Runtime, *thread, *project.Block) {
	t.Helper()
	r, th, block := constantExtensionRuntime(t)
	r.extensionBlocks[block.Opcode].block.Kind = "reporter"
	r.stage.source.Blocks = map[string]*project.Block{"value": block}
	for _, name := range []string{"A", "B", "C"} {
		block.Inputs[name] = []any{float64(1), []any{float64(12), name, name}}
		r.stage.variables[name] = &Variable{Value: "1"}
	}
	return r, th, block
}

func TestExtensionPureReporterInputsAvoidContinuations(t *testing.T) {
	r, th, _ := dynamicExtensionReporter(t)
	allocs := testing.AllocsPerRun(100, func() {
		th.expression = newExpression("call")
		budget := stepsPerFrame
		value, err := r.eval(context.Background(), th, "value", 0, &budget)
		if err != nil || value != "1" {
			t.Fatalf("reporter = %v, %v", value, err)
		}
	})
	if allocs > 7 {
		t.Fatalf("synchronous reporter inputs allocated resumable state: %g allocations", allocs)
	}
}

func BenchmarkExtensionReporterInputs(b *testing.B) {
	r, th, _ := dynamicExtensionReporter(b)
	b.ReportAllocs()
	for b.Loop() {
		th.expression.reset("call")
		budget := stepsPerFrame
		value, err := r.eval(context.Background(), th, "value", 0, &budget)
		if err != nil || value != "1" {
			b.Fatalf("reporter = %v, %v", value, err)
		}
	}
}

func TestExtensionReporterInputLimits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		depth  int
		budget int
		error  string
	}{
		{name: "enough budget", budget: 2},
		{name: "budget exhausted", budget: 1, error: "evaluation limit"},
		{name: "last depth", depth: 127, budget: 2},
		{name: "depth exhausted", depth: 128, budget: 2, error: "nesting limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, th, block := dynamicExtensionReporter(t)
			block.Inputs["A"] = []any{float64(2), "sum"}
			r.stage.source.Blocks["sum"] = &project.Block{Opcode: "operator_add", Inputs: literalInputs(map[string]any{"NUM1": "2", "NUM2": "3"})}
			called := false
			r.extensionBlocks[block.Opcode].block.Handler = func(c *extension.Context) (extension.Result, error) {
				called = true
				return extension.Result{Value: c.Args["A"]}, nil
			}
			budget := tc.budget
			value, err := r.eval(context.Background(), th, "value", tc.depth, &budget)
			if tc.error != "" {
				if err == nil || !strings.Contains(err.Error(), tc.error) || called {
					t.Fatalf("limit bypassed: handler=%t, error=%v", called, err)
				}
			} else if err != nil || value != float64(5) || !called || budget != 0 {
				t.Fatalf("reporter=%v, error=%v, handler=%t, remaining budget=%d", value, err, called, budget)
			}
		})
	}
}

func TestExtensionReporterRetainsInputsAcrossYield(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},
		"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"read"]}},
		"read":{"opcode":"example_read","inputs":{"A":[1,[12,"score","score"]]}}
	}`)
	err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "example", Blocks: []extension.Block{{
			Opcode: "example_read", Kind: "reporter", Inputs: []string{"A"},
			Handler: func(c *extension.Context) (extension.Result, error) {
				if c.Local["waiting"] == nil {
					c.Local["waiting"] = true
					return extension.Result{Again: true, YieldTick: true}, nil
				}
				return extension.Result{Value: c.Args["A"]}, nil
			},
		}}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	r.stage.variables["score"].Value = float64(5)
	run(t, r, 1, false)
	if r.Done() {
		t.Fatal("reporter did not yield")
	}
	r.stage.variables["score"].Value = float64(99)
	run(t, r, 1, false)
	if value := r.stage.variables["score"].Value; value != float64(5) || !r.Done() {
		t.Fatalf("reporter reread an input after yielding: value=%v, done=%t", value, r.Done())
	}
}

func TestExtensionPureInputsAvoidContinuations(t *testing.T) {
	r, th, block := constantExtensionRuntime(t)
	block.Inputs["B"] = []any{float64(1), []any{float64(12), "value", "value"}}
	r.extensionBlocks[block.Opcode].block.Handler = func(c *extension.Context) (extension.Result, error) {
		return extension.Result{Value: c.Args["B"]}, nil
	}
	reads := 0
	for _, value := range []any{float64(7), "changed", false} {
		r.stage.variables["value"] = &Variable{Value: value}
		th.expression.reset("call")
		budget := stepsPerFrame
		result, _, err := r.invokeExtension(context.Background(), th, block, func(name string) (any, error) {
			reads++
			return r.input(context.Background(), th, block, name, 0, &budget)
		}, false, 0, &budget)
		if err != nil || result.Value != value {
			t.Fatalf("live input = %v, want %v; error = %v", result.Value, value, err)
		}
	}
	if reads != 0 || th.expression.root.children != nil {
		t.Fatal("pure command inputs created interpreter continuations")
	}
}

func TestExtensionCommandKeepsInputsAcrossYield(t *testing.T) {
	for _, mode := range []struct{ inputYields, borrowed bool }{{false, false}, {true, false}, {true, true}} {
		inputYields := mode.inputYields
		r := fixture(t, `{
			"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"call"},
			"call":{"opcode":"example_command","inputs":{"A":[1,[12,"score","score"]],"B":[1,[4,"0"]]}},
			"wait":{"opcode":"example_wait"}
		}`)
		if inputYields {
			r.stage.source.Blocks["call"].Inputs["B"] = []any{float64(2), "wait"}
		}
		got, calls := any(nil), 0
		err := r.SetExtensions(extension.Bundle{func() extension.Extension {
			return extension.Extension{ID: "example", Blocks: []extension.Block{
				{Opcode: "example_command", Kind: "command", Inputs: []string{"A", "B"}, BorrowContext: mode.borrowed,
					Handler: func(c *extension.Context) (extension.Result, error) {
						if !inputYields && c.Local["waiting"] == nil {
							c.Local["waiting"] = true
							return extension.Result{Again: true, YieldTick: true}, nil
						}
						got = c.Args["A"]
						calls++
						return extension.Result{}, nil
					}},
				{Opcode: "example_wait", Kind: "reporter",
					Handler: func(c *extension.Context) (extension.Result, error) {
						if c.Local["waiting"] == nil {
							c.Local["waiting"] = true
							return extension.Result{Again: true, YieldTick: true}, nil
						}
						return extension.Result{Value: float64(0)}, nil
					}},
			}}
		}})
		if err != nil {
			t.Fatal(err)
		}
		*r.stage.variables["score"] = Variable{Value: float64(5)}
		run(t, r, 1, false)
		if calls != 0 {
			t.Fatal("extension did not wait for the next tick")
		}
		*r.stage.variables["score"] = Variable{Value: float64(99)}
		run(t, r, 1, false)
		if calls != 1 || got != float64(5) {
			t.Fatalf("inputYields=%v calls=%d value=%v: inputs changed across yield", inputYields, calls, got)
		}
	}
}

func TestExtensionCallAllocationLimit(t *testing.T) {
	r, th, block := constantExtensionRuntime(t)
	allocs := testing.AllocsPerRun(100, func() {
		budget := stepsPerFrame
		result, handled, err := r.invokeExtension(context.Background(), th, block, nil, false, 0, &budget)
		if err != nil || !handled || result.Value != "1" {
			t.Fatalf("extension call: %+v, %t, %v", result, handled, err)
		}
	})
	if allocs > 6 {
		t.Fatalf("extension calls repeatedly allocate a host for the same thread: %g allocations", allocs)
	}
}

func TestExtensionRetainedHostKeepsItsThread(t *testing.T) {
	r, first, block := constantExtensionRuntime(t)
	first.target.state.Name = "first"
	second := &thread{target: &target{owner: r, state: TargetState{Name: "second"}, source: first.target.source}}
	var hosts []extension.Host
	r.extensionBlocks[block.Opcode].block.Handler = func(c *extension.Context) (extension.Result, error) {
		hosts = append(hosts, c.Host)
		return extension.Result{}, nil
	}
	for _, th := range []*thread{first, second, first} {
		budget := stepsPerFrame
		if _, _, err := r.invokeExtension(context.Background(), th, block, nil, false, 0, &budget); err != nil {
			t.Fatal(err)
		}
	}
	for i, want := range []string{"first", "second", "first"} {
		got, err := hosts[i].Call("target.get", "name")
		if err != nil || got != want {
			t.Fatalf("retained host %d: %v, %v; want %s", i, got, err, want)
		}
	}
}

func TestExtensionCanceledPreparedInputsDoNotAllocateContext(t *testing.T) {
	r, th, block := constantExtensionRuntime(t)
	block.Inputs["C"] = []any{float64(1), []any{float64(12), "value", "value"}}
	r.stage.variables["value"] = &Variable{Value: float64(1)}
	r.extensionBlocks[block.Opcode].block.Handler = func(*extension.Context) (extension.Result, error) {
		t.Fatal("canceled input reached handler")
		return extension.Result{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	allocs := testing.AllocsPerRun(100, func() {
		budget := stepsPerFrame
		_, handled, err := r.invokeExtension(ctx, th, block, nil, false, 0, &budget)
		if !handled || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled input: handled=%v, error=%v", handled, err)
		}
	})
	if allocs != 0 {
		t.Fatalf("canceled prepared inputs allocated %g objects before a handler could run", allocs)
	}
}

func TestExtensionCompletedCallMemory(t *testing.T) {
	r, th, block := constantExtensionRuntime(t)
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			budget := stepsPerFrame
			value, handled, err := r.invokeExtension(context.Background(), th, block, nil, false, 0, &budget)
			if err != nil || !handled || value.Value != "1" {
				b.Fatalf("completed call: %+v, %v, %v", value, handled, err)
			}
		}
	})
	if bytes := result.AllocedBytesPerOp(); bytes > 768 {
		t.Fatalf("completed call allocated %d bytes; want at most 768", bytes)
	}
}
