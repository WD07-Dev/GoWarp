package packager

import (
	"errors"
	"strings"
	"testing"
)

func TestPasteIntoInput(t *testing.T) {
	for _, tc := range []struct {
		name, value, paste, want string
		cursor, wantCursor       int
		selected                 bool
	}{
		{"insert Unicode", "가🙂나", "한글", "가한글🙂나", 1, 3, false},
		{"replace selection", "old", "새 값", "새 값", 3, 3, true},
		{"single line", "", "a\r\nb\tc\x00", "a b c", 0, 5, false},
		{"empty clipboard", "keep", "", "keep", 4, 4, true},
		{"length limit", strings.Repeat("가", 4095), "🙂ab", strings.Repeat("가", 4095) + "🙂", 4095, 4096, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &application{editor: &tc.value, cursor: tc.cursor, selectAll: tc.selected}
			a.paste(tc.paste)
			if tc.value != tc.want || a.cursor != tc.wantCursor {
				t.Fatalf("paste = %q at %d, want %q at %d", tc.value, a.cursor, tc.want, tc.wantCursor)
			}
		})
	}
}

func TestClipboardCompletionKeepsNewerEdits(t *testing.T) {
	value := "original"
	a := &application{editor: &value, cursor: 8, selectAll: true}
	result := clipboardResult{target: &value, original: value, cursor: 8, selected: true, action: "cut"}
	result.err = errors.New("clipboard unavailable")
	a.applyClipboard(result)
	if value != "original" {
		t.Fatal("failed copy deleted text")
	}
	result.err = nil
	value = "new edit"
	a.applyClipboard(result)
	if value != "new edit" {
		t.Fatal("late clipboard result overwrote new input")
	}
	value = "original"
	a.editor = nil
	a.applyClipboard(result)
	if value != "original" {
		t.Fatal("late clipboard result edited an unfocused field")
	}
	a.editor = &value
	a.applyClipboard(result)
	if value != "" || a.cursor != 0 || a.selectAll {
		t.Fatal("successful cut did not clear selection")
	}
}
