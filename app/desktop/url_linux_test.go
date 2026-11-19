package desktop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOpenURLDoesNotBlockAndReapsLauncher(t *testing.T) {
	dir := t.TempDir()
	launched := filepath.Join(dir, "launched")
	release := filepath.Join(dir, "release")
	t.Setenv("PATH", dir)
	t.Setenv("URL_TEST_LAUNCHED", launched)
	t.Setenv("URL_TEST_RELEASE", release)
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0600) })
	script := "#!/bin/sh\nprintf '%s\\n' \"$$\" \"$1\" > \"$URL_TEST_LAUNCHED\"\nwhile [ ! -e \"$URL_TEST_RELEASE\" ]; do /bin/sleep 0.01; done\nexit 7\n"
	if err := os.WriteFile(filepath.Join(dir, "xdg-open"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	g := &game{}
	const url = "https://example.com/?a=1&b=two%20words"
	returned := make(chan error, 1)
	go func() { returned <- g.openURL(url) }()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("opening a URL blocked on the launcher")
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-g.urlErrors:
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 7 {
			t.Fatalf("launcher error: %v", err)
		}
		g.urlErrors <- err
	case <-time.After(3 * time.Second):
		t.Fatal("launcher error was lost")
	}
	if err := g.applyHostCommands(); err == nil {
		t.Fatal("update ignored URL error")
	}
	data, err := os.ReadFile(launched)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 || lines[1] != url {
		t.Fatalf("URL arguments: %q", data)
	}
	pid, err := strconv.Atoi(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	var status syscall.WaitStatus
	if _, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil); !errors.Is(err, syscall.ECHILD) {
		t.Fatalf("launcher was not reaped: %v", err)
	}
}

func TestOpenURLRejectsNUL(t *testing.T) {
	g := &game{}
	if err := g.openURL("https://example.com/\x00"); err == nil {
		t.Fatal("accepted a NUL in the native URL argument")
	}
}
