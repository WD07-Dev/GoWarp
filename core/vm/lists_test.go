package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"gowarp/core/extension"
	"gowarp/core/project"
)

func listRuntime(t *testing.T, blocks string, items string) *Runtime {
	t.Helper()
	var p project.Project
	data := `{"targets":[{"isStage":true,"name":"Stage","variables":{"score":["score",0]},"lists":{"items":["items",` + items + `]},"blocks":` + blocks + `}]}`
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatal(err)
	}
	r := New(&p)
	installTestExtensions(t, r)
	return r
}

func coordinateListWrite() (*Runtime, *thread) {
	blocks := map[string]*project.Block{
		"write": {Opcode: "data_replaceitemoflist", Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: map[string][]any{"INDEX": {float64(2), "index"}, "ITEM": {float64(2), "value"}}},
		"index": {Opcode: "operator_add", Inputs: map[string][]any{"NUM1": {float64(1), []any{float64(12), "offset", "offset"}}, "NUM2": {float64(1), []any{float64(4), "1"}}}},
		"value": {Opcode: "operator_multiply", Inputs: map[string][]any{"NUM1": {float64(1), []any{float64(12), "position", "position"}}, "NUM2": {float64(1), []any{float64(4), "0.75"}}}},
	}
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Blocks: blocks}}})
	r.stage.state.Lists["items"] = List{Items: []any{"unchanged", float64(0)}}
	r.stage.variables["offset"] = &Variable{Value: 1.5}
	r.stage.variables["position"] = &Variable{Value: 10.5}
	return r, &thread{target: r.stage}
}

func TestCoordinateListWriteAllocations(t *testing.T) {
	r, th := coordinateListWrite()
	allocs := testing.AllocsPerRun(100, func() {
		th.pc, th.done = "write", false
		if err := r.step(context.Background(), th); err != nil {
			t.Fatal(err)
		}
	})
	if got := r.stage.state.Lists["items"].Items; got[0] != "unchanged" || got[1] != 7.875 {
		t.Fatalf("coordinate write: %v", got)
	}
	/*
		Only the execution counter and stored result need heap storage; the
		numeric list index is temporary and must not allocate a boxed value.
	*/
	if allocs > 2 {
		t.Fatalf("coordinate update allocated %.0f objects", allocs)
	}
}

func BenchmarkCoordinateListWrite(b *testing.B) {
	r, th := coordinateListWrite()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		th.pc, th.done = "write", false
		if err := r.step(context.Background(), th); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPreparedListWritesKeepLiveValuesAndTypes(t *testing.T) {
	r := listRuntime(t, `{
		"write":{"opcode":"data_replaceitemoflist","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[12,"index","index"]],"ITEM":[1,[12,"item","item"]]}}
	}`, `["a","b"]`)
	th := &thread{target: r.stage}
	for _, tc := range []struct {
		index, item any
		want        []any
	}{
		{1.9, "007", []any{"007", "b"}},
		{"last", true, []any{"007", true}},
		{0.0, "invalid", []any{"007", true}},
		{math.Inf(1), "invalid", []any{"007", true}},
		{2.0, 12.5, []any{"007", 12.5}},
	} {
		r.stage.variables["index"] = &Variable{Value: tc.index}
		r.stage.variables["item"] = &Variable{Value: tc.item}
		th.pc, th.done = "write", false
		if err := r.step(context.Background(), th); err != nil {
			t.Fatal(err)
		}
		if got := r.stage.state.Lists["items"].Items; !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("index %v: %v, want %v", tc.index, got, tc.want)
		}
	}
	delete(r.stage.variables, "item")
	r.stage.variables["index"] = &Variable{Value: float64(0)}
	th.pc, th.done = "write", false
	if err := r.step(context.Background(), th); err == nil {
		t.Fatal("invalid index skipped the item input error")
	}
}

