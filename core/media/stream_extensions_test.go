package media

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"
	"time"
)

type gatedPCMReader struct {
	*bytes.Reader
	entered, release chan struct{}
}

func (r *gatedPCMReader) Read(p []byte) (int, error) {
	close(r.entered)
	<-r.release
	return r.Reader.Read(p)
}

func TestStreamControlsDoNotWaitForMusicDecode(t *testing.T) {
	data := make([]byte, 32)
	for i := 0; i < len(data); i += 4 {
		binary.LittleEndian.PutUint32(data[i:], math.Float32bits(1))
	}
	reader := &gatedPCMReader{Reader: bytes.NewReader(data), entered: make(chan struct{}), release: make(chan struct{})}
	stream := NewStream(&Sound{Rate: 100}, 100)
	stream.frames, stream.end = 4, 4
	stream.pcm = &streamPCM{reader: reader, data: make([]byte, len(data))}
	readDone := make(chan error, 1)
	go func() {
		_, err := stream.Read(make([]byte, 8))
		readDone <- err
	}()
	<-reader.entered
	setDone := make(chan struct{})
	go func() {
		stream.Set(25, 120, 100)
		stream.SetLoop(true)
		close(setDone)
	}()
	blocked := false
	select {
	case <-setDone:
	case <-time.After(time.Second):
		blocked = true
	}
	close(reader.release)
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	<-setDone
	if blocked {
		t.Fatal("stream controls waited for the audio decoder")
	}
	out := make([]byte, 8)
	if n, err := stream.Read(out); n != len(out) || err != nil {
		t.Fatalf("read after control update: %d, %v", n, err)
	}
	left := math.Float32frombits(binary.LittleEndian.Uint32(out))
	right := math.Float32frombits(binary.LittleEndian.Uint32(out[4:]))
	if left != 0 || right != .25 || stream.position != 3 {
		t.Fatalf("next buffer lost controls: left=%g right=%g position=%g", left, right, stream.position)
	}
	if n, err := stream.Read(make([]byte, 32)); n != 32 || err != nil {
		t.Fatalf("loop control did not apply: %d, %v", n, err)
	}
}

func TestStreamGainChangeIsContinuous(t *testing.T) {
	samples := make([]float32, 40)
	for i := range samples {
		samples[i] = 1
	}
	stream := NewStream(&Sound{Rate: 1000, Samples: samples}, 1000)
	frame := make([]byte, 8)
	if _, err := stream.Read(frame); err != nil {
		t.Fatal(err)
	}
	stream.Set(0, 0, 0)
	/*
		Five milliseconds of ramping, independent of Read buffer boundaries
		and repeated updates with an unchanged target volume.
	*/
	for i, want := range []float32{.8, .6, .4, .2, 0} {
		stream.Set(0, 0, 0)
		if _, err := io.ReadFull(stream, frame[:3]); err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadFull(stream, frame[3:]); err != nil {
			t.Fatal(err)
		}
		for channel := 0; channel < 2; channel++ {
			got := math.Float32frombits(binary.LittleEndian.Uint32(frame[channel*4:]))
			if math.Abs(float64(got-want)) > 1e-6 {
				t.Fatalf("frame %d channel %d: gain %g, want %g", i, channel, got, want)
			}
		}
	}
}

func TestStreamOffsetLoopAndEnd(t *testing.T) {
	s := NewStream(&Sound{Rate: 4, Samples: []float32{0, 0, .25, .25, .5, .5, .75, .75}}, 4)
	s.Playback(.5, 0, true)
	data := make([]byte, 40)
	if n, err := s.Read(data); n != 40 || err != nil {
		t.Fatalf("loop read: %d %v", n, err)
	}
	for i, want := range []float32{.5, .75, 0, .25, .5} {
		if got := math.Float32frombits(binary.LittleEndian.Uint32(data[i*8:])); got != want {
			t.Fatalf("frame %d=%g want %g", i, got, want)
		}
	}
	s.Playback(.25, .5, false)
	if n, err := s.Read(data); n != 8 || err != nil {
		t.Fatalf("range read: %d %v", n, err)
	}
	if _, err := s.Read(data); err != io.EOF {
		t.Fatalf("range did not stop: %v", err)
	}
}

