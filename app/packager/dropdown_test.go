package packager

import (
	"image"
	"testing"

	"gowarp/app/config"
)

func renderedApplication(t *testing.T) *application {
	t.Helper()
	a := &application{
		form:          newForm(config.Default(nil), nil),
		keyboardFocus: -1,
		section:       1,
	}
	w, h := a.layoutScale(840, 620, 1)
	t.Cleanup(func() {
		a.face.Close()
		a.title.Close()
	})
	a.pixels = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	a.render()
	return a
}

func TestDropdownOpensBeforeChangingValue(t *testing.T) {
	a := renderedApplication(t)
	// Header, sidebar, then the FPS input and turbo choice.
	turbo := &a.form.fields[3]
	if turbo.key != "turbo" {
		t.Fatal(turbo.key)
	}
	a.hits[4+len(sections)].action()
	if turbo.value != "false" {
		t.Fatal("opening the dropdown changed turbo mode")
	}
	a.render()
	if len(a.hits) != 2 {
		t.Fatalf("dropdown choices = %d", len(a.hits))
	}
	a.hits[0].action()
	s, err := a.form.settings()
	if err != nil {
		t.Fatal(err)
	}
	if !s.Turbo {
		t.Fatal("selected turbo option was not saved")
	}
}

func TestDropdownOutsideClickCancelsAndEnterCommits(t *testing.T) {
	a := renderedApplication(t)
	a.hits[4+len(sections)].action()
	a.dropdownClick(30, 160) // Sidebar behind the popup must not activate.
	if a.dropdown != nil || a.form.fields[3].value != "false" || a.section != 1 {
		t.Fatal("outside click changed the setting or section")
	}
	a.render()
	a.hits[4+len(sections)].action()
	a.dropdown.selected = 0
	a.activateEnter()
	if a.dropdown != nil || a.form.fields[3].value != "true" {
		t.Fatal("Enter did not commit the highlighted option")
	}
}

func TestDropdownFitsAboveBottomControl(t *testing.T) {
	a := renderedApplication(t)
	a.choice(210, 540, 250, []string{"one", "two", "three", "four", "five", "six"}, 0, func(int) {
	})
	a.hits[len(a.hits)-1].action()
	r := a.dropdownRect()
	if r.Min.Y < 0 || r.Max.Y > 540 || r.Max.X > a.width {
		t.Fatalf("popup outside window: %v", r)
	}
	a.layoutScale(760, 540, 1)
	if a.dropdown != nil {
		t.Fatal("resize retained a popup at stale coordinates")
	}
}
