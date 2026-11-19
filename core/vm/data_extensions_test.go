package vm

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gowarp/core/project"
)

func TestDataListJSONConversion(t *testing.T) {
	r := listRuntime(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"array"},
 "array":{"opcode":"data_set_list_to_array","next":"set","fields":{"LIST":["items","items"]},"inputs":{"ARRAY":[1,[10,"[1,true,null,[2,null,{}],{},\"<x>\"]"]]}},
 "set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"json"]}},
 "json":{"opcode":"data_get_list_as","fields":{"LIST":["items","items"]}}
 }`, `["old"]`)
	if err := r.Run(context.Background(), 1, 30, false); err != nil {
		t.Fatal(err)
	}
	if got := r.Snapshot()[0].Variables["score"].Value; got != `[1,true,null,"2,,[object Object]","[object Object]","<x>"]` {
		t.Fatalf("converted list = %v", got)
	}
}

func TestDataListJSONLeavesUnicodeSeparatorsLiteral(t *testing.T) {
	r := listRuntime(t, `{}`, `[]`)
	r.stage.state.Lists["items"] = List{Items: []any{"a\u2028b\u2029c", "literal \\u2028"}}
	b := &project.Block{Opcode: "data_get_list_as", Fields: map[string][]any{"LIST": {"items", "items"}}}
	got, _, err := r.dataReporter(&thread{target: r.stage}, b, dataArguments(nil))
	if err != nil || got != "[\"a\u2028b\u2029c\",\"literal \\\\u2028\"]" {
		t.Fatalf("JSON string escaping: %q (%v)", got, err)
	}
}

func TestDataSavedTypedValue(t *testing.T) {
	r := listRuntime(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"save"},
 "save":{"opcode":"mistwarpData_set","next":"set","inputs":{"KEY":[1,[10," score "]],"VALUE":[1,[10,"12"]]}},
 "set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"get"]}},
 "get":{"opcode":"mistwarpData_get","inputs":{"KEY":[1,[10,"score"]]}}
 }`, `[]`)
	if err := r.Run(context.Background(), 1, 30, false); err != nil {
		t.Fatal(err)
	}
	if got := r.Snapshot()[0].Variables["score"].Value; got != float64(12) {
		t.Fatalf("saved value = %#v", got)
	}
}

func dataArguments(values map[string]any) func(string) (any, error) {
	return func(name string) (any, error) {
		return values[name], nil
	}
}

func TestDataListFormatsInvalidJSONAndLimit(t *testing.T) {
	r := listRuntime(t, `{}`, `["old"]`)
	th := &thread{target: r.stage}
	block := &project.Block{Opcode: "data_set_list_to_array", Fields: map[string][]any{"LIST": {"items", "items"}}}
	for _, raw := range []string{`bad`, `null`, `{}`, `"value"`} {
		if handled, err := r.dataStatement(th, block, dataArguments(map[string]any{"VALUE": raw})); !handled || err != nil {
			t.Fatalf("%s: %v %v", raw, handled, err)
		}
		if got := r.stage.state.Lists["items"].Items; !reflect.DeepEqual(got, []any{"old"}) {
			t.Fatalf("invalid JSON replaced list: %v", got)
		}
	}
	block.Opcode = "data_get_list_as"
	block.Fields["TYPE"] = []any{"text"}
	r.stage.state.Lists["items"] = List{Items: []any{float64(1), nil, true}}
	got, _, err := r.dataReporter(th, block, dataArguments(nil))
	if err != nil || got != "1, null, true" {
		t.Fatalf("text format = %v, %v", got, err)
	}
	block.Fields["FORMAT"] = []any{"jSoN"}
	r.stage.state.Lists["items"] = List{Items: []any{math.NaN(), math.Inf(1), "\n\"<>&", true}}
	got, _, err = r.dataReporter(th, block, dataArguments(nil))
	if err != nil || got != `[null,null,"\n\"<>&",true]` {
		t.Fatalf("JSON format = %v, %v", got, err)
	}
	block.Opcode = "data_set_list_to_array"
	raw := "[" + strings.Repeat("1,", 200000) + "2]"
	if _, err := r.dataStatement(th, block, dataArguments(map[string]any{"VALUE": raw})); err != nil {
		t.Fatal(err)
	}
	if got := r.stage.state.Lists["items"].Items; len(got) != 200000 || got[len(got)-1] != float64(1) {
		t.Fatal("JSON array exceeded list limit")
	}
}

