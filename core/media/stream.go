package media

import (
	"encoding/binary"
	"io"
	"math"
	"sync"
	"weak"
)

type soundDecode struct {
	done  chan struct{}
	sound *Sound
	err   error
}

func (d *soundDecode) stream(meta *Sound, rate int) (*Stream, error) {
	select {
	case <-d.done:
		if d.err != nil {
			return nil, d.err
		}
		meta.decoded = weak.Make(d.sound)
		return NewStream(d.sound, rate), nil
	default:
		stream := NewStream(meta, rate)
		stream.frames, stream.end = meta.frames, float64(meta.frames)
		stream.decode = d
		return stream, nil
	}
}

/*
Stream resamples stereo PCM with live volume, pitch and pan controls.
Only the audio reader advances the cursor; host parameter changes are locked.
*/
type Stream struct {
	mu                              sync.Mutex
	parametersMu                    sync.Mutex
	sound                           *Sound
	decode                          *soundDecode
	source                          *cachedAsset
	pcm                             *streamPCM
	wav                             *wavPCM
	frames                          int
	outputRate                      int
	position, step, left, right     float64
	gainLeft, gainRight             float64
	gainTargetLeft, gainTargetRight float64
	gainFrames                      int
	gainReady                       bool
	end                             float64
	loop                            bool
	pending                         [8]byte
	pendingStart, pendingEnd        int
}

func NewStream(sound *Sound, rate int) *Stream {
	s := &Stream{sound: sound, frames: len(sound.Samples) / 2, outputRate: rate, end: float64(len(sound.Samples) / 2)}
	s.Set(100, 0, 0)
	return s
}

func (s *Stream) Set(volume, pitch, pan float64) {
	s.parametersMu.Lock()
	defer s.parametersMu.Unlock()
	s.step = float64(s.sound.Rate) / float64(s.outputRate) * math.Pow(2, math.Max(-360, math.Min(360, pitch))/120)
	gain := math.Max(0, math.Min(100, volume)) / 100
	pan = math.Max(-1, math.Min(1, pan/100))
	s.left = gain * math.Min(1, 1-pan)
	s.right = gain * math.Min(1, 1+pan)
}

// Playback sets a source offset, optional ending time, and looping. Times are seconds.
func (s *Stream) Playback(start, end float64, loop bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	frames := float64(s.frames)
	s.end = frames
	if end > 0 {
		s.end = math.Min(frames, end*float64(s.sound.Rate))
	}
	s.position = math.Max(0, math.Min(s.end, start*float64(s.sound.Rate)))
	s.SetLoop(loop)
	s.gainReady = false
	s.pendingStart, s.pendingEnd = 0, 0
}

func (s *Stream) SetLoop(loop bool) {
	s.parametersMu.Lock()
	defer s.parametersMu.Unlock()
	s.loop = loop
}

