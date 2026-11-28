package editorbridge

import (
	"context"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gowarp/core/project"

	"golang.org/x/net/websocket"
)

const maxFrameEdge = 1920
const maxFrameSize = 9 + maxFrameEdge*maxFrameEdge*4
const maxCommandSize = 4 << 20

// Command messages are applied by the player on its VM thread.
type Command struct {
	Monitor    *project.Monitor `json:"monitor,omitempty"`
	QuestionID uint64           `json:"questionId,omitempty"`
	Frames     int              `json:"frames,omitempty"`
	Width      int              `json:"width,omitempty"`
	Height     int              `json:"height,omitempty"`
	Edits      json.RawMessage  `json:"edits,omitempty"`
	ID         uint64           `json:"id,omitempty"`
	Action     string           `json:"action"`
	Target     int              `json:"target,omitempty"`
	Block      string           `json:"block,omitempty"`
	Reporter   bool             `json:"reporter,omitempty"`
	X          float64          `json:"x,omitempty"`
	Y          float64          `json:"y,omitempty"`
	Down       bool             `json:"down,omitempty"`
	Key        string           `json:"key,omitempty"`
	Text       string           `json:"text,omitempty"`
}

type Reply struct {
	Action     string  `json:"action,omitempty"`
	Question   *string `json:"question,omitempty"`
	QuestionID uint64  `json:"questionId,omitempty"`
	ID         uint64  `json:"id"`
	Value      string  `json:"value,omitempty"`
	Reporter   bool    `json:"reporter,omitempty"`
	Running    bool    `json:"running,omitempty"`
	Error      string  `json:"error,omitempty"`
}

type relay struct {
	mu              sync.Mutex
	player, browser *websocket.Conn
	commands        chan []byte
	replies         chan []byte
	frames          chan []byte
	buffers         chan []byte
	done            chan struct{}
	once            sync.Once
}

type Peer struct {
	Commands  chan Command
	frames    chan []byte
	replies   chan []byte
	free      chan []byte
	wanted    atomic.Int32
	frameSize atomic.Uint32
	done      chan struct{}
	once      sync.Once
	ws        *websocket.Conn
	Embedded  bool
}

func newRelay() *relay {
	r := &relay{
		commands: make(chan []byte, 64), replies: make(chan []byte, 32),
		frames: make(chan []byte, 2), buffers: make(chan []byte, 3),
		done: make(chan struct{}),
	}
	for range cap(r.buffers) {
		r.buffers <- nil
	}
	return r
}

func (s *relay) close() {
	s.once.Do(func() {
		close(s.done)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.player != nil {
			s.player.Close()
		}
		if s.browser != nil {
			s.browser.Close()
		}
	})
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request, player bool) {
	websocket.Server{
		Handshake: func(config *websocket.Config, req *http.Request) error {
			config.Protocol = []string{"gowarp-preview"}
			return nil // Host and Origin were checked before upgrading.
		},
		Handler: func(ws *websocket.Conn) {
			defer ws.Close()
			ws.MaxPayloadBytes = 256
			ws.SetReadDeadline(time.Now().Add(5 * time.Second))
			var auth struct {
				Token string `json:"token"`
			}
			if websocket.JSON.Receive(ws, &auth) != nil {
				return
			}
			s.mu.Lock()
			job := s.active
			token := s.token
			if player && job != nil {
				token = job.token
			}
			valid := job != nil && subtle.ConstantTimeCompare([]byte(auth.Token), []byte(token)) == 1
			s.mu.Unlock()
			if !valid {
				return
			}
			ws.SetReadDeadline(time.Time{})
			if player {
				ws.MaxPayloadBytes = maxFrameSize
			} else {
				ws.MaxPayloadBytes = maxCommandSize
			}
			job.stream.serve(ws, player)
		},
	}.ServeHTTP(w, r)
}

