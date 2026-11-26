package compiler

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gowarp/app/config"
	"gowarp/core/extension"
)

func TestExtensionExternalDependencyBuild(t *testing.T) {
	proxy := t.TempDir()
	module := "example.com/gowarp-external-fixture"
	helper := "example.com/gowarp-helper-fixture"
	writeProxyModule(t, proxy, helper, "v1.0.0", "module "+helper+"\n\ngo 1.25.0\n",
		"package library\nconst Value = \"external\"")
	for _, version := range []string{"v1.0.0", "v1.1.0"} {
		writeProxyModule(
			t, proxy, module, version,
			"module "+module+"\n\ngo 1.25.0\nrequire "+helper+" v1.0.0\n",
			"package library\nimport helper \""+helper+"\"\nconst Value = helper.Value + \"-"+version+"\"",
		)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(proxy)))
	defer server.Close()
	t.Setenv("GOPROXY", server.URL)
	t.Setenv("GOSUMDB", "off")
	goCache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", goCache)
	t.Setenv("LocalAppData", goCache)
	t.Cleanup(func() {
		err := filepath.WalkDir(filepath.Join(goCache, "gowarp", "go", "pkg", "mod"), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return os.Chmod(path, 0700)
			}
			return os.Chmod(path, 0600)
		})
		if err != nil {
			t.Error(err)
		}
	})

	sdk := t.TempDir()
	files := extensionSDKFixture(t)
	for name, data := range files {
		writeDependencyFile(t, filepath.Join(sdk, name), []byte(data))
	}
	source := func(version string) []extensionSource {
		js := fmt.Sprintf(`/*
@gowarp-config {
require %s %s
}
@gowarp-go {
package extension
import (
	e "gowarp/core/extension"
	lib "%s"
)
func New() e.Extension { return e.Extension{ID: lib.Value} }
}
*/`, module, version, module)
		code, err := extension.ExtractSource("external.js", []byte(js))
		if err != nil {
			t.Fatal(err)
		}
		return []extensionSource{{ID: "external", Go: code.Go, Requirements: code.Requirements}}
	}
	tpl := Template{Version: config.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}
	cache := t.TempDir()
	var last Template
	for _, version := range []string{"v1.0.0", "v1.1.0"} {
		built, err := buildExtensionRuntime(context.Background(), tpl, source(version), sdk, cache, nil)
		if err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(built.Path).CombinedOutput()
		if err != nil || string(output) != "external-"+version+"\n" {
			t.Fatalf("player output=%q, err=%v", output, err)
		}
		if last.Path == built.Path {
			t.Fatal("changing dependency version reused the old executable")
		}
		last = built
	}
	original, err := os.ReadFile(filepath.Join(sdk, "go.mod"))
	if err != nil || string(original) != files["go.mod"] {
		t.Fatalf("modified original SDK go.mod: %s (%v)", original, err)
	}
	env, err := extensionBuildEnv(tpl)
	if err != nil {
		t.Fatal(err)
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	nested := module + "/nested"
	writeProxyModule(t, proxy, nested, "v1.0.0", "module "+nested+"\n\ngo 1.25.0\n", "package library\nconst Value = \"nested\"")
	work := t.TempDir()
	writeDependencyFile(t, filepath.Join(work, "go.mod"), []byte("module gowarp\n\ngo 1.25.0\n"))
	writeDependencyFile(t, filepath.Join(work, "main.go"), []byte("package main\nimport _ \""+nested+"\"\nfunc main() {}"))
	err = prepareExtensionModules(context.Background(), goTool, work, env, []extension.ModuleRequirement{{Path: module, Version: "v1.0.0"}})
	if err == nil || !strings.Contains(err.Error(), "own require entry") {
		t.Fatalf("undeclared nested module was not rejected: %v", err)
	}
	legacy := "example.com/gowarp-legacy-fixture"
	writeProxyModule(t, proxy, legacy, "v1.0.0", "module "+legacy+"\n\ngo 1.25.0\n", "package library\nimport helper \""+helper+"\"\nconst Value = helper.Value")
	work = t.TempDir()
	writeDependencyFile(t, filepath.Join(work, "go.mod"), []byte("module gowarp\n\ngo 1.25.0\n"))
	writeDependencyFile(t, filepath.Join(work, "main.go"), []byte("package main\nimport _ \""+legacy+"\"\nfunc main() {}"))
	requirements := []extension.ModuleRequirement{{Path: legacy, Version: "v1.0.0"}}
	err = prepareExtensionModules(context.Background(), goTool, work, env, requirements)
	if err == nil || !strings.Contains(err.Error(), "not pinned") {
		t.Fatalf("accepted an unversioned transitive dependency: %v", err)
	}
	writeDependencyFile(t, filepath.Join(work, "go.mod"), []byte("module gowarp\n\ngo 1.25.0\n"))
	requirements = append(requirements, extension.ModuleRequirement{Path: helper, Version: "v1.0.0"})
	if err := prepareExtensionModules(context.Background(), goTool, work, env, requirements); err != nil {
		t.Fatalf("explicit transitive dependency: %v", err)
	}

	writeProxyModule(
		t, proxy, module, "v1.2.0",
		"module "+module+"\n\ngo 1.25.0\nrequire "+helper+" v1.1.0\n",
		"package library\nimport helper \""+helper+"\"\nconst Value = helper.Value",
	)
	writeProxyModule(
		t, proxy, helper, "v1.1.0", "module "+helper+"\n\ngo 1.25.0\n",
		"package library\nconst Value = \"changed\"",
	)
	for _, version := range []string{"v1.0.0", "v1.2.0"} {
		work := t.TempDir()
		writeDependencyFile(t, filepath.Join(work, "go.mod"), []byte("module gowarp\n\ngo 1.25.0\nrequire "+helper+" v1.0.0\n"))
		writeDependencyFile(t, filepath.Join(work, "main.go"), []byte("package main\nimport _ \""+module+"\"\nfunc main() {}"))
		requirements := []extension.ModuleRequirement{{Path: module, Version: version}}
		if version == "v1.0.0" {
			requirements = []extension.ModuleRequirement{{Path: helper, Version: "v1.1.0"}}
		}
		err := prepareExtensionModules(context.Background(), goTool, work, env, requirements)
		if err == nil || !strings.Contains(err.Error(), helper) {
			t.Fatalf("dependency changed a pinned runtime module: %v", err)
		}
	}
	server.Close()
	again, err := buildExtensionRuntime(context.Background(), tpl, source("v1.1.0"), sdk, cache, nil)
	if err != nil || again.Path != last.Path {
		t.Fatalf("offline cache reuse: %s, %v", again.Path, err)
	}
	if _, err := buildExtensionRuntime(context.Background(), tpl, source("v1.3.0"), sdk, cache, nil); err == nil || !strings.Contains(err.Error(), "dependencies") {
		t.Fatalf("missing module should report a dependency error: %v", err)
	}
	entries, err := os.ReadDir(cache)
	if err != nil || len(entries) != 4 {
		t.Fatalf("failed build published a cache entry: %v, %v", entries, err)
	}
}

