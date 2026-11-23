package packager

import (
	"image"
	"slices"
	"testing"

	"gowarp/app/config"
)

func fieldInputVisible(a *application, key string) bool {
	for i := range a.form.fields {
		f := &a.form.fields[i]
		if f.kind == "variable" || f.key != key {
			continue
		}
		for _, hit := range a.hits {
			if hit.edit == &f.value {
				return true
			}
		}
	}
	return false
}

func TestCloudFieldsFollowSelectedModes(t *testing.T) {
	a := renderedApplication(t)
	w, h := a.layoutScale(840, 1200, 1)
	a.pixels = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	a.section = 4
	for _, tc := range []struct {
		mode, variableMode string
		showConnection     bool
	}{
		{"off", "inherit", false},
		{"local", "inherit", false},
		{"websocket", "inherit", true},
		{"local", "websocket", true},
		{"off", "websocket", true},
		{"local", "off", false},
	} {
		t.Run(tc.mode+"/"+tc.variableMode, func(t *testing.T) {
			s := config.Default(nil)
			s.Cloud.Mode = tc.mode
			if tc.variableMode != "inherit" {
				s.Cloud.Variables = map[string]string{"score": tc.variableMode}
			}
			a.form = newForm(s, map[string]string{"score": "Score"})
			a.render()
			for _, key := range []string{"cloud.projectId", "cloud.contact"} {
				if got := fieldInputVisible(a, key); got != tc.showConnection {
					t.Fatalf("%s visible=%t, want %t", key, got, tc.showConnection)
				}
			}
		})
	}
}

func TestHiddenSettingsAreRetainedAndRestored(t *testing.T) {
	a := renderedApplication(t)
	w, h := a.layoutScale(840, 1200, 1)
	a.pixels = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	a.section = 4
	s := config.Default(nil)
	s.Cloud.Mode = "websocket"
	s.Cloud.Provider = "custom"
	s.Cloud.Server = "wss://example.invalid/cloud"
	s.Cloud.ProjectID = "saved-project"
	s.Player.Username = "saved-user"
	s.Cloud.Contact = "saved-contact"
	a.form = newForm(s, nil)
	a.render()
	a.hits[3+len(sections)].action()
	a.selectDropdown(1)
	a.render()
	if fieldInputVisible(a, "cloud.server") {
		t.Fatal("local mode still exposes the server input")
	}
	local, err := a.form.settings()
	if err != nil {
		t.Fatal(err)
	}
	if local.Cloud.Mode != "local" || local.Cloud.Provider != s.Cloud.Provider || local.Cloud.Server != s.Cloud.Server || local.Cloud.ProjectID != s.Cloud.ProjectID || local.Player.Username != s.Player.Username || local.Cloud.Contact != s.Cloud.Contact {
		t.Fatalf("hidden connection settings lost: %+v", local.Cloud)
	}
	a.hits[3+len(sections)].action()
	a.selectDropdown(2)
	a.render()
	if !fieldInputVisible(a, "cloud.server") {
		t.Fatal("WebSocket mode did not restore the server input")
	}
}

func TestCloudProviderFields(t *testing.T) {
	s := config.Default(nil)
	s.Cloud.Mode = "websocket"
	s.Cloud.Server = "wss://example.com/cloud"
	s.Cloud.ProjectID = "example.com/game"
	s.Cloud.Contact = "example.com/contact"
	f := newForm(s, nil)
	var provider *field
	for i := range f.fields {
		if f.fields[i].key == "cloud.provider" {
			provider = &f.fields[i]
		}
	}
	if provider == nil || !slices.Equal(provider.choices, []string{"turbowarp", "custom"}) {
		t.Fatal("cloud provider dropdown is missing")
	}
	for _, selected := range []string{"custom", "turbowarp"} {
		provider.value = selected
		visible := map[string]bool{}
		for _, index := range f.visibleFields(4) {
			visible[f.fields[index].key] = true
		}
		if !visible["cloud.provider"] || !visible["cloud.projectId"] ||
			visible["cloud.server"] != (selected == "custom") ||
			visible["cloud.contact"] != (selected == "turbowarp") {
			t.Fatalf("%s fields: %v", selected, visible)
		}
		got, err := f.settings()
		if err != nil {
			t.Fatal(err)
		}
		if got.Cloud.Provider != selected || got.Cloud.Server != s.Cloud.Server || got.Cloud.Contact != s.Cloud.Contact {
			t.Fatalf("switching provider lost settings: %+v", got.Cloud)
		}
	}
}

func TestAccentIsHiddenWithoutPlayerControls(t *testing.T) {
	a := renderedApplication(t)
	w, h := a.layoutScale(840, 1200, 1)
	a.pixels = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	a.section = 3
	s := config.Default(nil)
	s.Player.Controls = false
	s.Player.Accent = "unfinished color"
	a.form = newForm(s, nil)
	a.render()
	if fieldInputVisible(a, "player.accent") {
		t.Fatal("hidden controls still expose accent color")
	}
	if _, err := a.form.settings(); err != nil {
		t.Fatalf("unused hidden accent blocks saving: %v", err)
	}
	for i := range a.form.fields {
		if a.form.fields[i].key == "player.controls" {
			a.form.fields[i].value = "true"
		}
	}
	a.render()
	if !fieldInputVisible(a, "player.accent") {
		t.Fatal("accent did not reappear")
	}
	if _, err := a.form.settings(); err == nil {
		t.Fatal("active invalid accent was not validated")
	}
}

func TestHighQualityPenIsTheOnlyRoundingOption(t *testing.T) {
	for _, highQuality := range []bool{false, true} {
		s := config.Default(nil)
		s.HighQualityPen = &highQuality
		f := newForm(s, nil)
		for _, field := range f.fields {
			if field.key == "disablePositionRounding" {
				t.Fatal("standalone rounding option is still present")
			}
		}
		saved, err := f.settings()
		if err != nil {
			t.Fatal(err)
		}
		if saved.HighQualityPen == nil || *saved.HighQualityPen != highQuality {
			t.Fatal("high quality pen setting was not preserved")
		}
	}
}

func TestLimitDropdownsSavePlayerOverrides(t *testing.T) {
	a := renderedApplication(t)
	w, h := a.layoutScale(840, 1200, 1)
	a.pixels = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	a.section = 1
	a.form = newForm(config.Default(nil), nil)
	for _, selected := range []int{0, 1} {
		a.render()
		// Runtime fields five and six are the two limit dropdowns.
		a.hits[7+len(sections)].action()
		a.selectDropdown(selected)
		a.render()
		a.hits[8+len(sections)].action()
		a.selectDropdown(selected)
		s, err := a.form.settings()
		if err != nil {
			t.Fatal(err)
		}
		if s.RemoveFencing == nil || *s.RemoveFencing != (selected == 0) ||
			s.RemoveMiscLimits == nil || *s.RemoveMiscLimits != (selected == 0) {
			t.Fatal("limit dropdown selection did not reach saved settings")
		}
	}
}
