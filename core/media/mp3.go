package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"

	mp3 "github.com/nicholas-fedor/mp3-decoder"
)

// mp3PCM exposes the decoder's stereo samples as little-endian float32 bytes.
type mp3PCM struct {
	decoder  *mp3.Decoder
	length   int64
	position int64
	buffer   [1152 * 8]byte
	pending  []byte
	err      error
}

func decodeMP3(data []byte) (*mp3PCM, error) {
	decoder, err := mp3.Open(context.Background(), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	frames, known := decoder.Len()
	if !known || frames < 0 || frames > maxStreamSoundBytes/8 {
		return nil, fmt.Errorf("invalid or excessive MP3 length")
	}
	return &mp3PCM{decoder: decoder, length: frames * 8}, nil
}

func (s *mp3PCM) SampleRate() int { return s.decoder.Info().SampleRate }
func (s *mp3PCM) Length() int64   { return s.length }

func (s *mp3PCM) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(s.pending) == 0 {
		if s.err != nil {
			return 0, s.err
		}
		var samples [1152][2]float64
		var n int
		n, s.err = s.decoder.Read(samples[:])
		for i, sample := range samples[:n] {
			binary.LittleEndian.PutUint32(s.buffer[i*8:], math.Float32bits(float32(sample[0])))
			binary.LittleEndian.PutUint32(s.buffer[i*8+4:], math.Float32bits(float32(sample[1])))
		}
		s.pending = s.buffer[:n*8]
		if n == 0 {
			return 0, s.err
		}
	}
	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	s.position += int64(n)
	return n, nil
}

func (s *mp3PCM) Seek(offset int64, whence int) (int64, error) {
	var base int64
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = s.position
	case io.SeekEnd:
		base = s.length
	default:
		return s.position, fmt.Errorf("invalid MP3 seek origin %d", whence)
	}
	if offset < -base || offset > s.length-base {
		return s.position, fmt.Errorf("MP3 seek outside decoded audio")
	}
	position := base + offset
	if position == s.position {
		return position, nil
	}
	if err := s.decoder.Seek(context.Background(), position/8); err != nil {
		return s.position, err
	}
	s.position = position - position%8
	s.pending, s.err = nil, nil
	if skip := position % 8; skip != 0 {
		var discard [8]byte
		if _, err := io.ReadFull(s, discard[:skip]); err != nil {
			return s.position, err
		}
	}
	return s.position, nil
}
