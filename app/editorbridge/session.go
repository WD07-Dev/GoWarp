package editorbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"gowarp/app/config"
	"gowarp/core/project"
)

func (s *Server) SetProjectHandler(handler func(context.Context, *project.Project, Options) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projectHandler = handler
}

func (s *Server) Configure(settings config.Settings, err error) {
	var data []byte
	if err == nil {
		data, err = json.Marshal(settings)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.settingsError = err.Error()
		data = []byte(s.settingsError)
	} else {
		var snapshot config.Settings
		if err := json.Unmarshal(data, &snapshot); err != nil {
			s.settingsError = err.Error()
			return
		}
		s.settings, s.settingsError = &snapshot, ""
	}
	hash := sha256.Sum256(data)
	s.settingsRevision = hex.EncodeToString(hash[:])
}

func readOptions(r *http.Request) (Options, error) {
	var options Options
	raw := r.Header.Get("X-GoWarp-Options")
	decoded, err := url.PathUnescape(raw)
	if len(raw) > 4096 || err != nil || json.Unmarshal([]byte(decoded), &options) != nil || len(options.Name) > 256 || options.FPS < 0 || options.FPS > 1000 || options.Width < 1 || options.Height < 1 || options.Width > 4096 || options.Height > 4096 {
		return options, fmt.Errorf("invalid project settings")
	}
	if options.Display != "" && options.Display != "native" && options.Display != "embedded" {
		return options, fmt.Errorf("invalid display mode")
	}
	if options.Display == "" {
		options.Display = "native"
	}
	return options, nil
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	options, err := readOptions(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	s.mu.Lock()
	if s.closed || s.connected || s.connecting || s.active != nil {
		s.mu.Unlock()
		http.Error(w, "Disconnect the current project first", http.StatusConflict)
		return
	}
	s.connecting = true
	s.connectCancel = cancel
	handler := s.projectHandler
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.connecting = false; s.connectCancel = nil; s.mu.Unlock() }()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	r.Body = http.MaxBytesReader(w, r.Body, maxProjectSize)
	closeBody := context.AfterFunc(ctx, func() { r.Body.Close() })
	defer closeBody()
	file, err := os.CreateTemp(s.cacheDir, "connect-*.json")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(file.Name())
	_, err = io.Copy(file, r.Body)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	var p *project.Project
	if err == nil {
		p, err = project.Load(file.Name())
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && handler != nil {
		err = handler(ctx, p, options)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.connected = !s.closed && ctx.Err() == nil
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.Status())
}
