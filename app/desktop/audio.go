package desktop

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2/audio"

	"gowarp/core/media"
	"gowarp/core/vm"
)

const sampleRate = 44100

type playing struct {
	player  *audio.Player
	stream  *media.Stream
	paused  bool
	pending bool
}

type pendingURLSound struct {
	command vm.AudioCommand
	paused  bool
}

type urlSoundLoad struct {
	url       string
	done      chan struct{}
	cancel    context.CancelFunc
	cancelled bool
	sound     *media.Sound
	err       error
}

type audioOutput struct {
	assets      *media.Library
	mute        bool
	context     *audio.Context
	players     map[int]playing
	urls        map[string]*media.Sound
	urlBytes    int
	pendingURLs map[int]pendingURLSound
	loadingURL  *urlSoundLoad
}

func newAudioOutput(lib *media.Library, mute bool) *audioOutput {
	return &audioOutput{
		assets:      lib,
		mute:        mute,
		players:     map[int]playing{},
		urls:        map[string]*media.Sound{},
		pendingURLs: map[int]pendingURLSound{},
	}
}

func (a *audioOutput) apply(r *vm.Runtime, commands []vm.AudioCommand) error {
	for _, cmd := range commands {
		switch cmd.Kind {
		case "play":
			if cmd.URL == "" && (cmd.Source < 0 || cmd.Source >= len(a.assets.Sounds) || cmd.Sound < 0 || cmd.Sound >= len(a.assets.Sounds[cmd.Source])) {
				return fmt.Errorf("invalid sound asset %d/%d", cmd.Source, cmd.Sound)
			}
			if a.mute {
				r.AudioStarted(cmd.ID)
				r.AudioFinished(cmd.ID)
				continue
			}
			sound := a.urls[cmd.URL]
			if cmd.URL != "" && sound == nil {
				a.pendingURLs[cmd.ID] = pendingURLSound{command: cmd}
				continue
			}
			if err := a.play(r, cmd, sound, false); err != nil {
				return err
			}
		case "update":
			if pending, ok := a.pendingURLs[cmd.ID]; ok {
				pending.command.Volume, pending.command.Pitch, pending.command.Pan = cmd.Volume, cmd.Pitch, cmd.Pan
				if cmd.Rate != nil {
					pending.command.Rate = cmd.Rate
				}
				a.pendingURLs[cmd.ID] = pending
			}
			if p, ok := a.players[cmd.ID]; ok {
				p.stream.Set(cmd.Volume, cmd.Pitch, cmd.Pan)
				if cmd.Rate != nil {
					p.stream.SetRate(*cmd.Rate)
				}
			}
		case "loop":
			if pending, ok := a.pendingURLs[cmd.ID]; ok {
				pending.command.Loop = cmd.Loop
				a.pendingURLs[cmd.ID] = pending
			}
			if p, ok := a.players[cmd.ID]; ok {
				p.stream.SetLoop(cmd.Loop)
			}
		case "pause":
			if pending, ok := a.pendingURLs[cmd.ID]; ok {
				pending.paused = true
				a.pendingURLs[cmd.ID] = pending
			}
			if p, ok := a.players[cmd.ID]; ok {
				if !p.pending {
					p.player.Pause()
				}
				p.paused = true
				a.players[cmd.ID] = p
			}
		case "resume":
			if pending, ok := a.pendingURLs[cmd.ID]; ok {
				pending.paused = false
				a.pendingURLs[cmd.ID] = pending
			}
			if p, ok := a.players[cmd.ID]; ok {
				p.paused = false
				if !p.pending {
					p.player.Play()
					r.AudioStarted(cmd.ID)
				}
				a.players[cmd.ID] = p
			}
		case "stop":
			delete(a.pendingURLs, cmd.ID)
			if p, ok := a.players[cmd.ID]; ok {
				p.stream.Close()
				err := p.player.Close()
				if err != nil {
					return err
				}
				delete(a.players, cmd.ID)
			}
			r.AudioFinished(cmd.ID)
		}
	}
	a.startURLLoad()
	return nil
}

func (a *audioOutput) play(r *vm.Runtime, cmd vm.AudioCommand, sound *media.Sound, paused bool) error {
	if a.context == nil {
		a.context = audio.CurrentContext()
		if a.context == nil {
			a.context = audio.NewContext(sampleRate)
		}
	}
	var err error
	var stream *media.Stream
	if sound == nil {
		stream, err = a.assets.Stream(cmd.Source, cmd.Sound, sampleRate)
		if err != nil {
			return err
		}
	} else {
		stream = media.NewStream(sound, sampleRate)
	}
	stream.Set(cmd.Volume, cmd.Pitch, cmd.Pan)
	if cmd.Rate != nil {
		stream.SetRate(*cmd.Rate)
	}
	stream.Playback(cmd.Start, cmd.End, cmd.Loop)
	stream.StartReadAhead()
	player, err := a.context.NewPlayerF32(stream)
	if err != nil {
		stream.Close()
		return fmt.Errorf("audio player: %w", err)
	}
	player.SetBufferSize(50 * time.Millisecond)
	pending := paused || !stream.Ready()
	a.players[cmd.ID] = playing{player: player, stream: stream, pending: pending, paused: paused}
	if !pending {
		player.Play()
		r.AudioStarted(cmd.ID)
	}
	return nil
}

