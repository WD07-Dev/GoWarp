package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"gowarp/app/compiler"
	"gowarp/app/desktop"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func outputSourceFixture(t *testing.T) (string, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	files := map[string][]byte{
		"project.json": []byte(`{"targets":[{"isStage":true,"name":"Stage","costumes":[{"md5ext":"shape.svg","dataFormat":"svg"}],"sounds":[{"md5ext":"tone.wav","dataFormat":"wav"}]}]}`),
		"shape.svg":    []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"><rect width="1" height="1" fill="red"/></svg>`),
		"tone.wav":     []byte("original sound bytes"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, files
}

func sourceAlias(t *testing.T, path, kind string) string {
	t.Helper()
	if kind == "direct" {
		return path
	}
	alias := filepath.Join(t.TempDir(), "output")
	var err error
	if kind == "symlink" {
		err = os.Symlink(path, alias)
	} else {
		err = os.Link(path, alias)
	}
	if err != nil {
		t.Fatal(err)
	}
	return alias
}

func assertOutputSourcesUnchanged(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("source %s was modified (%v)", name, err)
		}
	}
}

func TestDumpProtectsDirectorySourcesBeforeSideEffects(t *testing.T) {
	for _, inputKind := range []string{"directory", "manifest"} {
		for _, name := range []string{"project.json", "shape.svg", "tone.wav"} {
			for _, aliasKind := range []string{"direct", "symlink", "hardlink"} {
				t.Run(inputKind+"/"+name+"/"+aliasKind, func(t *testing.T) {
					dir, files := outputSourceFixture(t)
					input := dir
					if inputKind == "manifest" {
						input = filepath.Join(dir, "project.json")
					}
					output := sourceAlias(t, filepath.Join(dir, name), aliasKind)
					uart := filepath.Join(t.TempDir(), "must-not-create")
					var out, errs bytes.Buffer
					if code := run(context.Background(), []string{input, "--headless", "0", "--dump", output, "--uart", uart}, &out, &errs); code != 1 {
						t.Errorf("accepted source output: exit%d %s", code, errs.String())
					}
					if _, err := os.Stat(uart); !os.IsNotExist(err) {
						t.Error("runtime side effect occurred before output validation")
					}
					assertOutputSourcesUnchanged(t, dir, files)
				})
			}
		}
	}
}

func TestScreenshotProtectsSourcesBeforeNativeRunner(t *testing.T) {
	old := nativeRunner
	defer func() {
		nativeRunner = old
	}()
	called := false
	nativeRunner = func(_ context.Context, _ string, p *project.Project, _ desktop.Options, _ []vm.InputEvent) (*vm.Runtime, error) {
		called = true
		return vm.New(p), nil
	}
	for _, name := range []string{"project.json", "shape.svg", "tone.wav"} {
		for _, aliasKind := range []string{"direct", "symlink", "hardlink"} {
			t.Run(name+"/"+aliasKind, func(t *testing.T) {
				dir, files := outputSourceFixture(t)
				output := sourceAlias(t, filepath.Join(dir, name), aliasKind)
				called = false
				var out, errs bytes.Buffer
				if code := run(context.Background(), []string{dir, "--window", "--frames", "1", "--screenshot", output}, &out, &errs); code != 1 {
					t.Errorf("accepted source screenshot: exit%d %s", code, errs.String())
				}
				if called {
					t.Error("native runner started before output validation")
				}
				assertOutputSourcesUnchanged(t, dir, files)
			})
		}
	}
}

func TestWriteDumpDirectProtectsSourceAliases(t *testing.T) {
	for _, name := range []string{"project.json", "shape.svg", "tone.wav"} {
		for _, aliasKind := range []string{"direct", "symlink", "hardlink"} {
			t.Run(name+"/"+aliasKind, func(t *testing.T) {
				dir, files := outputSourceFixture(t)
				p, err := project.Load(dir)
				if err != nil {
					t.Fatal(err)
				}
				if err := writeDump(vm.New(p), sourceAlias(t, filepath.Join(dir, name), aliasKind), dir); err == nil {
					t.Error("direct dump accepted a source file")
				}
				assertOutputSourcesUnchanged(t, dir, files)
			})
		}
	}
}

func TestDumpProtectsArchiveAliases(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			input := sourceOptionsProject(t, `{"targets":[{"isStage":true,"name":"Stage"}]}`)
			before, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			var out, errs bytes.Buffer
			if code := run(context.Background(), []string{input, "--headless", "0", "--dump", sourceAlias(t, input, kind)}, &out, &errs); code != 1 {
				t.Errorf("accepted archive alias: exit%d", code)
			}
			after, err := os.ReadFile(input)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("archive was modified")
			}
		})
	}
}

func TestOutputValidationProtectsExecutableAndAllowsNewFiles(t *testing.T) {
	dir, _ := outputSourceFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.ValidateOutputs(dir, nil, executable); err == nil {
		t.Fatal("accepted running executable as output")
	}
	if err := compiler.ValidateOutputs(dir, nil, sourceAlias(t, executable, "symlink")); err == nil {
		t.Fatal("accepted executable alias as output")
	}
	if err := compiler.ValidateOutputs(dir, nil, filepath.Join(dir, "new", "state.txt"), filepath.Join(dir, "screen.png")); err != nil {
		t.Fatalf("rejected unrelated outputs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new")); !os.IsNotExist(err) {
		t.Fatal("validation created output parent")
	}
}
