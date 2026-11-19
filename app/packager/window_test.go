package packager

import (
	"testing"

	"gowarp/app/config"
)

func TestWindowButtonsCanBeDisabledIndependently(t *testing.T) {
	for _, key := range []string{"window.disableMinimize", "window.disableMaximize"} {
		f := newForm(config.Default(nil), nil)
		found := false
		for i := range f.fields {
			field := &f.fields[i]
			if field.key == key {
				if field.section != 2 || field.kind != "bool" {
					t.Fatalf("%s is not a window toggle", key)
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
		if s.Window.DisableMinimize != (key == "window.disableMinimize") || s.Window.DisableMaximize != (key == "window.disableMaximize") || !s.Window.Resizable {
			t.Fatalf("button settings affected each other or resizing: %+v", s.Window)
		}
	}
}