func TestDataSavedValuesAndKeys(t *testing.T) {
	r := listRuntime(t, `{}`, `[]`)
	th := &thread{target: r.stage}
	for _, tc := range []struct {
		key         string
		value, want any
	}{
		{" number ", "12", float64(12)}, {"bool", "true", true}, {"native", false, false},
		{"null", "null", "null"}, {"quoted", `"hello"`, "hello"}, {"object", `{"x":[1,true]}`, `{"x":[1,true]}`},
		{"array", `[1,2]`, `[1,2]`}, {"plain", "hello", "hello"},
	} {
		_, err := r.dataStatement(th, &project.Block{Opcode: "mistwarpData_set"}, dataArguments(map[string]any{"KEY": tc.key, "VALUE": tc.value}))
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := r.dataReporter(th, &project.Block{Opcode: "mistwarpData_get"}, dataArguments(map[string]any{"KEY": strings.TrimSpace(tc.key)}))
		if err != nil || got != tc.want {
			t.Fatalf("key %q got %#v want %#v (%v)", tc.key, got, tc.want, err)
		}
	}
	for _, key := range []string{" ", "$reserved", strings.Repeat("a", 65), strings.Repeat("😀", 33)} {
		_, err := r.dataStatement(th, &project.Block{Opcode: "mistwarpData_set"}, dataArguments(map[string]any{"KEY": key, "VALUE": "bad"}))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := r.data.saved[strings.TrimSpace(key)]; ok {
			t.Fatalf("accepted invalid key %q", key)
		}
	}
	got, _, _ := r.dataReporter(th, &project.Block{Opcode: "mistwarpData_get"}, dataArguments(map[string]any{"KEY": " number "}))
	if got != "" {
		t.Fatal("get unexpectedly trimmed key")
	}
}

