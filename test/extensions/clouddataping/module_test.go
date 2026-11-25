package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"
	e "gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func testState(t *testing.T) *pingState {
	t.Helper()
	s := newPingState()
	s.hold, s.timeout, s.ttl = 30*time.Millisecond, time.Second, time.Minute
	t.Cleanup(s.close)
	return s
}

func awaitPing(t *testing.T, r *pingResult) bool {
	t.Helper()
	if r == nil {
		t.Fatal("ping did not start")
	}
	select {
	case <-r.done:
		return r.value
	case <-time.After(2 * time.Second):
		t.Fatal("ping did not finish")
		return false
	}
}

func TestPingHoldsConnectionCachesAndCoalesces(t *testing.T) {
	var connections atomic.Int32
	var origin atomic.Value
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		connections.Add(1)
		origin.Store(ws.Request().Header.Get("Origin"))
		// Receiving traffic must not count as either a close or a successful hold.
		websocket.Message.Send(ws, "cloud data")
		io.Copy(io.Discard, ws)
	}))
	t.Cleanup(server.Close)
	s := testState(t)
	u := "ws" + strings.TrimPrefix(server.URL, "http")
	start := time.Now()
	first := s.request(u)
	if second := s.request(u); second != first {
		t.Fatal("pending ping was not coalesced")
	}
	if !awaitPing(t, first) {
		t.Fatal("stable WebSocket reported down")
	}
	if time.Since(start) < s.hold {
		t.Fatal("success returned before stable connection interval")
	}
	if got := origin.Load(); got != "https://turbowarp.org" {
		t.Fatalf("Origin = %v", got)
	}
	if s.request(u) != first || connections.Load() != 1 {
		t.Fatal("cached success opened another connection")
	}
	s.mu.Lock()
	first.expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if next := s.request(u); next == first || !awaitPing(t, next) {
		t.Fatal("expired result was not refreshed")
	}
	if connections.Load() != 2 {
		t.Fatal("expected exactly two probes")
	}
}

func TestPingCloseErrorAndTimeout(t *testing.T) {
	for _, behavior := range []string{"close", "reject", "timeout", "tls"} {
		t.Run(behavior, func(t *testing.T) {
			s := testState(t)
			s.timeout = 60 * time.Millisecond
			var server *httptest.Server
			switch behavior {
			case "close":
				server = httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) { ws.Close() }))
			case "reject":
				server = httptest.NewServer(http.NotFoundHandler())
			case "tls":
				server = httptest.NewTLSServer(http.NotFoundHandler())
			case "timeout":
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
			}
			t.Cleanup(server.Close)
			u := "ws" + strings.TrimPrefix(server.URL, "http")
			result := s.request(u)
			if awaitPing(t, result) {
				t.Fatal("unavailable server reported up")
			}
			if s.request(u) != result {
				t.Fatal("negative result was not cached")
			}
		})
	}
}

func TestPingTotalTimeoutIncludesStableInterval(t *testing.T) {
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) { io.Copy(io.Discard, ws) }))
	t.Cleanup(server.Close)
	s := testState(t)
	s.hold, s.timeout = time.Second, 30*time.Millisecond
	if awaitPing(t, s.request("ws"+strings.TrimPrefix(server.URL, "http"))) {
		t.Fatal("hold exceeded total timeout")
	}
}

func TestPingInvalidURLDoesNotStartOrCache(t *testing.T) {
	s := testState(t)
	for _, u := range []string{"", "invalid", "http://example.org", "ftp://example.org", "ws://", "wss://example.org/#fragment", "wss://example.org/#", "ws://user:pass@example.org", "ws://example.org:bad"} {
		if s.request(u) != nil {
			t.Errorf("invalid URL accepted: %q", u)
		}
	}
	if len(s.pending) != 0 || len(s.computed) != 0 {
		t.Fatal("invalid URL was retained")
	}
}

