package extension

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"testing"

	e "gowarp/core/extension"
)

type testList struct {
	name   string
	values []any
}
type listHost map[string]map[string]*testList

func (h listHost) Call(op string, args ...any) (any, error) {
	switch op {
	case "list.list":
		lists := h[args[0].(string)]
		ids := make([]string, 0, len(lists))
		for id := range lists {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		out := []any{}
		for _, id := range ids {
			out = append(out, map[string]any{"id": id, "name": lists[id].name})
		}
		return out, nil
	case "list.get":
		list := h[args[1].(string)][args[0].(string)]
		return append([]any{}, list.values...), nil
	case "list.set":
		h[args[2].(string)][args[0].(string)].values = append([]any{}, args[1].([]any)...)
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected operation %s", op)
}

func invoke(t *testing.T, h listHost, opcode string, args map[string]any, fields map[string]string) any {
	t.Helper()
	for _, block := range New().Blocks {
		if block.Opcode == "lmsData_"+opcode {
			result, err := block.Handler(&e.Context{Host: h, Args: args, Fields: fields})
			if err != nil {
				t.Fatal(err)
			}
			return result.Value
		}
	}
	t.Fatalf("missing block %s", opcode)
	return nil
}

func TestScopedListLookupAndMenu(t *testing.T) {
	h := listHost{
		"_stage_":  {"global": {"shared", []any{3, 1, 2}}},
		"_myself_": {"local": {"shared", []any{}}, "own": {"private", []any{"value"}}},
	}
	for _, tc := range []struct {
		name  string
		empty bool
	}{{"shared", false}, {"private", false}, {"absent", true}, {"global", true}} {
		if got := invoke(t, h, "listIsEmpty", map[string]any{"LIST": tc.name}, nil); got != tc.empty {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
	invoke(t, h, "orderList", map[string]any{"LIST": "shared"}, map[string]string{"ORDER": "ascending"})
	if !reflect.DeepEqual(h["_stage_"]["global"].values, []any{1, 2, 3}) || len(h["_myself_"]["local"].values) != 0 {
		t.Fatal("stage-first lookup changed", h)
	}
	if got := invoke(t, h, "menu_lists", nil, map[string]string{"lists": "private"}); got != "private" {
		t.Fatal(got)
	}
}

func TestScratchSortingAndFieldDropdown(t *testing.T) {
	h := listHost{"_stage_": {"id": {"items", []any{"10", "2", "A", "a", "b"}}}}
	invoke(t, h, "orderList", map[string]any{"LIST": "items", "ORDER": "reversed"}, map[string]string{"ORDER": "ascending"})
	want := []any{"2", "10", "A", "a", "b"}
	if got := h["_stage_"]["id"].values; !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if got := invoke(t, h, "orderIs", map[string]any{"LIST": "items"}, map[string]string{"ORDER": "ascending"}); got != true {
		t.Fatal(got)
	}
	invoke(t, h, "orderList", map[string]any{"LIST": "items"}, map[string]string{"ORDER": "descending"})
	if got := h["_stage_"]["id"].values; !reflect.DeepEqual(got, []any{"b", "a", "A", "10", "2"}) {
		t.Fatal(got)
	}
}

func TestListEditsAndJavaScriptEquality(t *testing.T) {
	h := listHost{"_stage_": {"id": {"items", []any{float64(1), "1", "A", "a", true}}}}
	if got := invoke(t, h, "timesItemAppears", map[string]any{"LIST": "items", "ITEM": 1}, nil); got != 3 {
		t.Fatal(got)
	}
	if got := invoke(t, h, "timesItemAppears", map[string]any{"LIST": "items", "ITEM": "A"}, nil); got != 1 {
		t.Fatal(got)
	}
	invoke(t, h, "deleteAllOfItem", map[string]any{"LIST": "items", "ITEM": "1"}, nil)
	invoke(t, h, "replaceAllOfItem", map[string]any{"LIST": "items", "ITEM1": 1, "ITEM2": "changed"}, nil)
	if got := h["_stage_"]["id"].values; !reflect.DeepEqual(got, []any{"changed", "A", "a", true}) {
		t.Fatal(got)
	}
	invoke(t, h, "deleteItems", map[string]any{"LIST": "items", "NUM1": 3, "NUM2": 2}, nil)
	if got := h["_stage_"]["id"].values; !reflect.DeepEqual(got, []any{"changed", true}) {
		t.Fatal(got)
	}
}

func TestListCopyRepeatAndJSON(t *testing.T) {
	h := listHost{"_stage_": {"a": {"a", []any{"x"}}, "b": {"b", []any{"y"}}}}
	invoke(t, h, "repeatList", map[string]any{"LIST1": "a", "LIST2": "a", "NUM": 2}, nil)
	if got := h["_stage_"]["a"].values; !reflect.DeepEqual(got, []any{"x", "x", "x"}) {
		t.Fatal(got)
	}
	invoke(t, h, "setListToList", map[string]any{"LIST1": "a", "LIST2": "b"}, nil)
	invoke(t, h, "joinLists", map[string]any{"LIST1": "a", "LIST2": "b"}, nil)
	if got := h["_stage_"]["a"].values; !reflect.DeepEqual(got, []any{"y"}) {
		t.Fatal(got)
	}
	invoke(t, h, "setListArray", map[string]any{"LIST": "a", "ARRAY": "[1,null,\"<a>\"]"}, nil)
	invoke(t, h, "setListArray", map[string]any{"LIST": "a", "ARRAY": "{}"}, nil)
	if got := invoke(t, h, "getListArray", map[string]any{"LIST": "a"}, nil); got != "[1,null,\"<a>\"]" {
		t.Fatal(got)
	}
	if got := invoke(t, h, "getListJoin", map[string]any{"LIST": "a", "STRING": "|"}, nil); got != "1||<a>" {
		t.Fatal(got)
	}
}

func TestItemIndices(t *testing.T) {
	h := listHost{"_stage_": {"id": {"items", []any{"x", "y", "x"}}}}
	for _, tc := range []struct {
		index any
		want  int
	}{{"_first_", 1}, {"_last_", 3}, {2, 3}, {3, 0}, {"first", 0}} {
		if got := invoke(t, h, "itemIndex", map[string]any{"LIST": "items", "ITEM": "x", "INDEX": tc.index}, nil); got != tc.want {
			t.Errorf("%v: %v", tc.index, got)
		}
	}
	for _, tc := range []struct {
		index any
		want  bool
	}{{"last", true}, {"random", true}, {"any", true}, {"all", false}, {0, false}, {3.9, true}, {4, false}} {
		if got := invoke(t, h, "itemNumExists", map[string]any{"LIST": "items", "NUM": tc.index}, nil); got != tc.want {
			t.Errorf("%v: %v", tc.index, got)
		}
	}
}

func TestNonfiniteNumbers(t *testing.T) {
	h := listHost{"_stage_": {"id": {"items", []any{math.NaN(), math.Inf(1), float64(0)}}}}
	if got := invoke(t, h, "timesItemAppears", map[string]any{"LIST": "items", "ITEM": 0}, nil); got != 1 {
		t.Fatal(got)
	}
	if got := invoke(t, h, "timesItemAppears", map[string]any{"LIST": "items", "ITEM": math.NaN()}, nil); got != 0 {
		t.Fatal(got)
	}
	invoke(t, h, "deleteAllOfItem", map[string]any{"LIST": "items", "ITEM": math.NaN()}, nil)
	if got := invoke(t, h, "getListArray", map[string]any{"LIST": "items"}, nil); got != "[null,null,0]" {
		t.Fatal(got)
	}
	if !math.IsNaN(h["_stage_"]["id"].values[0].(float64)) {
		t.Fatal("serialization changed list contents")
	}
}