func TestPreparedListAppendKeepsObserver(t *testing.T) {
	r, th := coordinateListWrite()
	th.target.source.Blocks["write"].Opcode = "data_addtolist"
	th.pc = "write"
	if err := r.step(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	var observed []ListAppend
	r.SetListObserver(func(item ListAppend) { observed = append(observed, item) })
	r.stage.variables["position"] = &Variable{Value: 20.5}
	th.pc, th.done = "write", false
	if err := r.step(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	if got := r.stage.state.Lists["items"].Items; len(got) != 4 || got[2] != 7.875 || got[3] != 15.375 {
		t.Fatalf("append returned stale coordinates: %v", got)
	}
	if len(observed) != 1 || observed[0].Number != 15.375 {
		t.Fatalf("prepared append bypassed observer: %v", observed)
	}
}

func TestPreparedListWriteUsesCurrentClone(t *testing.T) {
	r := eventProject(t, `[
		{"isStage":true,"lists":{"items":["items",["stage"]]}},
		{"name":"Sprite","variables":{"value":["value","original"]},"lists":{"items":["items",["old"]]},"blocks":{
			"write":{"opcode":"data_replaceitemoflist","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[4,"1"]],"ITEM":[1,[12,"value","value"]]}}
		}}
	]`)
	original := r.targets[1]
	r.createClone(original)
	clone := r.targets[2]
	clone.variables["value"] = &Variable{Value: "clone"}
	for _, target := range []*target{original, clone, original} {
		if err := r.step(context.Background(), &thread{target: target, pc: "write"}); err != nil {
			t.Fatal(err)
		}
	}
	for target, want := range map[*target]string{r.stage: "stage", original: "original", clone: "clone"} {
		if got := target.state.Lists["items"].Items[0]; got != want {
			t.Fatalf("list write crossed sprite scope: %v, want %v", got, want)
		}
	}
}

func TestPreparedListWriteFallsBackForYieldingInputs(t *testing.T) {
	r := listRuntime(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"write"},
		"write":{"opcode":"data_replaceitemoflist","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[12,"score","score"]],"ITEM":[2,"wait"]}},
		"wait":{"opcode":"example_wait"}
	}`, `["a","b"]`)
	*r.stage.variables["score"] = Variable{Value: float64(1)}
	if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "example", Blocks: []extension.Block{{
			Opcode: "example_wait", Kind: "reporter", Handler: func(c *extension.Context) (extension.Result, error) {
				if c.Local["waiting"] == nil {
					c.Local["waiting"] = true
					return extension.Result{Again: true, YieldTick: true}, nil
				}
				return extension.Result{Value: "ready"}, nil
			},
		}}}
	}}); err != nil {
		t.Fatal(err)
	}
	run(t, r, 1, false)
	if got := r.stage.state.Lists["items"].Items; !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Fatalf("list changed before reporter completed: %v", got)
	}
	*r.stage.variables["score"] = Variable{Value: float64(2)}
	run(t, r, 1, false)
	if got := r.stage.state.Lists["items"].Items; !reflect.DeepEqual(got, []any{"ready", "b"}) {
		t.Fatalf("suspended index was evaluated again: %v", got)
	}
}

func TestListMutations(t *testing.T) {
	r := listRuntime(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"add"},
 "add":{"opcode":"data_addtolist","next":"insert","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[10,"c"]]}},
 "insert":{"opcode":"data_insertatlist","next":"replace","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[4,"2.9"]],"ITEM":[1,[10,"b"]]}},
 "replace":{"opcode":"data_replaceitemoflist","next":"delete","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[10,"last"]],"ITEM":[1,[10,"z"]]}},
 "delete":{"opcode":"data_deleteoflist","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[4,"1"]]}}
 }`, `["a"]`)
	run(t, r, 1, false)
	if got := r.Snapshot()[0].Lists["items"].Items; !reflect.DeepEqual(got, []any{"b", "z"}) {
		t.Fatalf("got %v", got)
	}
	snapshot := r.Snapshot()
	snapshot[0].Lists["items"].Items[0] = "changed"
	if r.Snapshot()[0].Lists["items"].Items[0] != "b" {
		t.Fatal("snapshot aliases live list")
	}
	if len(r.targets[0].source.Lists["items"][1].([]any)) != 1 {
		t.Fatal("mutated source list")
	}
}

func TestListScratchBufferReuse(t *testing.T) {
	for _, opcode := range []string{"data_deletealloflist", "data_deleteoflist"} {
		t.Run(opcode, func(t *testing.T) {
			r := listRuntime(t, `{}`, `["old"]`)
			b := &project.Block{Opcode: opcode, Fields: map[string][]any{"LIST": {"items", "items"}}}
			add := &project.Block{Opcode: "data_addtolist", Fields: b.Fields}
			input := func(string) (any, error) {
				return "all", nil
			}
			before := r.Snapshot()
			allocs := testing.AllocsPerRun(100, func() {
				if err := r.listStatement(r.stage, b, input); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 15; i++ {
					if err := r.listStatement(r.stage, add, input); err != nil {
						t.Fatal(err)
					}
				}
			})
			if allocs != 0 {
				t.Fatalf("clear/refill allocated %g times", allocs)
			}
			if before[0].Lists["items"].Items[0] != "old" {
				t.Fatal("clear/refill changed snapshot")
			}
			backing := r.stage.state.Lists["items"].Items
			if err := r.listStatement(r.stage, b, input); err != nil {
				t.Fatal(err)
			}
			for _, item := range backing {
				if item != nil {
					t.Fatal("cleared buffer retains an item")
				}
			}
		})
	}
}

func BenchmarkListAttackScratch(b *testing.B) {
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Name: "Stage"}}})
	r.stage.state.Lists["items"] = List{Name: "items"}
	clearBlock := &project.Block{Opcode: "data_deletealloflist", Fields: map[string][]any{"LIST": {"items", "items"}}}
	add := &project.Block{Opcode: "data_addtolist", Fields: clearBlock.Fields}
	input := func(string) (any, error) {
		return float64(1), nil
	}
	b.ReportAllocs()

	for b.Loop() {
		r.listStatement(r.stage, clearBlock, input)
		for j := 0; j < 15; j++ {
			r.listStatement(r.stage, add, input)
		}
	}
}

