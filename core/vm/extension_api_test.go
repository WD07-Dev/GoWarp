package vm

import (
	"errors"
	"testing"

	"gowarp/core/extension"
	splitblocks "gowarp/test/extensions/edgetalesplit"
	textblocks "gowarp/test/extensions/text"
)

func TestExtensionAPIRegistrationRoutesCallsAndCleansUpAfterDisposeError(t *testing.T) {
	r := fixture(t, `{}`)
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	closed := false
	hostAPIs["testapi"] = extensionAPI{
		call: func(context *extensionContext, operation string, args []any) (any, error) {
			if context.r != r || operation != "testapi.echo" || len(args) != 1 {
				return nil, errors.New("incorrect API dispatch")
			}
			return args[0], nil
		},
		close: func(runtime *Runtime) {
			if runtime != r {
				t.Error("closed another runtime")
			}
			closed = true
		},
	}
	t.Cleanup(func() { delete(hostAPIs, "testapi") })
	value, err := c.Call("testapi.echo", "value")
	if err != nil || value != "value" {
		t.Fatalf("registered API: %v, %v", value, err)
	}
	if _, err := c.Call("testapiExtra.echo"); err == nil {
		t.Fatal("namespace prefix matched a different API")
	}
	if value, err := c.Call("host.fullscreen"); err != nil || value != false {
		t.Fatalf("existing host API: %v, %v", value, err)
	}
	sentinel := errors.New("dispose error")
	r.extensions = append(r.extensions, &extension.Extension{
		ID: "testcleanup",
		Events: map[string]extension.Handler{
			"dispose": func(*extension.Context) (extension.Result, error) {
				return extension.Result{}, sentinel
			},
		},
	})
	if err := r.CloseExtensions(); !errors.Is(err, sentinel) {
		t.Fatalf("dispose error was lost: %v", err)
	}
	if !closed {
		t.Fatal("registered API cleanup was skipped after extension error")
	}
}

func TestExtensionAPINestedServices(t *testing.T) {
	r := fixture(t, `{}`)
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	for _, name := range []string{"testnested", "testnested.child", "testnested.exact"} {
		hostAPIs[name] = extensionAPI{call: func(_ *extensionContext, _ string, _ []any) (any, error) {
			return name, nil
		}}
		t.Cleanup(func() { delete(hostAPIs, name) })
	}
	for _, tc := range []struct{ operation, service string }{
		{"testnested.echo", "testnested"},
		{"testnested.child.echo", "testnested.child"},
		{"testnested.child.deep.echo", "testnested.child"},
		{"testnested.childExtra.echo", "testnested"},
		{"testnested.exact", "testnested.exact"},
	} {
		got, err := c.Call(tc.operation)
		if err != nil || got != tc.service {
			t.Errorf("%s: got %v, %v; want %s", tc.operation, got, err, tc.service)
		}
	}
	if _, err := c.Call("testnestedExtra.echo"); err == nil {
		t.Fatal("matched a different namespace")
	}
}

