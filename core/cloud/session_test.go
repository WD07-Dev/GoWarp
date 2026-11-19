package cloud

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestLocalPersistenceAndDisabledVariables(t *testing.T) {
	dir := t.TempDir()
	opts := Options{Mode: "local", Modes: map[string]string{"ignored": "off"}}
	s, err := Open(context.Background(), opts, dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Set("score", "123")
	s.Set("ignored", "999")
	s.Close()
	s, err = Open(context.Background(), opts, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	values := s.Poll()
	if values["score"] != "123" || values["ignored"] != "" {
		t.Fatalf("bad restore: %v", values)
	}
}

func TestLocalPersistenceKeepsTextAndEmptyValues(t *testing.T) {
	dir := t.TempDir()
	opts := Options{Mode: "local", Modes: map[string]string{"ignored": "off"}}
	want := map[string]string{
		"name":   "차라",
		"save":   `["아이템슬롯","pie","HP",92]`,
		"empty":  "",
		"digits": "000123",
	}
	s, err := Open(context.Background(), opts, dir)
	if err != nil {
		t.Fatal(err)
	}
	for id, value := range want {
		s.Set(id, value)
	}
	s.Set("ignored", "text")
	s.Set("oversize", strings.Repeat("x", 100001))
	s.Close()
	s, err = Open(context.Background(), opts, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := s.Poll()
	if len(got) != len(want) {
		t.Fatalf("restored %d values, want %d: %v", len(got), len(want), got)
	}
	for id, value := range want {
		if actual, exists := got[id]; !exists || actual != value {
			t.Errorf("restored %s = %q (exists %v), want %q", id, actual, exists, value)
		}
	}
}

func TestWebsocketValuesRemainNumeric(t *testing.T) {
	s := &Session{options: Options{Mode: "websocket"}, ready: true, outgoing: map[string]string{}}
	for _, value := range []string{"", "name", `["HP",92]`, strings.Repeat("1", 100001)} {
		s.Set("score", value)
	}
	if len(s.outgoing) != 0 {
		t.Fatal("network cloud accepted nonnumeric or excessive data")
	}
	s.Set("score", "00123")
	if s.outgoing["score"] != "00123" {
		t.Fatal("network cloud rejected valid numeric data")
	}
}

func TestWebsocketHandshakeAndUpdates(t *testing.T) {
	received := make(chan string, 1)
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()
		var text string
		if err := websocket.Message.Receive(ws, &text); err != nil {
			return
		}
		if !strings.Contains(text, `"method":"handshake"`) {
			return
		}
		websocket.Message.Send(ws, "{\"method\":\"set\",\"name\":\"score\",\"value\":\"42\"}\n")
		if err := websocket.Message.Receive(ws, &text); err != nil {
			return
		}
		received <- text
	}))
	defer server.Close()
	s, err := Open(context.Background(), Options{
		Mode:      "websocket",
		Server:    "ws" + strings.TrimPrefix(server.URL, "http"),
		ProjectID: "test",
		Contact:   "test@example.invalid",
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	deadline := time.After(3 * time.Second)
	for {
		if s.Poll()["score"] == "42" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no server update")
		case <-time.After(10 * time.Millisecond):
		}
	}
	for s.Status() != "Cloud: connected" {
		select {
		case <-deadline:
			t.Fatal("sync did not finish")
		case <-time.After(10 * time.Millisecond):
		}
	}
	s.Set("score", "43")
	select {
	case got := <-received:
		if !strings.Contains(got, `"value":"43"`) {
			t.Fatal(got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no client update")
	}
}

func TestInitialSynchronizationDoesNotSendEarlierChanges(t *testing.T) {
	early := make(chan bool, 1)
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()
		var message string
		if websocket.Message.Receive(ws, &message) != nil {
			return
		}
		ws.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
		err := websocket.Message.Receive(ws, &message)
		early <- err == nil
	}))
	defer server.Close()
	s, err := Open(context.Background(), Options{
		Mode:      "websocket",
		Server:    "ws" + strings.TrimPrefix(server.URL, "http"),
		ProjectID: "test",
		Contact:   "test@example.invalid",
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	time.Sleep(150 * time.Millisecond)
	s.Set("score", "99")
	if <-early {
		t.Fatal("sent local change before initial synchronization window")
	}
}
