package packager

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPreviewProcessCleanup(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX fixture")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "game")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	preview, err := startProcess(context.Background(), path, dir)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-preview.Done:
		if err == nil {
			t.Fatal("lost failure")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("process not reaped")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("preview directory retained")
	}
}

func TestPreviewStopAllowsCleanup(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX fixture")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "game")
	marker := filepath.Join(t.TempDir(), "flushed")
	script := "#!/bin/sh\nwhile [ ! -f \"$GOWARP_STOP_FILE\" ]; do sleep 0.05; done\nprintf done > \"$GOWARP_TEST_MARKER\"\n"
	t.Setenv("GOWARP_TEST_MARKER", marker)
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	p, err := startProcess(context.Background(), path, dir)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	p.cancel()
	select {
	case <-p.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop timed out")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("stop killed process before cleanup")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("preview directory retained")
	}
}
