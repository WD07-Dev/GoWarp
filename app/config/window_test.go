package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowButtonSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Window.DisableMinimize || s.Window.DisableMaximize {
		t.Fatal("legacy settings must keep window buttons enabled")
	}
	for _, minimize := range []bool{false, true} {
		for _, maximize := range []bool{false, true} {
			s.Window.DisableMinimize = minimize
			s.Window.DisableMaximize = maximize
			if err := Save(path, s); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if got.Window != s.Window {
				t.Fatalf("window settings changed: %+v", got.Window)
			}
		}
	}
}
