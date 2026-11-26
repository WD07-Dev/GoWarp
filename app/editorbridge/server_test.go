package editorbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gowarp/app/assets"
)

func request(t testing.TB, s *Server, method, path, origin, token string, body io.Reader) *http.Response {
	t.Helper()
	address, _ := url.Parse(s.URL())
	address.Fragment = ""
	address.Path = path
	req, err := http.NewRequest(method, address.String(), body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", origin)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x.scratch.sb3")
	req.Header.Set("X-GoWarp-Options", `{"name":"Preview","fps":60,"width":480,"height":360}`)
	client := http.Client{Timeout: 3 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func TestBrowserAccessRequiresConnectionToken(t *testing.T) {
	s, err := Start(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	for _, tc := range []struct {
		origin, token string
		status        int
	}{
		{"https://turbowarp.org", s.token, 200},
		{"https://turbowarp.org", "wrong", 401},
		{"https://example.com", s.token, 403},
		{"https://turbowarp.org.evil.test", s.token, 403},
		{"null", s.token, 200},
	} {
		res := request(t, s, "GET", "/status", tc.origin, tc.token, nil)
		if res.StatusCode != tc.status {
			t.Fatalf("origin %q: status %d", tc.origin, res.StatusCode)
		}
	}
	res := request(t, s, "OPTIONS", "/run", "https://turbowarp.org", "", nil)
	if res.StatusCode != 204 || res.Header.Get("Access-Control-Allow-Origin") != "https://turbowarp.org" || res.Header.Get("Access-Control-Allow-Private-Network") != "true" {
		t.Fatalf("preflight: %v", res)
	}
}

func TestEditorConnectionIsExplicit(t *testing.T) {
	s, err := Start(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	readConnected := func() bool {
		res := request(t, s, "GET", "/status", "https://turbowarp.org", s.token, nil)
		var status struct{ Connected bool }
		if err := json.NewDecoder(res.Body).Decode(&status); err != nil {
			t.Fatal(err)
		}
		return status.Connected
	}
	if readConnected() {
		t.Fatal("starting the local server connected a project")
	}
	res := request(t, s, "POST", "/connect", "https://turbowarp.org", s.token,
		strings.NewReader(`{"targets":[{"isStage":true,"name":"Stage"}]}`))
	if res.StatusCode != http.StatusOK || !readConnected() {
		t.Fatalf("connect did not establish a session: %d", res.StatusCode)
	}
	res = request(t, s, "POST", "/disconnect", "https://turbowarp.org", s.token, nil)
	if res.StatusCode != http.StatusAccepted || readConnected() {
		t.Fatal("disconnect left the editor session active")
	}
}

func TestDisconnectDiscardsPreviewSnapshot(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(fmt.Sprint("stopped=", stopped), func(t *testing.T) {
			s, err := Start(context.Background(), func(ctx context.Context, _ string, _ Options) (<-chan error, error) {
				done := make(chan error, 1)
				go func() { <-ctx.Done(); done <- nil }()
				return done, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			request(t, s, "POST", "/run", "https://turbowarp.org", s.token, strings.NewReader("snapshot"))
			waitPhase(t, s, "running")
			if stopped {
				s.Stop()
				waitPhase(t, s, "ready")
				if s.Status().Snapshot == "" {
					t.Fatal("Stop discarded the reusable snapshot")
				}
			}
			request(t, s, "POST", "/disconnect", "https://turbowarp.org", s.token, nil)
			waitPhase(t, s, "ready")
			if s.Status().Snapshot != "" {
				t.Fatal("disconnect retained the snapshot")
			}
			if _, err := os.Stat(filepath.Join(s.cacheDir, "project.sb3")); !os.IsNotExist(err) {
				t.Fatalf("disconnected project remains on disk: %v", err)
			}
		})
	}
}

func TestExtensionManagerCanLoadWithoutConnectionToken(t *testing.T) {
	s, err := Start(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	res := request(t, s, "GET", "/extension-manager.js", "https://turbowarp.org", "", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("extension script: status %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, assets.ExtensionManager) || bytes.Contains(body, []byte(s.token)) {
		t.Fatal("expected extension manager source without connection credentials")
	}
	if res.Header.Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("script content type: %q", res.Header.Get("Content-Type"))
	}
	for _, tc := range []struct {
		path, origin string
		status       int
	}{
		{"/status", "https://turbowarp.org", http.StatusUnauthorized},
		{"/extension-manager.js", "https://example.com", http.StatusForbidden},
	} {
		res := request(t, s, "GET", tc.path, tc.origin, "", nil)
		if res.StatusCode != tc.status {
			t.Fatalf("%s from %s: status %d", tc.path, tc.origin, res.StatusCode)
		}
	}
}

func TestTurboWarpURLLoadsLocalExtensionManager(t *testing.T) {
	s, err := Start(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	address, err := s.TurboWarpURL()
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	scriptURL := u.Query().Get("extension")
	if u.Scheme != "https" || u.Host != "turbowarp.org" || u.Path != "/editor" || scriptURL != "http://localhost:8000/extension-manager.js" || strings.Contains(address, s.token) {
		t.Fatalf("unexpected editor URL: %s", address)
	}
	repeated, err := s.TurboWarpURL()
	if err != nil || repeated != address {
		t.Fatalf("reopen editor: %q, %v", repeated, err)
	}
	client := http.Client{Timeout: 3 * time.Second}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/extension-manager.js", http.StatusOK},
		{"OPTIONS", "/extension-manager.js", http.StatusNoContent},
		{"GET", "/status", http.StatusForbidden},
	} {
		req, err := http.NewRequest(tc.method, "http://localhost:8000"+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "https://turbowarp.org")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != tc.status {
			t.Fatalf("%s %s: status %d, %v", tc.method, tc.path, res.StatusCode, err)
		}
		if tc.method == "GET" && tc.status == http.StatusOK && !bytes.Equal(body, assets.ExtensionManager) {
			t.Fatal("browser received a different extension script")
		}
	}
	s.Close()
	if _, err := s.TurboWarpURL(); err == nil {
		t.Fatal("closed connection must not restart the script server")
	}
}

func TestPreviewReusesLastUpload(t *testing.T) {
	started := make(chan string, 2)
	s, err := Start(context.Background(), func(ctx context.Context, path string, _ Options) (<-chan error, error) {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "unchanged project" {
			return nil, fmt.Errorf("wrong reused snapshot: %q, %v", data, err)
		}
		started <- path
		done := make(chan error, 1)
		go func() { <-ctx.Done(); done <- nil }()
		return done, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	send := func(snapshot, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://"+s.address+"/run", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+s.token)
		options, _ := json.Marshal(map[string]any{"fps": 60, "width": 480, "height": 360, "snapshot": snapshot})
		r.Header.Set("X-GoWarp-Options", string(options))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	first := send("", "unchanged project")
	var receipt struct{ Snapshot string }
	if first.Code != http.StatusAccepted || json.Unmarshal(first.Body.Bytes(), &receipt) != nil || receipt.Snapshot == "" {
		t.Fatalf("missing upload receipt: %d %s", first.Code, first.Body.String())
	}
	waitPhase(t, s, "running")
	firstPath := <-started
	s.Stop()
	waitPhase(t, s, "ready")
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("finished job directory remained: %v", err)
	}
	if stale := send("missing", ""); stale.Code != http.StatusConflict {
		t.Fatalf("stale snapshot accepted: %d", stale.Code)
	}
	reused := send(receipt.Snapshot, "")
	if reused.Code != http.StatusAccepted {
		t.Fatalf("reusing project requires another upload: %d %s", reused.Code, reused.Body.String())
	}
	waitPhase(t, s, "running")
	if path := <-started; path == firstPath {
		t.Fatal("preview reused a deleted working directory")
	}
	cacheDir := s.cacheDir
	s.Close()
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatalf("connection left its cached project behind: %v", err)
	}
}

func TestPreviewUploadStatusAndCancellation(t *testing.T) {
	started := make(chan string, 1)
	exited := make(chan struct{})
	s, err := Start(context.Background(), func(ctx context.Context, path string, options Options) (<-chan error, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if string(data) != "SB3 snapshot" || options.FPS != 60 {
			return nil, errors.New("wrong snapshot or settings")
		}
		done := make(chan error, 1)
		started <- path
		go func() { <-ctx.Done(); done <- nil; close(exited) }()
		return done, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	res := request(t, s, "POST", "/run", "https://turbowarp.org", s.token, strings.NewReader("SB3 snapshot"))
	if res.StatusCode != 202 {
		t.Fatalf("upload: %d", res.StatusCode)
	}
	var path string
	select {
	case path = <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("preview never started")
	}
	waitPhase(t, s, "running")
	res = request(t, s, "POST", "/run", "https://turbowarp.org", s.token, bytes.NewReader(nil))
	if res.StatusCode != 409 {
		t.Fatal("accepted a concurrent build")
	}
	res = request(t, s, "POST", "/stop", "https://turbowarp.org", s.token, nil)
	if res.StatusCode != 202 {
		t.Fatal("stop rejected")
	}
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("preview was not canceled")
	}
	waitPhase(t, s, "ready")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("uploaded project retained")
	}
}

func waitPhase(t testing.TB, s *Server, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		res := request(t, s, "GET", "/status", "https://turbowarp.org", s.token, nil)
		var status Status
		err := json.NewDecoder(res.Body).Decode(&status)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if status.Phase == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("did not reach %s: %+v", want, s.Status())
}

func TestBuildErrorIsReported(t *testing.T) {
	s, err := Start(context.Background(), func(context.Context, string, Options) (<-chan error, error) { return nil, errors.New("Go is missing") })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	request(t, s, "POST", "/run", "https://turbowarp.org", s.token, strings.NewReader("snapshot"))
	waitPhase(t, s, "error")
	if s.Status().Error != "Go is missing" {
		t.Fatal("lost compiler error")
	}
}

func TestCloseCancelsCompilationAndCleansUpload(t *testing.T) {
	started := make(chan string, 1)
	s, err := Start(context.Background(), func(ctx context.Context, path string, _ Options) (<-chan error, error) {
		started <- path
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	request(t, s, "POST", "/run", "https://turbowarp.org", s.token, strings.NewReader("snapshot"))
	var path string
	select {
	case path = <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("build never started")
	}
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("close did not cancel the build")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("canceled upload retained")
	}
}

func TestRejectMalformedRequests(t *testing.T) {
	s, err := Start(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	for _, tc := range []struct {
		host, options string
		length        int64
		want          int
	}{
		{s.address, `{}`, 1, 400},
		{s.address, `{"fps":60,"width":5000,"height":360}`, 1, 400},
		{s.address, `{"fps":60,"width":480,"height":360}`, maxProjectSize + 1, 413},
		{"rebinding.example", `{"fps":60,"width":480,"height":360}`, 1, 403},
	} {
		req := httptest.NewRequest("POST", "http://"+s.address+"/run", strings.NewReader("x"))
		req.Host = tc.host
		req.ContentLength = tc.length
		req.Header.Set("Authorization", "Bearer "+s.token)
		req.Header.Set("X-GoWarp-Options", tc.options)
		res := httptest.NewRecorder()
		s.ServeHTTP(res, req)
		if res.Code != tc.want {
			t.Fatalf("status=%d; want %d", res.Code, tc.want)
		}
	}
}
