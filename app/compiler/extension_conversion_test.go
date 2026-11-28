package compiler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gowarp/core/project"
)

func TestConvertExtensionsPreservesOriginalAndManifest(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "original.sb3")
	output := filepath.Join(dir, "converted.sb3")
	manifest := `{
		"targets": [{
			"isStage": true,
			"name": "Stage",
			"blocks": {"id": {"opcode": "fresh_value", "topLevel": true}},
			"comments": {"abc": {"text": "keep this"}}
		}],
		"extensions": ["pen", "fresh"],
		"extensionURLs": {"fresh": "https://example.invalid/original.js"},
		"extensionStorage": {"fresh": {"state": [1, "x"]}},
		"unknownFutureField": {"opaque": true}
	}`
	writeExtensionArchive(t, input, map[string]string{"project.json": manifest, "assets/keep.bin": "binary\x00asset"})
	before, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if err = ConvertExtensions(context.Background(), input, output, map[string][]byte{"fresh": []byte(testExtensionJS)}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("original archive changed")
	}
	converted, err := project.Load(output)
	if err != nil {
		t.Fatal(err)
	}
	if converted.ExtensionURLs["fresh"] != "data:application/javascript;base64,"+base64.StdEncoding.EncodeToString([]byte(testExtensionJS)) {
		t.Fatal("annotated JS not embedded verbatim")
	}
	oldZip, err := zip.OpenReader(input)
	if err != nil {
		t.Fatal(err)
	}
	defer oldZip.Close()
	newZip, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer newZip.Close()
	var oldManifest, newManifest map[string]any
	if err = json.Unmarshal([]byte(manifest), &oldManifest); err != nil {
		t.Fatal(err)
	}
	for _, entry := range newZip.File {
		if entry.Name == "project.json" {
			r, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(data, &newManifest); err != nil {
				t.Fatal(err)
			}
			continue
		}
		var original *zip.File
		for _, old := range oldZip.File {
			if old.Name == entry.Name {
				original = old
			}
		}
		if original == nil {
			t.Fatalf("unexpected entry %s", entry.Name)
		}
		a, err := original.OpenRaw()
		if err != nil {
			t.Fatal(err)
		}
		b, err := entry.OpenRaw()
		if err != nil {
			t.Fatal(err)
		}
		oldData, err := io.ReadAll(a)
		if err != nil {
			t.Fatal(err)
		}
		newData, err := io.ReadAll(b)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(oldData, newData) {
			t.Fatalf("compressed bytes changed for asset %s", entry.Name)
		}
		if original.CRC32 != entry.CRC32 || original.Method != entry.Method {
			t.Fatalf("ZIP metadata changed for asset %s: CRC32=%x, method=%d; want CRC32=%x, method=%d", entry.Name, entry.CRC32, entry.Method, original.CRC32, original.Method)
		}
	}
	delete(oldManifest, "extensionURLs")
	delete(newManifest, "extensionURLs")
	if !reflect.DeepEqual(oldManifest, newManifest) {
		t.Fatal("unrelated manifest fields changed")
	}
}

func TestConvertExtensionsFailurePreservesOutputs(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "original.sb3")
	output := filepath.Join(dir, "converted.sb3")
	writeExtensionArchive(t, input, map[string]string{"project.json": `{"targets":[{"isStage":true}],"extensions":["fresh"]}`})
	if err := os.WriteFile(output, []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, sources := range []map[string][]byte{nil, {"fresh": []byte("void 0;")}, {"fresh": []byte(testExtensionJS), "extra": []byte(testExtensionJS)}} {
		if err := ConvertExtensions(context.Background(), input, output, sources); err == nil {
			t.Fatal("accepted missing/invalid/unregistered source")
		}
		data, _ := os.ReadFile(output)
		if string(data) != "previous" {
			t.Fatal("failed conversion altered output")
		}
	}
	if err := ConvertExtensions(context.Background(), input, input, map[string][]byte{"fresh": []byte(testExtensionJS)}); err == nil {
		t.Fatal("accepted original as output")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ConvertExtensions(ctx, input, output, map[string][]byte{"fresh": []byte(testExtensionJS)}); err != context.Canceled {
		t.Fatalf("cancel=%v", err)
	}
}

func TestConvertExtensionsRejectsNullManifest(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "null.sb3")
	writeExtensionArchive(t, input, map[string]string{"project.json": "null"})
	if err := ConvertExtensions(context.Background(), input, filepath.Join(dir, "output.sb3"), nil); err == nil {
		t.Fatal("accepted null manifest")
	}
}

func TestConvertExtensionsAllowsManifestLargerThanJSSourceLimit(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "large.sb3")
	output := filepath.Join(dir, "converted.sb3")
	manifest := `{"targets":[{"isStage":true}],"extensions":["fresh"],"editorData":"` + string(bytes.Repeat([]byte("x"), extensionSourceLimit)) + `"}`
	writeExtensionArchive(t, input, map[string]string{"project.json": manifest})
	if err := ConvertExtensions(context.Background(), input, output, map[string][]byte{"fresh": []byte(testExtensionJS)}); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Load(output); err != nil {
		t.Fatal(err)
	}
}

func TestConvertExtensionsDoesNotRequireBuiltinSources(t *testing.T) {
	input := filepath.Join(t.TempDir(), "builtin.sb3")
	output := filepath.Join(t.TempDir(), "converted.sb3")
	writeExtensionArchive(t, input, map[string]string{"project.json": `{"targets":[{"isStage":true}],"extensions":["pen","music"]}`})
	if err := ConvertExtensions(context.Background(), input, output, nil); err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ExtensionURLs) != 0 {
		t.Fatal("builtin source embedded into project")
	}
}
