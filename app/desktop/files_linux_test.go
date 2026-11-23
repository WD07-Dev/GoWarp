package desktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Exercise the real dialog adapter and file reading without opening a modal UI.
func TestFilePickerReadsSelectedFile(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "zenity")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf '%s\\n' \"$FILES_TEST_PATH\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	for _, tc := range []struct{ name, encoding, contents, want string }{
		{"image.png", "url", "\x01\x02\x03", "data:image/png;base64,AQID"},
		{"unknown.gowarpunknown", "url", "abc", "data:application/octet-stream;base64,YWJj"},
		{"text.txt", "text", "\xef\xbb\xbfhello 한글", "hello 한글"},
	} {
		path := filepath.Join(dir, tc.name)
		if err := os.WriteFile(path, []byte(tc.contents), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("FILES_TEST_PATH", path)
		got, err := pickFile(context.Background(), tc.encoding)
		if err != nil || got != tc.want {
			t.Fatalf("%s: %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := pickFile(context.Background(), "url"); err == nil || got != "" {
		t.Fatalf("cancel: %q, %v", got, err)
	}
}