func (s *Stream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.playbackClosed() {
		return 0, io.EOF
	}
	sound := s.sound
	if s.decode != nil {
		<-s.decode.done
		if s.decode.err != nil {
			return 0, s.decode.err
		}
		sound = s.decode.sound
		if frames := len(sound.Samples) / 2; frames < s.frames {
			s.frames = frames
			s.end = math.Min(s.end, float64(frames))
			s.position = math.Min(s.position, s.end)
		}
	}
	/*
		Controls apply to the next buffer without making the game thread wait
		for compressed audio decoding or seeking under the playback lock.
	*/
	s.parametersMu.Lock()
	step, left, right, loop := s.step, s.left, s.right, s.loop
	s.parametersMu.Unlock()
	if !s.gainReady {
		s.gainLeft, s.gainRight = left, right
		s.gainTargetLeft, s.gainTargetRight = left, right
		s.gainFrames, s.gainReady = 0, true
	} else if left != s.gainTargetLeft || right != s.gainTargetRight {
		s.gainTargetLeft, s.gainTargetRight = left, right
		/*
			Smooth live gain changes over 5 ms to avoid a waveform discontinuity.
			Initial playback retains its requested gain without delaying the attack.
		*/
		s.gainFrames = max(1, s.outputRate/200)
	}
	n := 0
	for n < len(p) {
		if s.pendingStart < s.pendingEnd {
			copied := copy(p[n:], s.pending[s.pendingStart:s.pendingEnd])
			n += copied
			s.pendingStart += copied
			continue
		}
		frames := s.frames
		if loop && s.end > 0 && s.position >= s.end {
			s.position = math.Mod(s.position, s.end)
		}
		if s.position >= s.end {
			if n > 0 {
				return n, nil
			}
			return 0, io.EOF
		}
		at := int(s.position)
		next := at + 1
		if next >= frames {
			next = at
		}
		fraction := s.position - float64(at)
		var samples [4]float32
		if s.pcm != nil {
			var err error
			samples, err = s.pcm.pair(at, frames)
			if err != nil && s.playbackClosed() {
				// Cancellation must not enter the source EOF/loop retry path.
				return n, io.EOF
			}
			if s.pcm.eof {
				s.frames = s.pcm.end
				s.end = math.Min(s.end, float64(s.frames))
				if err == io.EOF {
					continue
				}
			}
			if err != nil {
				return n, err
			}
		} else if s.wav != nil {
			samples = s.wav.pair(at, next)
		} else {
			samples = [4]float32{sound.Samples[at*2], sound.Samples[at*2+1], sound.Samples[next*2], sound.Samples[next*2+1]}
		}
		if s.gainFrames > 0 {
			s.gainLeft += (s.gainTargetLeft - s.gainLeft) / float64(s.gainFrames)
			s.gainRight += (s.gainTargetRight - s.gainRight) / float64(s.gainFrames)
			s.gainFrames--
		}
		for channel, gain := range [2]float64{s.gainLeft, s.gainRight} {
			a, b := float64(samples[channel]), float64(samples[channel+2])
			value := float32((a + (b-a)*fraction) * gain)
			binary.LittleEndian.PutUint32(s.pending[channel*4:], math.Float32bits(value))
		}
		s.position += step
		s.pendingStart, s.pendingEnd = 0, len(s.pending)
	}
	return n, nil
}

/*
Each music player owns a decoder and a small PCM window, avoiding whole-track decoding on game
updates.
*/
type streamPCM struct {
	reader        io.ReadSeeker
	data          []byte
	offset, valid int
	end           int
	eof           bool
}

func (p *streamPCM) pair(at, frames int) ([4]float32, error) {
	if p.eof {
		frames = min(frames, p.end)
		if at >= frames {
			return [4]float32{}, io.EOF
		}
	}
	end := p.offset + p.valid/8
	if at < p.offset || min(at+2, frames) > end {
		carry := 0
		if at == end-1 && p.valid >= 8 {
			// Keep the interpolation sample at a chunk boundary.
			copy(p.data, p.data[p.valid-8:p.valid])
			carry = 8
		} else if at != end {
			if _, err := p.reader.Seek(int64(at)*8, io.SeekStart); err != nil {
				return [4]float32{}, err
			}
		}
		p.offset = at
		want := min(len(p.data), (frames-at)*8)
		n, err := io.ReadFull(p.reader, p.data[carry:want])
		p.valid = carry + n
		// Decoder Length is an estimate; accept complete samples at EOF even when shorter.
		if (err == io.EOF || err == io.ErrUnexpectedEOF) && p.valid%8 == 0 {
			p.end, p.eof = at+p.valid/8, true
			if p.valid == 0 {
				return [4]float32{}, io.EOF
			}
			err = nil
		}
		if err != nil {
			return [4]float32{}, err
		}
	}
	first := (at - p.offset) * 8
	next := min(first+8, p.valid-8)
	var samples [4]float32
	for i, offset := range [4]int{first, first + 4, next, next + 4} {
		value := math.Float32frombits(binary.LittleEndian.Uint32(p.data[offset:]))
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			value = 0
		}
		samples[i] = value
	}
	return samples, nil
}
