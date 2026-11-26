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

const (
	mp3DecodeBatchFrames    = 1152
	stereoFloat32FrameBytes = 2 * 4
)

// mp3PCM exposes decoded stereo float32 samples as a byte-addressed stream.
type mp3PCM struct {
	decoder     *mp3.Decoder
	lengthBytes int64
	offsetBytes int64
	buffer      [mp3DecodeBatchFrames * stereoFloat32FrameBytes]byte
	pending     []byte
	err         error
}

func openMP3Stream(data []byte) (*mp3PCM, error) {
	decoder, err := mp3.Open(context.Background(), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	frames, known := decoder.Len()
	if !known || frames < 0 || frames > maxStreamSoundBytes/stereoFloat32FrameBytes {
		return nil, fmt.Errorf("invalid or excessive MP3 length")
	}
	return &mp3PCM{decoder: decoder, lengthBytes: frames * stereoFloat32FrameBytes}, nil
}

func (s *mp3PCM) SampleRate() int {
	return s.decoder.Info().SampleRate
}

func (s *mp3PCM) Length() int64 {
	return s.lengthBytes
}

func (s *mp3PCM) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(s.pending) == 0 {
		if s.err != nil {
			return 0, s.err
		}
		var samples [mp3DecodeBatchFrames][2]float64
		var n int
		n, s.err = s.decoder.Read(samples[:])
		for i, sample := range samples[:n] {
			binary.LittleEndian.PutUint32(s.buffer[i*stereoFloat32FrameBytes:], math.Float32bits(float32(sample[0])))
			binary.LittleEndian.PutUint32(s.buffer[i*stereoFloat32FrameBytes+4:], math.Float32bits(float32(sample[1])))
		}
		s.pending = s.buffer[:n*stereoFloat32FrameBytes]
		if n == 0 {
			return 0, s.err
		}
	}
	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	s.offsetBytes += int64(n)
	return n, nil
}

func (s *mp3PCM) Seek(offset int64, whence int) (int64, error) {
	var base int64
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = s.offsetBytes
	case io.SeekEnd:
		base = s.lengthBytes
	default:
		return s.offsetBytes, fmt.Errorf("invalid MP3 seek origin %d", whence)
	}
	if offset < -base || offset > s.lengthBytes-base {
		return s.offsetBytes, fmt.Errorf("MP3 seek outside decoded audio")
	}
	position := base + offset
	if position == s.offsetBytes {
		return position, nil
	}
	if err := s.decoder.Seek(context.Background(), position/stereoFloat32FrameBytes); err != nil {
		return s.offsetBytes, err
	}
	s.offsetBytes = position - position%stereoFloat32FrameBytes
	s.pending, s.err = nil, nil
	if skip := position % stereoFloat32FrameBytes; skip != 0 {
		var discard [stereoFloat32FrameBytes]byte
		if _, err := io.ReadFull(s, discard[:skip]); err != nil {
			return s.offsetBytes, err
		}
	}
	return s.offsetBytes, nil
}