func (s *relay) serve(ws *websocket.Conn, player bool) {
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		return
	default:
	}
	if player {
		if s.player != nil {
			s.mu.Unlock()
			return
		}
		s.player = ws
	} else {
		if s.browser != nil {
			s.mu.Unlock()
			return
		}
		s.browser = ws
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if player {
			s.player = nil
		} else {
			s.browser = nil
		}
		s.mu.Unlock()
		if !player {
			select {
			case s.commands <- []byte(`{"action":"release-input"}`):
			default:
				s.close()
			}
		}
	}()
	ended := make(chan struct{})
	defer close(ended)
	go func() {
		defer ws.Close()
		for {
			var data []byte
			binaryFrame := false
			if player {
				select {
				case <-ended:
					return
				case <-s.done:
					return
				case data = <-s.commands:
				}
			} else {
				select {
				case <-ended:
					return
				case <-s.done:
					return
				case data = <-s.replies:
				case data = <-s.frames:
					binaryFrame = true
				}
			}
			ws.SetWriteDeadline(time.Now().Add(2 * time.Second))
			var err error
			if binaryFrame {
				err = websocket.Message.Send(ws, data)
			} else {
				err = websocket.Message.Send(ws, string(data))
			}
			if !player {
				s.buffers <- data
			}
			if err != nil {
				return
			}
		}
	}()
	var incoming []byte
	defer func() {
		if incoming != nil {
			s.buffers <- incoming
		}
	}()
	for {
		var data []byte
		if player {
			select {
			case incoming = <-s.buffers:
			case <-s.done:
				return
			}
			var err error
			incoming, err = readPlayerPacket(ws, incoming)
			if err != nil {
				return
			}
			data = incoming
		} else {
			if websocket.Message.Receive(ws, &data) != nil {
				return
			}
		}
		if player {
			if len(data) > 0 && data[0] == 1 {
				if !validFrame(data) {
					return
				}
				select {
				case s.frames <- data:
					incoming = nil
				case <-s.done:
					return
				}
			} else {
				if len(data) > 65536 || !json.Valid(data) {
					return
				}
				select {
				case s.replies <- data:
					incoming = nil
				case <-s.done:
					return
				}
			}
		} else {
			if len(data) > maxCommandSize {
				return
			}
			var command Command
			if json.Unmarshal(data, &command) != nil {
				return
			}
			switch command.Action {
			case "toggle", "flag", "stop", "frame", "key", "mouse", "text", "release-input", "edit", "monitor", "answer", "state":
			default:
				return
			}
			select {
			case s.commands <- data:
			default:
				return
			}
		}
	}
}

/*
Player packets carry their length so large payloads can be read into reusable
storage. The browser still receives the same RGBA frames and JSON replies.
*/
func readPlayerPacket(r io.Reader, storage []byte) ([]byte, error) {
	if cap(storage) < 4 {
		storage = make([]byte, 4)
	}
	if _, err := io.ReadFull(r, storage[:4]); err != nil {
		return storage, err
	}
	size := binary.LittleEndian.Uint32(storage[:4])
	if size == 0 || size > maxFrameSize {
		return storage, errors.New("invalid player packet size")
	}
	if cap(storage) < int(size) {
		storage = make([]byte, size)
	}
	storage = storage[:size]
	_, err := io.ReadFull(r, storage)
	return storage, err
}

func validFrame(data []byte) bool {
	if len(data) < 9 || data[0] != 1 {
		return false
	}
	w, h := int(binary.LittleEndian.Uint32(data[1:5])), int(binary.LittleEndian.Uint32(data[5:9]))
	return w > 0 && h > 0 && w <= maxFrameEdge && h <= maxFrameEdge && len(data) == 9+w*h*4
}

