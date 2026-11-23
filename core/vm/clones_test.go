package vm

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gowarp/core/project"
)

func BenchmarkCloneLifecycle(b *testing.B) {
	for _, count := range []int{0, 300, 3000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			r := New(&project.Project{Targets: []*project.Target{{IsStage: true}, {Name: "Sprite"}}})
			r.options.CloneLimit = count + 1
			source := r.targets[1]
			for range count {
				r.createClone(source)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				r.createClone(source)
				r.deleteClone(r.targets[len(r.targets)-1])
			}
		})
	}
}

func TestCloneInsertionAndDeletionKeepIndependentOrders(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"Sprite","layerOrder":1},{"name":"Other","layerOrder":2}]`)
	source := r.targets[1]
	other := r.targets[2]
	// Drawing changes alone must not change the clone's execution position.
	r.moveLayer(source, 2, false, false)
	r.createClone(source)
	first := r.targets[3]
	r.moveLayer(first, 1, false, false)
	r.createClone(first)
	second := r.targets[4]
	check := func(wantDraw, wantExecutable, wantTargets []*target) {
		t.Helper()
		for name, pair := range map[string][2][]*target{
			"drawing": {r.drawList, wantDraw}, "execution": {r.executable, wantExecutable}, "snapshot": {r.targets, wantTargets},
		} {
			if len(pair[0]) != len(pair[1]) {
				t.Fatalf("%s targets = %d, want %d", name, len(pair[0]), len(pair[1]))
			}
			for i, got := range pair[0] {
				if got != pair[1][i] {
					t.Fatalf("%s position %d: got %s clone %d, want %s clone %d", name, i, got.state.Name, got.state.CloneID, pair[1][i].state.Name, pair[1][i].state.CloneID)
				}
			}
		}
		for i, target := range r.drawList {
			if target.state.LayerOrder != i {
				t.Fatalf("layer at position %d = %d", i, target.state.LayerOrder)
			}
		}
	}
	check([]*target{r.stage, second, first, other, source}, []*target{r.stage, second, first, source, other}, []*target{r.stage, source, other, first, second})
	r.deleteClone(first)
	check([]*target{r.stage, second, other, source}, []*target{r.stage, second, source, other}, []*target{r.stage, source, other, second})
	r.deleteClone(second)
	check([]*target{r.stage, other, source}, []*target{r.stage, source, other}, []*target{r.stage, source, other})
}

const cloneProject = `[
 {"isStage":true,"name":"Stage","variables":{"score":["score",0]},"blocks":{}},
 {"name":"Sprite","variables":{"local":["local",7]},"lists":{"localList":["localList",["original"]]},"blocks":{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"before"},
 "before":{"opcode":"data_setvariableto","next":"clone","fields":{"VARIABLE":["local","local"]},"inputs":{"VALUE":[1,[4,"9"]]}},
 "clone":{"opcode":"control_create_clone_of","next":"after","inputs":{"CLONE_OPTION":[1,"menu"]}},
 "menu":{"opcode":"control_create_clone_of_menu","fields":{"CLONE_OPTION":["_myself_",null]}},
 "after":{"opcode":"data_setvariableto","fields":{"VARIABLE":["local","local"]},"inputs":{"VALUE":[1,[4,"10"]]}},
 "cloneHat":{"opcode":"control_start_as_clone","topLevel":true,"next":"inc"},
 "inc":{"opcode":"data_changevariableby","next":"append","fields":{"VARIABLE":["local","local"]},"inputs":{"VALUE":[1,[4,"100"]]}},
 "append":{"opcode":"data_addtolist","next":"report","fields":{"LIST":["localList","localList"]},"inputs":{"ITEM":[1,[10,"clone"]]}},
 "report":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[3,[12,"local","local"],[4,"0"]]}}
 }}]`

func TestCloneCopiesCurrentState(t *testing.T) {
	r := eventProject(t, cloneProject)
	run(t, r, 1, false)
	s := r.Snapshot()
	if !r.Done() || len(s) != 3 || s[2].CloneID == 0 {
		t.Fatalf("clone not created: %+v", s)
	}
	if number(s[0].Variables["score"].Value) != 109 || number(s[1].Variables["local"].Value) != 10 || number(s[2].Variables["local"].Value) != 109 {
		t.Fatal("clone state was not isolated")
	}
	if len(s[1].Lists["localList"].Items) != 1 || len(s[2].Lists["localList"].Items) != 2 {
		t.Fatal("clone list aliases original")
	}
	s[2].Lists["localList"].Items[0] = "changed"
	if r.Snapshot()[2].Lists["localList"].Items[0] != "original" {
		t.Fatal("snapshot aliases clone")
	}
}

func TestNamedCloneAndInvalidSources(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
	}{{"Sprite", 3}, {"missing", 2}, {"Stage", 2}} {
		data := strings.Replace(cloneProject, `"_myself_"`, `"`+tc.name+`"`, 1)
		r := eventProject(t, data)
		run(t, r, 1, false)
		if len(r.Snapshot()) != tc.count {
			t.Fatalf("clone source %q", tc.name)
		}
	}
}

func TestCloneOfCloneCopiesOwnState(t *testing.T) {
	r := eventProject(t, `[
 {"isStage":true,"name":"Stage","blocks":{}},
 {"name":"Sprite","variables":{"n":["n",1]},"blocks":{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"create"},
 "create":{"opcode":"control_create_clone_of","inputs":{"CLONE_OPTION":[1,[10,"_myself_"]]}},
 "hat":{"opcode":"control_start_as_clone","topLevel":true,"next":"if"},
 "if":{"opcode":"control_if","inputs":{"CONDITION":[2,"less"],"SUBSTACK":[2,"inc"]}},
 "less":{"opcode":"operator_lt","inputs":{"OPERAND1":[3,[12,"n","n"],[4,"0"]],"OPERAND2":[1,[4,"3"]]}},
 "inc":{"opcode":"data_changevariableby","next":"create","fields":{"VARIABLE":["n","n"]},"inputs":{"VALUE":[1,[4,"1"]]}}
 }}]`)
	run(t, r, 1, false)
	s := r.Snapshot()
	if len(s) != 5 || number(s[1].Variables["n"].Value) != 1 || number(s[2].Variables["n"].Value) != 2 || number(s[3].Variables["n"].Value) != 3 || number(s[4].Variables["n"].Value) != 3 {
		t.Fatalf("wrong clone ancestry: %+v", s)
	}
	if s[2].CloneID == s[3].CloneID {
		t.Fatal("duplicate clone identity")
	}
}

func TestCloneDeletionAndOriginalNoOp(t *testing.T) {
	data := strings.Replace(cloneProject, `"report":{"opcode":`, `"report":{"opcode":"control_delete_this_clone","next":"bad"},"bad":{"opcode":`, 1)
	data = strings.Replace(data, `"after":{"opcode":`, `"after":{"opcode":"control_delete_this_clone","next":"originalAfter"},"originalAfter":{"opcode":`, 1)
	r := eventProject(t, data)
	run(t, r, 1, false)
	if !r.Done() || len(r.Snapshot()) != 2 || number(r.Snapshot()[0].Variables["score"].Value) != 0 || number(r.Snapshot()[1].Variables["local"].Value) != 10 {
		t.Fatal("wrong delete-clone behavior")
	}
}

func TestCloneLimitAndReuse(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"blocks":{}},{"name":"Sprite","blocks":{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"repeat"},
 "repeat":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"301"]],"SUBSTACK":[2,"create"]}},
 "create":{"opcode":"control_create_clone_of","inputs":{"CLONE_OPTION":[1,[10,"_myself_"]]}}
 }}]`)
	run(t, r, 1, true)
	if len(r.Snapshot()) != 302 {
		t.Fatalf("clone cap: %d", len(r.Snapshot()))
	}
	oldID := r.targets[2].state.CloneID
	r.deleteClone(r.targets[2])
	r.createClone(r.targets[1])
	if len(r.Snapshot()) != 302 || r.targets[len(r.targets)-1].state.CloneID <= oldID {
		t.Fatal("deleted clone slot not reusable")
	}
	r.deleteClone(r.targets[0])
	if len(r.Snapshot()) != 302 {
		t.Fatal("stage deleted")
	}
}

func TestCloneDeletionReleasesBackingReferences(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remove int
		want   []int
	}{
		{"first", 0, []int{0, 0, 0, 2, 3}},
		{"middle", 1, []int{0, 0, 0, 1, 3}},
		{"last", 2, []int{0, 0, 0, 1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"Sprite","lists":{"local":["local",["copied"]]}},{"name":"Other"}]`)
			var clones []*target
			for range 3 {
				r.createClone(r.targets[1])
				clone := r.targets[len(r.targets)-1]
				clones = append(clones, clone)
				r.moveLayer(clone, float64(len(r.drawList)-1), false, false)
			}
			r.deleteClone(clones[tc.remove])
			if r.cloneCount != 2 {
				t.Fatalf("clone count = %d, want 2", r.cloneCount)
			}
			states := r.Snapshot()
			if len(states) != len(tc.want) || len(r.drawList) != len(tc.want) {
				t.Fatalf("remaining targets = %d, drawable targets = %d, want %d", len(states), len(r.drawList), len(tc.want))
			}
			for i, id := range tc.want {
				if states[i].CloneID != id || r.drawList[i].state.CloneID != id || r.drawList[i].state.LayerOrder != i {
					t.Fatalf("target %d: snapshot clone = %d, drawable clone = %d, layer = %d; want clone %d, layer %d", i, states[i].CloneID, r.drawList[i].state.CloneID, r.drawList[i].state.LayerOrder, id, i)
				}
			}
			// The GC scans the entire backing array, including slots beyond len.
			for name, list := range map[string][]*target{"targets": r.targets, "drawList": r.drawList, "executable": r.executable} {
				for i, retained := range list[len(list):cap(list)] {
					if retained != nil {
						t.Errorf("%s backing slot %d retains clone %d after deletion", name, len(list)+i, retained.state.CloneID)
					}
				}
			}
		})
	}
}

func TestStopAllRemovesClones(t *testing.T) {
	data := strings.Replace(cloneProject, `"report":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[3,[12,"local","local"],[4,"0"]]}}`, `"report":{"opcode":"control_stop","fields":{"STOP_OPTION":["all",null]}}`, 1)
	r := eventProject(t, data)
	run(t, r, 1, false)
	if !r.Done() || len(r.Snapshot()) != 2 {
		t.Fatal("stop all retained clone")
	}
}

func TestDeletingCloneStopsAllItsThreads(t *testing.T) {
	data := strings.Replace(cloneProject, `"cloneHat":`, `"aSlowCloneHat":{"opcode":"control_start_as_clone","topLevel":true,"next":"longWait"},"longWait":{"opcode":"control_wait","next":"shouldNotRun","inputs":{"DURATION":[1,[4,"0.1"]]}},"shouldNotRun":{"opcode":"not_supported"},"cloneHat":`, 1)
	data = strings.Replace(data, `"report":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[3,[12,"local","local"],[4,"0"]]}}`, `"report":{"opcode":"control_delete_this_clone"}`, 1)
	r := eventProject(t, data)
	run(t, r, 10, false)
	if !r.Done() || len(r.threads) != 0 || len(r.Snapshot()) != 2 {
		t.Fatal("deletion retained clone threads")
	}
}

func TestNamedCloneIgnoresStageWithSameName(t *testing.T) {
	data := strings.Replace(cloneProject, `"name":"Sprite"`, `"name":"Stage"`, 1)
	data = strings.Replace(data, `"_myself_"`, `"Stage"`, 1)
	r := eventProject(t, data)
	run(t, r, 1, false)
	if len(r.Snapshot()) != 3 || r.Snapshot()[2].IsStage || number(r.Snapshot()[0].Variables["score"].Value) != 109 {
		t.Fatal("stage shadowed named sprite clone source")
	}
}

func TestCloneVolumeResetsAndEffectsCopy(t *testing.T) {
	r := eventProject(t, `[{"isStage":true},{"name":"Sprite","volume":0}]`)
	r.targets[1].state.Pitch = 120
	r.targets[1].state.Pan = 25
	r.createClone(r.targets[1])
	s := r.Snapshot()[2]
	if s.Volume != 100 || s.Pitch != 120 || s.Pan != 25 {
		t.Fatalf("wrong clone audio state: %+v", s)
	}
}

func TestCloneBroadcastExecutableOrder(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"lists":{"log":["log",[]]}},{"name":"Sprite","layerOrder":2,"variables":{"local":["local",1]},"blocks":{"hat":{"opcode":"event_whenbroadcastreceived","topLevel":true,"fields":{"BROADCAST_OPTION":["go",null]},"next":"add"},"add":{"opcode":"data_addtolist","fields":{"LIST":["log","log"]},"inputs":{"ITEM":[1,[12,"local","local"]]}}}},{"name":"Other","layerOrder":1}]`)
	// Keep drawable and executable positions different to test clone insertion.
	r.moveLayer(r.targets[1], 1, false, false)
	r.createClone(r.targets[1])
	v := r.targets[1].variables["local"]
	v.Value = float64(2)
	r.targets[1].variables["local"] = v
	r.broadcast("go")
	run(t, r, 1, false)
	values := r.Snapshot()[0].Lists["log"].Items
	if len(values) != 2 || number(values[0]) != 2 || number(values[1]) != 1 {
		t.Fatalf("broadcast order %v, want [2 1]", values)
	}
	// Keep the established snapshot order (originals, then clone creation order).
	if r.Snapshot()[1].CloneID != 0 || r.Snapshot()[3].CloneID == 0 {
		t.Fatal("snapshot order changed")
	}
}