func TestDataSaveLoadEventsAndFailures(t *testing.T) {
	r := listRuntime(t, `{
 "loaded":{"opcode":"mistwarpData_whenLoaded","topLevel":true,"next":"inc"},
 "saved":{"opcode":"mistwarpData_whenSaved","topLevel":true,"next":"inc"},
 "inc":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
 }`, `[]`)
	path := filepath.Join(t.TempDir(), "nested", "save.json")
	if err := r.SetSavePath(path); err != nil {
		t.Fatal(err)
	}
	if r.data.status != "loaded" || len(r.threads) != 0 {
		t.Fatal("initial load fired event or wrong status")
	}
	th := &thread{target: r.stage}
	r.data.saved["score"] = float64(42)
	if _, err := r.dataStatement(th, &project.Block{Opcode: "mistwarpData_save"}, dataArguments(nil)); err != nil {
		t.Fatal(err)
	}
	if r.data.status != "saved" {
		t.Fatalf("save status %q", r.data.status)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"score":42}` {
		t.Fatalf("save file = %s", raw)
	}
	run(t, r, 1, false)
	if r.stage.variables["score"].Value != float64(1) {
		t.Fatal("saved hat did not run")
	}
	r.data.saved["score"] = float64(99)
	if _, err := r.dataStatement(th, &project.Block{Opcode: "mistwarpData_load"}, dataArguments(nil)); err != nil {
		t.Fatal(err)
	}
	run(t, r, 1, false)
	if r.data.saved["score"] != float64(42) || r.stage.variables["score"].Value != float64(2) {
		t.Fatal("load did not restore data and run hat")
	}
	for _, invalid := range []string{`broken`, `[]`, `null`} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if err := r.SetSavePath(path); err != nil {
			t.Fatal(err)
		}
		if r.data.status != "loaded" || len(r.data.saved) != 0 {
			t.Fatal("malformed save retained stale data")
		}
	}
	if err := r.SetSavePath("relative.json"); err == nil {
		t.Fatal("accepted relative save path")
	}
	// A file in the parent position makes creation fail without relying on permissions/root.
	if err := r.SetSavePath(filepath.Join(path, "child.json")); err != nil {
		t.Fatal(err)
	}
	_, err = r.dataStatement(th, &project.Block{Opcode: "mistwarpData_save"}, dataArguments(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.data.status, "error: could not write ") || len(r.threads) != 0 {
		t.Fatal("failed save fired success hat")
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "null" {
		t.Fatal("failed save changed existing file")
	}
	// Rename failure must also clean up its temporary file.
	directory := t.TempDir()
	if err := writeSavedData(directory, []byte(`{}`)); err == nil {
		t.Fatal("save over directory succeeded")
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(directory), ".gowarp-save-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary save leaked: %v %v", matches, err)
	}
}

func TestDataMonitorScopeCloneAndSnapshot(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage","variables":{"g":["global",1]},"lists":{"l":["list",[1,2]]}},{"name":"Sprite","variables":{"g":["local",2]}}],"monitors":[{"id":"g","opcode":"data_variable","visible":true},{"id":"g","opcode":"data_variable","spriteName":"Sprite","visible":false},{"id":"l","opcode":"data_listcontents","visible":false},{"id":"missing","opcode":"data_variable","visible":true}]}`), &p); err != nil {
		t.Fatal(err)
	}
	r := New(&p)
	r.createClone(r.targets[1])
	clone := r.targets[2]
	clone.variables["g"] = &Variable{Name: "local", Value: float64(99)}
	_, err := r.dataStatement(&thread{target: clone}, &project.Block{Opcode: "data_showvariable", Fields: map[string][]any{"VARIABLE": {"local", "g"}}}, dataArguments(nil))
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.dataStatement(&thread{target: clone}, &project.Block{Opcode: "data_showlist", Fields: map[string][]any{"LIST": {"list", "l"}}}, dataArguments(nil))
	if err != nil {
		t.Fatal(err)
	}
	got := r.Monitors()
	if len(got) != 3 || got[0].Label != "global" || got[0].Value != "1" || !got[1].Visible || got[1].Value != "2" || got[1].Label != "Sprite: local" || !got[2].Visible {
		t.Fatalf("wrong monitor scope: %+v", got)
	}
	got[2].Items[0] = "mutated"
	if r.Monitors()[2].Items[0] != "1" {
		t.Fatal("monitor aliases values")
	}
	_, err = r.dataStatement(&thread{target: clone}, &project.Block{Opcode: "data_hidevariable", Fields: map[string][]any{"VARIABLE": {"local", "g"}}}, dataArguments(nil))
	if err != nil {
		t.Fatal(err)
	}
	if r.Monitors()[1].Visible || !r.Monitors()[0].Visible {
		t.Fatal("hide affected wrong monitor")
	}
}

func TestDataVisibleMonitors(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage","variables":{"v":["score",7]},"lists":{"l":["bones",[1,2]]}}],"monitors":[{"id":"v","opcode":"data_variable","visible":true},{"id":"l","opcode":"data_listcontents","visible":false}]}`), &p); err != nil {
		t.Fatal(err)
	}
	r := New(&p)
	got := r.VisibleMonitors()
	if len(got) != 1 || got[0].ID != "v" || got[0].Value != "7" {
		t.Fatalf("visible monitors = %+v", got)
	}
	if all := r.Monitors(); len(all) != 2 || !reflect.DeepEqual(all[1].Items, []string{"1", "2"}) {
		t.Fatalf("full monitor snapshot changed: %+v", all)
	}

	th := &thread{target: r.stage}
	listBlock := &project.Block{Opcode: "data_showlist", Fields: map[string][]any{"LIST": {"bones", "l"}}}
	if _, err := r.dataStatement(th, listBlock, dataArguments(nil)); err != nil {
		t.Fatal(err)
	}
	got = r.VisibleMonitors()
	if len(got) != 2 || !reflect.DeepEqual(got[1].Items, []string{"1", "2"}) {
		t.Fatalf("shown list missing: %+v", got)
	}
	got[1].Items[0] = "changed snapshot"
	r.stage.state.Lists["l"].Items[1] = float64(3)
	if items := r.VisibleMonitors()[1].Items; !reflect.DeepEqual(items, []string{"1", "3"}) {
		t.Fatalf("visible list must be detached and current: %v", items)
	}
	listBlock.Opcode = "data_hidelist"
	if _, err := r.dataStatement(th, listBlock, dataArguments(nil)); err != nil {
		t.Fatal(err)
	}
	if got := r.VisibleMonitors(); len(got) != 1 {
		t.Fatalf("hidden list still included: %+v", got)
	}
	r.data.monitors[0].source.Visible = false
	if allocations := testing.AllocsPerRun(100, func() {
		if len(r.VisibleMonitors()) != 0 {
			t.Fatal("hidden monitors returned")
		}
	}); allocations != 0 {
		t.Fatalf("hidden monitors allocated %g objects per display frame", allocations)
	}
}

