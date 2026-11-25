package config

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlatformSettingsFileLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.gowarp.json")
	input := `{"window":{"width":800,"height":600,"fit":"preserve"},"platform":{"resizable":false,"fullscreen":false,"position":"custom","x":-320,"y":80,"border":false,"multiWindow":true}}`
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var model map[string]map[string]any
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	model = map[string]map[string]any{}
	for _, key := range []string{"window", "platform"} {
		var v map[string]any
		if err = json.Unmarshal(fields[key], &v); err != nil {
			t.Fatal(err)
		}
		model[key] = v
	}
	if model["window"]["border"] != nil || model["platform"]["border"] != false || model["platform"]["x"] != float64(-320) || model["platform"]["position"] != "custom" {
		t.Fatal(model)
	}
}

func TestPlatformWindowDefaultsAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, input := range []string{`{}`, `{"platform":{"border":false,"disableClose":true,"opacity":0.35}}`} {
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		s, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(s.Platform)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"border": true, "disableClose": false, "opacity": float64(1)}
		if input != `{}` {
			want = map[string]any{"border": false, "disableClose": true, "opacity": 0.35}
		}
		for key, value := range want {
			if got[key] != value {
				t.Fatalf("%s = %v, want %v", key, got[key], value)
			}
		}
		if err := Save(path, s); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(path)
		if err != nil || !reflect.DeepEqual(s.Platform, loaded.Platform) {
			t.Fatalf("platform settings changed: %+v, %v", loaded.Platform, err)
		}
	}
}

func TestNativeMultiWindowAllowsFullscreen(t *testing.T) {
	s := Default(nil)
	s.Platform.MultiWindow = true
	s.Platform.Fullscreen = true
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestStartPositionValidationAndFullscreen(t *testing.T) {
	s := Default(nil)
	if s.Platform.CustomPosition() {
		t.Fatal("default position must remain automatic")
	}
	s.Platform.Position, s.Platform.X, s.Platform.Y = "custom", -320, 40
	if err := s.Validate(); err != nil || !s.Platform.CustomPosition() {
		t.Fatalf("custom position: %v", err)
	}
	s.Platform.Fullscreen = true
	if s.Platform.CustomPosition() {
		t.Fatal("fullscreen must ignore custom start position")
	}
	for _, p := range []Platform{{Position: "center", Opacity: 1}, {Position: "custom", X: 100001, Opacity: 1}, {Position: "auto", Y: -100001, Opacity: 1}} {
		s.Platform = p
		if err := s.Validate(); err == nil {
			t.Fatalf("invalid position accepted: %+v", p)
		}
	}
}

func TestWindowOpacityRange(t *testing.T) {
	for _, opacity := range []float64{-0.01, 0, 0.35, 1, 1.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		s := Default(nil)
		s.Platform.Opacity = opacity
		wantValid := !math.IsNaN(opacity) && opacity >= 0 && opacity <= 1
		if err := s.Validate(); (err == nil) != wantValid {
			t.Fatalf("opacity %v validation = %v", opacity, err)
		}
	}
}

func TestWindowButtonSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Platform.DisableMinimize || s.Platform.DisableMaximize {
		t.Fatal("legacy settings must keep window buttons enabled")
	}
	for _, minimize := range []bool{false, true} {
		for _, maximize := range []bool{false, true} {
			s.Platform.DisableMinimize = minimize
			s.Platform.DisableMaximize = maximize
			if err := Save(path, s); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Platform, s.Platform) {
				t.Fatalf("platform settings changed: %+v", got.Platform)
			}
		}
	}
}
