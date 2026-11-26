package media

import "fmt"

/*
streamMP3 defers frame indexing until the audio reader needs the decoder.
Like the decoder itself, it is owned by one stream's reader goroutine.
*/
type streamMP3 struct {
	data    []byte
	rate    int
	length  int64
	decoder *mp3PCM
	err     error
}

func (s *streamMP3) SampleRate() int {
	return s.rate
}
func (s *streamMP3) Length() int64 {
	return s.length
}

func (s *streamMP3) prepare() error {
	if s.decoder == nil && s.err == nil {
		s.decoder, s.err = decodeMP3(s.data)
		s.data = nil
		if s.err != nil {
			// Initialization EOF is invalid input, not successful playback EOF.
			s.err = fmt.Errorf("decode MP3 stream: %w", s.err)
		} else if s.decoder.SampleRate() != s.rate || s.decoder.Length() != s.length {
			s.err = fmt.Errorf("MP3 stream metadata changed since loading")
		}
	}
	return s.err
}

func (s *streamMP3) Read(p []byte) (int, error) {
	if err := s.prepare(); err != nil {
		return 0, err
	}
	return s.decoder.Read(p)
}

func (s *streamMP3) Seek(offset int64, whence int) (int64, error) {
	if err := s.prepare(); err != nil {
		return 0, err
	}
	return s.decoder.Seek(offset, whence)
}
