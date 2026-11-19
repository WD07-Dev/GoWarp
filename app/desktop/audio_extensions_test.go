package desktop

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gowarp/app/config"
	"gowarp/core/extension"
	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
	tuneshark "gowarp/test/extensions/SPtuneShark3"
)

func TestCatchupDeliversAudioBetweenTicks(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage","variables":{"score":["score",0]},"sounds":[{"name":"beep","rate":120,"sampleCount":1}],"blocks":{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"play"},
		"play":{"opcode":"sound_playuntildone","next":"done","inputs":{"SOUND_MENU":[1,[10,"beep"]]}},
		"done":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
	}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	for _, invalidOutput := range []bool{false, true} {
		t.Run(fmt.Sprint(invalidOutput), func(t *testing.T) {
			lib := &media.Library{Sounds: [][]*media.Sound{{{Rate: 120, Samples: []float32{0, 0}}}}}
			s := config.Default(&p)
			s.FPS = 60
			c := newController(&p, lib, Options{FPS: 60, Settings: &s})
			var before, after int
			c.options.BeforeTick = func(r *vm.Runtime) error {
				before++
				if r.Frames() == 1 {
					if commands := r.DrainAudio(); len(commands) != 0 {
						t.Fatalf("second tick began before delivering audio: %+v", commands)
					}
				}
				return nil
			}
			c.options.AfterTick = func(*vm.Runtime) error {
				after++
				return nil
			}
			output := newAudioOutput(lib, true)
			if invalidOutput {
				output.assets = &media.Library{}
			}
			defer output.close()
			g := &game{controller: c, audio: output, ctx: context.Background(), updateBudget: time.Second}
			c.credit = 1
			err := g.advanceController(Input{})
			if invalidOutput {
				if err == nil || c.vm.Frames() != 1 || before != 1 || after != 1 {
					t.Fatalf("audio failure did not stop catch-up: err=%v frames=%d hooks=%d/%d", err, c.vm.Frames(), before, after)
				}
				return
			}
			if err != nil || c.vm.Frames() != 2 || before != 2 || after != 2 {
				t.Fatalf("catch-up: err=%v frames=%d hooks=%d/%d", err, c.vm.Frames(), before, after)
			}
		})
	}
}