func TestExtensionGraphicsUseGenericHostServices(t *testing.T) {
	r := fixture(t, "{}")
	host := &extensionContext{r: r, th: &thread{target: r.stage}}
	context := &extension.Context{Host: host, State: map[string]any{}}
	run := func(def extension.Extension, opcode string, args map[string]any) {
		t.Helper()
		context.Args = args
		for _, block := range def.Blocks {
			if block.Opcode == opcode {
				if _, err := block.Handler(context); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("missing block %s", opcode)
	}
	run(textblocks.New(), "text_setText", map[string]any{"TEXT": "hello"})
	if text := r.stage.state.Text; text == nil || text.Text != "hello" || text.FontSize != 24 {
		t.Fatalf("extension text layout=%+v", text)
	}
	run(splitblocks.New(), "edgetalesplit_setOriginalVisibility", map[string]any{"MODE": "show"})
	run(splitblocks.New(), "edgetalesplit_addVerticalSplit", map[string]any{"X": 12, "OFFSET": 10, "DIRECTION": "horizontal"})
	parts := r.SplitEffects()
	if len(parts) != 1 || !r.SplitShowsOriginal() {
		t.Fatalf("expected one split with original visible: %v", parts)
	}
	geometry := parts[0].Project(480, 360, 1)
	if geometry.MoveX != 10 || geometry.TopX != 252 {
		t.Fatalf("extension split geometry=%+v", geometry)
	}
}

func TestExtensionCallsUseCalleeStateAndCallerTarget(t *testing.T) {
	r := fixture(t, `{}`)
	err := r.SetExtensions(extension.Bundle{
		func() extension.Extension {
			return extension.Extension{ID: "caller", State: map[string]any{"calls": 99}, Blocks: []extension.Block{{
				Opcode: "caller_read", Kind: "reporter", Handler: func(c *extension.Context) (extension.Result, error) {
					value, err := c.Call("extensions.call", "other_read", c.Args, c.Fields)
					if err != nil {
						return extension.Result{}, err
					}
					return value.(extension.Result), nil
				},
			}}}
		},
		func() extension.Extension {
			return extension.Extension{ID: "other", State: map[string]any{"calls": 0}, Blocks: []extension.Block{{
				Opcode: "other_read", Kind: "reporter", Handler: func(c *extension.Context) (extension.Result, error) {
					c.State["calls"] = c.State["calls"].(int) + 1
					name, err := c.Call("target.get", "name")
					value := c.String("VALUE") + c.Field("MODE") + extension.String(name)
					c.Args["VALUE"], c.Fields["MODE"] = "changed", "changed"
					c.Local["scratch"] = true
					return extension.Result{Value: value}, err
				},
			}}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	args, fields := map[string]any{"VALUE": "a"}, map[string]string{"MODE": "b"}
	for _, name := range []string{"original", "clone"} {
		host := &extensionContext{r: r, th: &thread{target: &target{owner: r, source: r.stage.source, state: TargetState{Name: name}}}}
		for id, want := range map[string]bool{"caller": true, "other": true, "missing": false} {
			if got, err := host.Call("extensions.loaded", id); err != nil || got != want {
				t.Fatalf("loaded %s: %v, %v", id, got, err)
			}
		}
		value, err := host.Call("extensions.call", "caller_read", args, fields)
		if err != nil {
			t.Fatal(err)
		}
		if got := value.(extension.Result).Value; got != "ab"+name {
			t.Fatalf("callee target/inputs: %v", got)
		}
	}
	if args["VALUE"] != "a" || fields["MODE"] != "b" {
		t.Fatal("callee changed caller inputs")
	}
	if r.extensions[0].State["calls"] != 99 || r.extensions[1].State["calls"] != 2 {
		t.Fatal("calls did not share the callee's state")
	}
}

func TestExtensionCallsResumeWithinReporter(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},
		"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"read"]}},
		"read":{"opcode":"caller_read","inputs":{"A":[1,[12,"score","score"]]}}
	}`)
	calls := 0
	err := r.SetExtensions(extension.Bundle{
		func() extension.Extension {
			return extension.Extension{ID: "caller", Blocks: []extension.Block{{
				Opcode: "caller_read", Kind: "reporter", Inputs: []string{"A"}, Handler: func(c *extension.Context) (extension.Result, error) {
					local, ok := c.Local["child"].(map[string]any)
					if !ok {
						local = map[string]any{}
						c.Local["child"] = local
					}
					value, err := c.Call("extensions.call", "other_wait", c.Args, nil, local)
					if err != nil {
						return extension.Result{}, err
					}
					return value.(extension.Result), nil
				},
			}}}
		},
		func() extension.Extension {
			return extension.Extension{ID: "other", Blocks: []extension.Block{{
				Opcode: "other_wait", Kind: "reporter", Inputs: []string{"A"}, Handler: func(c *extension.Context) (extension.Result, error) {
					calls++
					if c.Local["waiting"] == nil {
						c.Local["waiting"] = true
						return extension.Result{Again: true, YieldTick: true}, nil
					}
					return extension.Result{Value: c.Args["A"]}, nil
				},
			}}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.stage.variables["score"].Value = float64(5)
	run(t, r, 1, false)
	if r.Done() || calls != 1 {
		t.Fatalf("child did not wait: calls=%d done=%t", calls, r.Done())
	}
	r.stage.variables["score"].Value = float64(99)
	run(t, r, 1, false)
	if got := r.stage.variables["score"].Value; got != float64(5) || !r.Done() || calls != 2 {
		t.Fatalf("resumed reporter: value=%v done=%t calls=%d", got, r.Done(), calls)
	}
}

func TestExtensionCallsRejectInvalidCalls(t *testing.T) {
	r := fixture(t, `{}`)
	sentinel := errors.New("callee failed")
	err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		blocks := []extension.Block{
			{Opcode: "other_error", Kind: "command", Handler: func(*extension.Context) (extension.Result, error) { return extension.Result{}, sentinel }},
			{Opcode: "other_recurse", Kind: "command", Handler: func(c *extension.Context) (extension.Result, error) {
				_, err := c.Call("extensions.call", "other_recurse")
				return extension.Result{}, err
			}},
			{Opcode: "other_wait", Kind: "command", Handler: func(*extension.Context) (extension.Result, error) {
				return extension.Result{Again: true, YieldTick: true}, nil
			}},
			{Opcode: "other_borrow", Kind: "command", BorrowContext: true, Handler: func(*extension.Context) (extension.Result, error) { return extension.Result{Yield: true}, nil }},
			{Opcode: "other_branchResult", Kind: "command", Handler: func(*extension.Context) (extension.Result, error) { return extension.Result{Branch: "SUBSTACK"}, nil }},
		}
		for _, kind := range []string{"hat", "branch", "terminal"} {
			blocks = append(blocks, extension.Block{Opcode: "other_" + kind, Kind: kind, Handler: func(*extension.Context) (extension.Result, error) {
				t.Fatal("unsupported handler executed")
				return extension.Result{}, nil
			}})
		}
		return extension.Extension{ID: "other", Blocks: blocks}
	}})
	if err != nil {
		t.Fatal(err)
	}
	host := &extensionContext{r: r, th: &thread{target: r.stage}}
	for _, args := range [][]any{
		nil, {"missing_block"}, {"other_wait"}, {"other_wait", nil, nil, map[string]any(nil)},
		{"other_wait", "not a map"}, {"other_wait", nil, "not fields"}, {"other_wait", nil, nil, "not local"},
		{"other_wait", nil, nil, nil, "extra"}, {"other_borrow"}, {"other_branchResult"},
		{"other_hat"}, {"other_branch"}, {"other_terminal"}, {"other_recurse"},
	} {
		if _, err := host.Call("extensions.call", args...); err == nil {
			t.Errorf("accepted invalid call: %v", args)
		}
	}
	if _, err := host.Call("extensions.call", "other_error"); !errors.Is(err, sentinel) {
		t.Fatalf("lost callee error: %v", err)
	}
	host.event = "tick"
	if _, err := host.Call("extensions.call", "other_wait", nil, nil, map[string]any{}); err == nil {
		t.Fatal("event handler called a waiting block")
	}
}
