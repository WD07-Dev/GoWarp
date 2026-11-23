package vm

import (
	"testing"

	"gowarp/core/project"
)

func TestExtensionTargetPropertyReadAllocations(t *testing.T) {
	r := fixture(t, `{}`)
	r.stage.state.X = 12.5
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	args := []any{"x"}
	allocs := testing.AllocsPerRun(100, func() {
		got, err := c.Call("target.get", args...)
		if err != nil || got != 12.5 {
			t.Fatalf("coordinate read: %v, %v", got, err)
		}
	})
	if allocs > 1 {
		t.Fatalf("reading one coordinate allocated unrelated properties: %.0f allocations", allocs)
	}
}

func TestExtensionTargetProperties(t *testing.T) {
	r := fixture(t, `{}`)
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	r.stage.state = TargetState{
		Name: "Stage", IsStage: true, Visible: true, X: 12.5, Y: -8,
		Direction: 45, Size: 75, Volume: 60, RotationStyle: "left-right", CurrentCostume: 2,
	}
	for name, want := range map[string]any{
		"id": "0:0", "x": 12.5, "y": -8.0, "direction": 45.0, "size": 75.0,
		"name": "Stage", "cloneId": 0.0, "source": 0.0, "isStage": true,
		"visible": true, "rotationStyle": "left-right", "currentCostume": 2.0, "volume": 60.0,
	} {
		got, err := c.Call("target.get", name)
		if err != nil || got != want {
			t.Errorf("%s = %v, %v; want %v", name, got, err, want)
		}
	}
	if _, err := c.Call("target.get", "missing"); err == nil {
		t.Fatal("unknown target property was accepted")
	}
	r.stage.state.X = -25
	if got, err := c.Call("target.get", "x"); err != nil || got != -25.0 {
		t.Fatalf("coordinate read returned stale state: %v, %v", got, err)
	}
}

func BenchmarkExtensionTargetCoordinate(b *testing.B) {
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true}}})
	r.stage.state.X = 12.5
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	args := []any{"x"}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		got, err := c.Call("target.get", args...)
		if err != nil || got != 12.5 {
			b.Fatalf("coordinate read: %v, %v", got, err)
		}
	}
}

func TestExtensionLogicDataCopiesAndErrors(t *testing.T) {
	r := fixture(t, `{}`)
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	call := func(op string, args ...any) any {
		t.Helper()
		v, err := c.Call(op, args...)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		return v
	}
	call("variable.set", "score", 7.0)
	if got := call("variable.get", "score"); got != 7.0 {
		t.Fatalf("variable=%v", got)
	}
	call("list.create", "test", "test")
	original := []any{"a", "b"}
	call("list.set", "test", original)
	original[0] = "changed"
	got := call("list.get", "test").([]any)
	got[1] = "changed"
	again := call("list.get", "test").([]any)
	if again[0] != "a" || again[1] != "b" {
		t.Fatalf("list aliases external storage: %v", again)
	}
	if _, err := c.Call("variable.get", "missing"); err == nil {
		t.Fatal("missing variable must fail")
	}
	if _, err := c.Call("unknown.operation"); err == nil {
		t.Fatal("unknown operation claimed")
	}
}

func TestExtensionLogicHostAndClonePrimitives(t *testing.T) {
	r := fixture(t, `{}`)
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	r.SetHostFrame(true, 42)
	for op, want := range map[string]any{"host.fullscreen": true, "host.frameMilliseconds": 42.0, "runtime.time": 0.0} {
		got, err := c.Call(op)
		if err != nil || got != want {
			t.Fatalf("%s=%v,%v", op, got, err)
		}
	}
	r.SetKeyState("a", true)
	got, _ := c.Call("input.keys")
	got.([]string)[0] = "b"
	if r.host.keys[0] != "a" {
		t.Fatal("key result aliases state")
	}
	_, err := c.Call("host.command", "open-url", "https://example.com")
	if err != nil || len(r.DrainHostCommands()) != 1 {
		t.Fatal("host command missing")
	}
}

func TestExtensionLogicCloneIdentityAndDataOwnership(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage","variables":{"global":["global",1]},"blocks":{}},{"name":"Sprite","variables":{"local":["local",7]},"lists":{"items":["items",["original"]]},"blocks":{}}]`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	call := func(op string, args ...any) any {
		t.Helper()
		v, err := c.Call(op, args...)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		return v
	}
	id := call("target.clone")
	call("variable.set", "local", 12.0, id)
	call("variable.change", "global", 2.0, id)
	call("list.append", "items", "clone", id)
	if got := call("variable.get", "local"); got != 7.0 {
		t.Fatalf("clone changed original: %v", got)
	}
	if got := call("variable.get", "global"); got != 3.0 {
		t.Fatalf("global ownership lost: %v", got)
	}
	if got := call("list.length", "items"); got != 1.0 {
		t.Fatalf("clone list aliases original: %v", got)
	}
	call("target.set", "direction", 450.0, id)
	if got := call("target.get", "direction", id); got != 90.0 {
		t.Fatalf("direction not normalized: %v", got)
	}
	call("target.delete", id)
	if r.cloneCount != 0 || len(r.targets) != 2 {
		t.Fatal("clone not deleted")
	}
	if _, err := c.Call("target.get", "name", id); err == nil {
		t.Fatal("deleted clone reference accepted")
	}
}

func TestExtensionLogicAcceptsGoIntegerArguments(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage","blocks":{}},{"name":"Sprite","variables":{"v":["value",1]},"lists":{"items":["items",["a","b"]]},"blocks":{}}]`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	call := func(op string, args ...any) any {
		t.Helper()
		value, err := c.Call(op, args...)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		return value
	}
	call("target.set", "x", 42)
	call("target.set", "direction", int64(450))
	call("target.set", "visible", 1)
	if call("target.get", "x") != 42.0 || call("target.get", "direction") != 90.0 || call("target.get", "visible") != true {
		t.Fatal("Go integer target arguments were not converted")
	}
	call("variable.change", "v", 3)
	if got := call("variable.get", "v"); got != 4.0 {
		t.Fatalf("integer variable increment=%v", got)
	}
	if got := call("list.item", "items", 2); got != "b" {
		t.Fatalf("integer list index=%v", got)
	}
	call("list.replace", "items", int64(1), "changed")
	if got := call("list.item", "items", 1); got != "changed" {
		t.Fatalf("integer list replacement=%v", got)
	}
}
