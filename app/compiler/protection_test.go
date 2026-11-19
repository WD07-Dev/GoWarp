package compiler

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func unpackedFixture(t *testing.T) (string, string, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	manifest := filepath.Join(dir, "project.json")
	files := map[string][]byte{
		manifest:                          []byte(`{"targets":[{"isStage":true,"name":"Stage","costumes":[{"md5ext":"costume.svg"}],"sounds":[{"md5ext":"sound.wav"}],"blocks":{}}]}`),
		filepath.Join(dir, "costume.svg"): []byte("original costume"),
		filepath.Join(dir, "sound.wav"):   []byte("original sound"),
	}
	for path, data := range files {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, manifest, files
}

func unchangedInputs(t *testing.T, files map[string][]byte) {
	t.Helper()
	for path, want := range files {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("input changed: %s: %v", path, err)
		}
	}
}

func TestBuildProtectsUnpackedManifestAndAssets(t *testing.T) {
	for _, jsonInput := range []bool{false, true} {
		for _, name := range []string{"project.json", "costume.svg", "sound.wav"} {
			t.Run(name+map[bool]string{false: "-directory", true: "-json"}[jsonInput], func(t *testing.T) {
				dir, manifest, files := unpackedFixture(t)
				input := dir
				if jsonInput {
					input = manifest
				}
				_, err := Build(context.Background(), Options{
					Project: input,
					Output:  filepath.Join(dir, name),
				})
				if err == nil {
					t.Error("accepted output over project source")
				}
				unchangedInputs(t, files)
			})
		}
	}
}

func TestDirectPackProtectsUnpackedInputs(t *testing.T) {
	dir, manifest, files := unpackedFixture(t)
	exe := filepath.Join(t.TempDir(), "runtime")
	if err := os.WriteFile(exe, []byte("runtime bytes"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := pack(context.Background(), exe, dir, manifest, nil); err == nil {
		t.Error("direct pack accepted manifest output")
	}
	unchangedInputs(t, files)
}

func TestSeparatePackOutputRetainsUnpackedInputs(t *testing.T) {
	dir, _, files := unpackedFixture(t)
	output := filepath.Join(t.TempDir(), "nested", "game")
	if _, err := Build(context.Background(), Options{
		Project: dir,
		Output:  output,
	}); err != nil {
		t.Fatal(err)
	}
	unchangedInputs(t, files)
	payload, err := ReadEmbedded(output)
	if err != nil || payload == nil {
		t.Fatalf("missing valid payload: %v", err)
	}
}

func TestAliasedAssetOutputIsRejected(t *testing.T) {
	dir, _, files := unpackedFixture(t)
	alias := filepath.Join(t.TempDir(), "project-alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skip(err)
	}
	_, err := Build(context.Background(), Options{
		Project: dir,
		Output:  filepath.Join(alias, "costume.svg"),
	})
	if err == nil {
		t.Error("accepted aliased asset output")
	}
	unchangedInputs(t, files)
}
