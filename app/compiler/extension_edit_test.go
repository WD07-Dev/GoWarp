package compiler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gowarp/core/project"
)

func TestExtensionDocumentRemoteSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/good.js" {
			w.Write([]byte("remote JS"))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "remote.sb3")
	manifest, err := json.Marshal(map[string]any{
		"extensions":    []string{"good", "bad"},
		"extensionURLs": map[string]string{"good": server.URL + "/good.js", "bad": server.URL + "/bad.js"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeExtensionArchive(t, path, map[string]string{"project.json": string(manifest)})
	doc, err := OpenExtensionDocument(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Sources) != 2 {
		t.Fatalf("sources = %+v, want good and bad entries", doc.Sources)
	}
	if got := doc.Sources[0].Source; got != "remote JS" {
		t.Fatalf("downloaded source = %q, want %q", got, "remote JS")
	}
	if doc.Sources[1].Error == "" {
		t.Fatal("missing remote source has no error")
	}
}

func TestExtensionDocumentEditsInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.sb3")
	source := "(function () { /* JS without Go yet */ })();"
	url := "data:application/javascript;base64," + base64.StdEncoding.EncodeToString([]byte(source))
	manifest, err := json.Marshal(map[string]any{
		"targets":        []any{map[string]any{"isStage": true, "name": "Stage"}},
		"extensions":     []string{"pen", "music", "inline", "local", "missing"},
		"extensionURLs":  map[string]string{"inline": url, "local": "extensions/local.js"},
		"customMetadata": map[string]int{"keep": 42},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeExtensionArchive(t, path, map[string]string{
		"project.json":        string(manifest),
		"extensions/local.js": "local source",
		"asset.bin":           "asset\x00",
	})
	doc, err := OpenExtensionDocument(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Sources) != 3 {
		t.Fatalf("custom sources = %d, want 3 (excluding builtins)", len(doc.Sources))
	}
	if got := doc.Sources[0].Source; got != source {
		t.Fatalf("inline source = %q, want %q", got, source)
	}
	if got := doc.Sources[1].Source; got != "local source" {
		t.Fatalf("local source = %q, want %q", got, "local source")
	}
	if doc.Sources[2].Error == "" {
		t.Fatal("missing source has no error")
	}

	draft := "// unfinished Go draft\n" + source
	if err = doc.Save(context.Background(), map[string]string{"inline": draft}); err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := readExtensionSource(context.Background(), path, p.ExtensionURLs["inline"])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != draft {
		t.Fatalf("saved source = %q, want %q", got, draft)
	}
	if p.ExtensionURLs["local"] != "extensions/local.js" {
		t.Fatal("unedited source changed")
	}
	if err = doc.Save(context.Background(), map[string]string{"local": "new local"}); err != nil {
		t.Fatal(err)
	}
}

func TestExtensionDocumentRejectsStaleAndCanceledSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.sb3")
	writeExtensionArchive(t, path, map[string]string{"project.json": `{"extensions":["fresh"]}`})
	doc, err := OpenExtensionDocument(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = doc.Save(ctx, map[string]string{"fresh": "draft"}); err == nil {
		t.Fatal("accepted canceled save")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("canceled save changed archive")
	}
	writeExtensionArchive(t, path, map[string]string{"project.json": `{"extensions":["fresh"],"externalEdit":true}`})
	before, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = doc.Save(context.Background(), map[string]string{"fresh": "draft"}); err == nil {
		t.Fatal("overwrote external edit")
	}
	after, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("stale save changed archive")
	}
}

func TestExtensionReplacementChecksRevisionAfterStaging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.sb3")
	staged := path + ".tmp"
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	revision, err := extensionArchiveHash(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("staged edits"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("external edit during staging"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = replaceExtensionArchive(context.Background(), staged, path, &revision); err == nil {
		t.Fatal("overwrote external edit")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "external edit during staging" {
		t.Fatalf("archive = %q, want external edit preserved", data)
	}
}
