package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/hajimehoshi/ebiten/v2/audio/vorbis"
	"gowarp/core/project"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/iotest"
	"time"
)

func mediaFixturePath(t *testing.T, name string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(cwd, "..", "..", "test", "fixtures", "media", name)
}

func readMediaFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(mediaFixturePath(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMislabeledVorbis(t *testing.T) {
	data := readMediaFixture(t, "tone.ogg")
	metadata, err := inspectSound("wav", data)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	decoded, err := DecodeSound("wav", data)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	limit := uint64(len(decoded.Samples)*4*2 + 768<<10)
	t.Logf("decoded PCM=%d bytes, allocated=%d bytes", len(decoded.Samples)*4, allocated)
	if allocated > limit {
		t.Errorf("decoding allocated %d bytes for %d bytes of PCM (limit %d)", allocated, len(decoded.Samples)*4, limit)
	}
	if decoded.Rate != 44100 || math.Abs(decoded.Duration()-10) > 1.0/44100 || math.Abs(decoded.Duration()-metadata.Duration()) > 1.0/44100 {
		t.Fatalf("Vorbis metadata and PCM differ: %g vs %g", metadata.Duration(), decoded.Duration())
	}
}

func TestLongMusicMetadata(t *testing.T) {
	metadata, err := inspectSound("mp3", readMediaFixture(t, "long-tone.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Duration() < 348 || metadata.Duration() > 349 {
		t.Fatalf("long music duration = %g", metadata.Duration())
	}
}

func TestReadSoundSamples(t *testing.T) {
	values := []float32{.5, -.25, float32(math.NaN()), float32(math.Inf(1)), 1, -1, 0, .125}
	data := make([]byte, len(values)*4)
	for i, value := range values {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(value))
	}
	want := []float32{.5, -.25, 0, 0, 1, -1, 0, .125}
	for _, hint := range []int64{-1, 0, 8, 32, 64} {
		got, err := readSoundSamples(iotest.OneByteReader(bytes.NewReader(data)), hint)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("hint %d: got %d samples", hint, len(got))
		}
		for i, value := range want {
			if got[i] != value {
				t.Fatalf("hint %d sample %d: got %g want %g", hint, i, got[i], value)
			}
		}
	}
	if _, err := readSoundSamples(bytes.NewReader(data[:len(data)-1]), 0); err == nil {
		t.Fatal("accepted a partial stereo frame")
	}
	if _, err := readSoundSamples(bytes.NewReader(nil), maxSoundBytes+1); err == nil {
		t.Fatal("accepted an excessive decoded size")
	}
	for _, failure := range []error{errors.New("decoder failed"), io.ErrUnexpectedEOF} {
		if _, err := readSoundSamples(failingSoundReader{failure}, 0); !errors.Is(err, failure) {
			t.Fatalf("decoder error lost: got %v, want %v", err, failure)
		}
	}
}

func TestResampleSound(t *testing.T) {
	for _, rate := range []int{22050, 32000, 44100, 48000} {
		t.Run(fmt.Sprint(rate), func(t *testing.T) {
			sound := &Sound{Rate: rate, Samples: make([]float32, rate/10*2)}
			for i := 0; i < len(sound.Samples)/2; i++ {
				sound.Samples[i*2] = .25
				sound.Samples[i*2+1] = -.125
			}
			converted, err := ResampleSound(sound, 44100)
			if err != nil {
				t.Fatal(err)
			}
			if converted.Rate != 44100 || len(converted.Samples) != 8820 {
				t.Fatalf("converted rate=%d samples=%d", converted.Rate, len(converted.Samples))
			}
			if rate == 44100 && converted != sound {
				t.Fatal("matching sample rate copied PCM")
			}
			for i := 64; i < len(converted.Samples)/2-64; i++ {
				if math.Abs(float64(converted.Samples[i*2])-.25) > .001 ||
					math.Abs(float64(converted.Samples[i*2+1])+.125) > .001 {
					t.Fatalf("stereo gain changed at frame %d: %v", i, converted.Samples[i*2:i*2+2])
				}
			}
		})
	}
}

func TestResampleSoundLimit(t *testing.T) {
	sound := &Sound{Rate: 1, Samples: make([]float32, 100)}
	if _, err := ResampleSound(sound, 384000); err == nil {
		t.Fatal("accepted excessive resampled PCM")
	}
	if _, err := ResampleSound(sound, 0); err == nil {
		t.Fatal("accepted zero output sample rate")
	}
}

type failingSoundReader struct{ err error }

func (r failingSoundReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestReadSoundSamplesAllocation(t *testing.T) {
	data := make([]byte, 8<<20)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	samples, err := readSoundSamples(bytes.NewReader(data), int64(len(data)))
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples)*4 != len(data) {
		t.Fatal("decoded length changed")
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > uint64(len(data)+(128<<10)) {
		t.Fatalf("allocated %d bytes for %d bytes of PCM", allocated, len(data))
	}
	t.Logf("PCM=%d bytes, allocated=%d bytes", len(data), allocated)
	runtime.KeepAlive(samples)
}

func TestMusicStreamStartsWithoutFullDecode(t *testing.T) {
	p := &project.Project{Targets: []*project.Target{{IsStage: true, Sounds: []project.Sound{{
		MD5Ext: "long-tone.mp3", DataFormat: "mp3",
	}}}}}
	path := writeMediaProject(t, p, map[string][]byte{"long-tone.mp3": readMediaFixture(t, "long-tone.mp3")})
	lib, err := LoadRuntime(context.Background(), path, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	stream, err := lib.Stream(0, 0, 44100)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4096)
	if n, err := io.ReadFull(stream, data); err != nil || n != len(data) {
		t.Fatalf("first playback: %d %v", n, err)
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("music ready in %s, allocated %d bytes", time.Since(start), allocated)
	if allocated > 1<<20 {
		t.Fatalf("starting music allocated %d bytes before first audio buffer", allocated)
	}
	replayed, err := lib.Stream(0, 0, 44100)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.pcm.reader == stream.pcm.reader {
		t.Fatal("overlapping music players shared a decoder and playback cursor")
	}
	got := make([]byte, len(data))
	if _, err := io.ReadFull(replayed, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("replayed music did not start with the same samples")
	}
	accounted := 0
	for _, element := range lib.cache.entries {
		accounted += element.Value.(*cachedAsset).bytes
	}
	if lib.cache.bytes != accounted || lib.cache.bytes > lib.cache.limit {
		t.Fatal("transferring the prepared decoder corrupted cache accounting")
	}
	lib.cache.limit = 1
	lib.cache.put(&cachedAsset{key: "evict", bytes: 1})
	if err := lib.cache.reader.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	again, err := lib.Stream(0, 0, 44100)
	if err != nil {
		t.Fatalf("active MP3 source was reread after eviction: %v", err)
	}
	if again.pcm.reader == replayed.pcm.reader {
		t.Fatal("reusing encoded MP3 data also reused its decoder")
	}
	if _, err := io.ReadFull(again, got); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("replaying evicted MP3 changed samples: %v", err)
	}

	cold, err := LoadRuntime(context.Background(), path, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cold.Close()
	cold.cache.limit = 1
	cold.cache.put(&cachedAsset{key: "evict", bytes: 1})
	fallback, err := cold.Stream(0, 0, 44100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(fallback, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) || cold.cache.bytes > cold.cache.limit {
		t.Fatal("evicting the prepared decoder changed playback or exceeded the cache budget")
	}
}

func TestCompressedMusicMatchesDecodedPCM(t *testing.T) {
	for _, name := range []string{"tone.mp3", "tone.ogg"} {
		t.Run(name, func(t *testing.T) {
			data := readMediaFixture(t, name)
			sound, err := DecodeSound("wav", data)
			if err != nil {
				t.Fatal(err)
			}
			for _, start := range []float64{0, .13} {
				var decoder io.ReadSeeker
				switch soundFormat("wav", data) {
				case "mp3":
					decoder, err = decodeMP3(data)
				case "ogg":
					decoder, err = vorbis.DecodeF32(bytes.NewReader(data))
				}
				if err != nil {
					t.Fatal(err)
				}
				streamed := NewStream(&Sound{Rate: sound.Rate}, sound.Rate)
				streamed.frames = len(sound.Samples) / 2
				streamed.pcm = &streamPCM{reader: decoder, data: make([]byte, 32<<10)}
				reference := NewStream(sound, sound.Rate)
				for _, s := range []*Stream{streamed, reference} {
					s.Playback(start, .2, true)
				}
				streamed.StartReadAhead()
				defer streamed.Close()
				got, want := make([]byte, sound.Rate*8/2), make([]byte, sound.Rate*8/2)
				if _, err := io.ReadFull(streamed, got); err != nil {
					t.Fatal(err)
				}
				if _, err := io.ReadFull(reference, want); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					for i := 0; i < len(got); i += 4 {
						a, b := math.Float32frombits(binary.LittleEndian.Uint32(got[i:])), math.Float32frombits(binary.LittleEndian.Uint32(want[i:]))
						if a != b {
							t.Fatalf("offset %g sample %d: streamed %g != decoded %g", start, i/4, a, b)
						}
					}
				}
			}
		})
	}
}

func TestLongMP3StreamsBeyondPCMBufferLimit(t *testing.T) {
	/*
		Silent MPEG-1 Layer III frames, 128 kbps at 44.1 kHz. The compressed
		fixture is small, but its decoded stereo float32 PCM exceeds 128 MiB.
	*/
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x00})
	data := bytes.Repeat(frame, maxSoundBytes/(1152*8)+1)
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
	if lib.Sounds[0][0].frames*8 <= maxSoundBytes {
		t.Fatal("fixture did not exceed the eager PCM limit")
	}
	for i := 0; i < 2; i++ {
		stream, err := lib.Stream(0, 0, 44100)
		if err != nil {
			t.Fatal(err)
		}
		if len(stream.sound.Samples) != 0 || stream.pcm == nil || len(stream.pcm.data) > 32<<10 {
			t.Fatal("music did not retain bounded streaming PCM")
		}
		buffer := make([]byte, 4096)
		if n, err := stream.Read(buffer); err != nil || n != len(buffer) {
			t.Fatalf("stream read: %d/%v", n, err)
		}
	}
	if _, err := DecodeSound("mp3", data); err == nil {
		t.Fatal("eager decoding must retain its memory limit")
	}
}
