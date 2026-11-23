package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDisplayFPSRoundTrip(t *testing.T) {
	s := Default(nil)
	if s.Player.RenderFPS != 0 {
		t.Fatal("default settings cap display FPS")
	}
	s.Player.RenderFPS = 30
	path := filepath.Join(t.TempDir(), "player.json")
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Player.RenderFPS != 30 || got.FPS != s.FPS || got.Width != s.Width {
		t.Fatal("display FPS changed simulation or was lost")
	}
	for _, fps := range []int{-1, 1, 120} {
		s.Player.RenderFPS = fps
		if s.Validate() == nil {
			t.Fatalf("invalid display FPS %d", fps)
		}
	}
}

func TestPlayerUnknownSettingsStillRejected(t *testing.T) {
	for _, player := range []string{
		`{"renderFSP":30}`,
		`{"quality":"auto"}`,
		`{"maxRenderSize":640}`,
	} {
		path := filepath.Join(t.TempDir(), "invalid.json")
		if err := os.WriteFile(path, []byte(`{"player":`+player+`}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("unknown player setting was accepted: %s", player)
		}
	}
}
