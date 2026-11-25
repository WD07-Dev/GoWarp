// Package editorbridge accepts explicitly connected TurboWarp preview requests.
package editorbridge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gowarp/app/assets"
)

const maxProjectSize = 512 << 20

type Options struct {
	Snapshot      string  `json:"snapshot,omitempty"`
	ClickStart    bool    `json:"clickStart"`
	Display       string  `json:"display"`
	RuntimeURL    string  `json:"-"`
	Name          string  `json:"name"`
	FPS           float64 `json:"fps"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	Turbo         bool    `json:"turbo"`
	Interpolation bool    `json:"interpolation"`
}

type Status struct {
	Snapshot string `json:"snapshot,omitempty"`
	Phase    string `json:"phase"`
	Error    string `json:"error,omitempty"`
	Display  string `json:"display,omitempty"`
}

type job struct {
	snapshot string
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	token    string
	stream   *relay
	display  string
}

type Server struct {
	mu             sync.Mutex
	ctx            context.Context
	cancel         context.CancelFunc
	http           *http.Server
	extensions     *http.Server
	address, token string
	launch         func(context.Context, string, Options) (<-chan error, error)
	status         Status
	active         *job
	closed         bool
	cacheDir       string
	snapshot       string
}

func Start(parent context.Context, launch func(context.Context, string, Options) (<-chan error, error)) (*Server, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	cacheDir, err := os.MkdirTemp("", "gowarp-editor-cache-*")
	if err != nil {
		listener.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Server{ctx: ctx, cancel: cancel, address: listener.Addr().String(), token: hex.EncodeToString(secret[:]), launch: launch, status: Status{Phase: "ready"}, cacheDir: cacheDir}
	s.http = &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		if err := s.http.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.mu.Lock()
			s.status = Status{Phase: "error", Error: err.Error()}
			s.mu.Unlock()
		}
	}()
	go func() { <-ctx.Done(); s.Close() }()
	return s, nil
}

func (s *Server) URL() string { return "http://" + s.address + "/#" + s.token }

func (s *Server) TurboWarpURL() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", errors.New("editor connection is closed")
	}
	if s.extensions == nil {
		listener, err := net.Listen("tcp4", "127.0.0.1:8000")
		if err != nil {
			return "", fmt.Errorf("serve Extension Manager on localhost:8000: %w", err)
		}
		s.extensions = &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
		go s.extensions.Serve(listener)
	}
	query := url.Values{"extension": {"http://localhost:8000/extension-manager.js"}}
	return "https://turbowarp.org/editor?" + query.Encode(), nil
}

func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	status.Snapshot = s.snapshot
	if s.active != nil {
		status.Display = s.active.display
	}
	return status
}

func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		s.status = Status{Phase: "stopping"}
		s.active.cancel()
	} else {
		s.status = Status{Phase: "ready"}
	}
}

func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	active := s.active
	extensions := s.extensions
	s.mu.Unlock()
	s.cancel()
	s.http.Close()
	if extensions != nil {
		extensions.Close()
	}
	if active != nil {
		<-active.done
	}
	os.RemoveAll(s.cacheDir)
}

func allowedOrigin(origin string) bool {
	if origin == "https://turbowarp.org" || origin == "null" || origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	origin := r.Header.Get("Origin")
	extensionPath := r.URL.Path == "/extension-manager.js" && (r.Method == http.MethodGet || r.Method == http.MethodOptions)
	if (r.Host != s.address && !(extensionPath && r.Host == "localhost:8000")) || !allowedOrigin(origin) {
		http.Error(w, "Untrusted editor origin or host", http.StatusForbidden)
		return
	}
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
	if extensionPath && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(assets.ExtensionManager)
		return
	}
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-GoWarp-Options")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodGet && (r.URL.Path == "/preview" || r.URL.Path == "/runtime") {
		s.stream(w, r, r.URL.Path == "/runtime")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) != 1 {
		http.Error(w, "Copy a new connection address from GoWarp", http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/status":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.Status())
	case r.Method == http.MethodPost && r.URL.Path == "/run":
		s.receive(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/stop":
		s.Stop()
		w.WriteHeader(http.StatusAccepted)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) receive(w http.ResponseWriter, r *http.Request) {
	var options Options
	raw := r.Header.Get("X-GoWarp-Options")
	decoded, decodeErr := url.PathUnescape(raw)
	if len(raw) > 4096 || decodeErr != nil || json.Unmarshal([]byte(decoded), &options) != nil || len(options.Name) > 256 || options.FPS < 0 || options.FPS > 1000 || options.Width < 1 || options.Height < 1 || options.Width > 4096 || options.Height > 4096 {
		http.Error(w, "Invalid project settings", http.StatusBadRequest)
		return
	}
	if options.Display != "" && options.Display != "native" && options.Display != "embedded" {
		http.Error(w, "Invalid display mode", http.StatusBadRequest)
		return
	}
	if options.Display == "" {
		options.Display = "native"
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		http.Error(w, "Could not create preview session", http.StatusInternalServerError)
		return
	}
	if r.ContentLength > maxProjectSize {
		http.Error(w, "Project exceeds 512 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()
		http.Error(w, "Connection closed", http.StatusServiceUnavailable)
		return
	}
	if s.active != nil {
		s.mu.Unlock()
		http.Error(w, "Stop the current preview before running again", http.StatusConflict)
		return
	}
	if options.Snapshot != "" && options.Snapshot != s.snapshot {
		s.mu.Unlock()
		http.Error(w, "Project snapshot is no longer available", http.StatusConflict)
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	j := &job{ctx: ctx, cancel: cancel, done: make(chan struct{}), token: hex.EncodeToString(secret[:]), stream: newRelay(), display: options.Display}
	options.RuntimeURL = "ws://" + s.address + "/runtime#" + j.token
	s.active = j
	s.status = Status{Phase: "receiving"}
	s.mu.Unlock()
	dir, err := os.MkdirTemp("", "gowarp-editor-*")
	if err != nil {
		s.finish(j, dir, err)
		http.Error(w, "Could not create project directory", 500)
		return
	}
	path := filepath.Join(dir, "project.sb3")
	r.Body = http.MaxBytesReader(w, r.Body, maxProjectSize)
	stop := context.AfterFunc(ctx, func() { r.Body.Close() })
	j.snapshot, err = s.receiveProject(r, path, options.Snapshot)
	stop()
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		s.finish(j, dir, err)
		code := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			code = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "Project upload failed", code)
		return
	}
	s.mu.Lock()
	if ctx.Err() == nil {
		s.status = Status{Phase: "building"}
	}
	s.mu.Unlock()
	go func() {
		var err error
		defer func() { s.finish(j, dir, err) }()
		if err = ctx.Err(); err != nil {
			return
		}
		if s.launch == nil {
			err = fmt.Errorf("preview launcher is unavailable")
			return
		}
		var done <-chan error
		done, err = s.launch(ctx, path, options)
		if err != nil {
			return
		}
		if done == nil {
			err = fmt.Errorf("preview launcher returned no process")
			return
		}
		s.mu.Lock()
		if ctx.Err() == nil {
			s.status = Status{Phase: "running"}
		}
		s.mu.Unlock()
		// The launcher reaps its process on cancellation before completing this channel.
		err = <-done
	}()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"snapshot": j.snapshot})
}

func (s *Server) receiveProject(r *http.Request, path, snapshot string) (string, error) {
	var source io.Reader = r.Body
	if snapshot != "" {
		if r.ContentLength != 0 {
			return "", fmt.Errorf("snapshot reuse must not include a project body")
		}
		cached := filepath.Join(s.cacheDir, "project.sb3")
		if err := os.Link(cached, path); err == nil {
			return snapshot, nil
		}
		file, err := os.Open(cached)
		if err != nil {
			return "", err
		}
		defer file.Close()
		source = file
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, hash), source)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil && n == 0 {
		err = fmt.Errorf("empty project")
	}
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Server) finish(j *job, dir string, err error) {
	j.stream.close()
	var snapshot string
	if j.snapshot != "" && s.ctx.Err() == nil {
		if err := os.Rename(filepath.Join(dir, "project.sb3"), filepath.Join(s.cacheDir, "project.sb3")); err == nil {
			snapshot = j.snapshot
		}
	}
	if dir != "" {
		os.RemoveAll(dir)
	}
	s.mu.Lock()
	if snapshot != "" {
		s.snapshot = snapshot
	}
	if j.ctx.Err() != nil || errors.Is(err, context.Canceled) {
		err = nil
	}
	j.cancel()
	if err != nil {
		s.status = Status{Phase: "error", Error: strings.TrimSpace(err.Error())}
	} else {
		s.status = Status{Phase: "ready"}
	}
	s.active = nil
	close(j.done)
	s.mu.Unlock()
}
