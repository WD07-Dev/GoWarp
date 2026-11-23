package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"reflect"
	"strings"
	"testing"
)

func call(t *testing.T, op string, args map[string]any, host e.Host) e.Result {
	t.Helper()
	for _, b := range New().Blocks {
		if b.Opcode == "skyhigh173JSON_"+op {
			r, err := b.Handler(&e.Context{Args: args, Host: host})
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
	}
	t.Fatalf("missing %s", op)
	return e.Result{}
}
func TestJSONSemantics(t *testing.T) {
	cases := []struct {
		op   string
		args map[string]any
		want any
	}{
		{"json_new", map[string]any{"json": "Array"}, "[]"},
		{"json_new", map[string]any{"json": "Object"}, "{}"},
		{"json_array_get", map[string]any{"json": `[true,false,"3"]`, "item": 0.0}, ""},
		{"json_array_get", map[string]any{"json": `[true,false,"3"]`, "item": -3.0}, true},
		{"json_array_get", map[string]any{"json": `[true,false,"3"]`, "item": -4.0}, ""},
		{"json_array_get", map[string]any{"json": `[true,false,"3"]`, "item": "2"}, false},
		{"json_set", map[string]any{"json": `{}`, "item": "x", "value": "false"}, `{"x":"false"}`},
		{"json_set", map[string]any{"json": `{}`, "item": "x", "value": false}, `{"x":false}`},
		{"json_set", map[string]any{"json": `{}`, "item": "x", "value": "{}"}, `{"x":{}}`},
		{"json_get", map[string]any{"json": `{"x":{}}`, "item": "x"}, `{}`},
		{"json_get", map[string]any{"json": `{"x":[]}`, "item": "x"}, `[]`},
		{"json_get", map[string]any{"json": `{"x":false}`, "item": "x"}, false},

		{"json_get", map[string]any{"json": `{"b":{"z":1,"a":2},"a":null}`, "item": "b"}, `{"z":1,"a":2}`},
		{"json_get", map[string]any{"json": `{"a":null}`, "item": "a"}, ""},
		{"json_array_get", map[string]any{"json": `[1,"two",{"z":3}]`, "item": -1.0}, `{"z":3}`},
		{"json_array_get", map[string]any{"json": `[1,2]`, "item": 1.5}, ""},
		{"json_array_push", map[string]any{"json": `[]`, "item": "100"}, `["100"]`},
		{"json_array_push", map[string]any{"json": `[]`, "item": `{"a":1}`}, `[{"a":1}]`},
		{"json_set", map[string]any{"json": `{"z":0}`, "item": "a", "value": `[1,2]`}, `{"z":0,"a":[1,2]}`},
		{"json_array_set", map[string]any{"json": `[1]`, "pos": 3.0, "item": "x"}, `[1,null,"x"]`},
		{"json_array_itemH", map[string]any{"json": `[1,"1"]`, "item": "1"}, "2"},
		{"json_array_itemH", map[string]any{"json": `[{"a":1}]`, "item": `{"a":1}`}, "0"},
		{"json_has_value", map[string]any{"json": `[1]`, "value": "1"}, false},
		{"json_has_value", map[string]any{"json": `["1"]`, "value": "1"}, true},
		{"json_array_from", map[string]any{"json": "中😀"}, `["中","😀"]`},
		{"json_length", map[string]any{"json": "broken"}, " "},
		{"json_length", map[string]any{"json": `{"a":1,"b":2}`}, 2.0},
	}
	for _, tt := range cases {
		t.Run(tt.op, func(t *testing.T) {
			got := call(t, tt.op, tt.args, nil).Value
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

type listHost struct{ items []any }

func (h *listHost) Call(op string, args ...any) (any, error) {
	if op == "list.get" {
		return h.items, nil
	}
	h.items = args[1].([]any)
	return nil, nil
}
func TestListTransfer(t *testing.T) {
	h := &listHost{}
	call(t, "json_vm_setlist", map[string]any{"list": "id", "json": `[null,{"b":2},3]`}, h)
	if !reflect.DeepEqual(h.items, []any{"null", `{"b":2}`, 3.0}) {
		t.Fatal(h.items)
	}
	got := call(t, "json_vm_getlist", map[string]any{"list": "id"}, h).Value
	if got != `["null","{\"b\":2}",3]` {
		t.Fatal(got)
	}
}

func TestRepeatedJSONReadAvoidsParsingAllocations(t *testing.T) {
	x := New()
	var get e.Handler
	for _, block := range x.Blocks {
		if block.Opcode == "skyhigh173JSON_json_get" {
			get = block.Handler
		}
	}
	c := &e.Context{Args: map[string]any{"json": `{"x":12,"y":34,"font":"Determination Mono","color":"#ffffff"}`, "item": "x"}}
	allocations := testing.AllocsPerRun(100, func() {
		got, err := get(c)
		if err != nil || got.Value != float64(12) {
			t.Fatalf("read=%v err=%v", got.Value, err)
		}
	})
	if allocations != 0 {
		t.Fatalf("unchanged JSON read allocated %g objects", allocations)
	}
}

func TestJSONReadsRemainUnchangedAfterWrites(t *testing.T) {
	handlers := map[string]e.Handler{}
	for _, block := range New().Blocks {
		handlers[block.Opcode] = block.Handler
	}
	invoke := func(op string, args map[string]any) any {
		t.Helper()
		result, err := handlers["skyhigh173JSON_"+op](&e.Context{Args: args})
		if err != nil {
			t.Fatal(err)
		}
		return result.Value
	}
	original := `{"x":12,"nested":{"v":7}}`

	if got := invoke("json_get", map[string]any{"json": original, "item": "nested"}); got != `{"v":7}` {
		t.Fatal(got)
	}
	changed := invoke("json_set", map[string]any{"json": original, "item": "nested", "value": `{"v":9}`})
	if got := invoke("json_get", map[string]any{"json": changed, "item": "nested"}); got != `{"v":9}` {
		t.Fatal("new source reused a stale serialized result", got)
	}
	if got := invoke("json_get", map[string]any{"json": original, "item": "nested"}); got != `{"v":7}` {
		t.Fatal("write mutated the cached serialized result", got)
	}
	if got := invoke("json_get", map[string]any{"json": original, "item": "x"}); got != float64(12) {
		t.Fatal(got)
	}
	if got := invoke("json_set", map[string]any{"json": original, "item": "x", "value": 99.0}); got != `{"x":99,"nested":{"v":7}}` {
		t.Fatal(got)
	}
	if got := invoke("json_get", map[string]any{"json": original, "item": "x"}); got != float64(12) {
		t.Fatal("cached source was modified", got)
	}
	for _, op := range []string{"json_array_push", "json_array_set"} {
		source := `[1,2]`
		invoke("json_array_get", map[string]any{"json": source, "item": 1.0})
		invoke(op, map[string]any{"json": source, "pos": 1.0, "item": 99.0})
		if got := invoke("json_array_get", map[string]any{"json": source, "item": 1.0}); got != float64(1) {
			t.Fatal("cached array was modified", got)
		}
	}
}

func BenchmarkRepeatedJSONRead(b *testing.B) {
	var get e.Handler
	for _, block := range New().Blocks {
		if block.Opcode == "skyhigh173JSON_json_get" {
			get = block.Handler
		}
	}
	c := &e.Context{Args: map[string]any{"json": `{"x":12,"y":34,"font":"Determination Mono","color":"#ffffff"}`, "item": "x"}}
	b.ReportAllocs()
	for b.Loop() {
		if result, err := get(c); err != nil || result.Value != float64(12) {
			b.Fatal(result, err)
		}
	}
}

func TestJSONReadCacheIsBounded(t *testing.T) {
	var cache readCache
	for i := 0; i < 100; i++ {
		source := fmt.Sprintf(`{"id":%d,"text":%q}`, i, strings.Repeat("a", 3000))
		if _, valid := cache.parse(source); !valid {
			t.Fatal("valid input rejected")
		}
		if len(cache.entries) > 32 || cache.bytes > 64<<10 {
			t.Fatal("cache exceeded bounds")
		}
	}
	for i := 0; i < 100; i++ {
		cache.parse(fmt.Sprint(i))
		if len(cache.entries) > 32 {
			t.Fatal("small entries exceeded the count bound")
		}
	}
	entries, bytes := len(cache.entries), cache.bytes
	for _, source := range []string{`{"large":"` + strings.Repeat("a", 5000) + `"}`, `broken`} {
		cache.parse(source)
		if len(cache.entries) != entries || cache.bytes != bytes {
			t.Fatal("large or invalid input displaced cached values")
		}
	}
	if _, valid := cache.parse(`{"id":0}`); !valid {
		t.Fatal("evicted input could not be read again")
	}
}

func TestRepeatedJSONContainerReadAvoidsSerialization(t *testing.T) {
	for _, test := range []struct {
		op, source string
		item       any
		want       string
	}{
		{"json_get", `{"style":{"font":"Mono","color":"#fff"},"x":1}`, "style", `{"font":"Mono","color":"#fff"}`},
		{"json_get", `{"points":[1,2,3]}`, "points", `[1,2,3]`},
		{"json_array_get", `[0,{"x":7,"y":9}]`, -1.0, `{"x":7,"y":9}`},
	} {
		t.Run(test.op+test.source, func(t *testing.T) {
			var handler e.Handler
			for _, block := range New().Blocks {
				if block.Opcode == "skyhigh173JSON_"+test.op {
					handler = block.Handler
				}
			}
			context := &e.Context{Args: map[string]any{"json": test.source, "item": test.item}}
			allocations := testing.AllocsPerRun(100, func() {
				result, err := handler(context)
				if err != nil || result.Value != test.want {
					t.Fatalf("result=%v error=%v", result.Value, err)
				}
			})
			if allocations > 1 {
				t.Fatalf("unchanged nested JSON allocated %g objects per read", allocations)
			}
		})
	}
}

func BenchmarkRepeatedJSONContainerRead(b *testing.B) {
	var handler e.Handler
	for _, block := range New().Blocks {
		if block.Opcode == "skyhigh173JSON_json_get" {
			handler = block.Handler
		}
	}
	context := &e.Context{Args: map[string]any{
		"json": `{"style":{"font":"Mono","color":"#fff","size":16,"position":[12,34]},"x":1}`,
		"item": "style",
	}}
	b.ReportAllocs()
	for b.Loop() {
		result, err := handler(context)
		if err != nil || result.Value != `{"font":"Mono","color":"#fff","size":16,"position":[12,34]}` {
			b.Fatal(result, err)
		}
	}
}

func TestJSONSerializedReadCacheIsBounded(t *testing.T) {
	var cache readCache
	for i := 0; i < 100; i++ {
		source := fmt.Sprintf(`{"id":%d,"nested":{"v":7},"array":[1,2]}`, i)
		value, valid := cache.parse(source)
		if !valid {
			t.Fatal("valid source rejected")
		}
		for _, key := range []string{"nested", "array"} {
			cache.result(source, key, get(value, key))
		}
		total := 0
		for text, entry := range cache.entries {
			if entry.resultBytes > len(text) || len(entry.results) > 64 {
				t.Fatal("serialized results exceeded their source budget")
			}
			total += entry.resultBytes
		}
		if total > 64<<10 || len(cache.entries) > 32 {
			t.Fatal("serialized results survived source eviction")
		}
	}
	source := `{"nested":{"text":"` + strings.Repeat("a", 5000) + `"}}`
	value, valid := cache.parse(source)
	if !valid {
		t.Fatal("large source rejected")
	}
	cache.result(source, "nested", get(value, "nested"))
	if cache.entries[source] != nil {
		t.Fatal("large read retained an unbounded result")
	}
}
