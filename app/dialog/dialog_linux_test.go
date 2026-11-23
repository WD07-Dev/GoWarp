package dialog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSelectionPreservesOptionsAndErrors(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "zenity")
	argsPath := filepath.Join(dir, "args")
	t.Setenv("PATH", dir)
	t.Setenv("DIALOG_TEST_ARGS", argsPath)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$DIALOG_TEST_ARGS\"\nprintf '/tmp/a project.sb3\\n'\n"
	if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	options := Options{Title: "Open", Directory: dir, Filename: "a project.sb3", Patterns: []string{"*.sb3"}}
	for _, save := range []bool{false, true} {
		options.Save = save
		path, err := Select(context.Background(), options)
		if err != nil || path != "/tmp/a project.sb3" {
			t.Fatalf("save=%t: %q %v", save, path, err)
		}
		data, err := os.ReadFile(argsPath)
		if err != nil {
			t.Fatal(err)
		}
		args := string(data)
		if !strings.Contains(args, options.Title) || !strings.Contains(args, filepath.Join(dir, options.Filename)) || !strings.Contains(args, "*.sb3") || strings.Contains(args, "--save\n") != save {
			t.Fatalf("dialog options lost: %q", args)
		}
	}
	for _, output := range []string{"exit 1", "printf ''"} {
		if err := os.WriteFile(helper, []byte("#!/bin/sh\n"+output+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := Select(context.Background(), Options{}); !errors.Is(err, ErrCancelled) {
			t.Fatalf("cancel=%v", err)
		}
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Select(context.Background(), Options{}); err == nil || errors.Is(err, ErrCancelled) || errors.Is(err, ErrUnavailable) {
		t.Fatalf("failure=%v", err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexec /bin/sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	deadline, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	if _, err := Select(deadline, Options{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("active dialog cancellation=%v", err)
	}
	if err := os.Remove(helper); err != nil {
		t.Fatal(err)
	}
	if _, err := Select(context.Background(), Options{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing helper=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Select(ctx, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("context=%v", err)
	}
}