func attackListRuntime() (*Runtime, []string) {
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Name: "Stage"}}})
	list := List{Name: "items"}
	keys := make([]string, 100)
	for i := range keys {
		keys[i] = fmt.Sprintf("일반骨%d", i)
		list.Items = append(list.Items, keys[i])
		for j := 0; j < 12; j++ {
			list.Items = append(list.Items, float64(j))
		}
	}
	r.stage.state.Lists["items"] = list
	return r, keys
}

func TestListAttackLookupAllocations(t *testing.T) {
	r, keys := attackListRuntime()
	b := &project.Block{Opcode: "data_listcontainsitem", Fields: map[string][]any{"LIST": {"items", "items"}}}
	var key any = keys[len(keys)-1]
	input := func(string) (any, error) {
		return key, nil
	}
	allocs := testing.AllocsPerRun(100, func() {
		value, err := r.listReporter(context.Background(), r.stage, b, input)
		if err != nil || value != true {
			t.Fatalf("lookup=%v, %v", value, err)
		}
	})
	if allocs != 0 {
		t.Fatalf("repeated identifier lookup allocated %g times", allocs)
	}
}

func BenchmarkListAttackLookup(b *testing.B) {
	r, keys := attackListRuntime()
	lookup := &project.Block{Opcode: "data_itemnumoflist", Fields: map[string][]any{"LIST": {"items", "items"}}}
	replace := &project.Block{Opcode: "data_replaceitemoflist", Fields: lookup.Fields}
	var key, index any
	query := func(string) (any, error) {
		return key, nil
	}
	update := func(name string) (any, error) {
		if name == "INDEX" {
			return index, nil
		}
		return float64(42), nil
	}
	b.ReportAllocs()

	for b.Loop() {
		for j, name := range keys {
			key, index = name, float64(j*13+2)
			r.listReporter(context.Background(), r.stage, lookup, query)
			r.listStatement(r.stage, replace, update)
		}
	}
}

func denseAttackListRuntime() (*Runtime, []string) {
	r, keys := attackListRuntime()
	list := r.stage.state.Lists["items"]
	for i := len(keys); i < 1000; i++ {
		key := fmt.Sprintf("일반骨%d", i)
		keys = append(keys, key)
		list.append(key)
		for j := 0; j < 12; j++ {
			list.append(float64(j))
		}
	}
	r.stage.state.Lists["items"] = list
	return r, keys
}