func BenchmarkHiddenListMonitors(b *testing.B) {
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true,
		Lists: map[string][]any{"bones": {"bones", []any{}}},
	}}, Monitors: []project.Monitor{{ID: "bones", Opcode: "data_listcontents"}}})
	items := make([]any, 13000)
	for i := range items {
		items[i] = float64(i) / 13
	}
	r.stage.state.Lists["bones"] = List{Name: "bones", Items: items}
	for _, tc := range []struct {
		name string
		read func() []MonitorState
	}{
		{"full_snapshot", r.Monitors},
		{"visible_only", r.VisibleMonitors},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = tc.read()
			}
		})
	}
}

func TestDataOversizedLoadClearsStateWithoutSuccessHat(t *testing.T) {
	r := listRuntime(t, `{"loaded":{"opcode":"mistwarpData_whenLoaded","topLevel":true,"next":"inc"},"inc":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}}`, `[]`)
	path := filepath.Join(t.TempDir(), "oversized.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate((16 << 20) + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSavePath(path); err != nil {
		t.Fatal(err)
	}
	r.data.saved["stale"] = "old"
	if _, err := r.dataStatement(&thread{target: r.stage}, &project.Block{Opcode: "mistwarpData_load"}, dataArguments(nil)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.data.status, "error:") || len(r.data.saved) != 0 || len(r.threads) != 0 {
		t.Fatalf("oversized load status=%q data=%v threads=%d", r.data.status, r.data.saved, len(r.threads))
	}
}

func TestDataOversizedSavePreservesFileAndRejectsAllReporter(t *testing.T) {
	r := listRuntime(t, `{"saved":{"opcode":"mistwarpData_whenSaved","topLevel":true,"next":"inc"},"inc":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}}`, `[]`)
	dir := t.TempDir()
	path := filepath.Join(dir, "save.json")
	if err := os.WriteFile(path, []byte(`{"old":42}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSavePath(path); err != nil {
		t.Fatal(err)
	}
	// Two references produce more than16MiB of JSON without allocating two large values.
	shared := strings.Repeat("x", 8<<20)
	r.data.saved = map[string]any{"a": shared, "b": shared}
	th := &thread{target: r.stage}
	if _, err := r.dataStatement(th, &project.Block{Opcode: "mistwarpData_save"}, dataArguments(nil)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.data.status, "error:") || len(r.threads) != 0 {
		t.Fatalf("oversized save status=%q threads=%d", r.data.status, len(r.threads))
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != `{"old":42}` {
		t.Fatalf("oversized save replaced previous data: bytes=%d error=%v", len(raw), err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("oversized save leaked temporary files: %v %v", entries, err)
	}
	if _, handled, err := r.dataReporter(th, &project.Block{Opcode: "mistwarpData_all"}, dataArguments(nil)); !handled || err == nil {
		t.Fatal("all reporter emitted oversized JSON")
	}
	oversized := make([]byte, (16<<20)+1)
	if err := writeSavedData(filepath.Join(dir, "new", "save.json"), oversized); err == nil {
		t.Fatal("write helper accepted oversized data")
	}
	if _, err := os.Stat(filepath.Join(dir, "new")); !os.IsNotExist(err) {
		t.Fatal("oversized write created destination directory")
	}
}

func TestDataJSONLimitCountsEscapingAndAcceptsBoundary(t *testing.T) {
	value := strings.Repeat("a", (16<<20)-2)
	encoded, err := jsonData(value)
	if err != nil || len(encoded) != 16<<20 || encoded[0] != '"' || encoded[len(encoded)-1] != '"' {
		t.Fatalf("exact limit: length=%d error=%v", len(encoded), err)
	}
	// Raw text still fits; the final control character requires a six-byte escape.
	value = value[:len(value)-1] + "\x00"
	encoded, err = jsonData(value)
	if err == nil || encoded != "" {
		t.Fatalf("escaped output exceeded cap: length=%d error=%v", len(encoded), err)
	}
}
