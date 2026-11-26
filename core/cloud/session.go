// Package cloud supplies persistent and Scratch-compatible network variables.
package cloud

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

type Options struct {
	Mode, Server, ProjectID, Username, Contact string
	Modes                                      map[string]string
	Names                                      map[string]string // stable variable ID -> network name
}

type Session struct {
	ready                     bool
	options                   Options
	mu                        sync.Mutex
	incoming, local, outgoing map[string]string
	dirty                     bool
	status                    string
	path                      string
	ctx                       context.Context
	cancel                    context.CancelFunc
	done                      chan struct{}
}

var numeric = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)

func ValidValue(value string) bool {
	return len(value) <= 100000 && numeric.MatchString(value)
}

func Open(ctx context.Context, options Options, dir string) (*Session, error) {
	ctx, cancel := context.WithCancel(ctx)
	s := &Session{
		options:  options,
		incoming: map[string]string{},
		local:    map[string]string{},
		outgoing: map[string]string{},
		path:     filepath.Join(dir, "cloud.json"),
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
		status:   "Cloud: off",
	}
	network := options.Mode == "websocket"
	local := options.Mode == "local"
	for _, mode := range options.Modes {
		network = network || mode == "websocket"
		local = local || mode == "local"
	}
	if local {
		data, err := os.ReadFile(s.path)
		if err != nil && !os.IsNotExist(err) {
			cancel()
			return nil, err
		}
		if len(data) > 16<<20 {
			cancel()
			return nil, fmt.Errorf("cloud save too large")
		}
		if len(data) > 0 {
			if err = json.Unmarshal(data, &s.local); err != nil {
				cancel()
				return nil, err
			}
		}
		if s.local == nil {
			s.local = map[string]string{}
		}
		for k, v := range s.local {
			if s.mode(k) == "local" && len(k) <= 1024 && len(v) <= 100000 {
				s.incoming[k] = v
			} else {
				delete(s.local, k)
			}
		}
		s.status = "Cloud: local"
	}
	go s.run(network)
	return s, nil
}

func (s *Session) mode(name string) string {
	if m, ok := s.options.Modes[name]; ok {
		return m
	}
	return s.options.Mode
}

func (s *Session) Set(name, value string) {
	if len(name) > 1024 || len(value) > 100000 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.mode(name) {
	case "local":
		previous, exists := s.local[name]
		if exists && previous == value {
			return
		}
		if !exists && len(s.local) >= 1024 {
			return
		}
		s.local[name] = value
		s.dirty = true
	case "websocket":
		if s.ready && ValidValue(value) && len(s.outgoing) < 1024 {
			s.outgoing[name] = value
		}
	}
}

func (s *Session) Poll() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.incoming
	s.incoming = map[string]string{}
	return v
}

func (s *Session) Status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Session) setStatus(text string) {
	s.mu.Lock()
	s.status = text
	s.mu.Unlock()
}

func (s *Session) Close() {
	s.cancel()
	<-s.done
}

func (s *Session) flush() {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	data, err := json.Marshal(s.local)
	s.dirty = false
	s.mu.Unlock()
	if err == nil {
		err = os.MkdirAll(filepath.Dir(s.path), 0700)
	}
	if err == nil {
		var f *os.File
		f, err = os.CreateTemp(filepath.Dir(s.path), ".cloud-*")
		if err == nil {
			_, err = f.Write(data)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(f.Name(), s.path)
			}
			os.Remove(f.Name())
		}
	}
	if err != nil {
		s.setStatus("Cloud save: " + err.Error())
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
	}
}

func (s *Session) run(network bool) {
	defer close(s.done)
	defer s.flush()
	var wg sync.WaitGroup
	if network {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.network()
		}()
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			wg.Wait()
			return
		case <-ticker.C:
			s.flush()
		}
	}
}