func TestDenseAttackLookupAllocations(t *testing.T) {
	r, keys := denseAttackListRuntime()
	lookup := &project.Block{Opcode: "data_listcontainsitem", Fields: map[string][]any{"LIST": {"items", "items"}}}
	var key any = keys[len(keys)-1]
	query := func(string) (any, error) {
		return key, nil
	}
	allocs := testing.AllocsPerRun(20, func() {
		value, err := r.listReporter(context.Background(), r.stage, lookup, query)
		if err != nil || value != true {
			t.Fatalf("lookup=%v, %v", value, err)
		}
	})
	if allocs != 0 {
		t.Fatalf("dense attack lookup allocated %.0f objects", allocs)
	}
}

func TestListSearchCacheOverflowKeepsResults(t *testing.T) {
	r, _ := attackListRuntime()
	list := List{Name: "items"}
	for i := 0; i < 8200; i++ {
		list.append(fmt.Sprintf("bullet-%d", i))
	}
	r.stage.state.Lists["items"] = list
	lookup := &project.Block{Opcode: "data_itemnumoflist", Fields: map[string][]any{"LIST": {"items", "items"}}}
	check := func(key string, want float64) {
		t.Helper()
		got, err := r.listReporter(context.Background(), r.stage, lookup, func(string) (any, error) {
			return key, nil
		})
		if err != nil || got != want {
			t.Fatalf("query %q: %v, %v; want %g", key, got, err, want)
		}
		cached := r.stage.state.Lists["items"]
		if len(cached.textIndex.values) == 0 || len(cached.textIndex.values) > 8192 {
			t.Fatalf("fallback lost bounded cache: %d entries", len(cached.textIndex.values))
		}
	}
	/*
		The first query is outside the index; its scan must still retain the
		bounded index for subsequent queries instead of rebuilding it.
	*/
	check("bullet-8199", 8200)
	check("bullet-0", 1)
	check("missing", 0)
	list = r.stage.state.Lists["items"]
	list.append("bullet-8199")
	list.append("new-bullet")
	r.stage.state.Lists["items"] = list
	check("bullet-8199", 8200)
	check("new-bullet", 8202)
	list = r.stage.state.Lists["items"]
	list.replace(0, "bullet-8199")
	r.stage.state.Lists["items"] = list
	check("bullet-8199", 1)
}

func BenchmarkDenseAttackLookup(b *testing.B) {
	r, keys := denseAttackListRuntime()
	lookup := &project.Block{Opcode: "data_listcontainsitem", Fields: map[string][]any{"LIST": {"items", "items"}}}
	var key any = keys[len(keys)-1]
	query := func(string) (any, error) {
		return key, nil
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.listReporter(context.Background(), r.stage, lookup, query); err != nil {
			b.Fatal(err)
		}
	}
}

