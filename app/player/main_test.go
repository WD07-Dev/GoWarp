package player

import (
	"context"
	"testing"

	"gowarp/app/config"
	"gowarp/core/cloud"
	"gowarp/core/project"
)

func TestCloudNumberEncodingKeepsDecimalDigits(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{{float64(1000000), "1000000"}, {0.0000001, "0.0000001"}, {"000123", "000123"}} {
		if got := cloudText(tc.value); got != tc.want {
			t.Fatalf("cloud %v = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestCloudUsesPlayerUsername(t *testing.T) {
	s := config.Default(nil)
	s.Player.Username = "Player One"
	options := cloudOptions(s, &project.Project{})
	if options.Username != s.Player.Username {
		t.Fatalf("cloud username = %q, want %q", options.Username, s.Player.Username)
	}
}

func TestLocalCloudIdentitySurvivesRename(t *testing.T) {
	p := &project.Project{Targets: []*project.Target{{
		IsStage:   true,
		Name:      "Stage",
		Variables: map[string][]any{"a": {"score", 0, true}, "b": {"score", 0, true}},
	}}}
	s := config.Default(nil)
	s.Cloud.Mode = "local"
	dir := t.TempDir()
	store, err := cloud.Open(context.Background(), cloudOptions(s, p), dir)
	if err != nil {
		t.Fatal(err)
	}
	store.Set("a", "12")
	store.Set("b", "34")
	store.Close()
	p.Targets[0].Variables["a"][0] = "renamed"
	store, err = cloud.Open(context.Background(), cloudOptions(s, p), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got := store.Poll()
	if got["a"] != "12" || got["b"] != "34" {
		t.Fatalf("lost local identity: %v", got)
	}
}

func TestInvalidSettingsDoNotStartEventLoop(t *testing.T) {
	started := false
	s := config.Default(nil)
	s.Width = 0
	err := runPrepared(context.Background(), "/missing/gowarp-project.sb3", s, false, &started, nil)
	if err == nil || started {
		t.Fatalf("startup err=%v started=%v", err, started)
	}
}