func TestAudioOutputStartsMusicClock(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(fmt.Sprint(paused), func(t *testing.T) {
			var projectData project.Project
			err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage","variables":{"position":["position",0]},"sounds":[{"name":"music","rate":44100,"sampleCount":44100}],"blocks":{
			"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"import"},
			"import":{"opcode":"SPtuneShark3_importMenu","next":"play","inputs":{"SOUND":[1,[10,"music"]],"NAME":[1,[10,"2"]]}},
			"play":{"opcode":"SPtuneShark3_startSound","next":"loop","inputs":{"NAME":[1,[10,"2"]]}},
			"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"set"]}},
			"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["position","position"]},"inputs":{"VALUE":[2,"clock"]}},
			"clock":{"opcode":"SPtuneShark3_soundProperty","inputs":{"NAME":[1,[10,"2"]],"PROP":[1,[10,"current time"]]}},
			"pauseKey":{"opcode":"event_whenkeypressed","topLevel":true,"fields":{"KEY_OPTION":["p",null]},"next":"pause"},
			"pause":{"opcode":"SPtuneShark3_pauseSound","inputs":{"NAME":[1,[10,"2"]]},"fields":{"UN_PAUSE":["pause",null]}},
			"resumeKey":{"opcode":"event_whenkeypressed","topLevel":true,"fields":{"KEY_OPTION":["r",null]},"next":"resume"},
			"resume":{"opcode":"SPtuneShark3_pauseSound","inputs":{"NAME":[1,[10,"2"]]},"fields":{"UN_PAUSE":["unpause",null]}}
			}}]}`), &projectData)
			if err != nil {
				t.Fatal(err)
			}
			r := vm.New(&projectData)
			if err := r.SetExtensions(extension.Bundle{tuneshark.New}); err != nil {
				t.Fatal(err)
			}
			r.ConfigureAudio([][]float64{{1}})
			sound := &media.Sound{Rate: sampleRate, Samples: make([]float32, sampleRate*2)}
			output := newAudioOutput(&media.Library{Sounds: [][]*media.Sound{{sound}}}, false)
			defer output.close()
			tick := func() {
				t.Helper()
				if err := r.Tick(context.Background(), 60, false); err != nil {
					t.Fatal(err)
				}
			}
			tick()
			commands := r.DrainAudio()
			if len(commands) != 1 || commands[0].Kind != "play" {
				t.Fatalf("missing play: %+v", commands)
			}
			tick()
			if got := r.Snapshot()[0].Variables["position"].Value; got != float64(0) {
				t.Fatalf("clock advanced before output: %v", got)
			}
			if paused {
				if err := output.play(r, commands[0], sound, true); err != nil {
					t.Fatal(err)
				}
				if err := output.poll(r); err != nil {
					t.Fatal(err)
				}
				tick()
				if got := r.Snapshot()[0].Variables["position"].Value; got != float64(0) {
					t.Fatalf("clock advanced while waiting paused: %v", got)
				}
				if err := output.apply(r, []vm.AudioCommand{{Kind: "resume", ID: commands[0].ID}}); err != nil {
					t.Fatal(err)
				}
			} else if err := output.apply(r, commands); err != nil {
				t.Fatal(err)
			}
			if err := output.poll(r); err != nil {
				t.Fatal(err)
			}
			tick()
			tick()
			if got := r.Snapshot()[0].Variables["position"].Value; extension.Number(got) <= 0 {
				t.Fatalf("output did not start music clock: %v", got)
			}
			press := func(key string) {
				t.Helper()
				if err := r.QueueEvents([]vm.InputEvent{{Frame: r.Frames(), Kind: "key", Key: key, Down: true}}); err != nil {
					t.Fatal(err)
				}
				tick()
			}
			press("p")
			if err := output.apply(r, r.DrainAudio()); err != nil {
				t.Fatal(err)
			}
			tick()
			pausedPosition := r.Snapshot()[0].Variables["position"].Value
			press("r")
			tick()
			if got := r.Snapshot()[0].Variables["position"].Value; got != pausedPosition {
				t.Fatalf("music clock resumed before host delivery: %v -> %v", pausedPosition, got)
			}
			if err := output.apply(r, r.DrainAudio()); err != nil {
				t.Fatal(err)
			}
			tick()
			tick()
			if got := r.Snapshot()[0].Variables["position"].Value; extension.Number(got) <= extension.Number(pausedPosition) {
				t.Fatalf("host resume did not restart music clock: %v -> %v", pausedPosition, got)
			}
		})
	}
}

func TestURLAudioDoesNotBlockUpdate(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	output := newAudioOutput(&media.Library{}, false)
	defer output.close()
	r := vm.New(&project.Project{})
	returned := make(chan error, 1)
	go func() {
		returned <- output.apply(r, []vm.AudioCommand{{Kind: "play", ID: 1, URL: server.URL}})
	}()
	select {
	case err := <-returned:
		close(release)
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		<-returned
		t.Fatal("game update waited for URL audio to download")
	}
}

func TestMutedURLAudioDoesNotFetch(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	output := newAudioOutput(&media.Library{}, true)
	defer output.close()
	err := output.apply(vm.New(&project.Project{}), []vm.AudioCommand{{Kind: "play", ID: 1, URL: server.URL}})
	if err != nil || requests.Load() != 0 || output.context != nil {
		t.Fatalf("muted URL opened network or device: requests=%d context=%v error=%v", requests.Load(), output.context, err)
	}
}

func TestEmbeddedURLAudioDecode(t *testing.T) {
	wav := urlAudioWAV()
	uri := "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(wav)
	first, err := loadURLSound(context.Background(), uri)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Samples) != 2 || first.Samples[0] != .5 {
		t.Fatalf("embedded PCM did not decode: %#v", first)
	}
	if _, err := loadURLSound(context.Background(), "data:audio/wav;base64,broken"); err == nil {
		t.Fatal("invalid embedded audio accepted")
	}
}

func urlAudioWAV() []byte {
	wav := make([]byte, 46)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], 38)
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 8000)
	binary.LittleEndian.PutUint32(wav[28:], 16000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], 2)
	binary.LittleEndian.PutUint16(wav[44:], 16384)
	return wav
}

func TestURLAudioStopThenReplay(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
			<-r.Context().Done()
			close(cancelled)
			return
		}
		w.Write(urlAudioWAV())
	}))
	defer server.Close()
	output := newAudioOutput(&media.Library{}, false)
	defer output.close()
	r := vm.New(&project.Project{})
	if err := output.apply(r, []vm.AudioCommand{{Kind: "play", ID: 1, URL: server.URL}}); err != nil {
		t.Fatal(err)
	}
	waitURLAudio(t, started)
	if err := output.apply(r, []vm.AudioCommand{{Kind: "stop", ID: 1}}); err != nil {
		t.Fatal(err)
	}
	waitURLAudio(t, cancelled)
	if err := output.apply(r, []vm.AudioCommand{{Kind: "play", ID: 2, URL: server.URL}, {Kind: "pause", ID: 2}}); err != nil {
		t.Fatal(err)
	}
	waitURLAudio(t, output.loadingURL.done)
	if err := output.poll(r); err != nil {
		t.Fatalf("cancelled request affected new playback: %v", err)
	}
	if output.loadingURL == nil {
		t.Fatal("new playback did not start a fresh request")
	}
	waitURLAudio(t, output.loadingURL.done)
	if err := output.poll(r); err != nil {
		t.Fatal(err)
	}
	if _, stale := output.players[1]; stale || len(output.players) != 1 || !output.players[2].paused || requests.Load() != 2 {
		t.Fatalf("unexpected playback after stop/replay: players=%v requests=%d", output.players, requests.Load())
	}
}

func TestURLAudioSharesLoadAndAppliesPendingControls(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Write(urlAudioWAV())
	}))
	defer server.Close()
	output := newAudioOutput(&media.Library{}, false)
	defer output.close()
	r := vm.New(&project.Project{})
	rate := .5
	if err := output.apply(r, []vm.AudioCommand{
		{Kind: "play", ID: 1, URL: server.URL, Volume: 100},
		{Kind: "play", ID: 2, URL: server.URL, Volume: 100},
		{Kind: "stop", ID: 1},
		{Kind: "pause", ID: 2},
		{Kind: "update", ID: 2, Volume: 25, Rate: &rate},
		{Kind: "loop", ID: 2, Loop: true},
	}); err != nil {
		t.Fatal(err)
	}
	waitURLAudio(t, output.loadingURL.done)
	if err := output.poll(r); err != nil {
		t.Fatal(err)
	}
	p, ok := output.players[2]
	if !ok || !p.paused || p.player.IsPlaying() || len(output.players) != 1 {
		t.Fatalf("pending controls lost: players=%v", output.players)
	}
	buffer := make([]byte, 256)
	n, err := p.stream.Read(buffer)
	if err != nil || n != len(buffer) {
		t.Fatalf("pending loop was lost: bytes=%d err=%v", n, err)
	}
	if value := math.Float32frombits(binary.LittleEndian.Uint32(buffer)); value != .125 {
		t.Fatalf("pending volume was lost: sample=%v want .125", value)
	}
	if err := output.apply(r, []vm.AudioCommand{{Kind: "play", ID: 3, URL: server.URL}, {Kind: "pause", ID: 3}}); err != nil {
		t.Fatal(err)
	}
	if len(output.players) != 2 || output.loadingURL != nil || requests.Load() != 1 {
		t.Fatalf("replay fetched cached sound: players=%d requests=%d", len(output.players), requests.Load())
	}
}

func TestClosingURLAudioCancelsDownload(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer server.Close()
	output := newAudioOutput(&media.Library{}, false)
	defer output.close()
	r := vm.New(&project.Project{})
	if err := output.apply(r, []vm.AudioCommand{{Kind: "play", ID: 1, URL: server.URL}}); err != nil {
		t.Fatal(err)
	}
	waitURLAudio(t, started)
	output.close()
	waitURLAudio(t, cancelled)
	if err := output.poll(r); err != nil || len(output.players) != 0 || len(output.pendingURLs) != 0 {
		t.Fatalf("closed URL sound survived: error=%v", err)
	}
}

func TestURLAudioReportsDecodeFailure(t *testing.T) {
	output := newAudioOutput(&media.Library{}, false)
	defer output.close()
	r := vm.New(&project.Project{})
	if err := output.apply(r, []vm.AudioCommand{{Kind: "play", ID: 1, URL: "data:audio/wav;base64,broken"}}); err != nil {
		t.Fatal(err)
	}
	waitURLAudio(t, output.loadingURL.done)
	if err := output.poll(r); err == nil {
		t.Fatal("failed URL decode was not reported to the player")
	}
	if len(output.players) != 0 || output.context != nil {
		t.Fatal("failed URL decode opened an audio player")
	}
}

func waitURLAudio(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("URL audio operation did not finish")
	}
}
