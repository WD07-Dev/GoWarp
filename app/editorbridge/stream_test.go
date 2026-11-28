package editorbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
	"gowarp/core/project"
)

func TestPreviewFrameSizeUsesBrowserPixels(t *testing.T) {
	peer := &Peer{Embedded: true}
	for _, size := range [][2]int{{960, 720}, {480, 360}, {600, 450}} {
		peer.frameSize.Store(uint32(size[0])<<16 | uint32(size[1]))
		for _, window := range [][2]int{{480, 360}, {320, 240}, {1920, 1080}} {
			w, h := peer.FrameSize(window[0], window[1])
			if w != size[0] || h != size[1] {
				t.Fatalf("browser=%v native=%v: rendered %dx%d", size, window, w, h)
			}
		}
	}
}

func TestPreviewAcceptsFullHDFrame(t *testing.T) {
	const width, height = 1920, 1080
	data := make([]byte, 9+width*height*4)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], width)
	binary.LittleEndian.PutUint32(data[5:9], height)
	if len(data) > maxFrameSize || !validFrame(data) {
		t.Fatal("full-HD preview was rejected")
	}
	binary.LittleEndian.PutUint32(data[1:5], 1921)
	data = append(data, make([]byte, height*4)...)
	if validFrame(data) {
		t.Fatal("invalid frame dimensions were accepted")
	}
}

func TestReadPlayerPacketReusesStorage(t *testing.T) {
	want := []byte("first frame")
	packet := make([]byte, 4+len(want))
	binary.LittleEndian.PutUint32(packet, uint32(len(want)))
	copy(packet[4:], want)
	storage := make([]byte, 128)
	reader := bytes.NewReader(append(packet, packet...))
	for range 2 {
		got, err := readPlayerPacket(reader, storage)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("packet=%q error=%v", got, err)
		}
		if &got[0] != &storage[0] {
			t.Fatal("packet allocated a new payload buffer")
		}
	}
}

func TestReadPlayerPacketRejectsInvalidLengths(t *testing.T) {
	for _, size := range []uint32{0, maxFrameSize + 1, ^uint32(0)} {
		packet := make([]byte, 4)
		binary.LittleEndian.PutUint32(packet, size)
		if _, err := readPlayerPacket(bytes.NewReader(packet), nil); err == nil {
			t.Fatalf("accepted packet length %d", size)
		}
	}
	packet := []byte{8, 0, 0, 0, 1, 2}
	if _, err := readPlayerPacket(bytes.NewReader(packet), nil); err != io.ErrUnexpectedEOF {
		t.Fatalf("truncated packet: %v", err)
	}
}

