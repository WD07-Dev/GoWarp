package packager

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"gowarp/app/compiler"
	"gowarp/app/config"
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
	if !s.Player.Controls {
		t.Fatal("connecting in embedded mode must not overwrite the native controls setting")
	}
}

func TestEditorProjectSettingsAndFileGuards(t *testing.T) {
	ctx := context.Background()
	server, err := editorbridge.Start(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	original := newForm(config.Default(nil), nil)
	a := &application{
		ctx: ctx, form: original, editorBridge: server, section: 7,
		templates: []compiler.Template{{OS: runtime.GOOS, Arch: runtime.GOARCH}},
	}
	p := &project.Project{}
	options := editorbridge.Options{Name: "Editor project", FPS: 60, Width: 640, Height: 360}
	if err := a.importEditorProject(editorProjectRequest{ctx: ctx, server: server, project: p, options: options}); err != nil {
		t.Fatal(err)
	}
	if !a.previewMode() || a.form == original || a.section == 7 {
		t.Fatal("preview mode did not replace local settings")
	}
	settings, err := a.form.settings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.FPS != 60 || settings.Width != 640 || settings.Player.Title != "Editor project" {
		t.Fatal("editor settings not imported")
	}
	revision := server.Status().SettingsRevision
	for i := range a.form.fields {
		if a.form.fields[i].key == "fps" {
			a.form.fields[i].value = "120"
		}
	}
	a.syncEditorSettings()
	if server.Status().SettingsRevision == revision {
		t.Fatal("settings edits not published")
	}
	a.openProject(filepath.Join(t.TempDir(), "missing.sb3"))
	a.loadSettings(filepath.Join(t.TempDir(), "missing.json"))
	a.openExtensionEditor()
	a.choose("project")
	a.choose("open")
	if a.busy || a.extensions != nil || a.form == original {
		t.Fatal("project change was allowed during preview")
	}
	a.leaveEditorPreview()
	if a.previewMode() || a.form != original {
		t.Fatal("disconnect did not restore original settings")
	}
}
