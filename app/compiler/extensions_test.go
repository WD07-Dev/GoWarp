package compiler

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"gowarp/app/assets"
	"gowarp/app/config"
	"gowarp/core/project"
)

const testExtensionJS = `/*
@gowarp-config {
}

@gowarp-go {
package extension

import "gowarp/core/extension"

func New() extension.Extension {
	return extension.Extension{ID: "fresh"}
}
}
*/
void 0;`

func TestResolveExtensionSources(t *testing.T) {
	p := &project.Project{Extensions: []string{"pen", "fresh"}, ExtensionURLs: map[string]string{"fresh": "data:application/javascript;base64," + base64.StdEncoding.EncodeToString([]byte(testExtensionJS))}}
	sources, err := resolveExtensionSources(context.Background(), filepath.Join(t.TempDir(), "project.json"), p)
	if err != nil || len(sources) != 1 || sources[0].ID != "fresh" {
		t.Fatalf("sources=%v err=%v", sources, err)
	}
	configured := strings.Replace(testExtensionJS, "@gowarp-config {\n}", "@gowarp-config {\nrequire example.com/library v1.2.3\n}", 1)
	p.ExtensionURLs["fresh"] = "data:application/javascript;base64," + base64.StdEncoding.EncodeToString([]byte(configured))
	withConfig, err := resolveExtensionSources(context.Background(), filepath.Join(t.TempDir(), "project.json"), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(withConfig[0].Requirements) != 1 || withConfig[0].Requirements[0].Version != "v1.2.3" || !bytes.Equal(sources[0].Go, withConfig[0].Go) {
		t.Fatalf("config was not kept separate from Go source: %+v", withConfig[0])
	}
	p.ExtensionURLs["fresh"] = "data:text/javascript," + "void%200;"
	if _, err = resolveExtensionSources(context.Background(), "", p); err == nil || !strings.Contains(err.Error(), "fresh") {
		t.Fatalf("missing implementation: %v", err)
	}
}

func TestExtensionRuntimeBuildCacheAndFailure(t *testing.T) {
	sdk := t.TempDir()
	files := extensionSDKFixture(t)
	for name, data := range files {
		path := filepath.Join(sdk, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	tpl := Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}
	sources := []extensionSource{{ID: "fresh", Go: []byte("package extension\nimport \"gowarp/core/extension\"\nfunc New() extension.Extension {return extension.Extension{ID:\"fresh\"}}")}}
	cache := t.TempDir()
	built, err := buildExtensionRuntime(context.Background(), tpl, sources, sdk, cache)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(built.Path).CombinedOutput()
	if err != nil || string(output) != "fresh\n" {
		t.Fatalf("output=%q err=%v", output, err)
	}
	before, _ := os.Stat(built.Path)
	again, err := buildExtensionRuntime(context.Background(), tpl, sources, sdk, cache)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(again.Path)
	if built.Path != again.Path || before.ModTime() != after.ModTime() {
		t.Fatal("unchanged runtime not reused")
	}
	sources[0].Go = []byte(strings.ReplaceAll(string(sources[0].Go), `ID:"fresh"`, `ID:"changed"`))
	changed, err := buildExtensionRuntime(context.Background(), tpl, sources, sdk, cache)
	if err != nil || changed.Path == built.Path {
		t.Fatalf("source did not invalidate cache: %v", err)
	}
	output, err = exec.Command(changed.Path).CombinedOutput()
	if err != nil || string(output) != "changed\n" {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if err = os.WriteFile(filepath.Join(sdk, "app", "player", "main.go"), []byte(strings.ReplaceAll(files["app/player/main.go"], "fmt.Println(f().ID)", "fmt.Println(f().ID + \" sdk2\")")), 0644); err != nil {
		t.Fatal(err)
	}
	updated, err := buildExtensionRuntime(context.Background(), tpl, sources, sdk, cache)
	if err != nil || updated.Path == changed.Path {
		t.Fatalf("SDK change did not invalidate cache: %v", err)
	}
	output, err = exec.Command(updated.Path).CombinedOutput()
	if err != nil || string(output) != "changed sdk2\n" {
		t.Fatalf("output=%q err=%v", output, err)
	}
	sources[0].Go = []byte("package extension\nfunc New() { missing() }")
	if _, err = buildExtensionRuntime(context.Background(), tpl, sources, sdk, cache); err == nil || !strings.Contains(err.Error(), "fresh") {
		t.Fatalf("compile error lacks extension: %v", err)
	}
	if _, err = os.Stat(built.Path); err != nil {
		t.Fatal("failure removed successful cache")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = buildExtensionRuntime(ctx, tpl, sources, sdk, cache); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
}

func extensionSDKFixture(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{"go.mod": "module gowarp\n\ngo 1.25.0\n", "go.sum": "", "app/player/main.go": "package player\nimport (\"fmt\"; \"gowarp/core/extension\")\nfunc MainWithExtensions(b extension.Bundle) { for _, f := range b { fmt.Println(f().ID) } }\n"}
	files["platform/linux/native/fixture.go"] = "package native\n"
	root, err := sourceRoot()
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "core", "extension", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files["core/extension/"+filepath.Base(path)] = string(data)
	}
	return files
}

func TestNativeRuntimeBuildCacheAndValidation(t *testing.T) {
	sdk := t.TempDir()
	files := extensionSDKFixture(t)
	files["app/player/main.go"] = "package player\nimport (\"fmt\"; \"gowarp/core/extension\"; \"gowarp/core/vm\")\nfunc MainWithExtensions(b extension.Bundle) { fmt.Println(vm.NativeValue) }\n"
	files["core/extension/native_fixture.go"] = "package extension\nimport \"gowarp/core/vm\"\nvar _ = vm.NativeValue\n"
	for name, data := range files {
		path := filepath.Join(sdk, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	tpl := Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}
	cache := t.TempDir()
	native := []byte("package vm\nconst NativeValue = 42\n")
	built, err := buildExtensionRuntime(context.Background(), tpl, nil, sdk, cache, native)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(built.Path).CombinedOutput()
	if err != nil || string(output) != "42\n" {
		t.Fatalf("native runtime output=%q err=%v", output, err)
	}
	if err = validateExtensionProject(context.Background(), built.validatorPath, &project.Project{}); err != nil {
		t.Fatalf("native-only validation: %v", err)
	}
	before, err := os.Stat(built.Path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := buildExtensionRuntime(context.Background(), tpl, nil, sdk, cache, native)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(again.Path)
	if err != nil || built.Path != again.Path || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("unchanged native runtime was not reused: %v", err)
	}
	native = []byte("package vm\nconst NativeValue = 43\n")
	changed, err := buildExtensionRuntime(context.Background(), tpl, nil, sdk, cache, native)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Path == built.Path || changed.validatorPath == built.validatorPath {
		t.Fatal("native source change reused cached runtime or validator")
	}
	output, err = exec.Command(changed.Path).CombinedOutput()
	if err != nil || string(output) != "43\n" {
		t.Fatalf("changed native runtime output=%q err=%v", output, err)
	}
	if _, err = os.Stat(filepath.Join(sdk, "core", "vm", "native_project_generated.go")); !os.IsNotExist(err) {
		t.Fatalf("native compilation modified source SDK: %v", err)
	}
}

func TestExportCompilesWithoutExtensions(t *testing.T) {
	dir := t.TempDir()
	writeExtensionArchive(t, filepath.Join(dir, "runtime-source.zip"), extensionSDKFixture(t))
	input := filepath.Join(dir, "empty.sb3")
	writeExtensionArchive(t, input, map[string]string{
		"project.json": `{"targets":[{"isStage":true,"name":"Stage","blocks":{}}]}`,
	})
	tpl := Template{
		Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Path: filepath.Join(dir, "runtimes", "fixture", "player"),
	}
	output := filepath.Join(dir, "game")
	if err := Export(context.Background(), tpl, input, output, config.Default(nil)); err != nil {
		t.Fatal(err)
	}
	packed, err := ReadPackage(output)
	if err != nil || len(packed.Project) == 0 {
		t.Fatalf("compiled game has no project: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if result, err := exec.Command(output).CombinedOutput(); err != nil {
		t.Fatalf("compiled game requires Go at runtime: %v\n%s", err, result)
	}
}

func TestExportRequiresGoForEveryProject(t *testing.T) {
	dir := t.TempDir()
	writeExtensionArchive(t, filepath.Join(dir, "runtime-source.zip"), extensionSDKFixture(t))
	runtimePath := filepath.Join(dir, "runtimes", "linux-amd64", "player")
	if err := os.MkdirAll(filepath.Dir(runtimePath), 0755); err != nil {
		t.Fatal(err)
	}
	runtimeData := []byte("prebuilt runtime")
	if err := os.WriteFile(runtimePath, runtimeData, 0755); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(runtimeData)
	tpl := Template{Version: config.Version, OS: "linux", Arch: "amd64", Path: runtimePath, SHA256: hex.EncodeToString(hash[:])}
	t.Setenv("PATH", t.TempDir())
	for _, test := range []struct {
		name       string
		warp       bool
		extensions string
	}{
		{"ordinary", false, "[]"},
		{"warp", true, "[]"},
		{"pen", true, "[\"pen\"]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := filepath.Join(t.TempDir(), "project.sb3")
			manifest := []byte(`{"targets":[{"isStage":true,"name":"Stage","variables":{"score":["score",0]},"blocks":{
				"def":{"opcode":"procedures_definition","topLevel":true,"next":"set","inputs":{"custom_block":[1,"proto"]}},
				"proto":{"opcode":"procedures_prototype","mutation":{"proccode":"calculate","argumentids":"[]","argumentnames":"[]","argumentdefaults":"[]","warp":"` + strconv.FormatBool(test.warp) + `"}},
				"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"42"]]}}
			}}],"extensions":` + test.extensions + `}`)
			writeExtensionArchive(t, input, map[string]string{"project.json": string(manifest)})
			original, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "game")
			err = Export(context.Background(), tpl, input, output, config.Default(nil))
			if !errors.Is(err, exec.ErrNotFound) {
				t.Fatalf("missing Go was not reported: %v", err)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("failed build published an output: %v", err)
			}
			after, err := os.ReadFile(input)
			if err != nil || !bytes.Equal(after, original) {
				t.Fatalf("export changed project source: %v", err)
			}
		})
	}
	t.Run("custom extension still requires Go", func(t *testing.T) {
		input := filepath.Join(t.TempDir(), "project.sb3")
		manifest := map[string]any{
			"targets":    []any{map[string]any{"isStage": true, "name": "Stage"}},
			"extensions": []string{"fresh"},
			"extensionURLs": map[string]string{
				"fresh": "data:application/javascript;base64," + base64.StdEncoding.EncodeToString([]byte(testExtensionJS)),
			},
		}
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		writeExtensionArchive(t, input, map[string]string{"project.json": string(data)})
		output := filepath.Join(t.TempDir(), "game")
		if err := os.WriteFile(output, []byte("previous game"), 0600); err != nil {
			t.Fatal(err)
		}
		err = Export(context.Background(), tpl, input, output, config.Default(nil))
		if !errors.Is(err, exec.ErrNotFound) {
			t.Fatalf("missing extension toolchain was not reported: %v", err)
		}
		after, err := os.ReadFile(output)
		if err != nil || string(after) != "previous game" {
			t.Fatalf("failed build changed existing output: %v", err)
		}
	})
}

func TestExportValidatesExtensionMetadataBeforePublication(t *testing.T) {
	dir := t.TempDir()
	writeExtensionArchive(t, filepath.Join(dir, "runtime-source.zip"), extensionSDKFixture(t))
	tpl := Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH, Path: filepath.Join(dir, "runtimes", runtime.GOOS+"-"+runtime.GOARCH, "player")}
	input := filepath.Join(dir, "project.json")
	output := filepath.Join(dir, "game")
	for name, definition := range map[string]string{
		"wrong ID":           `extension.Extension{ID:"other"}`,
		"duplicate opcode":   `extension.Extension{ID:"fresh",Blocks:[]extension.Block{{Opcode:"fresh_value",Kind:"hat"},{Opcode:"fresh_value",Kind:"hat"}}}`,
		"missing used block": `extension.Extension{ID:"fresh"}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := "/*\n@gowarp-go {\npackage extension\nimport \"gowarp/core/extension\"\nfunc New() extension.Extension{return " + definition + "}\n}\n*/"
			manifest := map[string]any{"targets": []any{map[string]any{"isStage": true, "blocks": map[string]any{"block": map[string]any{"opcode": "fresh_value"}}}}, "extensions": []string{"fresh"}, "extensionURLs": map[string]string{"fresh": "data:application/javascript;base64," + base64.StdEncoding.EncodeToString([]byte(source))}}
			data, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(input, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(output, []byte("previous output"), 0600); err != nil {
				t.Fatal(err)
			}
			if err = Export(context.Background(), tpl, input, output, config.Default(nil)); err == nil {
				t.Fatal("published invalid extension registration")
			}
			after, err := os.ReadFile(output)
			if err != nil || string(after) != "previous output" {
				t.Fatal("invalid registration changed existing output")
			}
		})
	}
}

func TestExportRevalidatesCachedExtensionsAgainstCurrentProject(t *testing.T) {
	dir := t.TempDir()
	writeExtensionArchive(t, filepath.Join(dir, "runtime-source.zip"), extensionSDKFixture(t))
	tpl := Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH, Path: filepath.Join(dir, "runtimes", runtime.GOOS+"-"+runtime.GOARCH, "player")}
	input := filepath.Join(dir, "project.json")
	output := filepath.Join(dir, "game")
	calls := filepath.Join(dir, "factory-calls")
	source := strings.ReplaceAll(`/*
@gowarp-config {
}

@gowarp-go {
package extension

import (
	"os"

	"gowarp/core/extension"
)

func New() extension.Extension {
	data, _ := os.ReadFile({{CALLS_PATH}})
	if err := os.WriteFile({{CALLS_PATH}}, append(data, 'x'), 0600); err != nil {
		panic(err)
	}
	return extension.Extension{
		ID: "fresh",
		Blocks: []extension.Block{
			{Opcode: "fresh_value", Kind: "hat"},
		},
		Events: map[string]extension.Handler{
			"load": func(*extension.Context) (extension.Result, error) {
				panic("lifecycle invoked during validation")
			},
		},
	}
}
}
*/`, "{{CALLS_PATH}}", strconv.Quote(calls))
	block := map[string]any{"opcode": "fresh_value"}
	manifest := map[string]any{
		"targets": []any{map[string]any{
			"isStage": true,
			"blocks":  map[string]any{"block": block},
		}},
		"extensions": []string{"fresh"},
		"extensionURLs": map[string]string{
			"fresh": "data:application/javascript;base64," + base64.StdEncoding.EncodeToString([]byte(source)),
		},
	}
	writeManifest := func() {
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(input, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest()
	for range 2 {
		if err := Export(context.Background(), tpl, input, output, config.Default(nil)); err != nil {
			t.Fatal(err)
		}
	}
	count, err := os.ReadFile(calls)
	if err != nil || string(count) != "xx" {
		t.Fatalf("cache hit skipped factory validation: %q %v", count, err)
	}
	before, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	block["opcode"] = "fresh_missing"
	writeManifest()
	if err = Export(context.Background(), tpl, input, output, config.Default(nil)); err == nil || !strings.Contains(err.Error(), "fresh_missing") {
		t.Fatalf("cached runtime accepted missing opcode: %v", err)
	}
	after, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed revalidation changed output")
	}
}

func TestExtensionValidatorUsesHostAndSupportsCancellation(t *testing.T) {
	dir := t.TempDir()
	sdk := filepath.Join(dir, "sdk")
	for name, data := range extensionSDKFixture(t) {
		path := filepath.Join(sdk, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	targetOS := "windows"
	if runtime.GOOS == "windows" {
		targetOS = "linux"
	}
	tpl := Template{Version: config.Version, OS: targetOS, Arch: "amd64"}
	sources := []extensionSource{{ID: "fresh", Go: []byte("package extension\nimport \"gowarp/core/extension\"\nfunc New() extension.Extension {return extension.Extension{ID:\"fresh\"}}")}}
	built, err := buildExtensionRuntime(context.Background(), tpl, sources, sdk, filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	p := &project.Project{Extensions: []string{"fresh"}}
	if err = validateExtensionProject(context.Background(), built.validatorPath, p); err != nil {
		t.Fatalf("host validator could not run for %s target: %v", targetOS, err)
	}
	sources[0].Go = []byte("package extension\nimport (\"time\";\"gowarp/core/extension\")\nfunc New() extension.Extension { for { time.Sleep(time.Second) } }")
	built, err = buildExtensionRuntime(context.Background(), tpl, sources, sdk, filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err = validateExtensionProject(ctx, built.validatorPath, p); err != context.DeadlineExceeded {
		t.Fatalf("validator cancellation: %v", err)
	}
}

func TestExportRejectsUndeclaredCustomOpcode(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "project.json")
	if err := os.WriteFile(input, []byte(`{"targets":[{"isStage":true,"blocks":{"x":{"opcode":"undeclared_value"}}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	err := Export(context.Background(), Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}, input, filepath.Join(dir, "game"), config.Default(nil))
	if err == nil || !strings.Contains(err.Error(), "undeclared_value") {
		t.Fatalf("missing source bypassed coverage validation: %v", err)
	}
}

func writeExtensionArchive(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, data := range files {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtensionArchiveAndLocalSourceProtection(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "project.sb3")
	writeExtensionArchive(t, input, map[string]string{"extensions/fresh.js": testExtensionJS})
	data, path, err := readExtensionSource(context.Background(), input, "extensions/fresh.js")
	if err != nil || string(data) != testExtensionJS || path != "" {
		t.Fatalf("archive: %s %s %v", data, path, err)
	}
	if _, _, err = readExtensionSource(context.Background(), input, "../fresh.js"); err == nil {
		t.Fatal("accepted traversal")
	}
	input = filepath.Join(dir, "project.json")
	source := filepath.Join(dir, "fresh.js")
	if err = os.WriteFile(input, []byte(`{"targets":[{"isStage":true}],"extensions":["fresh"],"extensionURLs":{"fresh":"fresh.js"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(source, []byte(testExtensionJS), 0600); err != nil {
		t.Fatal(err)
	}
	err = Export(context.Background(), Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}, input, source, config.Default(nil))
	if err == nil || !strings.Contains(err.Error(), "output must differ") {
		t.Fatalf("unprotected local source: %v", err)
	}
	data, _ = os.ReadFile(source)
	if string(data) != testExtensionJS {
		t.Fatal("local JS overwritten")
	}
}

func TestExtensionSDKArchiveValidation(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "runtime-source.zip")
	writeExtensionArchive(t, archive, map[string]string{"go.mod": "module gowarp\n", "core/extension/api.go": "package extension", "../../escape.go": "package bad"})
	if err := unpackExtensionSDK(context.Background(), archive, t.TempDir()); err == nil {
		t.Fatal("accepted archive traversal")
	}
	writeExtensionArchive(t, archive, map[string]string{"go.mod": "module gowarp\n", "core/extension/api.go": "package extension"})
	work := t.TempDir()
	tpl := Template{Path: filepath.Join(dir, "runtimes", "linux-amd64", "player"), SDKHash: "wrong"}
	if _, err := findExtensionSDK(context.Background(), tpl, work, ""); err == nil || !strings.Contains(err.Error(), "SDK checksum") {
		t.Fatalf("accepted mismatched SDK: %v", err)
	}
	tpl.SDKHash = ""
	if _, err := findExtensionSDK(context.Background(), tpl, work, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(work, "core", "extension", "api.go"))
	if err != nil || string(data) != "package extension" {
		t.Fatalf("SDK source missing: %v", err)
	}
}

func TestEmbeddedExtensionSDK(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "embedded.zip")
	writeExtensionArchive(t, archive, map[string]string{
		"go.mod":                "module gowarp\n",
		"core/extension/api.go": "package extension",
	})
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	previous := assets.RuntimeSource
	assets.RuntimeSource = data
	t.Cleanup(func() { assets.RuntimeSource = previous })
	hash := sha256.Sum256(data)
	tpl := Template{
		Path:    filepath.Join(dir, "runtimes", "linux-amd64", "player"),
		SDKHash: hex.EncodeToString(hash[:]),
	}
	t.Run("no sidecar", func(t *testing.T) {
		work := t.TempDir()
		if _, err := findExtensionSDK(context.Background(), tpl, work, ""); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(work, "core", "extension", "api.go"))
		if err != nil || string(got) != "package extension" {
			t.Fatalf("embedded source not extracted: %q, %v", got, err)
		}
	})
	t.Run("ignores stale sidecar", func(t *testing.T) {
		writeExtensionArchive(t, filepath.Join(dir, "runtime-source.zip"), map[string]string{"go.mod": "stale"})
		if _, err := findExtensionSDK(context.Background(), tpl, t.TempDir(), ""); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("mismatched runtime", func(t *testing.T) {
		mismatch := tpl
		mismatch.SDKHash = "wrong"
		if _, err := findExtensionSDK(context.Background(), mismatch, t.TempDir(), ""); err == nil || !strings.Contains(err.Error(), "embedded SDK checksum") {
			t.Fatalf("expected embedded SDK mismatch: %v", err)
		}
	})
	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := findExtensionSDK(ctx, tpl, t.TempDir(), ""); err != context.Canceled {
			t.Fatalf("expected cancellation: %v", err)
		}
	})
}

func TestExtensionSDKExcludesEmbeddedArchive(t *testing.T) {
	if extensionSDKFile("app/assets/embed/runtime-source.zip") {
		t.Fatal("source SDK recursively includes its generated archive")
	}
}

func TestExtensionCacheEvictsOldEntries(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "latest")
	for i := 0; i < 18; i++ {
		path := filepath.Join(dir, strings.Repeat("a", i+1))
		if err := os.WriteFile(path, []byte("runtime"), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(int64(i+1), 0)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(keep, []byte("latest runtime"), 0600); err != nil {
		t.Fatal(err)
	}
	trimExtensionCache(dir, keep)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 16 {
		t.Fatalf("cache unbounded: %d %v", len(entries), err)
	}
	if _, err = os.Stat(keep); err != nil {
		t.Fatal("evicted active runtime")
	}
	if _, err = os.Stat(filepath.Join(dir, "a")); !os.IsNotExist(err) {
		t.Fatal("oldest runtime retained")
	}
}

func TestExtensionSDKCannotBeExportDestination(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "runtime-source.zip")
	writeExtensionArchive(t, archive, map[string]string{"go.mod": "module gowarp\n"})
	tpl := Template{Path: filepath.Join(dir, "runtimes", "linux-amd64", "player")}
	if _, err := findExtensionSDK(context.Background(), tpl, t.TempDir(), archive); err == nil {
		t.Fatal("SDK archive accepted as destination")
	}
	root, err := sourceRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = findExtensionSDK(context.Background(), Template{Path: filepath.Join(t.TempDir(), "player")}, t.TempDir(), filepath.Join(root, "core", "extension", "api.go")); err == nil {
		t.Fatal("SDK Go source accepted as destination")
	}
}

func TestExportMissingExtensionPreservesOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "project.json")
	out := filepath.Join(dir, "game")
	if err := os.WriteFile(input, []byte(`{"targets":[{"isStage":true,"name":"Stage"}],"extensions":["fresh"],"extensionURLs":{"fresh":"data:text/javascript,void%200;"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	err := Export(context.Background(), Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}, input, out, config.Default(nil))
	if err == nil || !strings.Contains(err.Error(), "fresh") {
		t.Fatalf("error=%v", err)
	}
	data, _ := os.ReadFile(out)
	if string(data) != "previous" {
		t.Fatal("failed export overwrote output")
	}
}

func TestExtensionSDKPreservesPlatformSources(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                           "module gowarp\n",
		"go.sum":                           "",
		"app/player/main.go":               "package player\n",
		"core/extension/api.go":            "package extension\n",
		"core/vm/templates/native.go.tmpl": "{{define \"module\"}}package vm{{end}}\n",
		"platform/windows/native/command_windows.go": "package native\n",
		"platform/linux/native/dialog_linux.go":      "package native\n",
		"platform/windows/resource/icon.go":          "package resource\n",
	}
	for name, data := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	if found, err := sourceRoot(); err != nil || found != root {
		t.Fatalf("SDK without CLI sources was not found: %q, %v", found, err)
	}
	copied := t.TempDir()
	if _, err := copyExtensionSDK(context.Background(), root, copied); err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		data, err := os.ReadFile(filepath.Join(copied, filepath.FromSlash(name)))
		if err != nil || string(data) != want {
			t.Errorf("SDK copy %s: %q, %v", name, data, err)
		}
	}
	archive := filepath.Join(t.TempDir(), "sdk.zip")
	writeExtensionArchive(t, archive, files)
	extracted := t.TempDir()
	if err := unpackExtensionSDK(context.Background(), archive, extracted); err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		data, err := os.ReadFile(filepath.Join(extracted, filepath.FromSlash(name)))
		if err != nil || string(data) != want {
			t.Errorf("SDK extraction %s: %q, %v", name, data, err)
		}
	}
	if extensionSDKFile("platform/linux/native/dialog_linux_test.go") {
		t.Fatal("SDK includes native tests")
	}
}
