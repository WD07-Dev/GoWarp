package compiler

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"gowarp/app/config"
	"gowarp/core/project"
)

func writePreparationSDK(t *testing.T, files map[string]string) string {
	t.Helper()
	sdk := t.TempDir()
	for name, data := range files {
		path := filepath.Join(sdk, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return sdk
}

func TestExtensionPreparationGeneratesNativeAndInvalidatesCache(t *testing.T) {
	files := extensionSDKFixture(t)
	files["core/vm/generate.go"] = `package vm
import ("fmt"; "gowarp/core/project")
var NativeValue int
func GenerateNative(p *project.Project) ([]byte, error) {
 target := p.Targets[0]
 value := target.Blocks[target.ScriptOrder[0]].Fields["VALUE"][0]
 return []byte(fmt.Sprintf("package vm\nfunc init() { NativeValue = %v }\n", value)), nil
}`
	files["app/player/main.go"] = `package player
import ("fmt"; "gowarp/core/extension"; "gowarp/core/vm")
func MainWithExtensions(extension.Bundle) { fmt.Println(vm.NativeValue) }
`
	sdk := writePreparationSDK(t, files)
	tpl := Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}
	cache := t.TempDir()
	p := new(project.Project)
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"blocks":{"z":{"opcode":"operator_add","topLevel":true,"fields":{"VALUE":[40]}},"a":{"opcode":"operator_add","topLevel":true,"fields":{"VALUE":[90]}}}}],"extensions":["fresh"]}`), p); err != nil {
		t.Fatal(err)
	}
	sources := []extensionSource{{ID: "fresh", Go: []byte(`package fresh
import ("gowarp/core/extension"; "gowarp/core/project")
func New() extension.Extension {
 return extension.Extension{ID: "fresh", Prepare: func(p *project.Project) error {
  for _, block := range p.Targets[0].Blocks { block.Fields["VALUE"][0] = block.Fields["VALUE"][0].(float64) + 2 }
  return nil
 }}
}`)}}
	build := func(want string) Template {
		t.Helper()
		built, err := buildExtensionRuntime(context.Background(), tpl, sources, sdk, cache, p)
		if err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(built.Path).CombinedOutput()
		if err != nil || string(output) != want+"\n" {
			t.Fatalf("prepared native output=%q want=%q err=%v", output, want, err)
		}
		return built
	}
	first := build("42")
	before, err := os.Stat(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	again := build("42")
	after, err := os.Stat(again.Path)
	if err != nil || first.Path != again.Path || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("unchanged project missed cache: %v", err)
	}
	if got := p.Targets[0].Blocks["z"].Fields["VALUE"][0]; got != float64(40) {
		t.Fatalf("preparation mutated original: %v", got)
	}
	p.Targets[0].ScriptOrder = []string{"a", "z"}
	ordered := build("92")
	if ordered.Path == first.Path {
		t.Fatal("saved script order did not invalidate cache")
	}
	p.Targets[0].Blocks["a"].Fields["VALUE"][0] = float64(100)
	changed := build("102")
	if changed.Path == ordered.Path {
		t.Fatal("original project did not invalidate cache")
	}
	sources[0].Go = []byte(strings.Replace(string(sources[0].Go), "+ 2", "+ 3", 1))
	transformed := build("103")
	if transformed.Path == changed.Path {
		t.Fatal("preparation implementation did not invalidate cache")
	}
	if _, err := os.Stat(filepath.Join(sdk, "core", "vm", "native_project_generated.go")); !os.IsNotExist(err) {
		t.Fatalf("modified original SDK: %v", err)
	}
}

func TestExtensionPreparationRejectsErrorsAndUnsupportedOpcodes(t *testing.T) {
	sdk := writePreparationSDK(t, extensionSDKFixture(t))
	tpl := Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}
	p := &project.Project{Targets: []*project.Target{{IsStage: true, Blocks: project.Blocks{}}}, Extensions: []string{"fresh"}}
	for _, test := range []struct{ name, body, want string }{
		{"error", `return fmt.Errorf("transform failed")`, "transform failed"},
		{"opcode", `p.Targets[0].Blocks["bad"] = &project.Block{Opcode:"missing_block"}; return nil`, "missing_block"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sources := []extensionSource{{ID: "fresh", Go: []byte(`package fresh
import ("fmt"; "gowarp/core/extension"; "gowarp/core/project")
var _ = fmt.Errorf
func New() extension.Extension { return extension.Extension{ID:"fresh", Prepare:func(p *project.Project) error {` + test.body + `} } }
`)}}
			cache := t.TempDir()
			_, err := buildExtensionRuntime(context.Background(), tpl, sources, sdk, cache, p)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("preparation error=%v", err)
			}
			entries, err := os.ReadDir(cache)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed preparation published cache: %v %v", entries, err)
			}
			if len(p.Targets[0].Blocks) != 0 {
				t.Fatal("failed preparation changed original graph")
			}
		})
	}
}

func TestExtensionPreparationSupportsCancellation(t *testing.T) {
	sdk := writePreparationSDK(t, extensionSDKFixture(t))
	tpl := Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}
	sources := []extensionSource{{ID: "fresh", Go: []byte(`package fresh
