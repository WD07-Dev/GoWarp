package config

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"gowarp/core/project"
)

func TestSettingsRoundTripPreservesExplicitFalse(t *testing.T) {
	s := Default(nil)
	s.FPS = 1.5
	s.Player.AutoStart = false
	s.Player.Controls = false
	s.Player.CloseOnStop = true
	s.Player.Username = "Player One"
	s.Cloud.Provider = "custom"
	s.Cloud.Server = "wss://example.invalid/cloud"
	path := filepath.Join(t.TempDir(), "game.gowarp.json")
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.FPS != 1.5 || got.Player.AutoStart || got.Player.Controls || !got.Player.CloseOnStop || got.AppID != s.AppID {
		t.Fatalf("lost settings: %+v", got)
	}
	if got.Player.Username != "Player One" || got.Cloud.Provider != "custom" || got.Cloud.Server != "wss://example.invalid/cloud" {
		t.Fatalf("explicit connection settings overwritten: %+v", got)
	}
}

func TestRenderingSettingsProjectDefaultsAndRoundTrip(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"comments":{"c":{"text":"{\"hq\":true} // _twconfig_"}}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	s := Default(&p)
	if s.HighQualityPen == nil || !*s.HighQualityPen {
		t.Fatal("project high quality pen setting was not imported")
	}
	for _, highQuality := range []bool{true, false} {
		s.HighQualityPen = &highQuality
		path := filepath.Join(t.TempDir(), "rendering.json")
		if err := Save(path, s); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.HighQualityPen == nil || *loaded.HighQualityPen != highQuality {
			t.Fatalf("rendering settings changed after saving: %+v", loaded)
		}
	}
}

func TestStageAndWindowDimensionsRemainIndependent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.gowarp.json")
	if err := os.WriteFile(path, []byte(`{"width":800,"height":600,"window":{"width":1600,"height":1376}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Width != 800 || s.Height != 600 || s.Window.Width != 1600 || s.Window.Height != 1376 {
		t.Fatalf("display dimensions were overwritten: %+v", s)
	}
	s.Width, s.Height = 640, 480
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved Settings
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Width != 640 || saved.Height != 480 || saved.Window.Width != 1600 || saved.Window.Height != 1376 {
		t.Fatalf("saving logical dimensions changed display size: %+v", saved)
	}
}

func TestPartialSettingsUseProjectDefaultsAndExplicitFalse(t *testing.T) {
	dir := t.TempDir()
	projectData := `{"targets":[{"isStage":true,"name":"Stage","variables":{},"lists":{},"blocks":{},"comments":{"c":{"text":"_twconfig_ {\"framerate\":60,\"turbo\":true,\"interpolation\":true,\"width\":640,\"height\":480}"}}}]}`
	if err := os.WriteFile(filepath.Join(dir, "project.json"), []byte(projectData), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"project":"project.json","turbo":false,"player":{"autoStart":false}}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.FPS != 60 || got.Width != 640 || !got.Interpolation || got.Turbo || got.Player.AutoStart {
		t.Fatalf("bad precedence: %+v", got)
	}
	if got.Player.Title != "project" || got.Player.Username != "project" || got.Cloud.Provider != "turbowarp" {
		t.Fatalf("missing project defaults: %+v", got)
	}
}

func TestSettingsRejectInvalidValues(t *testing.T) {
	for _, mutate := range []func(*Settings){
		func(s *Settings) {
			s.FPS = math.NaN()
		},
		func(s *Settings) {
			s.FPS = 0
		},
		func(s *Settings) {
			s.Width = 4097
		},
		func(s *Settings) {
			s.Window.Width = -1
		},
		func(s *Settings) {
			s.Window.Height = 4097
		},
		func(s *Settings) {
			s.Version = 99
		},
		func(s *Settings) {
			s.Window.Fit = "unknown"
		},
		func(s *Settings) {
			s.Cloud.Mode = "websocket"
		},
		func(s *Settings) {
			s.AppID = "../escape"
		},
	} {
		s := Default(nil)
		mutate(&s)
		if s.Validate() == nil {
			t.Fatalf("accepted invalid settings: %+v", s)
		}
	}
}

func TestCloudProviderValidation(t *testing.T) {
	for _, tc := range []struct {
		name, cloud string
		valid       bool
	}{
		{"turbowarp", `{"provider":"turbowarp","projectId":"example.com/game","contact":"example.com/contact"}`, true},
		{"turbowarp without contact", `{"provider":"turbowarp","projectId":"example.com/game"}`, false},
		{"turbowarp without project", `{"provider":"turbowarp","contact":"example.com/contact"}`, false},
		{"custom uses app ID", `{"provider":"custom","server":"wss://example.com/cloud"}`, true},
		{"custom project", `{"provider":"custom","server":"ws://localhost:8080","projectId":"game"}`, true},
		{"custom missing server", `{"provider":"custom"}`, false},
		{"custom invalid URL", `{"provider":"custom","server":"https://example.com"}`, false},
		{"unknown provider", `{"provider":"unknown","projectId":"game","contact":"example.com"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Default(nil)
			s.Cloud.Mode = "websocket"
			if err := json.Unmarshal([]byte(tc.cloud), &s.Cloud); err != nil {
				t.Fatal(err)
			}
			if err := s.Validate(); (err == nil) != tc.valid {
				t.Fatalf("Validate() = %v, want valid=%t", err, tc.valid)
			}
		})
	}
}

