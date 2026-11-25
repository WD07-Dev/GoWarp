package extension

import (
	"encoding/json"
	"errors"
	"testing"

	e "gowarp/core/extension"
)

type recordingHost struct {
	operation string
	args      []any
	value     any
	err       error
}

func (h *recordingHost) Call(operation string, args ...any) (any, error) {
	h.operation, h.args = operation, args
	return h.value, h.err
}
func runBlock(t *testing.T, opcode string, c *e.Context) (e.Result, error) {
	t.Helper()
	for _, block := range New().Blocks {
		if block.Opcode == "gowarpWindows_"+opcode {
			return block.Handler(c)
		}
	}
	t.Fatalf("missing block %s", opcode)
	return e.Result{}, nil
}
func TestFieldMenusAndOpacity(t *testing.T) {
	for _, tc := range []struct {
		name, property string
		args           map[string]any
		fields         map[string]string
		want           any
	}{
		{"setBoolean", "disableClose", map[string]any{"ID": "main", "PROPERTY": "wrong", "VALUE": "false"}, map[string]string{"PROPERTY": "disableClose", "VALUE": "true"}, true},
		{"setBoolean", "border", map[string]any{"ID": "main"}, map[string]string{"PROPERTY": "border", "VALUE": "false"}, false},
		{"topmost", "topmost", map[string]any{"ID": "main", "VALUE": "false"}, map[string]string{"VALUE": "true"}, true},
		{"topmost", "topmost", map[string]any{"ID": "main"}, map[string]string{"VALUE": "false"}, false},
		{"opacity", "opacity", map[string]any{"ID": "main", "PERCENT": 55}, nil, 0.55},
	} {
		t.Run(tc.property, func(t *testing.T) {
			host := &recordingHost{}
			if _, err := runBlock(t, tc.name, &e.Context{Host: host, Args: tc.args, Fields: tc.fields}); err != nil {
				t.Fatal(err)
			}
			if host.operation != "window.set" || len(host.args) != 2 || host.args[0] != "main" {
				t.Fatalf("call: %+v", host)
			}
			var patch map[string]any
			if err := json.Unmarshal([]byte(host.args[1].(string)), &patch); err != nil {
				t.Fatal(err)
			}
			if len(patch) != 1 || patch[tc.property] != tc.want {
				t.Fatalf("patch=%v want %s=%v", patch, tc.property, tc.want)
			}
		})
	}
}
func TestCreateDefaultsAndHostErrors(t *testing.T) {
	host := &recordingHost{}
	c := &e.Context{Host: host, Args: map[string]any{"ID": "second", "W": 480, "H": 360, "X": 20, "Y": 40}, Fields: map[string]string{"BORDER": "true"}}
	if _, err := runBlock(t, "create", c); err != nil {
		t.Fatal(err)
	}
	var view map[string]any
	if host.operation != "window.create" || len(host.args) != 1 {
		t.Fatalf("call: %+v", host)
	}
	if err := json.Unmarshal([]byte(host.args[0].(string)), &view); err != nil {
		t.Fatal(err)
	}
	if view["border"] != true || view["resizable"] != true || view["opacity"] != float64(1) || view["stageWidth"] != float64(480) {
		t.Fatalf("view=%v", view)
	}
	host.err = errors.New("multiwindow is disabled")
	if _, err := runBlock(t, "create", c); !errors.Is(err, host.err) {
		t.Fatalf("error lost: %v", err)
	}
}
func TestListAndExplicitQuit(t *testing.T) {
	host := &recordingHost{value: []string{"main", "second"}}
	c := &e.Context{Host: host}
	result, err := runBlock(t, "list", c)
	if err != nil || result.Value != `["main","second"]` || host.operation != "window.list" {
		t.Fatalf("result=%v err=%v call=%s", result, err, host.operation)
	}
	if _, err = runBlock(t, "quit", c); err != nil {
		t.Fatal(err)
	}
	if host.operation != "host.command" || len(host.args) != 1 || host.args[0] != "close" {
		t.Fatalf("quit call: %+v", host)
	}
}

func TestViewportAndStageSize(t *testing.T) {
	host := &recordingHost{value: 960.0}
	c := &e.Context{Host: host, Args: map[string]any{"ID": "main", "X": -20.0, "Y": 30.0, "W": 240.0, "H": 180.0}}
	if _, err := runBlock(t, "viewport", c); err != nil {
		t.Fatal(err)
	}
	if host.operation != "window.viewport" || len(host.args) != 5 || host.args[1] != -20.0 || host.args[3] != 240.0 {
		t.Fatalf("viewport: %+v", host)
	}
	if _, err := runBlock(t, "moveViewport", c); err != nil {
		t.Fatal(err)
	}
	if host.operation != "window.viewport" || len(host.args) != 3 {
		t.Fatalf("move: %+v", host)
	}
	c.Fields = map[string]string{"DIMENSION": "width"}
	result, err := runBlock(t, "stageSize", c)
	if err != nil || result.Value != 960.0 || host.operation != "camera.get" || host.args[0] != "width" {
		t.Fatalf("stage: %+v %v", result, err)
	}
	if _, err := runBlock(t, "boundary", &e.Context{}); err != nil {
		t.Fatal("browser guide should do nothing in Go", err)
	}
}

func TestWindowStateReporter(t *testing.T) {
	host := &recordingHost{value: true}
	c := &e.Context{Host: host, Args: map[string]any{"ID": "second", "STATE": "wrong"}, Fields: map[string]string{"STATE": "minimized"}}
	result, err := runBlock(t, "state", c)
	if err != nil || result.Value != true || host.operation != "window.state" || len(host.args) != 2 || host.args[0] != "second" || host.args[1] != "minimized" {
		t.Fatalf("state result=%+v call=%+v err=%v", result, host, err)
	}
	host.err = errors.New("state unavailable")
	if _, err := runBlock(t, "state", c); !errors.Is(err, host.err) {
		t.Fatal("lost host error", err)
	}
}

func TestMonitorAndCenterBlocks(t *testing.T) {
	host := &recordingHost{value: `{"x":-1920,"y":0,"width":1920,"height":1080,"workX":-1920,"workY":40,"workWidth":1920,"workHeight":1040}`}
	c := &e.Context{Host: host, Args: map[string]any{"ID": "main", "PROPERTY": "wrong", "SCOPE": "wrong"}, Fields: map[string]string{"PROPERTY": "workWidth", "SCOPE": "all"}}
	got, err := runBlock(t, "monitor", c)
	if err != nil || got.Value != float64(1920) || host.operation != "window.monitor" || len(host.args) != 1 || host.args[0] != "main" {
		t.Fatalf("monitor=%+v call=%+v err=%v", got, host, err)
	}
	for _, scope := range []string{"all", "window"} {
		c.Fields["SCOPE"] = scope
		if _, err := runBlock(t, "center", c); err != nil {
			t.Fatal(err)
		}
		if host.operation != "window.center" || len(host.args) != 2 || host.args[0] != "main" || host.args[1] != (scope == "all") {
			t.Fatalf("center: %+v", host)
		}
	}
	host.err = errors.New("no monitor")
	if _, err := runBlock(t, "monitor", c); !errors.Is(err, host.err) {
		t.Fatal(err)
	}
	if _, err := runBlock(t, "center", c); !errors.Is(err, host.err) {
		t.Fatal(err)
	}
}
