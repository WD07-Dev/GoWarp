package vm

import (
	"gowarp/core/extension"
	"slices"
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

func TestExtensionVariableExists(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage","variables":{"global":["global",3]}},{"name":"Sprite","variables":{"local":["local",""]}}]`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	for _, id := range []string{"local", "global", "missing"} {
		got, err := c.Call("variable.exists", id)
		if err != nil || got != (id != "missing") {
			t.Fatalf("exists(%s) = %v, %v", id, got, err)
		}
	}
	hostResult(t, c, "variable.create", "nil", "nil", nil)
	if hostResult(t, c, "variable.exists", "nil") != true {
		t.Fatal("nil-valued variable was treated as absent")
	}
	hostResult(t, c, "variable.delete", "nil")
	if hostResult(t, c, "variable.exists", "nil") != false {
		t.Fatal("deleted variable still exists")
	}
	if _, err := c.Call("variable.exists", "local", "missing-target"); err == nil {
		t.Fatal("unknown target was treated as a missing variable")
	}
}

func TestExtensionTargetHandleResolution(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"Sprite"},{"name":"1:1"}]`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	first := hostResult(t, c, "target.clone").(string)
	second := hostResult(t, c, "target.clone").(string)
	// Names of originals are checked before clone IDs, as in the linear lookup.
	if got, err := c.logicTarget(c.th.target, first); err != nil || got != r.targets[2] {
		t.Fatal("numeric sprite name precedence changed", got, err)
	}
	clone, err := c.logicTarget(c.th.target, second)
	if err != nil || clone.state.CloneID != 2 || clone.sourceIndex != 1 {
		t.Fatal("clone handle did not resolve", err)
	}
	for _, ref := range []string{"01:2", "1:02", "+1:2", "2:2", "1:999", "1:-2"} {
		if _, err := c.logicTarget(c.th.target, ref); err == nil {
			t.Fatalf("invalid handle accepted: %s", ref)
		}
	}
	clone.dead = true
	deleted := &extensionContext{r: r, th: &thread{target: clone}, event: "clone-deleted"}
	clone.variables["value"] = &Variable{Name: "value", Value: nil}
	if hostResult(t, deleted, "variable.exists", "value", second) != true {
		t.Fatal("cleanup handler could not inspect its departing clone")
	}
	if _, err := c.Call("variable.exists", "value", second); err == nil {
		t.Fatal("ordinary caller could read a dead clone")
	}
	clone.dead = false
	r.deleteClone(clone)
	if _, err := c.logicTarget(c.th.target, second); err == nil {
		t.Fatal("removed clone handle resolved")
	}
	third := hostResult(t, c, "target.clone").(string)
	if _, err := c.logicTarget(c.th.target, third); err != nil {
		t.Fatal("clone lookup failed after deletion", err)
	}
}

func TestExtensionTargetQuery(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"variables":{"global":["global",5]}},{"name":"A","variables":{"tag":["tag","02"]}},{"name":"B","variables":{"tag":["tag",2]}}]`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	first := hostResult(t, c, "target.clone").(string)
	second := hostResult(t, c, "target.clone").(string)
	hostResult(t, c, "target.clone", "B")
	q := extension.TargetQuery{Source: "_myself_", ClonesOnly: true, Variable: "tag", Value: 2.0}
	check := func(want ...string) {
		t.Helper()
		got, err := c.Call("target.query", q)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.([]string), want) {
			t.Fatalf("query=%v want=%v", got, want)
		}
	}
	check(first, second)
	q.First = true
	check(first)
	hostResult(t, c, "variable.set", "tag", "different", first)
	check(second)
	q.First = false
	q.Variable = "global"
	q.Value = 5.0
	check(first, second)
	q.Variable = "missing"
	q.Value = ""
	check()
	hostResult(t, c, "variable.set", "tag", nil, second)
	q.Variable = "tag"
	q.Value = nil
	check(second)
	hostResult(t, c, "target.delete", second)
	check()
	q.Variable = ""
	q.ClonesOnly = false
	check("1:0", first)
	q.Source = first
	check("1:0", first)
	q.Source = "missing"
	if _, err := c.Call("target.query", q); err == nil {
		t.Fatal("unknown family accepted")
	}
	if _, err := c.Call("target.query", true); err == nil {
		t.Fatal("invalid query accepted")
	}
}