func TestDefaultsUseProjectConfiguration(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage","comments":{"c":{"text":"_twconfig_ {\"framerate\":60,\"turbo\":true,\"stageWidth\":640,\"stageHeight\":480}"}}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	s := Default(&p)
	if s.FPS != 60 || !s.Turbo || s.Width != 640 || s.Height != 480 {
		t.Fatalf("project settings lost: %+v", s)
	}
}

func TestProjectStoredSettings(t *testing.T) {
	for _, tc := range []struct {
		name, comment        string
		width, height        int
		fps                  float64
		turbo, interpolation bool
	}{
		{"partial width", `{"width":800,"turbo":true} // _twconfig_`, 800, 360, 30, true, false},
		{
			"partial height",
			`{"height":600,"framerate":75,"interpolation":true} // _twconfig_`,
			480,
			600,
			75,
			false,
			true,
		},
		{
			"extended JSON",
			`{"width":800,"height":600,"framerate":60,"turbo":true,"interpolation":true,"runtimeOptions":{"maxClones":Infinity}} // _twconfig_`,
			800,
			600,
			60,
			true,
			true,
		},
		{
			"surrounding text",
			"Example {not JSON}\n{\"framerate\":120,\"turbo\":true} // _twconfig_\nOther {text}",
			480,
			360,
			120,
			true,
			false,
		},
		{
			"invalid field",
			`{"width":"invalid","height":600,"framerate":60,"turbo":true} // _twconfig_`,
			480,
			600,
			60,
			true,
			false,
		},
		{"default", `ordinary comment`, 480, 360, 30, false, false},
		{"screen FPS fallback", `{"framerate":0} // _twconfig_`, 480, 360, 60, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(map[string]any{"targets": []any{map[string]any{"isStage": true, "comments": map[string]any{"c": map[string]any{"text": tc.comment}}}}})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "project.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			p, err := project.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			s := Default(p)
			if s.Width != tc.width || s.Height != tc.height || s.FPS != tc.fps || s.Turbo != tc.turbo || s.Interpolation != tc.interpolation {
				t.Fatalf("stored settings lost: %+v", s)
			}
			if err := s.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProjectLimitSettingsRoundTrip(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"comments":{"c":{"text":"{\"runtimeOptions\":{\"fencing\":false,\"miscLimits\":false}} // _twconfig_"}}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	s := Default(&p)
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"removeFencing", "removeMiscLimits"} {
		if values[key] != true {
			t.Errorf("%s = %v, want true from project", key, values[key])
		}
	}
	if err := json.Unmarshal([]byte(`{"removeFencing":false,"removeMiscLimits":false}`), &s); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "limits.json")
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"removeFencing", "removeMiscLimits"} {
		if values[key] != false {
			t.Errorf("%s = %v, explicit false lost", key, values[key])
		}
	}
}

func TestMissingWindowDimensionsFollowDecodedStage(t *testing.T) {
	for _, tc := range []struct {
		data          string
		width, height int
	}{
		{`{"width":800,"height":600}`, 800, 600},
		{`{"width":800,"height":600,"window":{"width":960}}`, 960, 600},
		{`{"width":800,"height":600,"window":{"width":0,"height":0}}`, 800, 600},
	} {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		s, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if s.Window.Width != tc.width || s.Window.Height != tc.height || s.Width != 800 || s.Height != 600 {
			t.Fatalf("missing display values did not inherit stage: %+v", s)
		}
	}
}

func TestCanvasAntialiasSettingsRoundTrip(t *testing.T) {
	s := Default(nil)
	if !s.Canvas.Antialias {
		t.Fatal("antialiasing should be enabled by default")
	}
	s.Canvas.Antialias = false
	path := filepath.Join(t.TempDir(), "pixel.gowarp.json")
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Canvas.Antialias {
		t.Fatal("saving and loading reenabled antialiasing")
	}
}

func TestIntegerWindowFitRoundTrip(t *testing.T) {
	s := Default(nil)
	if s.Window.Fit != "preserve" {
		t.Fatal("default window fit changed")
	}
	s.Window.Fit = "integer"
	path := filepath.Join(t.TempDir(), "integer.gowarp.json")
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Window.Fit != "integer" {
		t.Fatal("integer window fit was lost")
	}
}