func TestMusicPCMWindowMatchesCachedPlayback(t *testing.T) {
	samples := make([]float32, 400*2)
	data := make([]byte, len(samples)*4)
	for i := range samples {
		samples[i] = float32(i%31-15) / 16
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(samples[i]))
	}
	for _, pitch := range []float64{-120, 0, 120} {
		for _, loop := range []bool{false, true} {
			reference := NewStream(&Sound{Rate: 100, Samples: samples}, 130)
			streamed := NewStream(&Sound{Rate: 100}, 130)
			streamed.frames = len(samples) / 2
			streamed.pcm = &streamPCM{reader: bytes.NewReader(data), data: make([]byte, 64)}
			for _, stream := range []*Stream{reference, streamed} {
				stream.Set(73, pitch, -30)
				stream.Playback(.37, 1.83, loop)
			}
			for i := 0; i < 150; i++ {
				size := []int{1, 7, 19, 64, 97}[i%5]
				got, want := make([]byte, size), make([]byte, size)
				n, err := streamed.Read(got)
				wn, we := reference.Read(want)
				if n != wn || err != we || !bytes.Equal(got[:n], want[:wn]) {
					t.Fatalf("pitch=%g loop=%v chunk=%d: streamed %d/%v != cached %d/%v", pitch, loop, i, n, err, wn, we)
				}
				if i == 70 {
					streamed.SetLoop(false)
					reference.SetLoop(false)
				}
			}
		}
	}
}

func TestMusicPCMWindowRejectsTruncatedFrames(t *testing.T) {
	stream := NewStream(&Sound{Rate: 100}, 100)
	stream.frames, stream.end = 10, 10
	stream.pcm = &streamPCM{reader: bytes.NewReader(make([]byte, 9)), data: make([]byte, 32)}
	if _, err := stream.Read(make([]byte, 8)); err != io.ErrUnexpectedEOF {
		t.Fatalf("truncated music returned %v", err)
	}
}

func TestMusicPCMWindowAcceptsShortDecodedLength(t *testing.T) {
	/*
		Compressed stream lengths are estimates. A complete final PCM frame
		followed by EOF must play just like an eagerly decoded sound.
	*/
	for _, frames := range []int{0, 1, 17} {
		samples := make([]float32, frames*2)
		data := make([]byte, len(samples)*4)
		for i := range samples {
			samples[i] = float32(i%7-3) / 4
			binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(samples[i]))
		}
		for _, window := range []int{16, 32, 64, 256} {
			for _, pitch := range []float64{-120, 0, 120} {
				for _, loop := range []bool{false, true} {
					reference := NewStream(&Sound{Rate: 100, Samples: samples}, 130)
					streamed := NewStream(&Sound{Rate: 100}, 130)
					streamed.frames, streamed.end = frames+8, float64(frames+8)
					streamed.pcm = &streamPCM{reader: bytes.NewReader(data), data: make([]byte, window)}
					for _, stream := range []*Stream{reference, streamed} {
						stream.Set(73, pitch, -30)
						stream.Playback(0, 0, loop)
					}
					for chunk := 0; chunk < 20; chunk++ {
						got, want := make([]byte, 19), make([]byte, 19)
						n, err := streamed.Read(got)
						wn, we := reference.Read(want)
						if n != wn || err != we || !bytes.Equal(got[:n], want[:wn]) {
							t.Fatalf("frames=%d window=%d pitch=%g loop=%v chunk=%d: streamed %d/%v != cached %d/%v",
								frames, window, pitch, loop, chunk, n, err, wn, we)
						}
					}
				}
			}
		}
	}
}

func TestStreamReadAheadCloseDoesNotWaitForDecode(t *testing.T) {
	data := make([]byte, 8*(32<<10))
	reader := &gatedPCMReader{Reader: bytes.NewReader(data), entered: make(chan struct{}), release: make(chan struct{})}
	stream := NewStream(&Sound{Rate: 48000}, 44100)
	stream.frames, stream.end = len(data)/8, float64(len(data)/8)
	stream.pcm = &streamPCM{reader: reader, data: make([]byte, 32<<10)}
	stream.StartReadAhead()
	defer stream.Close()
	select {
	case <-reader.entered:
	case <-time.After(time.Second):
		close(reader.release)
		t.Fatal("compressed music was not prepared independently of mixer reads")
	}
	// Closing playback must wake a waiting mixer read even if decoding is busy.
	done := make(chan error, 1)
	go func() {
		_, err := stream.Read(make([]byte, 8))
		done <- err
	}()
	stream.Close()
	select {
	case err := <-done:
		if err != io.EOF {
			t.Fatalf("closed stream returned %v, want normal audio EOF", err)
		}
	case <-time.After(time.Second):
		close(reader.release)
		t.Fatal("stopped playback waited for decoding")
	}
	close(reader.release)
}

