package config

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestIconSurvivesSettingsRoundTrip(t *testing.T) {
	var icon bytes.Buffer
	if err := png.Encode(&icon, image.NewRGBA(image.Rect(0, 0, 32, 32))); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(icon.Bytes())
	path := filepath.Join(t.TempDir(), "settings.json")
	data := []byte(`{"player":{"icon":"` + encoded + `"}}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Player struct {
			Icon string
		}
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Player.Icon != encoded {
		t.Fatal("saved icon was lost")
	}
}
