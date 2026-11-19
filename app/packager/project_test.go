package packager

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenProjectAutofillsJSONAndSB3(t *testing.T) {
	data, err := json.Marshal(map[string]any{"targets": []any{map[string]any{
		"isStage": true,
		"name":    "Stage",
		"comments": map[string]any{"settings": map[string]any{
			"text": `{"width":800,"framerate":75,"turbo":true,"interpolation":true,"runtimeOptions":{"maxClones":Infinity}} // _twconfig_`,
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "project.json")
	if err := os.WriteFile(jsonPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	sb3Path := filepath.Join(dir, "game.sb3")
	f, err := os.Create(sb3Path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("project.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{jsonPath, sb3Path} {
		t.Run(filepath.Ext(path), func(t *testing.T) {
			a := &application{ctx: context.Background(), results: make(chan result, 1)}
			a.openProject(path)
			select {
			case result := <-a.results:
				if result.err != nil {
					t.Fatal(result.err)
				}
				s, err := result.form.settings()
				if err != nil {
					t.Fatal(err)
				}
				if s.Project != path || s.Width != 800 || s.Height != 360 || s.Window.Width != 800 || s.FPS != 75 || !s.Turbo || !s.Interpolation {
					t.Fatalf("auto-filled settings: %+v", s)
				}
				if s.Player.Username != s.Player.Title || s.Cloud.Username != s.Player.Title {
					t.Fatalf("usernames were not filled from the project name: %+v", s)
				}
				if s.Cloud.Server != "wss://clouddata.turbowarp.org" {
					t.Fatalf("default cloud server = %q", s.Cloud.Server)
				}
				for i := range result.form.fields {
					if result.form.fields[i].key == "turbo" {
						result.form.fields[i].value = "false"
					}
				}
				s, err = result.form.settings()
				if err != nil || s.Turbo {
					t.Fatalf("manual override lost: %+v, %v", s, err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("project open timed out")
			}
		})
	}
}