func cloneListRuntime(length int) (*Runtime, *target) {
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true}, {Name: "Sprite"}}})
	source := r.targets[1]
	items := make([]any, length, length+8)
	for i := range items {
		items[i] = float64(i)
	}
	source.state.Lists["data"] = List{Name: "data", Items: items}
	return r, source
}

func TestReadOnlyCloneListsAvoidElementAllocations(t *testing.T) {
	allocations := func(length int) float64 {
		r, source := cloneListRuntime(length)
		return testing.AllocsPerRun(100, func() {
			r.createClone(source)
			r.deleteClone(r.targets[len(r.targets)-1])
		})
	}
	empty, large := allocations(0), allocations(8192)
	if large > empty {
		t.Fatalf("read-only clone copied list storage: large=%g allocations, empty=%g", large, empty)
	}
}

func BenchmarkCloneListLifecycle(b *testing.B) {
	for _, length := range []int{0, 128, 8192, 65536} {
		b.Run(fmt.Sprint(length), func(b *testing.B) {
			r, source := cloneListRuntime(length)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				r.createClone(source)
				r.deleteClone(r.targets[len(r.targets)-1])
			}
		})
	}
}

func TestCloneListWritesStayIndependent(t *testing.T) {
	for _, operation := range []string{"append", "replace", "delete", "insert", "clear", "native-delete", "native-replace", "extension-set", "data_deleteoflist", "data_insertatlist"} {
		for _, writeSource := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/source=%v", operation, writeSource), func(t *testing.T) {
				r, source := cloneListRuntime(1024)
				r.createClone(source)
				clone := r.targets[len(r.targets)-1]
				r.createClone(clone)
				child := r.targets[len(r.targets)-1]
				writer, other := clone, source
				if writeSource {
					writer, other = source, clone
				}
				before := append([]any(nil), other.state.Lists["data"].Items...)
				host := &extensionContext{r: r, th: &thread{target: writer}}
				var err error
				switch operation {
				case "append":
					_, err = host.Call("list.append", "data", "added")
				case "replace":
					_, err = host.Call("list.replace", "data", 1.0, "changed")
				case "delete":
					_, err = host.Call("list.delete", "data", 1.0)
				case "insert":
					_, err = host.Call("list.insert", "data", 1.0, "inserted")
				case "clear":
					_, err = host.Call("list.clear", "data")
				case "extension-set":
					_, err = host.Call("list.set", "data", []any{"new"})
				case "data_deleteoflist", "data_insertatlist":
					block := &project.Block{Opcode: operation, Fields: map[string][]any{"LIST": {"data", "data"}}}
					err = r.listStatement(writer, block, func(name string) (any, error) {
						if name == "INDEX" {
							return 1.0, nil
						}
						return "inserted", nil
					})
				default:
					steps := 0
					frame := nativeFrame{ctx: context.Background(), r: r, t: writer, steps: &steps}
					var slot nativeList
					list := frame.list(&slot, "data")
					if operation == "native-delete" {
						nativeDelete(list, 1.0)
					} else {
						list.replace(0, "native")
					}
					slot.dirty = true
					frame.flushList(&slot, "data")
					err = frame.err
				}
				if err != nil {
					t.Fatal(err)
				}
				if reflect.DeepEqual(writer.state.Lists["data"].Items, before) {
					t.Fatal("write had no effect")
				}
				if !reflect.DeepEqual(other.state.Lists["data"].Items, before) ||
					!reflect.DeepEqual(child.state.Lists["data"].Items, before) {
					t.Fatal("write changed another clone's list")
				}
				snapshot := r.Snapshot()
				snapshot[len(snapshot)-1].Lists["data"].Items[0] = "snapshot"
				if child.state.Lists["data"].Items[0] == "snapshot" {
					t.Fatal("snapshot shares mutable list storage")
				}
			})
		}
	}
}