func previewSocket(t testing.TB, s *Server, token string) *websocket.Conn {
	t.Helper()
	u, _ := url.Parse(s.URL())
	u.Scheme, u.Path, u.Fragment = "ws", "/preview", ""
	config, err := websocket.NewConfig(u.String(), "https://turbowarp.org")
	if err != nil {
		t.Fatal(err)
	}
	config.Protocol = []string{"gowarp-preview"}
	ws, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	ws.SetDeadline(time.Now().Add(3 * time.Second))
	if err := websocket.JSON.Send(ws, map[string]string{"token": token}); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestPreviewStreamsFramesAndCommandsWithoutRebuilding(t *testing.T) {
	peers := make(chan *Peer, 1)
	builds := 0
	s, err := Start(context.Background(), func(ctx context.Context, _ string, options Options) (<-chan error, error) {
		builds++
		peer, err := ConnectRuntime(ctx, options.RuntimeURL, true)
		if err != nil {
			return nil, err
		}
		peers <- peer
		done := make(chan error, 1)
		go func() { <-ctx.Done(); peer.Close(); done <- nil }()
		return done, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	request(t, s, "POST", "/run", "https://turbowarp.org", s.token, strings.NewReader("snapshot"))
	var peer *Peer
	select {
	case peer = <-peers:
	case <-time.After(3 * time.Second):
		t.Fatal("player did not connect")
	}
	waitPhase(t, s, "running")
	ws := previewSocket(t, s, s.token)
	for _, id := range []uint64{1, 2} {
		if err := websocket.JSON.Send(ws, Command{ID: id, Action: "toggle", Target: 1, Block: "stack"}); err != nil {
			t.Fatal(err)
		}
		select {
		case command := <-peer.Commands:
			if command.ID != id || command.Block != "stack" || command.Target != 1 {
				t.Fatalf("command = %+v", command)
			}
			peer.Reply(Reply{ID: id, Running: id == 1})
		case <-time.After(3 * time.Second):
			t.Fatal("command not delivered")
		}
		var reply Reply
		if err := websocket.JSON.Receive(ws, &reply); err != nil {
			t.Fatal(err)
		}
		if reply.ID != id || reply.Running != (id == 1) {
			t.Fatalf("reply = %+v", reply)
		}
	}

	for _, sent := range []Command{
		{Action: "monitor", Monitor: &project.Monitor{ID: "score", Opcode: "data_variable", Visible: true}},
		{Action: "answer", QuestionID: 17, Text: "한글"},
		{Action: "state"},
	} {
		if err := websocket.JSON.Send(ws, sent); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-peer.Commands:
			a, _ := json.Marshal(sent)
			b, _ := json.Marshal(got)
			if !bytes.Equal(a, b) {
				t.Fatalf("command changed: %s / %s", a, b)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("preview UI command not delivered")
		}
	}
	question := "Name?"
	peer.Reply(Reply{Action: "question", QuestionID: 17, Question: &question})
	var prompt Reply
	if err := websocket.JSON.Receive(ws, &prompt); err != nil {
		t.Fatal(err)
	}
	if prompt.QuestionID != 17 || prompt.Question == nil || *prompt.Question != question {
		t.Fatalf("question: %+v", prompt)
	}
	if builds != 1 {
		t.Fatal("click rebuilt the project")
	}
	edits := json.RawMessage(`[{"target":0,"blocks":{"value":{"opcode":"looks_say","inputs":{"MESSAGE":[1,[10,"` + strings.Repeat("a", 20000) + `"]]}}}}]`)
	if err := websocket.JSON.Send(ws, Command{ID: 3, Action: "edit", Edits: edits}); err != nil {
		t.Fatal(err)
	}
	select {
	case command := <-peer.Commands:
		if command.Action != "edit" || string(command.Edits) != string(edits) {
			t.Fatal("block edit was not delivered intact")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("block edit was not delivered")
	}
	if err := websocket.JSON.Send(ws, map[string]any{"action": "frame", "width": 480, "height": 360, "frames": 100}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !peer.WantsFrame() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if width, height := peer.FrameSize(1920, 1080); width != 480 || height != 360 {
		t.Fatalf("preview did not fit the browser stage: %dx%d", width, height)
	}
	peer.Frame(2, 1, func(pixels []byte) { copy(pixels, []byte{255, 0, 0, 255, 0, 255, 0, 255}) })
	var frame []byte
	if err := websocket.Message.Receive(ws, &frame); err != nil {
		t.Fatal(err)
	}
	if !validFrame(frame) || binary.LittleEndian.Uint32(frame[1:5]) != 2 || frame[9] != 255 || frame[14] != 255 {
		t.Fatalf("frame = %v", frame)
	}
	if !peer.WantsFrame() {
		t.Fatal("next frame waits for a browser round trip")
	}
	peer.Frame(2, 1, func(pixels []byte) { clear(pixels) })
	if err := websocket.Message.Receive(ws, &frame); err != nil {
		t.Fatal(err)
	}
	if !validFrame(frame) || frame[9] != 0 {
		t.Fatalf("second frame = %v", frame)
	}
	if peer.WantsFrame() {
		t.Fatal("frame request exceeded the two-frame limit")
	}
	peer.Frame(2, 1, func([]byte) { t.Fatal("unrequested GPU readback") })
	if err := websocket.JSON.Send(ws, Command{Action: "frame", Width: 1440, Height: 1080, Frames: 1}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for !peer.WantsFrame() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if width, height := peer.FrameSize(480, 360); width != 1440 || height != 1080 {
		t.Fatalf("high-DPI request was reduced: %dx%d", width, height)
	}
	peer.Frame(1440, 1080, func(pixels []byte) {
		clear(pixels)
		pixels[0], pixels[len(pixels)-1] = 123, 255
	})
	if err := websocket.Message.Receive(ws, &frame); err != nil {
		t.Fatal(err)
	}
	if !validFrame(frame) || len(frame) != 9+1440*1080*4 || frame[9] != 123 || frame[len(frame)-1] != 255 {
		t.Fatal("high-DPI frame was altered or truncated")
	}
	ws.Close()
	select {
	case command := <-peer.Commands:
		if command.Action != "release-input" {
			t.Fatalf("disconnect command = %+v", command)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("browser disconnect left held input")
	}
}

func TestPreviewWebSocketRejectsWrongToken(t *testing.T) {
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
	ws := previewSocket(t, s, "wrong")
	var data []byte
	if err := websocket.Message.Receive(ws, &data); err == nil {
		t.Fatal("unauthenticated websocket remained usable")
	}
}

func TestNativePreviewDoesNotReadPixels(t *testing.T) {
	peer := &Peer{Embedded: false}
	if peer.WantsFrame() {
		t.Fatal("native preview requested a readback")
	}
	peer.Frame(2, 2, func([]byte) { t.Fatal("native preview read pixels") })
}

func BenchmarkPreviewFrameRelay(b *testing.B) {
	peers := make(chan *Peer, 1)
	s, err := Start(context.Background(), func(ctx context.Context, _ string, options Options) (<-chan error, error) {
		peer, err := ConnectRuntime(ctx, options.RuntimeURL, true)
		if err != nil {
			return nil, err
		}
		peers <- peer
		done := make(chan error, 1)
		go func() { <-ctx.Done(); peer.Close(); done <- nil }()
		return done, nil
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(s.Close)
	request(b, s, "POST", "/run", "https://turbowarp.org", s.token, strings.NewReader("snapshot"))
	waitPhase(b, s, "running")
	peer := <-peers
	ws := previewSocket(b, s, s.token)
	ws.SetDeadline(time.Time{})
	b.ReportAllocs()
	b.SetBytes(480 * 360 * 4)
	for b.Loop() {
		peer.wanted.Store(1)
		peer.Frame(480, 360, func(pixels []byte) { clear(pixels) })
		var frame []byte
		if err := websocket.Message.Receive(ws, &frame); err != nil {
			b.Fatal(err)
		}
		if !validFrame(frame) {
			b.Fatal("invalid frame")
		}
	}
}
