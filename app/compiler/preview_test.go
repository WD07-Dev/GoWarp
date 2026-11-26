package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gowarp/app/config"
)

func TestPreviewUsesCachedRuntimeWithoutPackagingProject(t *testing.T) {
	dir := t.TempDir()
	writeExtensionArchive(t, filepath.Join(dir, "runtime-source.zip"), extensionSDKFixture(t))
	input := filepath.Join(dir, "preview.sb3")
	writeExtensionArchive(t, input, map[string]string{
		"project.json": `{"targets":[{"isStage":true,"name":"Stage","blocks":{}}]}`,
		"sound.mp3":    "unchanged compressed asset",
	})
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	tpl := Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Path: filepath.Join(dir, "runtimes", "fixture", "player")}
	s := config.Default(nil)
	s.Width, s.Height = 640, 480
	s.FPS = 60
	s.Player.AutoStart = false
	var first string
	for range 2 {
		work := t.TempDir()
		path, err := PreparePreview(context.Background(), tpl, input, work, s)
		if err != nil {
			t.Fatal(err)
		}
		if first != "" && path != first {
			t.Fatal("unchanged preview did not reuse its runtime")
		}
		first = path
		if _, err := ReadPackage(path); err == nil {
			t.Fatal("preview created a packaged executable")
		}
		entries, err := os.ReadDir(work)
		if err != nil || len(entries) != 1 || entries[0].Name() != "preview.json" {
			t.Fatalf("preview copied more than its settings: %v, %v", entries, err)
		}
		p, err := ReadPreview(work)
		if err != nil {
			t.Fatal(err)
		}
		if p.Settings.Project != input || p.Settings.Width != 640 || p.Settings.Height != 480 ||
			p.Settings.FPS != 60 || p.Settings.Player.AutoStart || len(p.Project) != 0 {
			t.Fatalf("preview settings or external project lost: %+v", p)
		}
		if err := os.RemoveAll(work); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("closing a preview removed the cached runtime: %v", err)
		}
	}
	after, err := os.ReadFile(input)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatalf("preview changed the original archive: %v", err)
	}
}

func TestReadPreviewRejectsInvalidSettings(t *testing.T) {
	for _, name := range []string{"empty project", "relative project", "invalid fps", "trailing data"} {
		t.Run(name, func(t *testing.T) {
			s := config.Default(nil)
			s.Project = filepath.Join(t.TempDir(), "project.sb3")
			switch name {
			case "empty project":
				s.Project = ""
			case "relative project":
				s.Project = "project.sb3"
			case "invalid fps":
				s.FPS = -1
			}
			data, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			if name == "trailing data" {
				data = append(data, []byte(` {}`)...)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "preview.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadPreview(dir); err == nil {
				t.Fatal("invalid preview accepted")
			}
		})
	}
}
