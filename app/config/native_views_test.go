package config

import (
	"gowarp/core/window"
	"path/filepath"
	"testing"
)

func TestNativeViewsCanBeDisabledWithoutDeletingConfiguration(t *testing.T) {
	s := Default(nil)
	s.Platform.MultiWindow = true
	s.Platform.Views = []window.View{{ID: "main", Width: 480, Height: 360, StageWidth: 480, StageHeight: 360}}
	path := filepath.Join(t.TempDir(), "views.json")
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Platform.MultiWindow || len(loaded.Platform.Views) != 1 {
		t.Fatal("lost window settings")
	}
	loaded.Platform.MultiWindow = false
	if err := Save(path, loaded); err != nil {
		t.Fatal("cannot turn feature off:", err)
	}
}