func (a *audioOutput) poll(r *vm.Runtime) error {
	if err := a.finishURLLoad(r); err != nil {
		return err
	}
	for id, p := range a.players {
		if p.paused {
			continue
		}
		if p.pending {
			if p.stream.Ready() {
				p.player.Play()
				r.AudioStarted(id)
				p.pending = false
				a.players[id] = p
			}
			continue
		}
		if !p.player.IsPlaying() {
			p.stream.Close()
			p.player.Close()
			delete(a.players, id)
			r.AudioFinished(id)
		}
	}
	return nil
}

func (a *audioOutput) close() {
	if a.loadingURL != nil {
		a.loadingURL.cancel()
		a.loadingURL = nil
	}
	clear(a.pendingURLs)
	for id, p := range a.players {
		p.stream.Close()
		p.player.Close()
		delete(a.players, id)
	}
}

/*
Only one download/decoder runs at a time. All playback state stays on the
update thread; closing done publishes the worker's result.
*/
func (a *audioOutput) startURLLoad() {
	if a.loadingURL != nil {
		for _, pending := range a.pendingURLs {
			if pending.command.URL == a.loadingURL.url {
				return
			}
		}
		a.loadingURL.cancel()
		a.loadingURL.cancelled = true
		return
	}
	var first vm.AudioCommand
	found := false
	for _, pending := range a.pendingURLs {
		if !found || pending.command.ID < first.ID {
			first = pending.command
			found = true
		}
	}
	if !found {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	load := &urlSoundLoad{url: first.URL, done: make(chan struct{}), cancel: cancel}
	a.loadingURL = load
	go func() {
		defer close(load.done)
		load.sound, load.err = loadURLSound(ctx, load.url)
	}()
}

func (a *audioOutput) finishURLLoad(r *vm.Runtime) error {
	load := a.loadingURL
	if load == nil {
		return nil
	}
	select {
	case <-load.done:
	default:
		return nil
	}
	a.loadingURL = nil
	load.cancel()
	if load.cancelled {
		a.startURLLoad()
		return nil
	}
	cached := false
	for id, pending := range a.pendingURLs {
		if pending.command.URL != load.url {
			continue
		}
		if load.err != nil {
			return load.err
		}
		if !cached {
			a.cacheURLSound(load.url, load.sound)
			cached = true
		}
		if err := a.play(r, pending.command, load.sound, pending.paused); err != nil {
			return err
		}
		delete(a.pendingURLs, id)
	}
	a.startURLLoad()
	return nil
}

func (a *audioOutput) cacheURLSound(path string, sound *media.Sound) {
	const limit = 64 << 20
	size := len(sound.Samples)*4 + len(path)
	if a.urlBytes+size > limit {
		clear(a.urls)
		a.urlBytes = 0
	}
	if size <= limit {
		a.urls[path] = sound
		a.urlBytes += size
	}
}

func loadURLSound(ctx context.Context, path string) (*media.Sound, error) {
	const limit = 64 << 20
	var data []byte
	var err error
	format := ""
	if strings.HasPrefix(path, "data:") {
		header, payload, ok := strings.Cut(path, ",")
		if !ok {
			return nil, fmt.Errorf("invalid audio data URL")
		}
		if len(payload) > limit*4/3+4 {
			return nil, fmt.Errorf("excessive audio data URL")
		}
		if strings.HasSuffix(header, ";base64") {
			data, err = base64.StdEncoding.DecodeString(payload)
		} else {
			var decoded string
			decoded, err = url.PathUnescape(payload)
			data = []byte(decoded)
		}
		if strings.Contains(header, "mpeg") || strings.Contains(header, "mp3") {
			format = "mp3"
		}
	} else {
		address, parseErr := url.Parse(path)
		if parseErr != nil || (address.Scheme != "http" && address.Scheme != "https") {
			return nil, fmt.Errorf("unsupported audio URL")
		}
		client := http.Client{Timeout: 5 * time.Second}
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
		if requestErr != nil {
			return nil, fmt.Errorf("fetch audio: %w", requestErr)
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			return nil, fmt.Errorf("fetch audio: %w", requestErr)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch audio: HTTP %d", response.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
		if strings.Contains(response.Header.Get("Content-Type"), "mpeg") || strings.HasSuffix(strings.ToLower(address.Path), ".mp3") {
			format = "mp3"
		}
	}
	if err != nil {
		return nil, fmt.Errorf("decode audio URL: %w", err)
	}
	if len(data) > limit {
		return nil, fmt.Errorf("excessive URL audio")
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		format = "wav"
	}
	if format == "" {
		format = "mp3"
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sound, err := media.DecodeSound(format, data)
	if err != nil {
		return nil, fmt.Errorf("decode URL audio: %w", err)
	}
	return sound, nil
}
