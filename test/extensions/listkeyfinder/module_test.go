package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"testing"
)

type list struct {
	id, name string
	items    []any
}
type listHost struct{ local, stage []list }

func (h *listHost) Call(op string, args ...any) (any, error) {
	ref := "_myself_"
	if len(args) > 0 {
		ref = e.String(args[len(args)-1])
	}
	lists := h.local
	if ref == "_stage_" {
		lists = h.stage
	}
	if op == "list.list" {
		records := []any{}
		for _, l := range lists {
			records = append(records, map[string]any{"id": l.id, "name": l.name})
		}
		return records, nil
	}
	if op == "list.get" {
		for _, l := range lists {
			if l.id == e.String(args[0]) {
				return l.items, nil
			}
		}
	}
	return nil, fmt.Errorf("unexpected %s %v", op, args)
}
func search(t *testing.T, h *listHost, token, key string, wanted any) any {
	t.Helper()
	c := &e.Context{Host: h, Args: map[string]any{"LIST": token, "KEY": key, "VALUE": wanted}}
	r, err := New().Blocks[1].Handler(c)
	if err != nil {
		t.Fatal(err)
	}
	return r.Value
}
func TestJSONKeySearch(t *testing.T) {
	h := &listHost{local: []list{{"local", "inventory", []any{"invalid", "[]", "null", `{"other":20}`, `{"id":"020"}`, `{"id":"Hello"}`, `{"id":null}`, `{"id":{"b":2,"a":1}}`, map[string]any{"id": true}, `{"id":[1,"two"]}`, `{"id":{"2":2,"1":1,"z":0}}`}}}}
	for _, tc := range []struct {
		wanted any
		index  float64
	}{{20, 5}, {"hello", 6}, {"", 7}, {`{"b":2,"a":1}`, 8}, {true, 9}, {`[1,"two"]`, 10}, {`{"1":1,"2":2,"z":0}`, 11}, {"missing", 0}} {
		if got := search(t, h, "inventory", "id", tc.wanted); got != tc.index {
			t.Fatalf("value %v: got %v, want %v", tc.wanted, got, tc.index)
		}
	}
	if got := search(t, h, "missing", "id", 20); got != 0.0 {
		t.Fatal(got)
	}
	if got := search(t, h, "local", "toString", ""); got != 0.0 {
		t.Fatal("inherited property matched")
	}
}
func TestListLookupPrefersIDsBeforeNamesAndLocalBeforeStage(t *testing.T) {
	h := &listHost{local: []list{{"local-id", "token", []any{`{"id":"local-name"}`}}, {"shared", "same", []any{`{"id":"local-id"}`}}}, stage: []list{{"token", "global", []any{`{"id":"stage-id"}`}}, {"shared", "same", []any{`{"id":"stage-shadow"}`}}}}
	if got := search(t, h, "token", "id", "stage-id"); got != 1.0 {
		t.Fatal("local name shadowed stage ID", got)
	}
	if got := search(t, h, "shared", "id", "local-id"); got != 1.0 {
		t.Fatal("stage shadowed local ID", got)
	}
	if got := search(t, h, "same", "id", "local-id"); got != 1.0 {
		t.Fatal("stage name shadowed local name", got)
	}
	if got := search(t, h, "global", "id", "stage-id"); got != 1.0 {
		t.Fatal("stage name lookup", got)
	}
}
func TestNestedObjectComparisonRetainsOrderAndDuplicateKeys(t *testing.T) {
	h := &listHost{local: []list{{"l", "l", []any{`{"id":{"z":1,"a":"<&>"}}`, `{"id":{"a":1,"b":2,"a":3}}`, `{"id":1} trailing`}}}}
	if got := search(t, h, "l", "id", `{"z":1,"a":"<&>"}`); got != 1.0 {
		t.Fatal(got)
	}
	if got := search(t, h, "l", "id", `{"a":"<&>","z":1}`); got != 0.0 {
		t.Fatal("object key order lost", got)
	}
	if got := search(t, h, "l", "id", `{"a":3,"b":2}`); got != 2.0 {
		t.Fatal("duplicate key semantics lost", got)
	}
}

func TestObjectComparisonPreservesUnicodeAndLiteralEscapes(t *testing.T) {
	h := &listHost{local: []list{{"l", "l", []any{`{"id":["\u2028"]}`, `{"id":["\\u2028"]}`}}}}
	if got := search(t, h, "l", "id", "[\"\u2028\"]"); got != 1.0 {
		t.Fatal(got)
	}
	if got := search(t, h, "l", "id", `["\u2028"]`); got != 0.0 {
		t.Fatal("JSON escape was not normalized", got)
	}
	if got := search(t, h, "l", "id", `["\\u2028"]`); got != 2.0 {
		t.Fatal("literal escape corrupted", got)
	}
}
