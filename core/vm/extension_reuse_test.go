package vm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gowarp/core/extension"
	"gowarp/core/project"
)

func TestBorrowedExtensionAllocations(t *testing.T) {
	r, th, block := constantExtensionRuntime(t)
	r.extensionBlocks[block.Opcode].block.BorrowContext = true
	allocs := testing.AllocsPerRun(100, func() {
		budget := stepsPerFrame
		result, handled, err := r.invokeExtension(context.Background(), th, block, nil, false, 0, &budget)
		if err != nil || !handled || result.Value != "1" {
			t.Fatalf("borrowed call: %+v, %v, %v", result, handled, err)
		}
	})
	if allocs != 0 {
		t.Fatalf("borrowed call allocated %g objects", allocs)
	}
}

func TestBorrowedExtensionResetsOwnedMaps(t *testing.T) {
	r, th, block := constantExtensionRuntime(t)
	registered := r.extensionBlocks[block.Opcode]
	registered.block.BorrowContext = true
	externalArgs := map[string]any{"keep": true}
	externalFields := map[string]string{"keep": "true"}
	externalLocal := map[string]any{"keep": true}
	failure := errors.New("handler failed")
	calls := 0
	registered.block.Handler = func(c *extension.Context) (extension.Result, error) {
		if c.Args["A"] != "1" || len(c.Args) != 3 || c.Field("MODE") != "fixed" || len(c.Local) != 0 {
			t.Fatal("previous call leaked into borrowed storage")
		}
		calls++
		c.Args["extra"], c.Fields["extra"], c.Local["extra"] = true, "true", true
		c.Args, c.Fields, c.Local = externalArgs, externalFields, externalLocal
		if calls == 1 {
			return extension.Result{}, failure
		}
		return extension.Result{}, nil
	}
	for i := range 2 {
		budget := stepsPerFrame
		_, _, err := r.invokeExtension(context.Background(), th, block, nil, false, 0, &budget)
		if (i == 0 && !errors.Is(err, failure)) || (i == 1 && err != nil) {
			t.Fatal(err)
		}
	}
	if externalArgs["keep"] != true || externalFields["keep"] != "true" || externalLocal["keep"] != true {
		t.Fatal("recycling cleared maps owned by the extension")
	}
}

func TestBorrowedExtensionNestedCalls(t *testing.T) {
	r, outer, block := constantExtensionRuntime(t)
	registered := r.extensionBlocks[block.Opcode]
	registered.block.BorrowContext = true
	inner := &thread{target: &target{owner: r, source: r.stage.source, state: TargetState{Name: "inner"}}}
	nested := &project.Block{Opcode: block.Opcode, Inputs: literalInputs(map[string]any{"A": "nested", "B": "2", "C": "3"})}
	var active *extension.Context
	registered.block.Handler = func(c *extension.Context) (extension.Result, error) {
		if c.Args["A"] == "nested" {
			if c == active || active.Args["A"] != "1" {
				t.Fatal("nested call reused active storage")
			}
			name, err := c.Call("target.get", "name")
			if err != nil || name != "inner" {
				t.Fatalf("nested host: %v, %v", name, err)
			}
			return extension.Result{}, nil
		}
		active = c
		c.Local["outer"] = true
		budget := stepsPerFrame
		if _, _, err := r.invokeExtension(context.Background(), inner, nested, nil, false, 0, &budget); err != nil {
			t.Fatal(err)
		}
		if c.Args["A"] != "1" || c.Field("MODE") != "fixed" || c.Local["outer"] != true {
			t.Fatal("nested call changed outer context")
		}
		return extension.Result{}, nil
	}
	for range 3 {
		budget := stepsPerFrame
		if _, _, err := r.invokeExtension(context.Background(), outer, block, nil, false, 0, &budget); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBorrowedExtensionCannotSuspend(t *testing.T) {
	for _, result := range []extension.Result{{Again: true}, {Yield: true}, {YieldTick: true}, {Branch: "SUBSTACK"}} {
		r, th, block := constantExtensionRuntime(t)
		registered := r.extensionBlocks[block.Opcode]
		registered.block.BorrowContext = true
		registered.block.Handler = func(*extension.Context) (extension.Result, error) { return result, nil }
		budget := stepsPerFrame
		_, _, err := r.invokeExtension(context.Background(), th, block, nil, false, 0, &budget)
		if err == nil || !strings.Contains(err.Error(), "borrowed context") {
			t.Fatalf("accepted suspended borrowed call: %+v, %v", result, err)
		}
		if th.extensionCall != nil || th.wakeFrame != 0 || len(th.stack) != 0 {
			t.Fatal("invalid borrowed call scheduled continuation")
		}
	}
}

func BenchmarkExtensionContextStorage(b *testing.B) {
	for _, borrowed := range []bool{false, true} {
		name := "owned"
		if borrowed {
			name = "borrowed"
		}
		b.Run(name, func(b *testing.B) {
			r, th, block := constantExtensionRuntime(b)
			r.extensionBlocks[block.Opcode].block.BorrowContext = borrowed
			b.ReportAllocs()
			for b.Loop() {
				budget := stepsPerFrame
				if _, _, err := r.invokeExtension(context.Background(), th, block, nil, false, 0, &budget); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestBorrowedExtensionInputErrorThenReuse(t *testing.T) {
	r, th, block := constantExtensionRuntime(t)
	r.extensionBlocks[block.Opcode].block.BorrowContext = true
	block.Inputs = nil
	failure := errors.New("input failed")
	calls := 0
	input := func(name string) (any, error) {
		calls++
		if calls == 2 {
			return nil, failure
		}
		return name, nil
	}
	for i := range 2 {
		budget := stepsPerFrame
		result, _, err := r.invokeExtension(context.Background(), th, block, input, false, 0, &budget)
		if i == 0 {
			if !errors.Is(err, failure) {
				t.Fatal(err)
			}
		} else if err != nil || result.Value != "A" {
			t.Fatalf("after input failure: %+v, %v", result, err)
		}
	}
}

func TestBorrowedExtensionDynamicTargetInputs(t *testing.T) {
	r, first, block := constantExtensionRuntime(t)
	registered := r.extensionBlocks[block.Opcode]
	registered.block.BorrowContext = true
	registered.block.Handler = func(c *extension.Context) (extension.Result, error) {
		return extension.Result{Value: c.Args["B"]}, nil
	}
	block.Inputs["B"] = []any{float64(1), []any{float64(12), "value", "value"}}
	second := &thread{target: &target{owner: r, source: r.stage.source, variables: map[string]*Variable{}}}
	for _, th := range []*thread{first, second, first} {
		for _, value := range []any{1.25, "007", false} {
			th.target.variables["value"] = &Variable{Value: value}
			budget := stepsPerFrame
			result, _, err := r.invokeExtension(context.Background(), th, block, nil, false, 0, &budget)
			if err != nil || result.Value != value {
				t.Fatalf("dynamic target input: got %+v, want %v, error %v", result, value, err)
			}
		}
	}
}
