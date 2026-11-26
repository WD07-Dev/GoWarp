package player

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gowarp/app/config"
	"gowarp/core/cloud"
	"gowarp/core/project"
)

func TestPreviewPlayerRunsWithoutPackagedProject(t *testing.T) {
	if os.Getenv("GOWARP_TEST_PREVIEW") == "1" {
		os.Exit(runMainWithExtensions(nil))
	}
	dir := t.TempDir()
	s := config.Default(nil)
	s.Project = filepath.Join(dir, "project.json")
	s.Player.CloseOnStop = true
	s.Player.Mute = true
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "preview.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	project := `{"targets":[{"isStage":true,"name":"Stage","blocks":{
		"flag":{"opcode":"event_whenflagclicked","next":"stop","topLevel":true},
		"stop":{"opcode":"control_stop","parent":"flag","fields":{"STOP_OPTION":["all",null]}}
	}}]}`
	if err := os.WriteFile(s.Project, []byte(project), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPreviewPlayerRunsWithoutPackagedProject$")
	cmd.Env = append(os.Environ(), "GOWARP_TEST_PREVIEW=1", "GOWARP_PREVIEW=1", "GOWARP_WORKDIR="+dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("preview did not execute its stop-all block: %v\n%s", err, output)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("preview extracted an extra project: %v, %v", entries, err)
	}
}

func TestPreviewLoadsExternalProjectOnlyInPreviewMode(t *testing.T) {
	dir := t.TempDir()
	s := config.Default(nil)
	s.Project = filepath.Join(dir, "project.sb3")
	s.Width, s.Height = 640, 480
	s.Player.AutoStart = false
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "preview.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(dir, "unpackaged-player")
	if err := os.WriteFile(executable, []byte("runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWARP_WORKDIR", dir)
	t.Setenv("GOWARP_PREVIEW", "1")
	p, err := readPlayerProject(executable)
	if err != nil {
		t.Fatal(err)
	}
	if p.Settings.Project != s.Project || p.Settings.Width != 640 || p.Settings.Player.AutoStart || len(p.Project) != 0 {
		t.Fatalf("external preview was not loaded: %+v", p)
	}
	t.Setenv("GOWARP_PREVIEW", "")
	if _, err := readPlayerProject(executable); err == nil {
		t.Fatal("normal player used preview settings")
	}
	t.Setenv("GOWARP_PREVIEW", "1")
	if err := os.Remove(filepath.Join(dir, "preview.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := readPlayerProject(executable); !os.IsNotExist(err) {
		t.Fatalf("missing preview settings: %v", err)
	}
}

func TestCloudNumberEncodingKeepsDecimalDigits(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{{float64(1000000), "1000000"}, {0.0000001, "0.0000001"}, {"000123", "000123"}} {
		if got := cloudText(tc.value); got != tc.want {
			t.Fatalf("cloud %v = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestCloudUsesPlayerUsername(t *testing.T) {
	s := config.Default(nil)
	s.Player.Username = "Player One"
	options := cloudOptions(s, &project.Project{})
	if options.Username != s.Player.Username {
		t.Fatalf("cloud username = %q, want %q", options.Username, s.Player.Username)
	}
}

func TestCloudProviderConnection(t *testing.T) {
	for _, tc := range []struct {
		name, cloud, server, projectID, contact string
	}{
		{"turbowarp", `{"provider":"turbowarp","server":"wss://unused.invalid","projectId":"example.com/game","contact":"example.com/contact"}`, "wss://clouddata.turbowarp.org", "example.com/game", "example.com/contact"},
		{"custom default ID", `{"provider":"custom","server":"ws://localhost:8080","contact":"unused"}`, "ws://localhost:8080", "test-game", ""},
		{"custom explicit ID", `{"provider":"custom","server":"wss://example.com/cloud","projectId":"shared-game"}`, "wss://example.com/cloud", "shared-game", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := config.Default(nil)
			s.AppID = "test-game"
			if err := json.Unmarshal([]byte(tc.cloud), &s.Cloud); err != nil {
				t.Fatal(err)
			}
			o := cloudOptions(s, &project.Project{})
			if o.Server != tc.server || o.ProjectID != tc.projectID || o.Contact != tc.contact {
				t.Fatalf("connection = %+v", o)
			}
		})
	}
}

func TestLocalCloudIdentitySurvivesRename(t *testing.T) {
	p := &project.Project{Targets: []*project.Target{{
		IsStage:   true,
		Name:      "Stage",
		Variables: map[string][]any{"a": {"score", 0, true}, "b": {"score", 0, true}},
	}}}
	s := config.Default(nil)
	s.Cloud.Mode = "local"
	dir := t.TempDir()
	store, err := cloud.Open(context.Background(), cloudOptions(s, p), dir)
	if err != nil {
		t.Fatal(err)
	}
	store.Set("a", "12")
	store.Set("b", "34")
	store.Close()
	p.Targets[0].Variables["a"][0] = "renamed"
	store, err = cloud.Open(context.Background(), cloudOptions(s, p), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got := store.Poll()
	if got["a"] != "12" || got["b"] != "34" {
		t.Fatalf("lost local identity: %v", got)
	}
}

func TestInvalidSettingsDoNotStartEventLoop(t *testing.T) {
	started := false
	s := config.Default(nil)
	s.Width = 0
	err := runPrepared(context.Background(), "/missing/gowarp-project.sb3", s, false, &started, nil)
	if err == nil || started {
		t.Fatalf("startup err=%v started=%v", err, started)
	}
}
