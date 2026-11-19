package vm

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"testing"

	"gowarp/core/project"
)

func TestVariableStorageSharesWrites(t *testing.T) {
	r := fixture(t, "{}")
	_, first, err := r.variable(r.stage, "score")
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := r.variable(r.stage, "score")
	if err != nil {
		t.Fatal(err)
	}
	first.Value = "007"
	if second.Value != "007" {
		t.Fatal("variable lookup returned a detached copy")
	}
	snapshot := r.Snapshot()
	first.Value = float64(12)
	if snapshot[0].Variables["score"].Value != "007" {
		t.Fatal("snapshot changed with the live variable")
	}
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	if _, err := c.Call("variable.set", "score", "extension"); err != nil {
		t.Fatal(err)
	}
	if first.Value != "extension" {
		t.Fatal("extension replaced the variable storage")
	}
}

func TestNativeVariableBindingsGenerated(t *testing.T) {
	source, err := GenerateNative(nativeCodegenFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(source, []byte("nativeVariableIDs =")) || !bytes.Contains(source, []byte("v0.binding =")) {
		t.Fatal("compiled variables have no prebound storage")
	}
}

func TestVariableBindingsFollowScopeChanges(t *testing.T) {
	previous := nativeVariableIDs
	nativeVariableIDs = map[int][]string{1: {"shared", "x", "missing"}}
	t.Cleanup(func() { nativeVariableIDs = previous })
	p := nativeCodegenFixture(t)
	p.Targets[0].Variables = make(map[string][]any)
	p.Targets[0].Variables["shared"] = []any{"shared", float64(10), true}
	r := New(p)
	r.native = true
	r.refreshNativeVariables()
	original := r.targets[1]
	r.createClone(original)
	clone := r.targets[len(r.targets)-1]
	if original.nativeVariables[0] != clone.nativeVariables[0] || original.nativeVariables[1] == clone.nativeVariables[1] {
		t.Fatal("global storage must be shared and clone locals must be separate")
	}
	plans := map[string]*numericExpression{}
	for _, id := range []string{"shared", "x", "missing"} {
		plans[id] = r.numericInput(original, &project.Block{Inputs: map[string][]any{
			"VALUE": {float64(1), []any{float64(12), id, id}},
		}}, "VALUE", 0)
	}
	c := &extensionContext{r: r, th: &thread{target: clone}}
	call := func(op string, args ...any) {
		t.Helper()
		if _, err := c.Call(op, args...); err != nil {
			t.Fatal(err)
		}
	}
	read := func(target *target, binding int, id string) (any, error) {
		f := nativeFrame{ctx: context.Background(), r: r, t: target}
		slot := nativeVariable{binding: binding}
		value := f.variable(&slot, id)
		got, err := plans[id].read(context.Background(), r, &thread{target: target})
		if (err == nil) != (f.err == nil) || got != value {
			t.Fatalf("prepared read = %v, %v; native read = %v, %v", got, err, value, f.err)
		}
		numeric, numericErr := plans[id].evaluate(context.Background(), r, &thread{target: target})
		wantNumber, _ := value.(float64)
		if (numericErr == nil) != (f.err == nil) || numeric != wantNumber {
			t.Fatalf("numeric read = %v, %v; want %v", numeric, numericErr, wantNumber)
		}
		return value, f.err
	}
	check := func(target *target, want any) {
		t.Helper()
		got, err := read(target, 1, "shared")
		if err != nil || got != want {
			t.Fatalf("bound variable = %v, %v; want %v", got, err, want)
		}
	}
	call("variable.create", "shared", "local shadow", "local")
	check(clone, "local")
	check(original, float64(10))
	call("variable.delete", "shared")
	check(clone, float64(10))
	snapshot := r.CloudValues()
	r.ApplyCloud("shared", "cloud")
	check(original, "cloud")
	check(clone, "cloud")
	if snapshot["shared"].Value != float64(10) {
		t.Fatal("cloud snapshot aliases live storage")
	}
	call("variable.delete", "shared")
	if _, err := read(clone, 1, "shared"); err == nil {
		t.Fatal("binding retained a deleted global")
	}
	c.th.target = r.stage
	call("variable.create", "shared", "shared", "new")
	check(original, "new")
	check(clone, "new")
	if _, err := read(clone, 3, "missing"); err == nil {
		t.Fatal("missing binding must report an error when accessed")
	}
	r.deleteClone(clone)
	if clone.variables != nil || clone.nativeVariables != nil {
		t.Fatal("deleted clone retained variable storage")
	}
	check(clone, "new")
	if _, err := read(clone, 2, "x"); err == nil {
		t.Fatal("deleted clone still exposes its local variable")
	}
}

type variableCallbackGraphics struct {
	nativeDrawingRecorder
	call func(DisplayTarget)
}

func (g *variableCallbackGraphics) CostumeSize(d DisplayTarget) (float64, float64) {
	g.call(d)
	return 20, 30
}

func TestGraphicsVariableWritesSurviveReentry(t *testing.T) {
	r := fixture(t, "{}")
	g := &variableCallbackGraphics{call: func(d DisplayTarget) {
		d.State.Variables["score"] = Variable{Value: "007"}
		c := &extensionContext{r: r, th: &thread{target: r.stage}}
		value, err := c.Call("variable.get", "score")
		if err != nil || value != "007" {
			t.Fatalf("reentrant read = %v, %v", value, err)
		}
		if _, err := c.Call("variable.set", "score", float64(12)); err != nil {
			t.Fatal(err)
		}
		if d.State.Variables["score"].Value != float64(12) {
			t.Fatal("callback view missed a reentrant write")
		}
		d.State.Variables["score"] = Variable{Value: "final"}
		if r.Snapshot()[0].Variables["score"].Value != "final" {
			t.Fatal("snapshot missed a callback write")
		}
	}}
	r.SetGraphics(g)
	r.graphics.CostumeSize(display(r.stage))
	if r.stage.variables["score"].Value != "final" || len(r.variableObservations) != 0 {
		t.Fatal("callback changes were not committed or observation was retained")
	}
	if r.graphicsHost() != g {
		t.Fatal("graphics capabilities no longer resolve to the host")
	}
}

func TestGraphicsVariableWritePreservesNegativeZero(t *testing.T) {
	r := fixture(t, "{}")
	r.SetGraphics(&variableCallbackGraphics{call: func(d DisplayTarget) {
		v := d.State.Variables["score"]
		v.Value = math.Copysign(0, -1)
		d.State.Variables["score"] = v
	}})
	r.graphics.CostumeSize(display(r.stage))
	if !math.Signbit(r.stage.variables["score"].Value.(float64)) {
		t.Fatal("graphics variable write lost the sign of zero")
	}
}

func BenchmarkPreparedVariableAccess(b *testing.B) {
	previous := nativeVariableIDs
	nativeVariableIDs = map[int][]string{1: {"shared", "local", "result"}}
	b.Cleanup(func() { nativeVariableIDs = previous })
	for _, compiled := range []bool{false, true} {
		b.Run(fmt.Sprintf("native=%v", compiled), func(b *testing.B) {
			variables := map[string][]any{"shared": {"shared", float64(3)}}
			for i := 0; i < 128; i++ {
				variables[fmt.Sprintf("variable-%d", i)] = []any{"unused", float64(i)}
			}
			input := func(id string) []any { return []any{float64(1), []any{float64(12), id, id}} }
			blocks := project.Blocks{
				"sum":     {Opcode: "operator_add", Inputs: map[string][]any{"NUM1": input("local"), "NUM2": input("shared")}},
				"product": {Opcode: "operator_multiply", Inputs: map[string][]any{"NUM1": {float64(2), "sum"}, "NUM2": {float64(2), "sum"}}},
				"set":     {Opcode: "data_setvariableto", Fields: map[string][]any{"VARIABLE": {"result", "result"}}, Inputs: map[string][]any{"VALUE": {float64(2), "product"}}},
			}
			r := New(&project.Project{Targets: []*project.Target{
				{IsStage: true, Variables: variables},
				{Variables: map[string][]any{"local": {"local", float64(2)}, "result": {"result", float64(0)}}, Blocks: blocks},
			}})
			r.native = compiled
			r.refreshNativeVariables()
			th := &thread{target: r.targets[1]}
			write := r.prepareVariableWrite(th.target, blocks["set"])
			b.ReportAllocs()
			for b.Loop() {
				if err := write.run(context.Background(), r, th); err != nil {
					b.Fatal(err)
				}
			}
			if th.target.variables["result"].Value != float64(25) {
				b.Fatal("incorrect expression result")
			}
		})
	}
}

func TestPreparedVariableWritesFollowScopeChanges(t *testing.T) {
	previous := nativeVariableIDs
	nativeVariableIDs = map[int][]string{1: {"shared"}}
	t.Cleanup(func() { nativeVariableIDs = previous })
	r := New(&project.Project{Targets: []*project.Target{
		{IsStage: true, Variables: map[string][]any{"shared": {"shared", float64(10)}}},
		{Name: "Sprite"},
	}})
	r.native = true
	r.refreshNativeVariables()
	original := r.targets[1]
	r.createClone(original)
	clone := r.targets[len(r.targets)-1]
	set := r.prepareVariableWrite(original, &project.Block{Opcode: "data_setvariableto",
		Fields: map[string][]any{"VARIABLE": {"shared", "shared"}}, Inputs: literalInputs(map[string]any{"VALUE": "007"})})
	change := r.prepareVariableWrite(original, &project.Block{Opcode: "data_changevariableby",
		Fields: map[string][]any{"VARIABLE": {"shared", "shared"}}, Inputs: literalInputs(map[string]any{"VALUE": float64(1)})})
	th := &thread{target: clone}
	c := &extensionContext{r: r, th: th}
	call := func(op string, args ...any) {
		t.Helper()
		if _, err := c.Call(op, args...); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		call("variable.create", "shared", "local", float64(3))
		if err := set.run(context.Background(), r, th); err != nil || clone.variables["shared"].Value != "007" {
			t.Fatalf("local write lost raw value: %v", err)
		}
		if err := change.run(context.Background(), r, th); err != nil || clone.variables["shared"].Value != float64(8) {
			t.Fatalf("local change failed: %v", err)
		}
		if r.stage.variables["shared"].Value != float64(10+i) {
			t.Fatal("local write changed the stage")
		}
		call("variable.delete", "shared")
		if err := change.run(context.Background(), r, th); err != nil || r.stage.variables["shared"].Value != float64(11+i) {
			t.Fatalf("write did not return to stage scope: %v", err)
		}
	}
	call("variable.delete", "shared")
	if err := set.run(context.Background(), r, th); err == nil {
		t.Fatal("write retained a deleted variable")
	}
}