func TestListSearchMatchesCompareAfterMutations(t *testing.T) {
	r, _ := attackListRuntime()
	lookup := &project.Block{Opcode: "data_itemnumoflist", Fields: map[string][]any{"LIST": {"items", "items"}}}
	values := []any{"일반骨0", "일반骨99", "missing", "HELLO", "hello", "Σ", "σ", "ς", "K", "k",
		"", " ", "02", float64(2), true, "true", nil, "null", "NaN", math.NaN(), "infinity", math.Inf(1)}
	check := func() {
		t.Helper()
		for _, value := range values {
			want := float64(0)
			for i, item := range r.stage.state.Lists["items"].Items {
				if compare(item, value) == 0 {
					want = float64(i + 1)
					break
				}
			}
			got, err := r.listReporter(context.Background(), r.stage, lookup, func(string) (any, error) {
				return value, nil
			})
			if err != nil || got != want {
				t.Fatalf("query %v: got %v, %v; want %v", value, got, err, want)
			}
		}
	}
	check()
	// Exercise both Scratch statements and the public extension API.
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	for _, value := range values {
		for _, opcode := range []string{"data_addtolist", "data_replaceitemoflist", "data_insertatlist", "data_deleteoflist"} {
			b := &project.Block{Opcode: opcode, Fields: lookup.Fields}
			err := r.listStatement(r.stage, b, func(name string) (any, error) {
				if name == "INDEX" {
					return float64(1), nil
				}
				return value, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			check()
		}
		for _, op := range []string{"list.append", "list.replace", "list.insert", "list.delete"} {
			args := []any{"items", value}
			switch op {
			case "list.replace", "list.insert":
				args = []any{"items", float64(1), value}
			case "list.delete":
				args = []any{"items", float64(1)}
			}
			if _, err := c.Call(op, args...); err != nil {
				t.Fatalf("%s: %v", op, err)
			}
			check()
		}
	}
	replacement := make([]any, 64)
	copy(replacement, values)
	for _, op := range []string{"list.set", "list.clear"} {
		if _, err := c.Call(op, "items", replacement); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		check()
	}
}

func TestListSearchCloneAndSnapshotIsolation(t *testing.T) {
	r, keys := attackListRuntime()
	// Clone a sprite with a populated search index.
	source := r.stage
	source.state.IsStage = false
	lookup := &project.Block{Opcode: "data_itemnumoflist", Fields: map[string][]any{"LIST": {"items", "items"}}}
	query := func(string) (any, error) {
		return keys[0], nil
	}
	if _, err := r.listReporter(context.Background(), source, lookup, query); err != nil {
		t.Fatal(err)
	}
	snapshot := r.Snapshot()
	if snapshot[0].Lists["items"].textIndex != nil {
		t.Fatal("snapshot retained live search index")
	}
	r.createClone(source)
	clone := r.targets[len(r.targets)-1]
	c := &extensionContext{r: r, th: &thread{target: clone}}
	if _, err := c.Call("list.replace", "items", float64(1), "clone-only"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		target *target
		want   float64
	}{{source, 1}, {clone, 0}} {
		got, err := r.listReporter(context.Background(), tc.target, lookup, query)
		if err != nil || got != tc.want {
			t.Fatalf("lookup after clone mutation=%v, %v; want %v", got, err, tc.want)
		}
	}
	if snapshot[0].Lists["items"].Items[0] != keys[0] {
		t.Fatal("clone changed snapshot contents")
	}
}

func TestListSearchCacheBoundsAndCancellation(t *testing.T) {
	r, keys := attackListRuntime()
	lookup := &project.Block{Opcode: "data_itemnumoflist", Fields: map[string][]any{"LIST": {"items", "items"}}}
	query := func(string) (any, error) {
		return keys[0], nil
	}
	if _, err := r.listReporter(context.Background(), r.stage, lookup, query); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.listReporter(ctx, r.stage, lookup, query); err != context.Canceled {
		t.Fatalf("cached search ignored cancellation: %v", err)
	}
	list := r.stage.state.Lists["items"]
	for len(list.Items) <= 8192 {
		list.append("more")
	}
	if index, handled, err := list.findText(context.Background(), "more"); err != nil || !handled || index != 1301 {
		t.Fatalf("growing list lost cached lookup: %d, %v, %v", index, handled, err)
	}
	if len(list.textIndex.values) > 8192 {
		t.Fatal("search cache exceeded distinct-name limit")
	}
	list.clear()
	if cap(list.Items) != 0 || len(list.Items) != 0 || list.textIndex != nil {
		t.Fatal("clear retained large list storage")
	}
}

func TestListSearchInputCanMutateList(t *testing.T) {
	for _, op := range []string{"list.append", "list.replace", "list.set", "list.clear", "list.remove"} {
		t.Run(op, func(t *testing.T) {
			r, keys := attackListRuntime()
			b := &project.Block{Opcode: "data_itemnumoflist", Fields: map[string][]any{"LIST": {"items", "items"}}}
			if _, err := r.listReporter(context.Background(), r.stage, b, func(string) (any, error) {
				return keys[0], nil
			}); err != nil {
				t.Fatal(err)
			}
			c := &extensionContext{r: r, th: &thread{target: r.stage}}
			input := func(string) (any, error) {
				args := []any{"items", "new"}
				switch op {
				case "list.clear", "list.remove":
					args = []any{"items"}
				case "list.replace":
					args = []any{"items", float64(1), "new"}
				case "list.set":
					items := make([]any, 64)
					items[0] = "new"
					args = []any{"items", items}
				}
				_, err := c.Call(op, args...)
				return "new", err
			}
			got, err := r.listReporter(context.Background(), r.stage, b, input)
			if op == "list.remove" {
				if err == nil {
					t.Fatal("removed list accepted")
				}
				if _, exists := r.stage.state.Lists["items"]; exists {
					t.Fatal("search restored removed list")
				}
				return
			}
			want := float64(1)
			if op == "list.append" {
				want = 1301
			} else if op == "list.clear" {
				want = 0
			}
			if err != nil || got != want {
				t.Fatalf("%s during input: got %v, %v; want %v", op, got, err, want)
			}
			if op == "list.append" && len(r.stage.state.Lists["items"].Items) != 1301 {
				t.Fatal("search discarded appended item")
			}
			if op == "list.clear" && len(r.stage.state.Lists["items"].Items) != 0 {
				t.Fatal("search restored cleared items")
			}
			again, err := r.listReporter(context.Background(), r.stage, b, func(string) (any, error) {
				return "new", nil
			})
			if err != nil || again != want {
				t.Fatalf("search overwrote input mutation: %v, %v", again, err)
			}
		})
	}
}

func TestListReporters(t *testing.T) {
	for _, tc := range []struct {
		opcode, inputs, items string
		want                  any
	}{
		{"data_itemoflist", `{"INDEX":[1,[4,"0"]]}`, `[1,2]`, ""},
		{"data_itemoflist", `{"INDEX":[1,[10,"last"]]}`, `[1,"02"]`, "02"},
		{"data_itemnumoflist", `{"ITEM":[1,[10,"HELLO"]]}`, `[1,"hello"]`, float64(2)},
		{"data_itemnumoflist", `{"ITEM":[1,[4,"2"]]}`, `["02"]`, float64(1)},
		{"data_listcontainsitem", `{"ITEM":[1,[4,"4"]]}`, `[1,2]`, false},
		{"data_lengthoflist", `{}`, `[1,2,3]`, float64(3)},
		{"data_listcontents", `{}`, `["a","b"]`, "ab"},
		{"data_listcontents", `{}`, `[1,2]`, "1 2"},
		{"data_listcontents", `{}`, `["😀","a"]`, "😀 a"},
	} {
		blocks := `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"report"]}},"report":{"opcode":"` + tc.opcode + `","fields":{"LIST":["items","items"]},"inputs":` + tc.inputs + `}}`
		r := listRuntime(t, blocks, tc.items)
		run(t, r, 1, false)
		if got := r.Snapshot()[0].Variables["score"].Value; got != tc.want {
			t.Errorf("%s: got %v want %v", tc.opcode, got, tc.want)
		}
	}
}

func TestListIndices(t *testing.T) {
	for _, tc := range []struct {
		v          any
		size, want int
	}{
		{"last", 0, -1},
		{"last", 2, 1},
		{"2.9", 3, 1},
		{"0", 2, -1},
		{"-1", 2, -1},
		{"no", 2, -1},
		{math.Inf(1), 2, -1},
		{"ALL", 2, -1},
	} {
		if got := listIndex(tc.v, tc.size); got != tc.want {
			t.Errorf("%v size %d: %d", tc.v, tc.size, got)
		}
	}
	for _, v := range []string{"random", "any"} {
		if got := listIndex(v, 0); got != -1 {
			t.Fatal("empty random index")
		}
		for i := 0; i < 20; i++ {
			if got := listIndex(v, 3); got < 0 || got > 2 {
				t.Fatal("random index out of bounds")
			}
		}
	}
}

func TestListClearAndInvalidIndex(t *testing.T) {
	for _, tc := range []struct {
		opcode, index string
		want          int
	}{
		{"data_deleteoflist", "all", 0},
		{"data_deletealloflist", "0", 0},
		{"data_deleteoflist", "0", 2},
		{"data_insertatlist", "99", 2},
		{"data_replaceitemoflist", "-1", 2},
	} {
		r := listRuntime(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"act"},"act":{"opcode":"`+tc.opcode+`","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[10,"`+tc.index+`"]],"ITEM":[1,[10,"x"]]}}}`, `[1,2]`)
		run(t, r, 1, false)
		if len(r.Snapshot()[0].Lists["items"].Items) != tc.want {
			t.Fatalf("%s %s", tc.opcode, tc.index)
		}
	}
}

func TestListScopeAndMissingID(t *testing.T) {
	r := listRuntime(t, `{}`, `[1]`)
	var sprite project.Target
	if err := json.Unmarshal([]byte(`{"name":"Sprite","lists":{"local":["items",[2]]},"blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"add"},"add":{"opcode":"data_addtolist","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[10,"global"]]}}}}`), &sprite); err != nil {
		t.Fatal(err)
	}
	r = New(&project.Project{Targets: []*project.Target{r.targets[0].source, &sprite}})
	run(t, r, 1, false)
	if len(r.Snapshot()[0].Lists["items"].Items) != 2 || len(r.Snapshot()[1].Lists["local"].Items) != 1 {
		t.Fatal("wrong list scope")
	}
	sprite.Blocks["add"].Fields["LIST"][1] = "missing"
	r = New(&project.Project{Targets: []*project.Target{r.targets[0].source, &sprite}})
	if err := r.Run(context.Background(), 1, 30, false); err == nil {
		t.Fatal("missing list accepted")
	}
}

func TestListMutationLimit(t *testing.T) {
	for _, opcode := range []string{"data_addtolist", "data_insertatlist"} {
		r := listRuntime(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"act"},"act":{"opcode":"`+opcode+`","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[4,"1"]],"ITEM":[1,[10,"new"]]}}}`, `[]`)
		list := r.targets[0].state.Lists["items"]
		list.Items = make([]any, 200000)
		for i := range list.Items {
			list.Items[i] = "old"
		}
		r.targets[0].state.Lists["items"] = list
		run(t, r, 1, false)
		got := r.Snapshot()[0].Lists["items"].Items
		if len(got) != 200000 {
			t.Fatal("list exceeded limit")
		}
		want := "old"
		if opcode == "data_insertatlist" {
			want = "new"
		}
		if got[0] != want {
			t.Fatalf("first=%v", got[0])
		}
	}
}

func TestListFindComparisonParity(t *testing.T) {
	values := []any{nil, false, true, float64(0), math.Copysign(0, -1), float64(2),
		math.NaN(), math.Inf(1), math.Inf(-1), "", " \t", "02", "2", "NaN", "Infinity",
		"0x2", "seed", "SEED", "İ", "i", "K", "k", "ς", "σ"}
	for _, size := range []int{0, 1, 31, 32, 40} {
		for _, item := range values {
			items := make([]any, size)
			for i := range items {
				items[i] = item
			}
			list := List{Items: items}
			for _, query := range values {
				want := 0
				if size != 0 && compare(item, query) == 0 {
					want = 1
				}
				got, err := list.find(context.Background(), query)
				if err != nil || got != want {
					t.Fatalf("size %d, item %#v query %#v: got %d (%v), want %d", size, item, query, got, err, want)
				}
			}
		}
	}
}

func TestListFindCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	list := List{Items: []any{float64(2)}}
	if _, err := list.find(ctx, "02"); err != context.Canceled {
		t.Fatalf("canceled numeric search: %v", err)
	}
}

func benchmarkListSearchBefore(list *List, ctx context.Context, value any) (int, error) {
	if index, handled, err := list.findText(ctx, value); handled || err != nil {
		return index, err
	}
	for i, item := range list.Items {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
		}
		if compare(item, value) == 0 {
			return i + 1, nil
		}
	}
	return 0, nil
}

func BenchmarkListSearch(b *testing.B) {
	numbers := make([]any, 1300)
	for i := range numbers {
		numbers[i] = float64(i)
	}
	for _, tc := range []struct {
		name  string
		items []any
		query any
		want  int
	}{
		{"numeric-tail", numbers, "01299", 1300},
		{"number-tail", numbers, float64(1299), 1300},
		{"short-numeric-tail", numbers[:24], " 023 ", 24},
		{"short-number-tail", numbers[:24], float64(23), 24},
		{"short-mixed-missing", []any{"a", "b", "c", "d", "e", "f", "g", "h"}, 1234.5, 0},
		{"text-first", []any{"seed", "other"}, "seed", 1},
		{"text-missing", []any{"seed", "other"}, "missing", 0},
		{"number-first", numbers, float64(0), 1},
		{"empty", nil, "long identifier", 0},
	} {
		b.Run(tc.name, func(b *testing.B) {
			for _, implementation := range []struct {
				name string
				find func(*List, context.Context, any) (int, error)
			}{
				{"before", benchmarkListSearchBefore},
				{"current", (*List).find},
			} {
				b.Run(implementation.name, func(b *testing.B) {
					list := List{Items: tc.items}
					ctx := context.Background()
					b.ReportAllocs()
					for b.Loop() {
						got, err := implementation.find(&list, ctx, tc.query)
						if err != nil || got != tc.want {
							b.Fatalf("got %d (%v), want %d", got, err, tc.want)
						}
					}
				})
			}
		})
	}
}

func TestListFindExactStringAllocations(t *testing.T) {
	query := strings.Repeat("9", 1000)
	list := List{Items: []any{query}}
	ctx := context.Background()
	allocations := testing.AllocsPerRun(100, func() {
		index, err := list.find(ctx, query)
		if err != nil || index != 1 {
			t.Fatalf("index %d: %v", index, err)
		}
	})
	if allocations != 0 {
		t.Fatalf("exact string search allocated %g times", allocations)
	}
}

func TestListFindConvertsNumericQueryOnce(t *testing.T) {
	list := List{Items: []any{"a", "b", "c", "d", "e", "f", "g", "h"}}
	ctx := context.Background()
	allocations := testing.AllocsPerRun(100, func() {
		index, err := list.find(ctx, 1234.5)
		if err != nil || index != 0 {
			t.Fatalf("index %d: %v", index, err)
		}
	})
	// Formatting the query may allocate, but not once for every list item.
	if allocations > 2 {
		t.Fatalf("numeric query allocated %g times", allocations)
	}
}

func mutableSearchList() List {
	items := make([]any, 256)
	for i := range items {
		items[i] = fmt.Sprintf("entry-%d", i)
	}
	return List{Items: items}
}

func TestListSearchReusesStorageAfterReplace(t *testing.T) {
	list := mutableSearchList()
	ctx := context.Background()
	allocs := testing.AllocsPerRun(100, func() {
		list.replace(0, "changed")
		if got, err := list.find(ctx, "entry-255"); got != 256 || err != nil {
			t.Fatalf("search after replacement: %d, %v", got, err)
		}
		list.replace(0, "entry-0")
		if got, err := list.find(ctx, "changed"); got != 0 || err != nil {
			t.Fatalf("search retained replaced value: %d, %v", got, err)
		}
	})
	if allocs != 0 {
		t.Fatalf("rebuilding a same-size list search allocates: %g", allocs)
	}
}

func BenchmarkListSearchAfterReplace(b *testing.B) {
	list := mutableSearchList()
	ctx := context.Background()
	if _, err := list.find(ctx, "entry-255"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		list.replace(0, "changed")
		if got, err := list.find(ctx, "entry-255"); got != 256 || err != nil {
			b.Fatal(got, err)
		}
		list.replace(0, "entry-0")
		if got, err := list.find(ctx, "changed"); got != 0 || err != nil {
			b.Fatal(got, err)
		}
	}
}

type listSearchCancelContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *listSearchCancelContext) Err() error {
	c.checks++
	if c.checks == 3 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestListCanceledRebuildReleasesPartialKeys(t *testing.T) {
	list := List{Items: make([]any, 2048)}
	for i := range list.Items {
		list.Items[i] = fmt.Sprintf("entry-%d", i)
	}
	if _, err := list.find(context.Background(), "entry-2047"); err != nil {
		t.Fatal(err)
	}
	list.replace(0, "changed")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := list.find(&listSearchCancelContext{Context: ctx, cancel: cancel}, "entry-2047"); err != context.Canceled {
		t.Fatalf("rebuild was not interrupted: %v", err)
	}
	if len(list.textIndex.values) != 0 {
		t.Fatal("canceled rebuild retained partially indexed strings")
	}
	list.replace(0, "after-cancel")
	if got, err := list.find(context.Background(), "after-cancel"); err != nil || got != 1 {
		t.Fatalf("search after cancellation: %d, %v", got, err)
	}
}