func (s *Session) network() {
	delay := time.Second
	for s.ctx.Err() == nil {
		s.setStatus("Cloud: connecting")
		err := s.connect()
		if s.ctx.Err() != nil {
			return
		}
		s.setStatus(fmt.Sprintf("Cloud disconnected: %v", err))
		timer := time.NewTimer(delay)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}

func (s *Session) connect() error {
	cfg, err := websocket.NewConfig(s.options.Server, "https://gowarp.invalid")
	if err != nil {
		return err
	}
	cfg.Header.Set("User-Agent", "GoWarp/1 "+s.options.Contact)
	address := cfg.Location.Host
	if cfg.Location.Port() == "" {
		port := "80"
		if cfg.Location.Scheme == "wss" {
			port = "443"
		}
		address = net.JoinHostPort(cfg.Location.Hostname(), port)
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	var conn net.Conn
	if cfg.Location.Scheme == "wss" {
		d := tls.Dialer{NetDialer: dialer}
		conn, err = d.DialContext(s.ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(s.ctx, "tcp", address)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(s.ctx, func() {
		conn.Close()
	})
	defer stop()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	ws, err := websocket.NewClient(cfg, conn)
	if err != nil {
		return err
	}
	defer ws.Close()
	ws.MaxPayloadBytes = 1 << 20
	conn.SetDeadline(time.Time{})
	username := s.options.Username
	if username == "" {
		username = fmt.Sprintf("player%d", time.Now().UnixNano()%10000000)
	}
	send := func(method, name, value string) error {
		data, err := json.Marshal(map[string]string{
			"method":     method,
			"project_id": s.options.ProjectID,
			"user":       username,
			"name":       name,
			"value":      value,
		})
		if err != nil {
			return err
		}
		ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return websocket.Message.Send(ws, string(data)+"\n")
	}
	if err = send("handshake", "", ""); err != nil {
		return err
	}
	s.mu.Lock()
	s.outgoing = map[string]string{}
	s.ready = false
	s.status = "Cloud: synchronizing"
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.ready = false
		s.mu.Unlock()
	}()
	readyAt := time.Now().Add(time.Second)
	received := make(chan error, 1)
	go func() {
		for {
			var data string
			ws.SetReadDeadline(time.Now().Add(90 * time.Second))
			if err := websocket.Message.Receive(ws, &data); err != nil {
				received <- err
				return
			}
			for _, line := range strings.Split(data, "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				var msg struct {
					Method, Name string
					Value        json.RawMessage
				}
				if err := json.Unmarshal([]byte(line), &msg); err != nil {
					s.setStatus("Cloud: invalid server message")
					continue
				}
				if msg.Method != "set" {
					continue
				}
				var value string
				if json.Unmarshal(msg.Value, &value) != nil {
					value = string(msg.Value)
				}
				if len(msg.Name) > 1024 || !ValidValue(value) {
					continue
				}
				s.mu.Lock()
				keys := []string{msg.Name}
				if len(s.options.Names) > 0 {
					keys = nil
					for id, name := range s.options.Names {
						if name == msg.Name {
							keys = append(keys, id)
						}
					}
				}
				for _, id := range keys {
					if s.mode(id) == "websocket" && len(s.incoming) < 1024 {
						s.incoming[id] = value
						delete(s.outgoing, id)
					}
				}
				s.mu.Unlock()
			}
		}
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	defer func() {
		conn.Close()
		select {
		case <-received:
		case <-time.After(time.Second):
		}
	}()
	for {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case err := <-received:
			received <- err
			return err
		case <-ticker.C:
			s.mu.Lock()
			if !s.ready {
				if !time.Now().Before(readyAt) {
					s.ready = true
					s.status = "Cloud: connected"
				}
				s.mu.Unlock()
				continue
			}
			updates := s.outgoing
			s.outgoing = map[string]string{}
			s.mu.Unlock()
			for k, v := range updates {
				if name, ok := s.options.Names[k]; ok {
					k = name
				}
				if err = send("set", k, v); err != nil {
					return err
				}
			}
		}
	}
}