func TestReporterYieldsAndDisposeCancelsHandshake(t *testing.T) {
	entered, exited := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(exited)
	}))
	t.Cleanup(server.Close)
	ext := New()
	t.Cleanup(func() { ext.State["ping"].(*pingState).close() })
	s := ext.State["ping"].(*pingState)
	if s.hold != 2*time.Second || s.timeout != 5*time.Second || s.ttl != time.Minute {
		t.Fatal("source timing changed")
	}
	if New().State["ping"] == s {
		t.Fatal("state shared across runtimes")
	}
	if err := e.Validate(e.Bundle{New}, []string{"clouddataping"}, []string{"clouddataping_ping"}); err != nil {
		t.Fatal(err)
	}
	c := &e.Context{Args: map[string]any{"SERVER": "ws" + strings.TrimPrefix(server.URL, "http")}, State: ext.State, Local: map[string]any{}}
	start := time.Now()
	r, err := ext.Blocks[0].Handler(c)
	if err != nil || !r.Again || !r.YieldTick || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("reporter blocked instead of yielding: %+v, %v", r, err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handshake did not start")
	}
	if _, err := ext.Events["dispose"](&e.Context{State: ext.State}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("dispose did not cancel handshake")
	}
	if awaitPing(t, c.Local["ping"].(*pingResult)) {
		t.Fatal("disposed ping returned true")
	}
	r, err = ext.Blocks[0].Handler(c)
	if err != nil || r.Again || r.Value != false {
		t.Fatalf("resumed reporter = %+v, %v", r, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) != 0 || len(s.computed) != 0 {
		t.Fatal("disposed state retained pending/cache entries")
	}
}

func TestEmbeddedGoImplementationMatchesTestedModule(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	path := filepath.Join(dir, "..", "..", "..", "extensions", "examples", "clouddataping.js")
	js, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source, err := e.ExtractGo(path, js)
	if err != nil {
		t.Fatal(err)
	}
	source, err = format.Source(source)
	if err != nil {
		t.Fatal(err)
	}
	tested, err := os.ReadFile(filepath.Join(dir, "module.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source, tested) {
		t.Fatal("embedded Go differs from tested module")
	}
}

func TestReporterContinuationInVM(t *testing.T) {
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) { io.Copy(io.Discard, ws) }))
	t.Cleanup(server.Close)
	for _, turbo := range []bool{false, true} {
		t.Run(fmt.Sprint("turbo=", turbo), func(t *testing.T) {
			var p project.Project
			data := fmt.Sprintf(`{"extensions":["clouddataping"],"targets":[{"isStage":true,"name":"Stage","variables":{"result":["result","pending"],"other":["other",0]},"blocks":{
				"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"save"},
				"save":{"opcode":"data_setvariableto","fields":{"VARIABLE":["result","result"]},"inputs":{"VALUE":[2,"ping"]}},
				"ping":{"opcode":"clouddataping_ping","inputs":{"SERVER":[1,[10,%q]]}},
				"otherflag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"progress"},
				"progress":{"opcode":"data_setvariableto","fields":{"VARIABLE":["other","other"]},"inputs":{"VALUE":[1,[4,"1"]]}}
			}}]}`, "ws"+strings.TrimPrefix(server.URL, "http"))
			if err := json.Unmarshal([]byte(data), &p); err != nil {
				t.Fatal(err)
			}
			rt := vm.New(&p)
			factory := func() e.Extension {
				ext := New()
				ext.State["ping"].(*pingState).hold = 30 * time.Millisecond
				return ext
			}
			if err := rt.SetExtensions(e.Bundle{factory}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := rt.CloseExtensions(); err != nil {
					t.Error(err)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := rt.Run(ctx, 1, 30, turbo); err != nil {
				t.Fatal(err)
			}
			state := rt.Snapshot()[0].Variables
			if state["result"].Value != "pending" || e.Number(state["other"].Value) != 1 {
				t.Fatalf("pending reporter blocked another script: %v", state)
			}
			for !rt.Done() {
				time.Sleep(time.Millisecond)
				if err := rt.Run(ctx, 1, 30, turbo); err != nil {
					t.Fatal(err)
				}
			}
			if got := rt.Snapshot()[0].Variables["result"].Value; got != true {
				t.Fatalf("resumed reporter = %v", got)
			}
		})
	}
}
