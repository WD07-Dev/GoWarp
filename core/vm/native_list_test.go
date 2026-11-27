package vm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"gowarp/core/project"
)

func addNativeListBlocks(t testing.TB, blocks project.Blocks) {
	t.Helper()
	var extra project.Blocks
	err := json.Unmarshal([]byte(`{
        "listSearchSet":{"opcode":"data_setvariableto","next":"listContainsSet","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"listSearchIndex"]}},
        "listSearchIndex":{"opcode":"data_itemnumoflist","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[12,"x","x"]]}},
        "listContainsSet":{"opcode":"data_setvariableto","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[2,"listContains"]}},
        "listContains":{"opcode":"data_listcontainsitem","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[12,"x","x"]]}},
        "missingSearchSet":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"missingSearchIndex"]}},
        "missingSearchIndex":{"opcode":"data_itemnumoflist","fields":{"LIST":["absent","absent"]},"inputs":{"ITEM":[1,[12,"missingVariable","missingVariable"]]}}
    }`), &extra)
	if err != nil {
		t.Fatal(err)
	}
	for id, block := range extra {
		blocks[id] = block
	}
}

func testNativeListSearch(t *testing.T) {
	for _, code := range []string{"listSearch", "missingSearch"} {
		native, interpreted := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
		interpreted.native = false
		for _, r := range []*Runtime{native, interpreted} {
			if err := prepareProcedures(r.targets[1]); err != nil {
				t.Fatal(err)
			}
		}
		if native.targets[1].procedures[code].native == nil {
			t.Fatalf("%s did not compile", code)
		}
		for _, query := range []any{"seed", "SEED", "missing", float64(2), "02", "", "new"} {
			var errors [2]error
			for i, r := range []*Runtime{native, interpreted} {
				target := r.targets[1]
				*target.variables["x"] = Variable{Value: query}
				if query == "seed" {
					items := make([]any, 40)
					for j := range items {
						items[j] = float64(j)
					}
					items[30] = "seed"
					target.state.Lists["items"] = List{Items: items}
				}
				if query == "new" {
					list := target.state.Lists["items"]
					list.replace(30, "new")
					target.state.Lists["items"] = list
				}
				r.startHat(target, code+"Hat", false)
				errors[i] = r.Run(context.Background(), 1, 60, false)
			}
			if fmt.Sprint(errors[0]) != fmt.Sprint(errors[1]) {
				t.Fatalf("%s query %v: native error %v, interpreted %v", code, query, errors[0], errors[1])
			}
			for _, id := range []string{"score", "y"} {
				got := native.targets[1].variables[id].Value
				want := interpreted.targets[1].variables[id].Value
				if got != want {
					t.Fatalf("%s query %v variable %s: got %v, want %v", code, query, id, got, want)
				}
			}
			if code == "listSearch" && query == "SEED" && native.targets[1].state.Lists["items"].textIndex == nil {
				t.Fatal("native search discarded the reusable text index")
			}
		}
	}
}

func BenchmarkNativeCachedList(b *testing.B) {
	r := New(nativeCodegenFixture(b))
	f := nativeFrame{ctx: context.Background(), r: r, t: r.targets[1]}
	slot := nativeList{}
	f.list(&slot, "items")
	b.ReportAllocs()
	for b.Loop() {
		list := f.list(&slot, "items")
		if len(list.Items) != 1 {
			b.Fatal("changed list")
		}
	}
}

func addNativeSharedListBlocks(blocks project.Blocks) {
	blocks["sharedListsRepeat"] = &project.Block{Opcode: "control_repeat", Inputs: map[string][]any{
		"TIMES": {float64(1), []any{float64(12), "x", "x"}}, "SUBSTACK": {float64(2), "sharedListsCall"},
	}}
	blocks["sharedListsCall"] = &project.Block{Opcode: "procedures_call", Next: "sharedListsRead", Mutation: map[string]json.RawMessage{"proccode": json.RawMessage(`"sharedListHelper"`)}}
	blocks["sharedListsRead"] = &project.Block{Opcode: "data_changevariableby", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), "sharedListsLength"}}}
	blocks["sharedListsLength"] = &project.Block{Opcode: "data_lengthoflist", Fields: map[string][]any{"LIST": {"items", "items"}}}
	blocks["sharedListClear"] = &project.Block{Opcode: "data_deletealloflist", Next: "sharedListRead0", Fields: map[string][]any{"LIST": {"items", "items"}}}
	for i := range 3 {
		next := fmt.Sprintf("sharedListRead%d", i+1)
		if i == 2 {
			next = "sharedListAppend"
		}
		blocks[fmt.Sprintf("sharedListRead%d", i)] = &project.Block{Opcode: "data_setvariableto", Next: next, Fields: map[string][]any{"VARIABLE": {"y", "y"}}, Inputs: map[string][]any{"VALUE": {float64(2), "sharedListsLength"}}}
	}
	blocks["sharedListAppend"] = &project.Block{Opcode: "data_addtolist", Next: "sharedListStamp", Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: literalInputs(map[string]any{"ITEM": "007"})}
	blocks["sharedListStamp"] = &project.Block{Opcode: "pen_stamp"}
	blocks["sharedListObserverStart"] = &project.Block{Opcode: "data_addtolist", Next: "sharedListObserverCall", Fields: map[string][]any{"LIST": {"trail", "trail"}}, Inputs: literalInputs(map[string]any{"ITEM": "pending"})}
	blocks["sharedListObserverCall"] = &project.Block{Opcode: "procedures_call", Next: "sharedListObserverRead", Mutation: map[string]json.RawMessage{"proccode": json.RawMessage(`"sharedListHelper"`)}}
	blocks["sharedListObserverRead"] = &project.Block{Opcode: "data_setvariableto", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), "sharedListObserverItem"}}}
	blocks["sharedListObserverItem"] = &project.Block{Opcode: "data_itemoflist", Fields: map[string][]any{"LIST": {"trail", "trail"}}, Inputs: literalInputs(map[string]any{"INDEX": float64(1)})}
}

