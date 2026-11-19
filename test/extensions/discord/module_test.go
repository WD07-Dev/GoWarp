package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"go/format"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	e "gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestRichPresencePayload(t *testing.T) {
	ext := New()
	if err := e.Validate(e.Bundle{New}, []string{"cubesterRichPresence"}, []string{"cubesterRichPresence_menu_DATA", "cubesterRichPresence_menu_SIZE", "cubesterRichPresence_menu_TOGGLE"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client := &Client{manager: newManager(ctx, transport{
		login:  func(string) error { return nil },
		update: func(Activity) error { return nil },
		logout: func() {},
	})}
	ext.State["client"] = client
	t.Cleanup(client.Close)
	call := func(name string, args map[string]any, fields map[string]string) any {
		t.Helper()
		for _, block := range ext.Blocks {
			if block.Opcode == "cubesterRichPresence_"+name {
				result, err := block.Handler(&e.Context{Args: args, Fields: fields, State: ext.State})
				if err != nil {
					t.Fatal(err)
				}
				return result.Value
			}
		}
		t.Fatalf("missing block %s", name)
		return nil
	}
	id := "123456789012345678"
	call("setClientID", map[string]any{"ID": id}, nil)
	if client.Status().ApplicationID != id || call("clientID", nil, nil) != id {
		t.Fatal("lost application ID precision")
	}
	call("setData", map[string]any{"INPUT": "4"}, map[string]string{"DATA": "party max"})
	call("setData", map[string]any{"INPUT": "2"}, map[string]string{"DATA": "party size"})
	call("setImage", map[string]any{"SIZE": "large", "IMAGE": "https://example.com/logo.png", "TEXT": "Game"}, nil)
	call("setImage", map[string]any{"SIZE": "small", "IMAGE": "icon", "TEXT": "Playing"}, nil)
	call("setTimestamp", nil, map[string]string{"TOGGLE": "on"})
	call("updatePresence", nil, nil)
	activity := client.manager.snapshot().activity
	if activity == nil || activity.LargeImageKey != "https://example.com/logo.png" || activity.SmallImageKey != "icon" || activity.PartyMax != 4 || activity.PartySize != 2 {
		t.Fatalf("activity: %+v", activity)
	}
	if delta := time.Now().Unix() - activity.StartTimestamp; delta < 0 || delta > 2 {
		t.Fatal("timestamp units", activity.StartTimestamp)
	}
	call("setImage", map[string]any{"SIZE": "large", "IMAGE": "new", "TEXT": "New"}, nil)
	if activity.LargeImageKey != "https://example.com/logo.png" {
		t.Fatal("activity aliased mutable state")
	}
	before := call("presenceData", nil, nil)
	for _, input := range []string{"[]", "null", "{"} {
		call("setData", map[string]any{"INPUT": input}, map[string]string{"DATA": "JSON"})
		if call("presenceData", nil, nil) != before || call("lastError", nil, nil) == "" {
			t.Fatal("invalid JSON replaced data or lost error")
		}
	}
	call("setTimestamp", nil, map[string]string{"TOGGLE": "off"})
	var data map[string]any
	if err := json.Unmarshal([]byte(call("presenceData", nil, nil).(string)), &data); err != nil {
		t.Fatal(err)
	}
	if _, ok := data["startTimestamp"]; ok {
		t.Fatal("timestamp not removed")
	}
	call("setData", map[string]any{"INPUT": `{"details":"Menu","instance":false}`}, map[string]string{"DATA": "JSON"})
	call("updatePresence", nil, nil)
	if got := client.manager.snapshot().activity; got == nil || *got != (Activity{Details: "Menu"}) {
		t.Fatalf("JSON did not replace activity: %+v", got)
	}
	call("clearPresence", nil, nil)
	if client.manager.snapshot().activity != nil {
		t.Fatal("activity not cleared")
	}
	call("disconnect", nil, nil)
	if client.Status().ApplicationID != "" || call("clientID", nil, nil) != "" {
		t.Fatal("client ID not cleared")
	}
	call("updatePresence", nil, nil)
	if !strings.Contains(call("lastError", nil, nil).(string), "connect with an application ID") {
		t.Fatal("connection error not exposed")
	}
	if New().State["data"].(map[string]any)["largeImageKey"] != nil {
		t.Fatal("state leaked between runtimes")
	}
}

func TestRichPresenceErrorsDoNotStopVM(t *testing.T) {
	literal := func(value string) []any { return []any{1, []any{10, value}} }
	for _, tc := range []struct{ name, opcode, input, value, want string }{
		{"invalid ID", "setClientID", "ID", "invalid", "nonzero decimal string"},
		{"not connected", "updatePresence", "", "", "connect with an application ID"},
		{"bad JSON", "setData", "INPUT", "[]", "cannot unmarshal array"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputs := map[string]any{tc.input: literal(tc.value)}
			fields := map[string]any{}
			if tc.opcode == "setData" {
				fields["DATA"] = []any{"JSON", nil}
			}
			data, err := json.Marshal(map[string]any{
				"extensions": []string{"cubesterRichPresence"},
				"targets": []any{map[string]any{
					"isStage": true, "name": "Stage",
					"variables": map[string]any{"result": []any{"result", ""}},
					"blocks": map[string]any{
						"flag":    map[string]any{"opcode": "event_whenflagclicked", "topLevel": true, "next": "command"},
						"command": map[string]any{"opcode": "cubesterRichPresence_" + tc.opcode, "inputs": inputs, "fields": fields, "next": "save"},
						"save":    map[string]any{"opcode": "data_setvariableto", "fields": map[string]any{"VARIABLE": []any{"result", "result"}}, "inputs": map[string]any{"VALUE": []any{2, "error"}}},
						"error":   map[string]any{"opcode": "cubesterRichPresence_lastError"},
					},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var p project.Project
			if err := json.Unmarshal(data, &p); err != nil {
				t.Fatal(err)
			}
			rt := vm.New(&p)
			if err := rt.SetExtensions(e.Bundle{New}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := rt.CloseExtensions(); err != nil {
					t.Error(err)
				}
			})
			if err := rt.Run(context.Background(), 1, 30, false); err != nil {
				t.Fatal(err)
			}
			value := rt.Snapshot()[0].Variables["result"].Value
			if message, ok := value.(string); !ok || !strings.Contains(message, tc.want) {
				t.Fatalf("error reporter = %v", value)
			}
		})
	}
}

func TestRichPresenceMenusInVM(t *testing.T) {
	var p project.Project
	err := json.Unmarshal([]byte(`{"extensions":["cubesterRichPresence"],"targets":[{"isStage":true,"name":"Stage","variables":{"result":["result",""]},"blocks":{
        "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"details"},
        "details":{"opcode":"cubesterRichPresence_setData","next":"state","fields":{"DATA":["details",null]},"inputs":{"INPUT":[1,[10,"챕터 1"]]}},
        "state":{"opcode":"cubesterRichPresence_setData","next":"timestamp","fields":{"DATA":["state",null]},"inputs":{"INPUT":[1,[10,"심판"]]}},
        "timestamp":{"opcode":"cubesterRichPresence_setTimestamp","next":"image","fields":{"TOGGLE":["on",null]}},
        "image":{"opcode":"cubesterRichPresence_setImage","next":"save","inputs":{"SIZE":[1,"size"],"IMAGE":[1,[10,"game_logo"]],"TEXT":[1,[10,"My game"]]}},
        "size":{"opcode":"cubesterRichPresence_menu_SIZE","shadow":true,"fields":{"SIZE":["large",null]}},
        "save":{"opcode":"data_setvariableto","fields":{"VARIABLE":["result","result"]},"inputs":{"VALUE":[2,"data"]}},
        "data":{"opcode":"cubesterRichPresence_presenceData"}
    }}]}`), &p)
	if err != nil {
		t.Fatal(err)
	}
	rt := vm.New(&p)
	if err := rt.SetExtensions(e.Bundle{New}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rt.CloseExtensions(); err != nil {
			t.Error(err)
		}
	})
	if err := rt.Run(context.Background(), 1, 30, false); err != nil {
		t.Fatal(err)
	}
	value := rt.Snapshot()[0].Variables["result"].Value
	var activity map[string]any
	if err := json.Unmarshal([]byte(value.(string)), &activity); err != nil {
		t.Fatal(err)
	}
	if activity["details"] != "챕터 1" || activity["state"] != "심판" {
		t.Errorf("dropdowns did not populate details/state: %v", activity)
	}
	if start, ok := activity["startTimestamp"].(float64); !ok || start <= 0 {
		t.Errorf("timestamp dropdown did not enable timestamp: %v", activity)
	}
	if activity["largeImageKey"] != "game_logo" || activity["largeImageText"] != "My game" {
		t.Fatalf("image menu did not populate activity: %v", activity)
	}
}

func TestEmbeddedGoImplementationMatchesTestedModule(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	path := filepath.Join(dir, "..", "..", "..", "extensions", "examples", "cubesterRichPresence.js")
	js, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source, err := e.ExtractGo(path, js)
	if err != nil {
		t.Fatal(err)
	}
	source, err = format.Source(source)
	if err != nil {
		t.Fatal(err)
	}
	tested, err := os.ReadFile(filepath.Join(dir, "module.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source, tested) {
		t.Fatal("JS Go implementation differs from the tested module")
	}
}
