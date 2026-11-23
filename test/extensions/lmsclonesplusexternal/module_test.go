package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"reflect"
	"testing"
)

type data struct {
	name  string
	value any
}
type fakeTarget struct {
	id, name         string
	source, clone    float64
	variables, lists map[string]data
	x                float64
}
type targetHost struct {
	targets []*fakeTarget
	next    float64
	refuse  bool
	deleted []string
}

func (h *targetHost) Call(op string, args ...any) (any, error) {
	if op == "target.list" {
		out := []any{}
		for _, t := range h.targets {
			out = append(out, map[string]any{"id": t.id, "name": t.name, "source": t.source, "cloneId": t.clone, "isStage": t.source == 0})
		}
		return out, nil
	}
	ref := e.String(args[len(args)-1])
	var target *fakeTarget
	for _, t := range h.targets {
		if t.id == ref {
			target = t
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("unknown target %s", ref)
	}
	switch op {
	case "variable.list", "list.list":
		values := target.variables
		if op == "list.list" {
			values = target.lists
		}
		out := []any{}
		for id, d := range values {
			out = append(out, map[string]any{"id": id, "name": d.name})
		}
		return out, nil
	case "variable.get":
		return target.variables[e.String(args[0])].value, nil
	case "list.get":
		return append([]any{}, target.lists[e.String(args[0])].value.([]any)...), nil
	case "variable.set":
		id := e.String(args[0])
		v, ok := target.variables[id]
		if !ok {
			return nil, fmt.Errorf("unexpected variable creation %s", id)
		}
		v.value = args[1]
		target.variables[id] = v
		return nil, nil
	case "list.replace":
		id := e.String(args[0])
		d := target.lists[id]
		d.value.([]any)[int(e.Number(args[1]))-1] = args[2]
		return nil, nil
	case "target.get":
		switch args[0] {
		case "x":
			return target.x, nil
		case "currentCostume":
			return float64(1), nil
		case "volume":
			return float64(80), nil
		}
		return nil, fmt.Errorf("unexpected property %v", args[0])
	case "target.costumes":
		return []any{map[string]any{"name": "first"}, map[string]any{"name": "second"}}, nil
	case "target.delete":
		for i, t := range h.targets {
			if t == target {
				h.targets = append(h.targets[:i], h.targets[i+1:]...)
				break
			}
		}
		h.deleted = append(h.deleted, ref)
		return nil, nil
	case "target.clone":
		if h.refuse {
			return nil, nil
		}
		h.next++
		n := *target
		n.id = fmt.Sprintf("1:%g", h.next)
		n.clone = h.next
		n.variables = map[string]data{}
		for k, v := range target.variables {
			n.variables[k] = v
		}
		n.lists = map[string]data{}
		for k, v := range target.lists {
			v.value = append([]any{}, v.value.([]any)...)
			n.lists[k] = v
		}
		h.targets = append(h.targets, &n)
		return n.id, nil
	}
	return nil, fmt.Errorf("unexpected %s", op)
}
func fixture() (*targetHost, *e.Context) {
	mk := func(id string, clone float64, value any) *fakeTarget {
		return &fakeTarget{id: id, name: "sprite", source: 1, clone: clone, variables: map[string]data{"match": {"tag", value}, "value": {"health", float64(10)}}, lists: map[string]data{"items": {"inventory", []any{"potion", "key"}}}, x: clone * 10}
	}
	host := &targetHost{targets: []*fakeTarget{mk("1:0", 0, "YES"), mk("1:2", 2, "yes"), mk("1:1", 1, "YES")}, next: 2}
	return host, &e.Context{State: map[string]any{}, Host: host, Args: map[string]any{}, Fields: map[string]string{}}
}
func invoke(t *testing.T, c *e.Context, op string, args map[string]any, fields map[string]string) any {
	t.Helper()
	c.Args = args
	c.Fields = fields
	for _, b := range New().Blocks {
		if b.Opcode == "lmsclonesplusexternal_"+op {
			v, err := b.Handler(c)
			if err != nil {
				t.Fatal(err)
			}
			return v.Value
		}
	}
	t.Fatalf("missing %s", op)
	return nil
}
func selectSet(t *testing.T, c *e.Context, first bool) string {
	op := "selectClonesByVariable"
	if first {
		op = "selectFirstCloneByVariable"
	}
	return invoke(t, c, op, map[string]any{"SPRITE": "sprite", "MATCH_VALUE": "yes"}, map[string]string{"MATCH_VAR": "tag"}).(string)
}
func TestSelectionIntersectionAndLocalMutation(t *testing.T) {
	h, c := fixture()
	all := selectSet(t, c, false)
	first := selectSet(t, c, true)
	if again := selectSet(t, c, true); again != first {
		t.Fatal("first clone handle was not reused")
	}
	count := func(token string) any { return invoke(t, c, "targetSetCount", map[string]any{"TARGETS": token}, nil) }
	if count(all) != 2.0 || count(first) != 1.0 {
		t.Fatal("selected original or missed clones")
	}
	both := invoke(t, c, "intersectTargetSets", map[string]any{"TARGETS_A": all, "TARGETS_B": first}, nil).(string)
	if count(both) != 1.0 {
		t.Fatal("intersection")
	}
	invoke(t, c, "setVariableOfTargetSet", map[string]any{"TARGETS": both, "VALUE": 99.0}, map[string]string{"VALUE_VAR": "health"})
	if h.targets[0].variables["value"].value != 10.0 || h.targets[1].variables["value"].value != 10.0 || h.targets[2].variables["value"].value != 99.0 {
		t.Fatal("wrong mutation targets")
	}
	if got := invoke(t, c, "getVariableOfFirstInTargetSet", map[string]any{"TARGETS": all}, map[string]string{"VALUE_VAR": "value"}); got != 99.0 {
		t.Fatalf("first clone ordering: %v", got)
	}
	if got := invoke(t, c, "getThingOfFirstInTargetSet", map[string]any{"TARGETS": all}, map[string]string{"THING": "x position"}); got != 10.0 {
		t.Fatal(got)
	}
	for field, want := range map[string]any{"costume name": "second", "costume num": 2.0, "volume": 80.0} {
		if got := invoke(t, c, "getThingOfFirstInTargetSet", map[string]any{"TARGETS": first}, map[string]string{"THING": field}); got != want {
			t.Fatalf("%s: %v", field, got)
		}
	}
	invoke(t, c, "deleteTargetSet", map[string]any{"TARGETS": all}, nil)
	if !reflect.DeepEqual(h.deleted, []string{"1:2", "1:1"}) || count(all) != 0.0 {
		t.Fatal("deletion/stale handle", h.deleted)
	}
}
func TestListQueriesAndLocalizedIndices(t *testing.T) {
	_, c := fixture()
	token := invoke(t, c, "selectClonesByListItem", map[string]any{"SPRITE": "sprite", "MATCH_INDEX": "마지막", "MATCH_VALUE": "KEY"}, map[string]string{"MATCH_LIST": "inventory"}).(string)
	invoke(t, c, "replaceInListOfTargetSet", map[string]any{"TARGETS": token, "INDEX": 1.9, "ITEM": "gem"}, map[string]string{"VALUE_LIST": "items"})
	got := invoke(t, c, "getItemOfListOfFirstInTargetSet", map[string]any{"TARGETS": token, "INDEX": 1}, map[string]string{"VALUE_LIST": "items"})
	if got != "gem" {
		t.Fatal(got)
	}
	contains := invoke(t, c, "selectClonesByListContaining", map[string]any{"SPRITE": "sprite", "ITEM": "GEM"}, map[string]string{"MATCH_LIST": "items"}).(string)
	if got := invoke(t, c, "targetSetCount", map[string]any{"TARGETS": contains}, nil); got != 2.0 {
		t.Fatal(got)
	}
	if got := invoke(t, c, "getLengthOfListOfFirstInTargetSet", map[string]any{"TARGETS": token}, map[string]string{"VALUE_LIST": "items"}); got != 2.0 {
		t.Fatal(got)
	}
	for _, idx := range []any{0, -1, 3, "all", "모두"} {
		if got := invoke(t, c, "getItemOfListOfFirstInTargetSet", map[string]any{"TARGETS": token, "INDEX": idx}, map[string]string{"VALUE_LIST": "items"}); got != "" {
			t.Fatalf("invalid %v: %v", idx, got)
		}
	}
	if got := invoke(t, c, "selectClonesByListItem", map[string]any{"SPRITE": "sprite", "MATCH_INDEX": 99, "MATCH_VALUE": ""}, map[string]string{"MATCH_LIST": "items"}).(string); len(state(c).sets[got].ids) != 0 {
		t.Fatal("invalid index matched empty string")
	}
}
func TestMissingDataCloneRefusalAndReset(t *testing.T) {
	h, c := fixture()
	all := selectSet(t, c, false)
	invoke(t, c, "setVariableOfTargetSet", map[string]any{"TARGETS": all, "VALUE": "bad"}, map[string]string{"VALUE_VAR": "missing"})
	if got := invoke(t, c, "getVariableOfFirstInTargetSet", map[string]any{"TARGETS": all}, map[string]string{"VALUE_VAR": "missing"}); got != "" {
		t.Fatal(got)
	}
	h.refuse = true
	invoke(t, c, "createCloneInSpriteWithVar", map[string]any{"SPRITE": "sprite", "MATCH_VALUE": "new"}, map[string]string{"MATCH_VAR": "tag"})
	if h.targets[2].variables["match"].value != "YES" {
		t.Fatal("refused clone changed existing clone")
	}
	h.refuse = false
	invoke(t, c, "createCloneInSpriteWithVar", map[string]any{"SPRITE": "sprite", "MATCH_VALUE": "new"}, map[string]string{"MATCH_VAR": "tag"})
	if h.targets[3].variables["match"].value != "new" || h.targets[0].variables["match"].value != "YES" {
		t.Fatal("clone initialization")
	}
	New().Events["flag"](c)
	if got := invoke(t, c, "targetSetHasAny", map[string]any{"TARGETS": all}, nil); got != false {
		t.Fatal("stale handle survived reset")
	}
}
func TestRegistryCleanupProtectsStoredHandles(t *testing.T) {
	h, c := fixture()
	token := selectSet(t, c, false)
	d := h.targets[0].variables["value"]
	d.value = token
	h.targets[0].variables["value"] = d
	s := state(c)
	for i := 0; i < 4100; i++ {
		s.serial++
		s.sets[fmt.Sprint(i)] = &targetSet{used: s.serial}
	}
	ts, _ := targets(c)
	latest, err := store(c, nil, ts)
	if err != nil {
		t.Fatal(err)
	}
	if s.sets[token] == nil || s.sets[latest] == nil || len(s.sets) > 3072 {
		t.Fatal("registry cleanup lost protected handle or failed to bound temporary sets")
	}
}
