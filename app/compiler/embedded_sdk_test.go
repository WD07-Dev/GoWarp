//go:build gowarp_packager

package compiler

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gowarp/app/assets"
	"gowarp/app/config"
)

// Requires the packager-only source SDK asset to be present during compilation.
func TestEmbeddedSDKPackagerExport(t *testing.T) {
	source := assets.RuntimeSource
	if len(source) == 0 {
		t.Fatal("authoring build has no embedded SDK")
	}
	archive, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range archive.File {
		if strings.HasSuffix(file.Name, "runtime-source.zip") || strings.Contains(file.Name, "/testdata/") {
			t.Fatalf("generated archive or regression fixture embedded: %s", file.Name)
		}
	}
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("LocalAppData", filepath.Join(dir, "cache"))
	/*
		Go makes downloaded modules read-only. This isolated cache belongs to
		the test, so make it removable before TempDir performs its cleanup.
	*/
	t.Cleanup(func() {
		modules := filepath.Join(dir, "cache", "gowarp", "go", "pkg", "mod")
		err := filepath.WalkDir(modules, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return os.Chmod(path, 0700)
			}
			return os.Chmod(path, 0600)
		})
		if err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
	})
	input := filepath.Join(dir, "project.json")
	output := filepath.Join(dir, "game")
	manifest := map[string]any{
		"targets": []any{map[string]any{
			"isStage": true, "name": "Stage",
			"variables": map[string][]any{"score": {"score", 0}},
			"blocks": json.RawMessage(`{
				"definition":{"opcode":"procedures_definition","topLevel":true,"next":"increment","inputs":{"custom_block":[1,"prototype"]}},
				"prototype":{"opcode":"procedures_prototype","mutation":{"proccode":"calculate","argumentids":"[]","argumentnames":"[]","argumentdefaults":"[]","warp":"true"}},
				"increment":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
			}`),
		}},
		"extensions": []string{"fresh"},
		"extensionURLs": map[string]string{
			"fresh": "data:application/javascript;base64," + base64.StdEncoding.EncodeToString([]byte(testExtensionJS)),
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(source)
	tpl := Template{
		Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Path:    filepath.Join(dir, "runtimes", runtime.GOOS+"-"+runtime.GOARCH, "player"),
		SDKHash: hex.EncodeToString(hash[:]),
	}
	if err = Export(context.Background(), tpl, input, output, config.Default(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadPackage(output); err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(binary, source) {
		t.Fatal("exported game includes the authoring SDK")
	}
	if !bytes.Contains(binary, []byte("gowarp/core/vm.nativeCalc")) {
		t.Fatal("exported game lacks the compiled calculation procedure")
	}
}
