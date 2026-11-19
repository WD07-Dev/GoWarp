package compiler

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"gowarp/app/host"
)

func TestBuildEnvironmentKeepsGoDataInsideGoWarp(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("LocalAppData", cache)
	for _, key := range []string{"GOPATH", "GOMODCACHE", "GOCACHE", "GOBIN"} {
		t.Setenv(key, filepath.Join(t.TempDir(), "outside"))
	}
	t.Setenv("gopath", filepath.Join(t.TempDir(), "lowercase-outside"))
	env, err := extensionBuildEnv(Template{OS: "windows", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(cache, "gowarp", "go")
	want := map[string]string{
		"GOPATH": root, "GOMODCACHE": filepath.Join(root, "pkg", "mod"),
		"GOCACHE": filepath.Join(root, "cache"), "GOBIN": filepath.Join(root, "bin"),
		"GOOS": "windows", "GOARCH": "arm64",
	}
	seen := map[string]int{}
	for _, item := range env {
		key, value, _ := strings.Cut(item, "=")
		if expected, ok := want[strings.ToUpper(key)]; ok {
			seen[strings.ToUpper(key)]++
			if value != expected {
				t.Errorf("%s = %q, want %q", key, value, expected)
			}
		}
	}
	for key := range want {
		if seen[key] != 1 {
			t.Errorf("%s occurs %d times", key, seen[key])
		}
	}
	command := host.Command(context.Background(), "go", "env", "-json", "GOPATH", "GOMODCACHE", "GOCACHE", "GOBIN")
	command.Env = env
	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var actual map[string]string
	if err = json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	for key, value := range actual {
		if value != want[key] {
			t.Errorf("go env %s = %q", key, value)
		}
	}
}
