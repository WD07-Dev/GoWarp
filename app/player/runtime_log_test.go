package player

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"testing"
	"time"
)

func TestRuntimeLogCapturesProfileWithoutSlowFrames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "my-game.cpu.pprof")
	if err := os.WriteFile(path, []byte("previous profile"), 0600); err != nil {
		t.Fatal(err)
	}
	diagnostic, err := startRuntimeLog(dir, "my-game")
	if err != nil {
		t.Fatal(err)
	}
	diagnostic.record("ordinary frame")
	diagnostic.finish(0)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("CPU profile was not saved on early exit: %v", err)
	}
	defer reader.Close()
	profile, err := io.ReadAll(reader)
	if err != nil || len(profile) == 0 {
		t.Fatalf("incomplete CPU profile: bytes=%d error=%v", len(profile), err)
	}
	logData, err := os.ReadFile(filepath.Join(dir, "my-game.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(logData), "cpu_profile start") != 1 || strings.Count(string(logData), "cpu_profile saved") != 1 {
		t.Fatalf("profiling was repeated or not finalized: %s", logData)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 3 {
		t.Fatalf("expected one log and two replaced profiles: %v, %v", files, err)
	}
}

func TestRuntimeLogLeavesExistingProfilerRunning(t *testing.T) {
	var existing bytes.Buffer
	if err := pprof.StartCPUProfile(&existing); err != nil {
		t.Fatal(err)
	}
	defer pprof.StopCPUProfile()
	diagnostic, err := startRuntimeLog(t.TempDir(), "my-game")
	if err != nil {
		t.Fatal(err)
	}
	diagnostic.record("ordinary frame")
	diagnostic.finish(0)
	var another bytes.Buffer
	if err := pprof.StartCPUProfile(&another); err == nil {
		t.Fatal("diagnostics stopped a profiler that it did not own")
	}
}

func TestRuntimeLogRecordsNormalExit(t *testing.T) {
	dir := t.TempDir()
	log, err := startRuntimeLog(dir, "my-game")
	if err != nil {
		t.Fatal(err)
	}
	log.record("slow update test")
	log.finish(0)
	log.finish(0)
	if log.file.Name() != filepath.Join(dir, "my-game.log") {
		t.Fatalf("log name does not use app ID: %s", log.file.Name())
	}
	data, err := os.ReadFile(log.file.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"start", "gomaxprocs=", "heap_objects_bytes=", "heap_allocated_bytes=", "gc_cpu_seconds=", "gc_assist_cpu_seconds=", "slow update test", "exit code=0"} {
		if !strings.Contains(string(data), marker) {
			t.Errorf("missing diagnostic %q: %s", marker, data)
		}
	}
}

func TestRuntimeLogDoesNotBlockWhenWriterIsBusy(t *testing.T) {
	log := &runtimeLog{messages: make(chan string, 2), done: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			log.record("busy")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("diagnostics blocked the game while the writer was busy")
	}
}

func TestRuntimeLogReplacesPreviousRun(t *testing.T) {
	dir := t.TempDir()
	for _, marker := range []string{"previous run", "current run"} {
		log, err := startRuntimeLog(dir, "my-game")
		if err != nil {
			t.Fatal(err)
		}
		log.record(marker)
		log.finish(0)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 3 {
		t.Fatalf("expected one log and two profiles after two runs: %v, %v", files, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "my-game.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "previous run") || !strings.Contains(string(data), "current run") {
		t.Fatalf("log was not replaced: %s", data)
	}
}

func TestRuntimeLogCapturesBackgroundPanic(t *testing.T) {
	if dir := os.Getenv("GOWARP_TEST_CRASH_LOG"); dir != "" {
		if _, err := startRuntimeLog(dir, "crashing-game"); err != nil {
			panic(err)
		}
		go func() { panic("runtime diagnostic test panic") }()
		select {}
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRuntimeLogCapturesBackgroundPanic$")
	cmd.Env = append(os.Environ(), "GOWARP_TEST_CRASH_LOG="+dir)
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("crashing child exited successfully: %s", output)
	}
	files, err := filepath.Glob(filepath.Join(dir, "crashing-game.log"))
	if err != nil || len(files) != 1 {
		t.Fatalf("crash logs: %v, %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "panic: runtime diagnostic test panic") || !strings.Contains(string(data), "goroutine") {
		t.Fatalf("unhandled worker panic was lost: %s", data)
	}
	if strings.Contains(string(data), "exit code=0") {
		t.Fatal("crash was marked as a normal exit")
	}
}

func TestRuntimeDiagnosticsRequiresArgument(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("LOCALAPPDATA", cache)
	for _, args := range [][]string{nil, {"project.sb3"}, {"--debug=false"}} {
		diagnostic, err := startRuntimeDiagnostics(args, "my-game")
		if err != nil {
			t.Fatal(err)
		}
		if diagnostic != nil {
			diagnostic.finish(0)
			t.Fatalf("diagnostics enabled without --debug: %v", args)
		}
	}
	if _, err := os.Stat(filepath.Join(cache, "gowarp")); !os.IsNotExist(err) {
		t.Fatalf("normal launch created diagnostics directory: %v", err)
	}
	diagnostic, err := startRuntimeDiagnostics([]string{"--debug"}, "my-game")
	if err != nil || diagnostic == nil {
		t.Fatalf("diagnostic launch: %v", err)
	}
	diagnostic.finish(0)
	for _, suffix := range []string{".log", ".cpu.pprof", ".allocs.pprof"} {
		path := filepath.Join(cache, "gowarp", "logs", "my-game"+suffix)
		before, err := os.ReadFile(path)
		if err != nil || len(before) == 0 {
			t.Fatalf("missing diagnostic file %s: %v", suffix, err)
		}
		diagnostic, err := startRuntimeDiagnostics(nil, "my-game")
		if err != nil || diagnostic != nil {
			t.Fatalf("normal launch started diagnostics: %v", err)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("normal launch changed existing %s: %v", suffix, err)
		}
	}
}

func TestRuntimeLogSavesAllocationProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "my-game.allocs.pprof")
	if err := os.WriteFile(path, []byte("previous run"), 0600); err != nil {
		t.Fatal(err)
	}
	diagnostic, err := startRuntimeLog(dir, "my-game")
	if err != nil {
		t.Fatal(err)
	}
	diagnostic.finish(0)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("allocation profile was not replaced: %v", err)
	}
	defer reader.Close()
	profile, err := io.ReadAll(reader)
	if err != nil || len(profile) == 0 {
		t.Fatalf("incomplete allocation profile: bytes=%d error=%v", len(profile), err)
	}
}
