package extension_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gowarp/app/config"
	"gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
	windows "gowarp/test/extensions/gowarpWindows"
)

func TestSelfContainedProjectAndSettings(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load(filepath.Join(dir, "..", "..", "fixtures", "projects", "native-windows.gowarp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Platform.MultiWindow || settings.Width != 960 || settings.Height != 360 {
		t.Fatalf("settings=%+v", settings)
	}
	p, err := project.Load(settings.Project)
	if err != nil {
		t.Fatal(err)
	}
	defaults := config.DefaultForProject(settings.Project, p)
	if defaults.Width != 960 || defaults.Height != 360 {
		t.Fatalf("opening the SB3 without settings gives %dx%d; want 960x360", defaults.Width, defaults.Height)
	}
	source, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(p.ExtensionURLs["gowarpWindows"], "data:application/javascript;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(filepath.Join(dir, "..", "..", "..", "extensions", "examples", "gowarpWindows.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source, current) {
		t.Fatal("native-windows.sb3 contains an outdated gowarpWindows extension")
	}
	extracted, err := extension.ExtractSource("gowarpWindows.js", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(extracted.Go) == 0 {
		t.Fatal("missing native implementation")
	}
	archive, err := zip.OpenReader(settings.Project)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	files := map[string]bool{}
	for _, file := range archive.File {
		files[file.Name] = true
	}
	stopAll := false
	for _, target := range p.Targets {
		for _, costume := range target.Costumes {
			if !files[costume.MD5Ext] {
				t.Fatalf("missing costume %s", costume.MD5Ext)
			}
		}
		for _, block := range target.Blocks {
			if block.Opcode == "control_stop" {
				stopAll = true
			}
		}
	}
	if !stopAll {
		t.Fatal("demo needs a stop-all block")
	}
}

func TestScratchExampleStopsOnQ(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(filepath.Join(dir, "..", "..", "fixtures", "projects", "native-windows.sb3"))
	if err != nil {
		t.Fatal(err)
	}
	r := vm.New(p)
	if err := r.SetStageSize(960, 360); err != nil {
		t.Fatal(err)
	}
	if err := r.SetExtensions(extension.Bundle{windows.New}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.CloseExtensions(); err != nil {
			t.Error(err)
		}
	})
	type call struct {
		op   string
		args []any
	}
	var calls []call
	r.SetWindowHandler(func(op string, args []any) (any, error) {
		calls = append(calls, call{op, append([]any(nil), args...)})
		switch op {
		case "set", "create", "move", "viewport":
			return nil, nil
		case "list":
			return []string{"main", "second"}, nil
		case "get":
			return `{"id":"second","stageX":480}`, nil
		default:
			return nil, fmt.Errorf("unexpected window operation %q", op)
		}
	})
	if err := r.Run(context.Background(), 1000, 30, false); err != nil {
		t.Fatal(err)
	}
	if len(calls) < 2 || calls[0].op != "set" || calls[0].args[0] != "main" || calls[1].op != "create" {
		t.Fatalf("startup calls=%+v", calls)
	}
	var main, created map[string]any
	if err := json.Unmarshal([]byte(calls[0].args[1].(string)), &main); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(calls[1].args[0].(string)), &created); err != nil {
		t.Fatal(err)
	}
	if main["width"] != float64(480) || main["stageWidth"] != float64(480) || main["border"] != true {
		t.Fatalf("main setup=%v", main)
	}
	if created["id"] != "second" || created["border"] != true || created["resizable"] != true || created["opacity"] != float64(1) {
		t.Fatalf("create dropdown/defaults=%v", created)
	}
	if len(calls) != 6 || calls[3].op != "viewport" {
		t.Fatalf("window configuration changed after startup: %+v", calls)
	}
	if args := calls[3].args; len(args) != 5 || args[0] != "second" || args[1] != float64(480) || args[2] != float64(0) || args[3] != float64(480) || args[4] != float64(360) {
		t.Fatalf("second window does not show the right half: %v", args)
	}
	stage := r.Snapshot()[0]
	if stage.Variables["window IDs"].Value != `["main","second"]` || stage.Variables["second properties"].Value != `{"id":"second","stageX":480}` {
		t.Fatalf("reporter results=%v", stage.Variables)
	}
	if commands := r.DrainHostCommands(); len(commands) != 0 {
		t.Fatalf("demo quit prematurely: %v", commands)
	}
	if err := r.QueueEvents([]vm.InputEvent{{Frame: 1000, Kind: "key", Key: "q", Down: true}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background(), 1, 30, false); err != nil {
		t.Fatal(err)
	}
	commands := r.DrainHostCommands()
	if len(commands) != 1 || commands[0].Kind != "stop-all" {
		t.Fatalf("Q did not request stop-all: %v", commands)
	}
	if !r.Stopped() {
		t.Fatal("Q did not stop all scripts")
	}
}
