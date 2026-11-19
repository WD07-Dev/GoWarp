package packager

import (
	"testing"

	"gowarp/app/config"
)

func TestFormValidatesAndPreservesFalse(t *testing.T) {
	s := config.Default(nil)
	f := newForm(s, nil)
	for i := range f.fields {
		switch f.fields[i].key {
		case "fps":
			f.fields[i].value = "1.5"
		case "player.autoStart":
			f.fields[i].value = "false"
		}
	}
	got, err := f.settings()
	if err != nil {
		t.Fatal(err)
	}
	if got.FPS != 1.5 || got.Player.AutoStart {
		t.Fatal("field binding lost")
	}
	for i := range f.fields {
		if f.fields[i].key == "fps" {
			f.fields[i].value = "wrong"
		}
	}
	if _, err = f.settings(); err == nil {
		t.Fatal("invalid numeric field accepted")
	}
}

func TestNewFormKeepsUnselectedProjectEmpty(t *testing.T) {
	f := newForm(config.Default(nil), nil)
	s, err := f.settings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Project != "" {
		t.Fatalf("unselected project is %q", s.Project)
	}
}

func TestFormDisplayFPS(t *testing.T) {
	f := newForm(config.Default(nil), nil)
	found := 0
	for _, i := range f.visibleFields(2) {
		field := &f.fields[i]
		if field.key == "player.renderFPS" {
			found++
			field.value = "30"
			if len(field.choices) == 0 {
				t.Fatal("display FPS must remain a dropdown")
			}
		}
	}
	for _, field := range f.fields {
		if field.key == "player.quality" || field.key == "player.maxRenderSize" {
			t.Fatal("removed quality control is still editable")
		}
	}
	s, err := f.settings()
	if err != nil {
		t.Fatal(err)
	}
	if found != 1 || s.Player.RenderFPS != 30 || s.FPS != config.Default(nil).FPS {
		t.Fatalf("display FPS was not visible and saved independently: %+v", s)
	}
}

func TestFormDisplaySizeDoesNotChangeStageCoordinates(t *testing.T) {
	f := newForm(config.Default(nil), nil)
	for i := range f.fields {
		switch f.fields[i].key {
		case "window.width":
			f.fields[i].value = "800"
		case "window.height":
			f.fields[i].value = "600"
		}
	}
	s, err := f.settings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Width != 480 || s.Height != 360 || s.Window.Width != 800 || s.Window.Height != 600 {
		t.Fatalf("display size changed stage coordinates: %+v", s)
	}
}

func TestFormWindowBackground(t *testing.T) {
	initial := config.Default(nil)
	f := newForm(initial, nil)
	found := false
	for i := range f.fields {
		field := &f.fields[i]
		if field.key == "window.background" {
			found = true
			if field.section != 2 {
				t.Fatal("window background is outside the stage/window section")
			}
			field.value = "#000000"
		}
	}
	if !found {
		t.Fatal("window background field is missing")
	}
	s, err := f.settings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Window.Background != "#000000" {
		t.Fatalf("window background = %q", s.Window.Background)
	}
	if s.Loading.Background != initial.Loading.Background {
		t.Fatal("window background changed the loading screen background")
	}
}

func TestBrowserEnterActivatesFocusedAction(t *testing.T) {
	called := false
	a := &application{
		browser:       &browser{mode: "project"},
		keyboardFocus: 0,
		hits: []hit{
			{
				action: func() {
					called = true
				},
			},
		},
	}
	a.activateEnter()
	if !called {
		t.Fatal("browser ignored focused button")
	}
}

func TestFormCloseOnStopRoundTrip(t *testing.T) {
	f := newForm(config.Default(nil), nil)
	found := false
	for i := range f.fields {
		field := &f.fields[i]
		if field.key == "player.closeOnStop" {
			found = true
			if field.value != "false" || field.kind != "bool" || field.section != 3 {
				t.Fatalf("unexpected close-on-stop field: %+v", field)
			}
			field.value = "true"
		}
	}
	if !found {
		t.Fatal("close-on-stop option is missing")
	}
	s, err := f.settings()
	if err != nil {
		t.Fatal(err)
	}
	loaded := newForm(s, nil)
	for _, field := range loaded.fields {
		if field.key == "player.closeOnStop" && field.value != "true" {
			t.Fatal("close-on-stop setting was lost")
		}
	}
}

func TestFormCanvasAntialias(t *testing.T) {
	f := newForm(config.Default(nil), nil)
	found := false
	for i := range f.fields {
		field := &f.fields[i]
		if field.key == "canvas.antialias" {
			if field.kind != "bool" || field.value != "true" {
				t.Fatal("antialias toggle did not use the enabled default")
			}
			field.value = "false"
			found = true
		}
	}
	s, err := f.settings()
	if err != nil {
		t.Fatal(err)
	}
	if !found || s.Canvas.Antialias {
		t.Fatal("antialias toggle did not save its disabled state")
	}
}

func TestFormIntegerWindowFit(t *testing.T) {
	f := newForm(config.Default(nil), nil)
	found := false
	for i := range f.fields {
		field := &f.fields[i]
		if field.key != "window.fit" {
			continue
		}
		if field.value != "preserve" {
			t.Fatal("default fit changed")
		}
		for _, choice := range field.choices {
			if choice == "integer" {
				found = true
			}
		}
		field.value = "integer"
	}
	if !found {
		t.Fatal("integer fit is missing from the dropdown")
	}
	s, err := f.settings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Window.Fit != "integer" {
		t.Fatal("integer fit selection was lost")
	}
}
