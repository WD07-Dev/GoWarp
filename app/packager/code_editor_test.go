package packager

import "testing"

func TestCodeEditorUnicodeSelectionAndHistory(t *testing.T) {
	const original = "first\n한글 last"
	const edited = "first\nGo\n\tcode last"
	e := newCodeEditor(original)
	e.move(e.lines[1], false)
	e.move(e.cursor+2, true)
	if got := e.selected(); got != "한글" {
		t.Fatalf("selection = %q, want %q", got, "한글")
	}

	e.insert("Go\r\n\tcode")
	if e.text != edited {
		t.Fatalf("after insert = %q, want %q", e.text, edited)
	}

	e.undoEdit(false)
	if e.text != original {
		t.Fatalf("after undo = %q, want %q", e.text, original)
	}
	if got := e.selected(); got != "한글" {
		t.Fatalf("selection after undo = %q, want %q", got, "한글")
	}

	e.undoEdit(true)
	if e.text != edited {
		t.Fatalf("after redo = %q, want %q", e.text, edited)
	}

	e.move(len([]rune(e.text)), false)
	e.erase(true)
	if want := "first\nGo\n\tcode las"; e.text != want {
		t.Fatalf("after backspace = %q, want %q", e.text, want)
	}
}

func TestCodeEditorNewlineIndentAndVerticalMovement(t *testing.T) {
	const original = "\tfunc call() {\n\t\tvalue\n}"
	e := newCodeEditor(original)
	e.move(e.lineEnd(0), false)
	e.newline()
	if want := "\tfunc call() {\n\t\n\t\tvalue\n}"; e.text != want {
		t.Fatalf("indented newline = %q, want %q", e.text, want)
	}

	e.undoEdit(false)
	const column = 5
	e.move(e.lines[1]+column, false)
	e.vertical(-1, false)
	if e.cursor != column {
		t.Fatalf("cursor after moving up = %d, want column %d on first line", e.cursor, column)
	}

	e.move(0, false)
	e.erase(true)
	if e.text != original {
		t.Fatalf("backspace at start changed source to %q", e.text)
	}
}

func TestCodeEditorCRLFBackspace(t *testing.T) {
	e := newCodeEditor("// comment\r\ncall();")
	e.move(e.lines[1], false)
	e.erase(true)
	if e.text != "// commentcall();" {
		t.Fatalf("unexpected hidden line break: %q", e.text)
	}
	e.undoEdit(false)
	if e.text != "// comment\ncall();" {
		t.Fatalf("after undo = %q, want normalized LF line break", e.text)
	}
}
