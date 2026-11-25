package packager

import (
	"image"
	"testing"

	"gowarp/app/compiler"
	"gowarp/app/config"
)

func TestWindowButtonsCanBeDisabledIndependently(t *testing.T) {
	for _, key := range []string{"platform.disableMinimize", "platform.disableMaximize"} {
		f := newForm(config.Default(nil), nil)
		found := false
		for i := range f.fields {
			field := &f.fields[i]
			if field.key == key {
				if field.section != 6 || field.kind != "bool" {
					t.Fatalf("%s is not a platform toggle", key)
				}
				field.value = "true"
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s", key)
		}
		s, err := f.settings()
		if err != nil {
			t.Fatal(err)
		}
		if s.Platform.DisableMinimize != (key == "platform.disableMinimize") || s.Platform.DisableMaximize != (key == "platform.disableMaximize") || !s.Platform.Resizable {
			t.Fatalf("button settings affected each other or resizing: %+v", s.Window)
		}
	}
}

func TestPlatformFieldsFollowTargetOS(t *testing.T) {
	f := newForm(config.Default(nil), nil)
	common := []string{"platform.resizable", "platform.disableMinimize", "platform.disableMaximize", "platform.fullscreen", "platform.border", "platform.disableClose"}
	for _, targetOS := range []string{"windows", "linux", "android", "ios", ""} {
		visible := map[string]bool{}
		for _, i := range f.visibleFields(6, targetOS) {
			visible[f.fields[i].key] = true
		}
		for _, key := range common {
			if visible[key] != (targetOS == "windows" || targetOS == "linux") {
				t.Errorf("%s: %s visible = %t", targetOS, key, visible[key])
			}
		}
		if visible["platform.multiWindow"] != (targetOS == "windows") {
			t.Errorf("%s: native multiwindow visibility = %t", targetOS, visible["platform.multiWindow"])
		}
	}
}

func TestPlatformSettingsPreserveSavedMultiWindow(t *testing.T) {
	s := config.Default(nil)
	s.Platform.MultiWindow = true
	f := newForm(s, nil)
	for _, targetOS := range []string{"windows", "linux", "android"} {
		saved, err := f.settings()
		if err != nil {
			t.Fatal(err)
		}
		exported := settingsForPlatform(saved, targetOS)
		if exported.Platform.MultiWindow != (targetOS == "windows") || !saved.Platform.MultiWindow || !f.base.Platform.MultiWindow {
			t.Fatalf("%s: exported multiwindow = %t, saved = %t", targetOS, exported.Platform.MultiWindow, saved.Platform.MultiWindow)
		}
	}
}

func TestPlatformFieldsStayBelowTargetSelector(t *testing.T) {
	a := renderedApplication(t)
	w, h := a.layoutScale(840, 1200, 1)
	a.pixels = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	a.section = 6
	a.templates = []compiler.Template{{OS: "windows"}, {OS: "linux"}, {OS: "android"}}
	for target := range a.templates {
		a.target = target
		a.render()
		for _, hit := range a.hits {
			if hit.rect.Min.X >= 200 && hit.rect.Min.Y >= 146 && hit.rect.Min.Y < a.height-128 && hit.rect.Min.Y != 184 && hit.rect.Min.Y < 256 {
				t.Fatalf("platform field overlaps target selector: %v", hit.rect)
			}
		}
	}
}

func TestPlatformFormBindsNewWindowOptions(t *testing.T) {
	f := newForm(config.Default(nil), nil)
	for i := range f.fields {
		switch f.fields[i].key {
		case "platform.border":
			f.fields[i].value = "false"
		case "platform.disableClose", "platform.topmost":
			f.fields[i].value = "true"
		case "platform.opacity", "platform.position", "platform.x", "platform.y":
			t.Fatalf("%s must only be exposed through the Window API", f.fields[i].key)
		}
	}
	s, err := f.settings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Platform.Border || !s.Platform.DisableClose || !s.Platform.Topmost {
		t.Fatalf("platform form did not bind new settings: %+v", s.Window)
	}
}
