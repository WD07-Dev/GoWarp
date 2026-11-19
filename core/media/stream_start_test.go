package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"gowarp/core/project"
)

func BenchmarkRepeatedMP3StreamStart(b *testing.B) {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x00})
	data := bytes.Repeat(frame, 15000)
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "music.mp3"), data, 0600); err != nil {
		b.Fatal(err)
	}
	p := &project.Project{Targets: []*project.Target{{Sounds: []project.Sound{{MD5Ext: "music.mp3", DataFormat: "mp3"}}}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer lib.Close()
	if _, err := lib.Stream(0, 0, 44100); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s, err := lib.Stream(0, 0, 44100)
		if err != nil {
			b.Fatal(err)
		}
		s.Close()
	}
}

func TestRepeatedMP3StreamStartDoesNotIndexOnHost(t *testing.T) {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x00})
	data := bytes.Repeat(frame, 15000)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "music.mp3"), data, 0600); err != nil {
		t.Fatal(err)
	}
	p := &project.Project{Targets: []*project.Target{{Sounds: []project.Sound{{MD5Ext: "music.mp3", DataFormat: "mp3"}}}}}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	prepared, err := lib.Stream(0, 0, 44100)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	replayed, err := lib.Stream(0, 0, 44100)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	defer replayed.Close()
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 256<<10 {
		t.Fatalf("repeated music start indexed compressed frames on host: allocated %d bytes", allocated)
	}
	replayed.StartReadAhead()
	if _, err := io.ReadFull(replayed, make([]byte, 4096)); err != nil {
		t.Fatal(err)
	}
}

func TestShortMP3StreamDefersDecodeAndPreservesPlayback(t *testing.T) {
	for _, format := range []string{"mp3", "wav"} {
		t.Run(format, func(t *testing.T) {
			data := readMediaFixture(t, "tone.mp3")
			p := &project.Project{Targets: []*project.Target{{Sounds: []project.Sound{{MD5Ext: "effect.mp3", DataFormat: format}}}}}
			path := writeMediaProject(t, p, map[string][]byte{"effect.mp3": data})
			lib, err := LoadRuntime(context.Background(), path, p, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer lib.Close()
			stream, err := lib.Stream(0, 0, 44100)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			pending := lib.Sounds[0][0].decoding.Value()
			if pending == nil {
				t.Fatal("cold short MP3 decoded fully on the game thread")
			}
			sound, err := DecodeSound("mp3", data)
			if err != nil {
				t.Fatal(err)
			}
			eager := NewStream(sound, 44100)
			defer eager.Close()
			stream.Playback(0.08, 0.1, true)
			eager.Playback(0.08, 0.1, true)
			stream.Set(65, 0, 0)
			eager.Set(65, 0, 0)
			stream.StartReadAhead()
			got, want := make([]byte, 4096), make([]byte, 4096)
			for i := 0; i < 8; i++ {
				n, err := stream.Read(got)
				wn, we := eager.Read(want)
				if n != wn || err != we || !bytes.Equal(got[:n], want[:wn]) {
					t.Fatalf("short MP3 playback differed at buffer %d: %d/%v != %d/%v", i, n, err, wn, we)
				}
			}
			cached, err := lib.Stream(0, 0, 44100)
			if err != nil {
				t.Fatal(err)
			}
			defer cached.Close()
			if cached.decode != nil || cached.sound != pending.sound {
				t.Fatal("repeated effect did not share its background-decoded PCM")
			}
			lib.cache.limit = 1
			lib.cache.put(&cachedAsset{key: "eviction", bytes: 1})
			overlap, err := lib.Stream(0, 0, 44100)
			if err != nil {
				t.Fatal(err)
			}
			defer overlap.Close()
			if overlap.sound != cached.sound {
				t.Fatal("cache eviction discarded PCM still held by an active voice")
			}
		})
	}
}

func TestPendingSoundDecodeDoesNotBlockControls(t *testing.T) {
	pending := &soundDecode{done: make(chan struct{})}
	meta := &Sound{Rate: 44100, frames: 2}
	stream, err := pending.stream(meta, 44100)
	if err != nil {
		t.Fatal(err)
	}
	if stream.Ready() {
		t.Fatal("sound became playable before samples were decoded")
	}
	finished := make(chan struct{})
	go func() {
		stream.Set(50, 0, 0)
		stream.SetLoop(true)
		stream.Close()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		close(pending.done)
		t.Fatal("pending decode blocked playback controls")
	}
	pending.err = fmt.Errorf("decode failed")
	close(pending.done)
	if !stream.Ready() {
		t.Fatal("decode error was not made available to the audio host")
	}
	if _, err := pending.stream(meta, 44100); err != pending.err {
		t.Fatalf("lost decode error: %v", err)
	}
}

func TestShortMP3WithTruncatedLastFrame(t *testing.T) {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x00})
	data := append(bytes.Repeat(frame, 100), frame[:16]...)
	p := &project.Project{Targets: []*project.Target{{Sounds: []project.Sound{{MD5Ext: "effect.mp3", DataFormat: "mp3"}}}}}
	path := writeMediaProject(t, p, map[string][]byte{"effect.mp3": data})
	lib, err := LoadRuntime(context.Background(), path, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	decoded, err := DecodeSound("mp3", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Samples)/2 >= lib.Sounds[0][0].frames {
		t.Fatal("fixture did not overestimate the final frame")
	}
	stream, err := lib.Stream(0, 0, 44100)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	got, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	eager := NewStream(decoded, 44100)
	defer eager.Close()
	want, err := io.ReadAll(eager)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("truncated MP3 changed playable samples")
	}
	pending := &soundDecode{done: make(chan struct{})}
	looped, err := pending.stream(lib.Sounds[0][0], 44100)
	if err != nil {
		t.Fatal(err)
	}
	defer looped.Close()
	start := lib.Sounds[0][0].Duration()
	looped.Playback(start, 0, true)
	eager.Playback(start, 0, true)
	pending.sound = decoded
	close(pending.done)
	got, want = make([]byte, 4096), make([]byte, 4096)
	if _, err := io.ReadFull(looped, got); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(eager, want); err != nil {
		t.Fatal(err)
	}
	if looped.position != eager.position || !bytes.Equal(got, want) {
		t.Fatal("truncated looping sound did not clamp its initial position")
	}
}

