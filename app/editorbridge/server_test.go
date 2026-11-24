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
	"strings"
	"testing"
	"time"
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
