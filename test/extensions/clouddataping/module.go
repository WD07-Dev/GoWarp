// Ping Cloud Data by GarboMuffin, original by TheShovel.
// License: MIT AND MPL-2.0
package extension

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	e "gowarp/core/extension"

	"golang.org/x/net/websocket"
)

type pingResult struct {
	done    chan struct{}
	value   bool
	expires time.Time
}

type pingState struct {
	mu                 sync.Mutex
	ctx                context.Context
	cancel             context.CancelFunc
	closed             bool
	pending            map[string]*pingResult
	computed           map[string]*pingResult
	hold, timeout, ttl time.Duration
}

func newPingState() *pingState {
	ctx, cancel := context.WithCancel(context.Background())
	return &pingState{ctx: ctx, cancel: cancel, pending: map[string]*pingResult{}, computed: map[string]*pingResult{}, hold: 2 * time.Second, timeout: 5 * time.Second, ttl: time.Minute}
}

func (s *pingState) close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.computed = map[string]*pingResult{}
	s.mu.Unlock()
}

func (s *pingState) request(server string) *pingResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if result := s.pending[server]; result != nil {
		return result
	}
	if result := s.computed[server]; result != nil && time.Now().Before(result.expires) {
		return result
	}
	u, err := url.Parse(server)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Hostname() == "" || u.User != nil || strings.Contains(server, "#") {
		return nil
	}
	// The desktop extension uses the same Origin as the TurboWarp editor.
	config, err := websocket.NewConfig(server, "https://turbowarp.org")
	if err != nil {
		return nil
	}
	result := &pingResult{done: make(chan struct{})}
	s.pending[server] = result
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, s.timeout)
		defer cancel()
		value := pingWebSocket(ctx, config, s.hold)
		s.mu.Lock()
		defer s.mu.Unlock()
		result.value = value && !s.closed
		result.expires = time.Now().Add(s.ttl)
		delete(s.pending, server)
		if !s.closed {
			s.computed[server] = result
		}
		close(result.done)
	}()
	return result
}

func pingWebSocket(ctx context.Context, config *websocket.Config, hold time.Duration) bool {
	u := config.Location
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "wss" {
			port = "443"
		}
	}
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return false
	}
	defer raw.Close()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			raw.Close()
		case <-finished:
		}
	}()
	deadline, _ := ctx.Deadline()
	if err := raw.SetDeadline(deadline); err != nil {
		return false
	}
	var conn net.Conn = raw
	if u.Scheme == "wss" {
		secure := tls.Client(raw, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(ctx); err != nil {
			return false
		}
		conn = secure
	}
	ws, err := websocket.NewClient(config, conn)
	if err != nil {
		return false
	}
	defer ws.Close()
	stableAt := time.Now().Add(hold)
	if stableAt.Before(deadline) {
		deadline = stableAt
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return false
	}
	// Read and discard data so close/error frames are observed during the hold.
	_, err = io.Copy(io.Discard, ws)
	netErr, timedOut := err.(net.Error)
	return timedOut && netErr.Timeout() && !time.Now().Before(stableAt) && ctx.Err() == nil
}

func New() e.Extension {
	return e.Extension{
		ID:    "clouddataping",
		State: map[string]any{"ping": newPingState()},
		Events: map[string]e.Handler{
			"dispose": func(c *e.Context) (e.Result, error) {
				if state, ok := c.State["ping"].(*pingState); ok {
					state.close()
				}
				return e.Result{}, nil
			},
		},
		Blocks: []e.Block{{
			Opcode: "clouddataping_ping", Kind: "boolean", Text: "is cloud data server [SERVER] up?", Inputs: []string{"SERVER"},
			Handler: func(c *e.Context) (e.Result, error) {
				if c.Local == nil {
					c.Local = map[string]any{}
				}
				result, started := c.Local["ping"].(*pingResult)
				if !started {
					result = c.State["ping"].(*pingState).request(c.String("SERVER"))
					if result == nil {
						return e.Result{Value: false}, nil
					}
					c.Local["ping"] = result
				}
				select {
				case <-result.done:
					return e.Result{Value: result.value}, nil
				default:
					return e.Result{Again: true, YieldTick: true}, nil
				}
			},
		}},
	}
}
