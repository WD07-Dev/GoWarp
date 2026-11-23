package packager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gowarp/app/compiler"
	"gowarp/app/ui"
	"gowarp/core/extension"
)

func TestExtensionEditorHasOwnScreen(t *testing.T) {
	a := renderedApplication(t)
	a.extensions = newExtensionEditor(&compiler.ExtensionDocument{Sources: []compiler.EditableExtension{{ID: "sample", Source: "a();"}}})
	a.render()
	if got := a.pixels.RGBAAt(2, 100); got != ui.Background {
		t.Fatalf("editor still dims the settings behind a modal: %v", got)
	}
	a.form = nil
	a.render()
}

func TestImportExtensionFileReplacesDraftAndCanUndo(t *testing.T) {
	a := renderedApplication(t)
	a.ctx = context.Background()
	a.results = make(chan result, 1)
	a.extensions = newExtensionEditor(&compiler.ExtensionDocument{Sources: []compiler.EditableExtension{
		{ID: "sample", Source: "original();"},
		{ID: "other", Source: "other();"},
	}})
	buffer := a.extensions.current()
	buffer.insert("// draft\n")
	draft := buffer.text
	path := filepath.Join(t.TempDir(), "source.js")
	if err := os.WriteFile(path, []byte("// 한글\r\nupdated();"), 0600); err != nil {
		t.Fatal(err)
	}
	a.acceptPath("extension", path)
	select {
	case r := <-a.results:
		if r.err != nil {
			t.Fatal(r.err)
		}
		a.busy = false
		a.applyExtensionImport(r.importedExtension)
	case <-time.After(3 * time.Second):
		t.Fatal("import did not finish")
	}
	if buffer.text != "// 한글\nupdated();" {
		t.Fatal(buffer.text)
	}
	if a.extensions.document.Sources[0].Source != "original();" || a.extensions.buffers[1].text != "other();" {
		t.Fatal("import changed saved source or another extension")
	}
	buffer.undoEdit(false)
	if buffer.text != draft {
		t.Fatal("import cannot be undone")
	}
}

func TestExtensionImportFallbackBrowser(t *testing.T) {
	a := renderedApplication(t)
	a.ctx = context.Background()
	a.results = make(chan result, 1)
	a.extensions = newExtensionEditor(&compiler.ExtensionDocument{Sources: []compiler.EditableExtension{{ID: "sample", Source: "original();"}}})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty.js"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	a.browser = &browser{path: dir, name: "empty.js", mode: "extension"}
	a.render()
	found := false
	for _, h := range a.hits {
		if h.edit == &a.browser.name {
			found = true
		}
	}
	if !found {
		t.Fatal("file browser is hidden behind extension screen")
	}
	a.acceptFile()
	if a.browser != nil || !a.busy {
		t.Fatal("fallback picker did not start import")
	}
	select {
	case r := <-a.results:
		if r.err != nil {
			t.Fatal(r.err)
		}
		a.busy = false
		a.applyExtensionImport(r.importedExtension)
	case <-time.After(3 * time.Second):
		t.Fatal("import did not finish")
	}
	if a.extensions.current().text != "" {
		t.Fatal("empty file did not replace source")
	}
	a.extensions.current().undoEdit(false)
	if a.extensions.current().text != "original();" {
		t.Fatal("cannot undo empty import")
	}
}

func TestExtensionImportDoesNotReplaceNewerEdits(t *testing.T) {
	a := renderedApplication(t)
	a.extensions = newExtensionEditor(&compiler.ExtensionDocument{Sources: []compiler.EditableExtension{{ID: "sample", Source: "original();"}}})
	buffer := a.extensions.current()
	pending := &extensionImport{editor: a.extensions, buffer: buffer, revision: buffer.revision, source: "imported();"}
	buffer.insert("// newer\n")
	a.applyExtensionImport(pending)
	if buffer.text != "// newer\noriginal();" {
		t.Fatal("stale import replaced newer edit")
	}
}