func TestRepeatedMP3ReadAheadMatchesPreparedPlayback(t *testing.T) {
	p := &project.Project{Targets: []*project.Target{{Sounds: []project.Sound{{MD5Ext: "music.mp3", DataFormat: "mp3"}}}}}
	path := writeMediaProject(t, p, map[string][]byte{"music.mp3": readMediaFixture(t, "long-tone.mp3")})
	lib, err := LoadRuntime(context.Background(), path, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	prepared, err := lib.Stream(0, 0, 44100)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	replayed, err := lib.Stream(0, 0, 44100)
	if err != nil {
		t.Fatal(err)
	}
	defer replayed.Close()
	for _, stream := range []*Stream{prepared, replayed} {
		stream.Playback(0.08, 0.1, true)
		stream.StartReadAhead()
	}
	got, want := make([]byte, 4096), make([]byte, 4096)
	for i := 0; i < 8; i++ {
		n, err := replayed.Read(got)
		wn, we := prepared.Read(want)
		if n != wn || err != we || !bytes.Equal(got[:n], want[:wn]) {
			t.Fatalf("repeated MP3 offset/loop playback differed at buffer %d: %d/%v != %d/%v", i, n, err, wn, we)
		}
	}
	replayed.Close()
	if n, err := replayed.Read(got); n != 0 || err != io.EOF {
		t.Fatalf("closed stream: %d/%v", n, err)
	}
}

func TestDeferredMP3DecodeErrorsReachReader(t *testing.T) {
	for _, offset := range []int64{0, 8} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			source := &streamMP3{rate: 44100, length: 16}
			reader := newPCMPrefetch(source, offset, source.Length())
			defer reader.Close()
			if n, err := reader.Read(make([]byte, 8)); n != 0 || err == nil || err == io.EOF {
				t.Fatalf("decoder initialization failure became successful end of audio: %d/%v", n, err)
			}
		})
	}
}

func TestDeferredMP3RejectsChangedMetadata(t *testing.T) {
	data := readMediaFixture(t, "long-tone.mp3")
	meta, err := inspectSound("mp3", data)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []struct {
		rate   int
		length int64
	}{
		{rate: meta.Rate + 1, length: int64(meta.frames) * 8},
		{rate: meta.Rate, length: int64(meta.frames+1) * 8},
	} {
		source := &streamMP3{data: data, rate: changed.rate, length: changed.length}
		if n, err := source.Read(make([]byte, 8)); n != 0 || err == nil || err == io.EOF {
			t.Fatalf("changed metadata accepted: %d/%v", n, err)
		}
		if _, err := source.Seek(0, io.SeekStart); err == nil {
			t.Fatal("decoder failure lost on seek")
		}
	}
}

type startupGateReader struct {
	*bytes.Reader
	entered, release chan struct{}
	once             sync.Once
}

func (r *startupGateReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.entered); <-r.release })
	return r.Reader.Read(p)
}

func TestStreamReadyWaitsForInitialReadAhead(t *testing.T) {
	data := make([]byte, 64)
	source := &startupGateReader{Reader: bytes.NewReader(data), entered: make(chan struct{}), release: make(chan struct{})}
	stream := NewStream(&Sound{Rate: 44100}, 44100)
	stream.frames, stream.end = 8, 8
	stream.pcm = &streamPCM{reader: source, data: make([]byte, 32)}
	stream.StartReadAhead()
	defer stream.Close()
	<-source.entered
	if stream.Ready() {
		close(source.release)
		t.Fatal("stream ready before initial decode finished")
	}
	close(source.release)
	reader := stream.pcm.reader.(*pcmPrefetch)
	select {
	case <-reader.ready:
	case <-time.After(time.Second):
		t.Fatal("initial decode never became ready")
	}
	if !stream.Ready() {
		t.Fatal("decoded initial buffer not ready")
	}
	if _, err := io.ReadFull(stream, make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	if !stream.Ready() {
		t.Fatal("readiness lost after playback started")
	}
	if !NewStream(&Sound{Rate: 44100, Samples: []float32{0, 0}}, 44100).Ready() {
		t.Fatal("in-memory sound needs no startup wait")
	}
}