import ("time"; "gowarp/core/extension"; "gowarp/core/project")
func New() extension.Extension { return extension.Extension{ID:"fresh", Prepare:func(*project.Project) error { for {time.Sleep(time.Second)} } } }
`)}}
	built, err := buildExtensionRuntime(context.Background(), tpl, sources, sdk, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &project.Project{Targets: []*project.Target{{IsStage: true}}, Extensions: []string{"fresh"}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err = prepareExtensionProject(ctx, built.validatorPath, p, ""); err != context.DeadlineExceeded {
		t.Fatalf("prepare cancellation: %v", err)
	}
}

func TestExtensionPreparationNativeRuntimeParity(t *testing.T) {
	files := extensionSDKFixture(t)
	delete(files, "core/vm/generate.go")
	root, err := sourceRoot()
	if err != nil {
		t.Fatal(err)
	}
	// Use the real generator and VM; only the desktop player is replaced.
	paths, err := filepath.Glob(filepath.Join(root, "core", "vm", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, filepath.Join(root, "core", "vm", "templates", "native.go.tmpl"))
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		files[filepath.ToSlash(relative)] = string(data)
	}
	files["core/vm/preparation_fixture.go"] = `package vm
import ("context"; "fmt"; "gowarp/core/extension"; "gowarp/core/project")
func RunPreparationFixture(p *project.Project, b extension.Bundle, interpreted bool) (any,error) {
 r := New(p)
 if err := r.SetExtensions(b); err != nil { return nil,err }
 if !r.native || len(nativeScripts[0]["hat"]) == 0 { return nil,fmt.Errorf("prepared native script not bound") }
 if interpreted { r.native = false }
 r.GreenFlag()
 if err := r.Run(context.Background(),10,60,false); err != nil { return nil,err }
 return r.Snapshot()[0].Variables["score"].Value,nil
}
`
	manifest := `{"targets":[{"isStage":true,"name":"Stage","variables":{"score":["score",0]},"blocks":{"hat":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},"set":{"opcode":"data_setvariableto","next":"add","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},"add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}}}],"extensions":["fresh"]}`
	files["app/player/main.go"] = `package player
import ("encoding/json"; "fmt"; "gowarp/core/extension"; "gowarp/core/project"; "gowarp/core/vm")
func MainWithExtensions(b extension.Bundle) {
 var p project.Project
 if err := json.Unmarshal([]byte(` + strconv.Quote(manifest) + `), &p); err != nil {panic(err)}
 for _, interpreted := range []bool{false,true} {
  value, err := vm.RunPreparationFixture(&p,b,interpreted)
  if err != nil {panic(err)}
  fmt.Println(value)
 }
}
`
	sdk := writePreparationSDK(t, files)
	sources := []extensionSource{{ID: "fresh", Go: []byte(`package fresh
import ("gowarp/core/extension"; "gowarp/core/project")
func New() extension.Extension { return extension.Extension{ID:"fresh", Prepare:func(p *project.Project) error {
 p.Targets[0].Blocks["set"].Inputs["VALUE"] = []any{1,[]any{4,"41"}}
 return nil
} } }
`)}}
	var p project.Project
	if err := json.Unmarshal([]byte(manifest), &p); err != nil {
		t.Fatal(err)
	}
	tpl := Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}
	built, err := buildExtensionRuntime(context.Background(), tpl, sources, sdk, t.TempDir(), &p)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(built.Path).CombinedOutput()
	if err != nil || string(output) != "42\n42\n" {
		t.Fatalf("native/interpreted prepared execution=%q err=%v", output, err)
	}
}

func TestExtensionPreparationCanRewriteUnsupportedSourceOpcode(t *testing.T) {
	sdk := writePreparationSDK(t, extensionSDKFixture(t))
	tpl := Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}
	p := &project.Project{
		Targets:    []*project.Target{{IsStage: true, Blocks: project.Blocks{"rewrite": {Opcode: "fresh_source"}}}},
		Extensions: []string{"fresh"},
	}
	sources := []extensionSource{{ID: "fresh", Go: []byte(`package fresh
import "gowarp/core/extension"
func New() extension.Extension {
 return extension.Extension{ID:"fresh", Prepare:func(p *extension.Project) error {
  p.Targets[0].Blocks["rewrite"].Opcode = "operator_add"
  return nil
 }}
}`)}}
	built, err := buildExtensionRuntime(context.Background(), tpl, sources, sdk, t.TempDir(), p)
	if err != nil {
		t.Fatalf("source opcode was checked before preparation: %v", err)
	}
	if err := prepareExtensionProject(context.Background(), built.validatorPath, p, ""); err != nil {
		t.Fatalf("cached helper rejected prepared opcode: %v", err)
	}
	if err := validateExtensionProject(context.Background(), built.validatorPath, p); err == nil || !strings.Contains(err.Error(), "fresh_source") {
		t.Fatalf("registration-only mode failed to check original opcode: %v", err)
	}
	if got := p.Targets[0].Blocks["rewrite"].Opcode; got != "fresh_source" {
		t.Fatalf("preparation changed original opcode: %s", got)
	}
}
