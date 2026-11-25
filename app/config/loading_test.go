package config

import (
	"bytes"
	"image"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadingSettingsRoundTripAndLegacyDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	defaults := DefaultLoadingSettings()
	if defaults.Text != "loading" || !defaults.ShowProgress {
		t.Fatalf("default loading text = %q", defaults.Text)
	}
	if s.Loading.Background != defaults.Background || s.Loading.Progress != defaults.Progress || s.Loading.Text != defaults.Text || !s.Loading.ShowProgress {
		t.Fatalf("missing loading defaults: %+v", s.Loading)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 12, 6))); err != nil {
		t.Fatal(err)
	}
	s.Loading = LoadingSettings{Width: 640, Height: 360, Background: "#123456", Progress: "#abcdef", Text: "", Logo: encoded.Bytes(), Image: encoded.Bytes()}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Loading.Width != 640 || got.Loading.Height != 360 || got.Loading.Background != s.Loading.Background || got.Loading.Progress != s.Loading.Progress || got.Loading.Text != "" || got.Loading.ShowProgress || !bytes.Equal(got.Loading.Logo, s.Loading.Logo) || !bytes.Equal(got.Loading.Image, s.Loading.Image) {
		t.Fatalf("loading settings changed: %+v", got.Loading)
	}
}

func TestInvalidLoadingSettings(t *testing.T) {
	for _, change := range []func(*LoadingSettings){
		func(s *LoadingSettings) {
			s.Background = "red"
		},
		func(s *LoadingSettings) {
			s.Progress = "#12345z"
		},
		func(s *LoadingSettings) {
			s.Logo = []byte("broken image")
		},
		func(s *LoadingSettings) {
			s.Image = []byte("broken image")
		},
	} {
		s := Default(nil)
		change(&s.Loading)
		if s.Validate() == nil {
			t.Fatal("invalid loading settings accepted")
		}
	}
}

func TestLoadingImageSettingsExceedLegacyLimit(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 640, 640))
	if _, err := rand.New(rand.NewSource(1)).Read(source.Pix); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	s := Default(nil)
	s.Loading.Image = encoded.Bytes()
	path := filepath.Join(t.TempDir(), "loading.json")
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 1<<20 {
		t.Fatal("test image did not exceed old settings size limit")
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Loading.Image, s.Loading.Image) {
		t.Fatal("large loading background changed")
	}
}

func TestLoadingWindowSize(t *testing.T) {
	s := DefaultLoadingSettings()
	if w, h := s.WindowSize(800, 600); w != 800 || h != 600 {
		t.Fatalf("default: %dx%d", w, h)
	}
	s.Width, s.Height = 640, 360
	if w, h := s.WindowSize(800, 600); w != 640 || h != 360 {
		t.Fatalf("custom: %dx%d", w, h)
	}
	s.Height = 0
	if w, h := s.WindowSize(800, 600); w != 640 || h != 600 {
		t.Fatalf("partial: %dx%d", w, h)
	}
	for _, size := range []int{-1, 4097} {
		s.Width, s.Height = size, 0
		if err := s.Validate(); err == nil {
			t.Fatalf("invalid width %d", size)
		}
		s.Width, s.Height = 0, size
		if err := s.Validate(); err == nil {
			t.Fatalf("invalid height %d", size)
		}
	}
}
