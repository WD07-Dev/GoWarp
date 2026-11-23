package packager

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gowarp/app/compiler"
	"gowarp/app/config"
)

type preview struct {
	Done   chan error
	cancel context.CancelFunc
}

type limitedLog struct {
	sync.Mutex
	data []byte
}

func (l *limitedLog) Write(p []byte) (int, error) {
	l.Lock()
	defer l.Unlock()
	n := len(p)
	if remaining := 16384 - len(l.data); remaining > 0 {
		l.data = append(l.data, p[:min(len(p), remaining)]...)
	}
	return n, nil
}

func startProcess(ctx context.Context, path, dir string) (*preview, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, path)
	stopPath := filepath.Join(dir, "stop")
	cmd.Env = append(os.Environ(), "GOWARP_PREVIEW=1", "GOWARP_WORKDIR="+dir, "GOWARP_STOP_FILE="+stopPath)
	cmd.Cancel = func() error {
		return os.WriteFile(stopPath, nil, 0600)
	}
	cmd.WaitDelay = 3 * time.Second
	log := &limitedLog{}
	cmd.Stderr = log
	cmd.Stdout = log
	if err := cmd.Start(); err != nil {
		cancel()
		os.RemoveAll(dir)
		return nil, err
	}
	p := &preview{Done: make(chan error, 1), cancel: cancel}
	go func() {
		err := cmd.Wait()
		if ctx.Err() != nil {
			err = nil
		}
		cancel()
		os.RemoveAll(dir)
		if err != nil {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(log.data)))
		}
		p.Done <- err
		close(p.Done)
	}()
	return p, nil
}

func startPreview(ctx, runContext context.Context, t compiler.Template, input string, s config.Settings) (*preview, error) {
	dir, err := os.MkdirTemp("", "gowarp-preview-*")
	if err != nil {
		return nil, err
	}
	name := "game"
	if t.OS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err = compiler.Export(ctx, t, input, path, s); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return startProcess(runContext, path, dir)
}
