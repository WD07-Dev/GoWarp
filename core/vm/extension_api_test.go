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