func ConnectRuntime(ctx context.Context, address string, embedded bool) (*Peer, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "ws" || u.Hostname() != "127.0.0.1" || u.Port() == "" || len(u.Fragment) != 64 {
		return nil, errors.New("invalid editor runtime connection")
	}
	token := u.Fragment
	u.Fragment = ""
	config, err := websocket.NewConfig(u.String(), "http://127.0.0.1")
	if err != nil {
		return nil, err
	}
	config.Protocol = []string{"gowarp-preview"}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	ws, err := websocket.NewClient(config, conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err = websocket.JSON.Send(ws, map[string]string{"token": token}); err != nil {
		ws.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	ws.MaxPayloadBytes = maxCommandSize
	p := &Peer{Commands: make(chan Command, 64), frames: make(chan []byte, 2), replies: make(chan []byte, 32), free: make(chan []byte, 2), done: make(chan struct{}), ws: ws, Embedded: embedded}
	p.free <- nil
	p.free <- nil
	go p.read()
	go p.write()
	go func() {
		select {
		case <-ctx.Done():
			p.Close()
		case <-p.done:
		}
	}()
	return p, nil
}

func (p *Peer) Close() {
	p.once.Do(func() {
		close(p.done)
		p.ws.Close()
	})
}

func (p *Peer) Done() <-chan struct{} {
	return p.done
}

func (p *Peer) read() {
	defer p.Close()
	for {
		var command Command
		if websocket.JSON.Receive(p.ws, &command) != nil {
			return
		}
		if command.Action == "frame" {
			if command.Width > 0 && command.Height > 0 {
				p.frameSize.Store(uint32(min(command.Width, maxFrameEdge))<<16 | uint32(min(command.Height, maxFrameEdge)))
			}
			if p.Embedded {
				count := int32(max(1, min(command.Frames, 2)))
				for {
					pending := p.wanted.Load()
					if p.wanted.CompareAndSwap(pending, min(2, pending+count)) {
						break
					}
				}
			}
			continue
		}
		select {
		case p.Commands <- command:
		case <-p.done:
			return
		}
	}
}

func (p *Peer) write() {
	defer p.Close()
	for {
		var data []byte
		frame := false
		select {
		case <-p.done:
			return
		case data = <-p.replies:
		case data = <-p.frames:
			frame = true
		}
		p.ws.SetWriteDeadline(time.Now().Add(2 * time.Second))
		err := websocket.Message.Send(p.ws, data)
		if frame {
			p.free <- data
		}
		if err != nil {
			return
		}
	}
}

func (p *Peer) Reply(reply Reply) {
	if len(reply.Value) > 8192 {
		reply.Value = strings.ToValidUTF8(reply.Value[:8192], "") + "…"
	}
	if len(reply.Error) > 4096 {
		reply.Error = strings.ToValidUTF8(reply.Error[:4096], "") + "…"
	}
	data, err := json.Marshal(reply)
	if err != nil {
		return
	}
	packet := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(packet, uint32(len(data)))
	copy(packet[4:], data)
	select {
	case p.replies <- packet:
	default:
		p.Close()
	}
}

func (p *Peer) WantsFrame() bool {
	return p.Embedded && p.wanted.Load() > 0
}

func (p *Peer) FrameSize(width, height int) (int, int) {
	if width <= 0 || height <= 0 {
		return 0, 0
	}
	if size := p.frameSize.Load(); size != 0 {
		return int(size >> 16), int(size & 0xffff)
	}
	scale := min(1, 960/float64(width), 960/float64(height))
	return max(1, int(float64(width)*scale)), max(1, int(float64(height)*scale))
}

func (p *Peer) Frame(width, height int, read func([]byte)) {
	if !p.WantsFrame() || width < 1 || height < 1 || width > maxFrameEdge || height > maxFrameEdge {
		return
	}
	var data []byte
	select {
	case data = <-p.free:
	default:
		return
	}
	p.wanted.Add(-1)
	size := 13 + width*height*4
	if cap(data) < size {
		data = make([]byte, size)
	} else {
		data = data[:size]
	}
	binary.LittleEndian.PutUint32(data[:4], uint32(size-4))
	data[4] = 1
	binary.LittleEndian.PutUint32(data[5:9], uint32(width))
	binary.LittleEndian.PutUint32(data[9:13], uint32(height))
	read(data[13:])
	select {
	case p.frames <- data:
	default:
		p.free <- data
	}
}