func TestCloneListSearchIndexStaysIndependent(t *testing.T) {
	r, source := cloneListRuntime(1024)
	list := source.state.Lists["data"]
	list.Items[0] = "original"
	if index, _, err := list.findText(context.Background(), "original"); err != nil || index != 1 {
		t.Fatalf("initial search = %d, %v", index, err)
	}
	source.state.Lists["data"] = list
	r.createClone(source)
	clone := r.targets[len(r.targets)-1]
	host := &extensionContext{r: r, th: &thread{target: clone}}
	if _, err := host.Call("list.replace", "data", 1.0, "clone"); err != nil {
		t.Fatal(err)
	}
	for target, expected := range map[*target]string{source: "original", clone: "clone"} {
		list := target.state.Lists["data"]
		if index, _, err := list.findText(context.Background(), expected); err != nil || index != 1 {
			t.Fatalf("clone %d search %q = %d, %v", target.state.CloneID, expected, index, err)
		}
	}
}

func TestListInputCreatingCloneDoesNotShareWrite(t *testing.T) {
	r, source := cloneListRuntime(1024)
	block := &project.Block{Opcode: "data_replaceitemoflist", Fields: map[string][]any{"LIST": {"data", "data"}}}
	err := r.listStatement(source, block, func(name string) (any, error) {
		if name == "INDEX" {
			return 1.0, nil
		}
		r.createClone(source)
		return "changed after cloning", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if source.state.Lists["data"].Items[0] != "changed after cloning" ||
		r.targets[len(r.targets)-1].state.Lists["data"].Items[0] != float64(0) {
		t.Fatal("an input-created clone observed the parent's later write")
	}
}