func TestExtensionImportPreservesSourceCharacters(t *testing.T) {
	a := renderedApplication(t)
	a.extensions = newExtensionEditor(&compiler.ExtensionDocument{Sources: []compiler.EditableExtension{{ID: "sample", Source: "original();"}}})
	buffer := a.extensions.current()
	source := "const\fvalue = 1;\r\nlet\vother = 2;"
	a.applyExtensionImport(&extensionImport{editor: a.extensions, buffer: buffer, revision: buffer.revision, source: source})
	if buffer.text != normalizeCodeNewlines(source) {
		t.Fatalf("source changed: %q", buffer.text)
	}
	buffer.undoEdit(false)
	if buffer.text != "original();" {
		t.Fatal("cannot undo source replacement")
	}
}

func TestReadExtensionFileRejectsInvalidSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.js")
	if err := os.WriteFile(path, []byte{0xff, 0xfe}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readExtensionFile(context.Background(), path); err == nil {
		t.Fatal("accepted non-UTF-8")
	}
	if err := os.Truncate(path, (16<<20)+1); err != nil {
		t.Fatal(err)
	}
	if _, err := readExtensionFile(context.Background(), path); err == nil {
		t.Fatal("accepted oversized file")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readExtensionFile(ctx, path); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
}

func TestExtensionSaveCommittedBeforeCancellation(t *testing.T) {
	a := renderedApplication(t)
	a.ctx = context.Background()
	a.results = make(chan result, 1)
	finish := make(chan struct{})
	a.job("saving", func(context.Context) result {
		<-finish
		return result{committed: true, savedExtensions: &compiler.ExtensionDocument{}}
	})
	a.jobCancel()
	close(finish)
	select {
	case r := <-a.results:
		if r.err != nil || r.savedExtensions == nil {
			t.Fatal("successful file replacement reported as canceled")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("save did not finish")
	}
}

func TestExtensionEditorDraftsAndHeader(t *testing.T) {
	doc := &compiler.ExtensionDocument{Sources: []compiler.EditableExtension{
		{ID: "fresh", Source: "Scratch.extensions.register({});"},
		{ID: "other", Source: "other();"},
	}}
	e := newExtensionEditor(doc)
	e.addGoHeader()
	if _, err := extension.ExtractGo("fresh.js", []byte(e.current().text)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(e.current().text, doc.Sources[0].Source) {
		t.Fatal("JS lost")
	}
	if len(e.edits()) != 1 {
		t.Fatal("unedited sources marked dirty")
	}
	before := e.current().text
	e.addGoHeader()
	if e.current().text != before {
		t.Fatal("duplicated header")
	}
	e.current().undoEdit(false)
	if len(e.edits()) != 0 {
		t.Fatal("undo did not restore original")
	}
}

func TestExtensionEditorModalAndDiscard(t *testing.T) {
	a := renderedApplication(t)
	a.extensions = newExtensionEditor(&compiler.ExtensionDocument{Sources: []compiler.EditableExtension{{ID: "sample", Source: "a();"}}})
	a.extensions.current().insert("// draft\n")
	a.closeExtensionEditor()
	if a.extensions == nil || !a.extensions.confirmClose {
		t.Fatal("unsaved work discarded")
	}
	a.render()
	for _, h := range a.hits {
		if h.edit != nil {
			t.Fatal("form input remains active behind modal")
		}
	}
	a.extensions.current().undoEdit(false)
	a.closeExtensionEditor()
	if a.extensions != nil {
		t.Fatal("clean editor did not close")
	}
}

func TestExtensionEditorNativeCloseProtectsDrafts(t *testing.T) {
	a := renderedApplication(t)
	a.extensions = newExtensionEditor(&compiler.ExtensionDocument{Sources: []compiler.EditableExtension{{ID: "sample", Source: "a();\r\n"}}})
	if len(a.extensions.edits()) != 0 {
		t.Fatal("normalizing CRLF marked source dirty")
	}
	a.extensions.current().insert("// draft\n")
	if a.requestWindowClose() {
		t.Fatal("native close discarded draft")
	}
	if !a.extensions.confirmClose || !a.extensions.quitAfterClose {
		t.Fatal("no close confirmation")
	}
	a.discardExtensions()
	if !a.quit {
		t.Fatal("explicit discard did not close window")
	}
}

func TestWindowCloseWithoutExtensionDraftKeepsExistingBehavior(t *testing.T) {
	a := renderedApplication(t)
	a.busy = true
	if !a.requestWindowClose() {
		t.Fatal("extension editor changed native close behavior for unrelated jobs")
	}
}