type nativeListGraphics struct {
	nativeDrawingRecorder
	independent bool
}

func (g *nativeListGraphics) ScriptDataIndependent() bool {
	return g.independent
}

func (g *nativeListGraphics) Pen(command PenCommand) {
	if g.independent {
		return
	}
	list := command.Target.State.Lists["items"]
	list.append("observer")
	command.Target.State.Lists["items"] = list
}

type nativeCallerListGraphics struct {
	nativeDrawingRecorder
	seen []any
}

func (g *nativeCallerListGraphics) Pen(command PenCommand) {
	g.seen = append(g.seen, command.Target.State.Lists["trail"].Items...)
	command.Target.State.Lists["trail"] = List{Items: []any{"updated"}}
}

func testNativeSharedListObservers(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		r := New(nativeCodegenFixture(t))
		r.native = compiled
		g := &nativeCallerListGraphics{}
		r.SetGraphics(g)
		target := r.targets[1]
		target.state.Lists["trail"] = List{}
		r.startHat(target, "sharedListObserverHat", false)
		if err := r.Run(context.Background(), 1, 60, false); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(g.seen, []any{"pending"}) || target.variables["score"].Value != "updated" {
			t.Fatalf("compiled=%v: observer saw %v, caller read %v", compiled, g.seen, target.variables["score"].Value)
		}
	}
}

type nativeListCancelContext struct {
	context.Context
	steps *int
	after int
}

func (c nativeListCancelContext) Err() error {
	if *c.steps >= c.after {
		return context.Canceled
	}
	return nil
}

func testNativeSharedListCancellation(t *testing.T) {
	for after := 1; after <= 24; after++ {
		var want []TargetState
		var wantCanceled bool
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			r.SetGraphics(&nativeListGraphics{independent: true})
			target := r.targets[1]
			target.state.Lists["trail"] = List{}
			th := r.startHat(target, "sharedListObserverHat", false)
			steps := 0
			th.sliceSteps, th.sliceLimit = &steps, stepsPerFrame
			err := r.stepBody(nativeListCancelContext{Context: context.Background(), steps: &steps, after: after}, th)
			canceled := errors.Is(err, context.Canceled)
			if err != nil && !canceled {
				t.Fatal(err)
			}
			if !compiled {
				want, wantCanceled = r.Snapshot(), canceled
			} else if canceled != wantCanceled || !reflect.DeepEqual(want, r.Snapshot()) {
				t.Fatalf("cancellation at step %d changed completed list writes", after)
			}
		}
	}
}

func testNativeSharedListClones(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		for _, global := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			r.SetGraphics(&nativeListGraphics{independent: true})
			original := r.targets[1]
			if global {
				r.stage.state.Lists["items"] = original.state.Lists["items"]
				delete(original.state.Lists, "items")
			}
			r.createClone(original)
			clone := r.targets[len(r.targets)-1]
			for _, th := range r.threads {
				th.done = true
			}
			clone.variables["x"].Value = float64(1)
			clone.variables["score"].Value = float64(0)
			r.startHat(clone, "sharedListsHat", false)
			if err := r.Run(context.Background(), 1, 60, false); err != nil {
				t.Fatal(err)
			}
			owner := clone
			if global {
				owner = r.stage
			}
			if !reflect.DeepEqual(owner.state.Lists["items"].Items, []any{"007"}) || clone.variables["score"].Value != float64(1) {
				t.Fatalf("compiled=%v global=%v: wrong clone list result", compiled, global)
			}
			if !global && !reflect.DeepEqual(original.state.Lists["items"].Items, []any{"seed"}) {
				t.Fatal("clone changed the original list")
			}
		}
	}
}

func runNativeSharedLists(t testing.TB, r *Runtime) {
	t.Helper()
	target := r.targets[1]
	target.variables["x"].Value = float64(100)
	target.variables["score"].Value = float64(0)
	target.state.Lists["items"] = List{Items: []any{}}
	r.startHat(target, "sharedListsHat", false)
	if err := r.Run(context.Background(), 1, 60, false); err != nil {
		t.Fatal(err)
	}
	if !r.Done() {
		t.Fatal("list loop did not finish")
	}
}

func testNativeSharedLists(t *testing.T) {
	for _, independent := range []bool{false, true} {
		var want []TargetState
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			r.SetGraphics(&nativeListGraphics{independent: independent})
			runNativeSharedLists(t, r)
			score := float64(100)
			if !independent {
				score = 200
			}
			if r.targets[1].variables["score"].Value != score {
				t.Fatalf("compiled=%v independent=%v score=%v, want %v", compiled, independent, r.targets[1].variables["score"].Value, score)
			}
			if !compiled {
				want = r.Snapshot()
			} else if !reflect.DeepEqual(want, r.Snapshot()) {
				t.Fatal("compiled list calls changed state")
			}
		}
	}
	r := New(nativeCodegenFixture(t))
	r.SetGraphics(&nativeListGraphics{independent: true})
	allocations := testing.AllocsPerRun(5, func() { runNativeSharedLists(t, r) })
	if allocations > 20 {
		t.Fatalf("repeated list calls allocate per invocation: %g allocations", allocations)
	}
}

func BenchmarkNativeSharedLists(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	r := New(nativeCodegenFixture(b))
	r.SetGraphics(&nativeListGraphics{independent: true})
	b.ReportAllocs()
	for b.Loop() {
		runNativeSharedLists(b, r)
	}
}