func TestStreamReadAheadMatchesPlaybackControls(t *testing.T) {
	samples := make([]float32, 25000*2)
	data := make([]byte, len(samples)*4)
	for i := range samples {
		samples[i] = float32(i%47-23) / 24
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(samples[i]))
	}
	for _, pitch := range []float64{0, 150, -80} {
		reference := NewStream(&Sound{Rate: 48000, Samples: samples}, 44100)
		stream := NewStream(&Sound{Rate: 48000}, 44100)
		stream.frames = len(samples) / 2
		stream.pcm = &streamPCM{reader: bytes.NewReader(data), data: make([]byte, 32<<10)}
		for _, s := range []*Stream{stream, reference} {
			s.Set(73, pitch, -20)
			s.Playback(.13, .43, true)
		}
		stream.StartReadAhead()
		for i := 0; i < 100; i++ {
			if i == 40 {
				stream.Set(15, pitch, 50)
				reference.Set(15, pitch, 50)
			}
			got, want := make([]byte, 4093), make([]byte, 4093)
			n, err := stream.Read(got)
			wn, we := reference.Read(want)
			if n != wn || err != we || !bytes.Equal(got[:n], want[:wn]) {
				stream.Close()
				t.Fatalf("pitch %g read %d: got %d/%v, want %d/%v", pitch, i, n, err, wn, we)
			}
		}
		stream.Close()
	}
}

type readAheadGateReader struct {
	*bytes.Reader
	reads            int
	entered, release chan struct{}
}

func (r *readAheadGateReader) Read(p []byte) (int, error) {
	r.reads++
	if r.reads == 2 {
		close(r.entered)
		<-r.release
	}
	return r.Reader.Read(p)
}

func TestStreamReadAheadServesAudioWhileNextWindowDecodes(t *testing.T) {
	data := make([]byte, 4*pcmPrefetchBytes)
	for i := 0; i < len(data); i += 4 {
		binary.LittleEndian.PutUint32(data[i:], math.Float32bits(.5))
	}
	reader := &readAheadGateReader{Reader: bytes.NewReader(data), entered: make(chan struct{}), release: make(chan struct{})}
	stream := NewStream(&Sound{Rate: 48000}, 48000)
	stream.frames, stream.end = len(data)/8, float64(len(data)/8)
	stream.pcm = &streamPCM{reader: reader, data: make([]byte, 32<<10)}
	stream.StartReadAhead()
	defer stream.Close()
	defer close(reader.release)
	select {
	case <-reader.entered:
	case <-time.After(time.Second):
		t.Fatal("decoder did not run ahead of playback")
	}
	done := make(chan error, 1)
	out := make([]byte, 800)
	go func() {
		_, err := io.ReadFull(stream, out)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil || !bytes.Equal(out, data[:len(out)]) {
			t.Fatalf("ready audio changed while decoder was busy: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("mixer read waited for the next decode despite ready samples")
	}
}

func TestPCMReadAheadEOFSeekAndClose(t *testing.T) {
	data := make([]byte, pcmPrefetchBytes+17)
	for i := range data {
		data[i] = byte(i % 251)
	}
	p := newPCMPrefetch(bytes.NewReader(data), 0, int64(len(data)))
	for _, offset := range []int64{0, 17, int64(len(data)), 13, 0} {
		if _, err := p.Seek(offset, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(p)
		if err != nil || !bytes.Equal(got, data[offset:]) {
			t.Fatalf("offset %d: read %d bytes, %v", offset, len(got), err)
		}
	}
	p.Close()
	select {
	case <-p.done:
	case <-time.After(time.Second):
		t.Fatal("read-ahead worker survived playback close")
	}
}

func TestClosedLoopingStreamReturnsEOFWithBufferedSamples(t *testing.T) {
	stream := NewStream(&Sound{Rate: 48000}, 48000)
	stream.frames = 16000
	stream.pcm = &streamPCM{reader: bytes.NewReader(make([]byte, stream.frames*8)), data: make([]byte, 32<<10)}
	stream.Playback(0, 0, true)
	stream.StartReadAhead()
	if _, err := io.ReadFull(stream, make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	stream.Close()
	if n, err := stream.Read(make([]byte, 8)); n != 0 || err != io.EOF {
		t.Fatalf("closed loop returned %d, %v instead of EOF", n, err)
	}
}

type endpointSeekReader struct{ *bytes.Reader }

func (r endpointSeekReader) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekStart && offset >= r.Size() {
		return 0, io.ErrUnexpectedEOF
	}
	return r.Reader.Seek(offset, whence)
}

func TestPCMReadAheadAtEndDoesNotSeekDecoder(t *testing.T) {
	p := newPCMPrefetch(endpointSeekReader{bytes.NewReader(make([]byte, 8))}, 8, 8)
	defer p.Close()
	if n, err := p.Read(make([]byte, 8)); n != 0 || err != io.EOF {
		t.Fatalf("playback at end touched decoder: %d, %v", n, err)
	}
}