func TestExtensionDependencyConflicts(t *testing.T) {
	sources := []extensionSource{
		{ID: "first", Requirements: []extension.ModuleRequirement{{Path: "example.com/library", Version: "v1.0.0"}}},
		{ID: "second", Requirements: []extension.ModuleRequirement{{Path: "example.com/library", Version: "v1.1.0"}}},
	}
	_, err := collectExtensionModules(sources)
	if err == nil || !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "second") {
		t.Fatalf("conflict should identify both extensions: %v", err)
	}
}

func writeDependencyFile(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func writeProxyModule(t *testing.T, root, module, version, mod, source string) {
	t.Helper()
	dir := filepath.Join(root, module, "@v")
	writeDependencyFile(t, filepath.Join(dir, "list"), []byte(version+"\n"))
	writeDependencyFile(t, filepath.Join(dir, version+".mod"), []byte(mod))
	writeDependencyFile(t, filepath.Join(dir, version+".info"), fmt.Appendf(nil, "{\"Version\":%q,\"Time\":\"2026-01-01T00:00:00Z\"}", version))
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for name, text := range map[string]string{"go.mod": mod, "library.go": source} {
		w, err := archive.Create(module + "@" + version + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	writeDependencyFile(t, filepath.Join(dir, version+".zip"), data.Bytes())
}
