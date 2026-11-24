package packager

import (
	"path/filepath"
	"testing"

	"gowarp/app/editorbridge"
	"gowarp/core/project"
)

func TestEditorPreviewUsesLiveEditorSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.sb3")
	options := editorbridge.Options{Name: "테스트 게임", FPS: 120, Width: 640, Height: 480, Turbo: true, Interpolation: true}
	s := editorPreviewSettings(path, &project.Project{}, options)
	if s.Project != path || s.Player.Title != options.Name || s.Player.Username != options.Name || !s.Player.AutoStart {
		t.Fatal("preview identity was not configured")
	}
	if s.FPS != 120 || s.Width != 640 || s.Height != 480 || s.Window.Width != 640 || s.Window.Height != 480 || !s.Turbo || !s.Interpolation {
		t.Fatalf("live editor settings lost: %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	options.FPS = 0
	options.Name = " "
	s = editorPreviewSettings(path, &project.Project{}, options)
	if s.FPS != 60 || s.Player.Title != "TurboWarp" {
		t.Fatal("unlimited FPS or blank title fallback is invalid")
	}
	if !s.Player.Controls {
		t.Fatal("native preview lost player controls")
	}
	options.Display = "embedded"
	s = editorPreviewSettings(path, &project.Project{}, options)
	if s.Player.Controls {
		t.Fatal("embedded preview should use the editor controls")
	}
}
