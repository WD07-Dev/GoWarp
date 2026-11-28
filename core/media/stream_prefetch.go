package media

import (
	"fmt"
	"io"
	"sync"
)

const pcmPrefetchBytes = 32 << 10

type pcmReadRequest struct {
	position   int64
	generation uint64
}

type pcmReadResult struct {
	buffer     []byte
	n          int
	err        error
	generation uint64
}

type pcmPrefetch struct {
	requests         chan pcmReadRequest
	results          chan pcmReadResult
	buffers          chan []byte
	stop             chan struct{}
	done             chan struct{}
	ready            chan struct{}
	closeOnce        sync.Once
	position, length int64
	generation       uint64
	current          pcmReadResult
	used             int
	terminal         error
}

func newPCMPrefetch(source io.ReadSeeker, position, length int64) *pcmPrefetch {
	p := &pcmPrefetch{
		requests: make(chan pcmReadRequest, 1), results: make(chan pcmReadResult, 2),
		buffers: make(chan []byte, 3), stop: make(chan struct{}), done: make(chan struct{}), ready: make(chan struct{}),
		position: position, length: length,
	}
	for i := 0; i < cap(p.buffers); i++ {
		p.buffers <- make([]byte, pcmPrefetchBytes)
	}
	p.requests <- pcmReadRequest{position: position}
	go p.run(source)
	return p
}

func (p *pcmPrefetch) run(source io.ReadSeeker) {
	defer close(p.done)
	request := <-p.requests
	var position int64
	var terminal error
	ready := false
	for {
		select {
		case <-p.stop:
			return
		default:
		}
		if request.position >= p.length {
			position, terminal = request.position, io.EOF
		} else if position != request.position {
			_, terminal = source.Seek(request.position, io.SeekStart)
			position = request.position
		}
		var buffer []byte
		select {
		case <-p.stop:
			return
		case next := <-p.requests:
			request, terminal = next, nil
			continue
		case buffer = <-p.buffers:
		}
		var n int
		if terminal == nil {
			n, terminal = io.ReadFull(source, buffer)
			if terminal == io.ErrUnexpectedEOF {
				terminal = io.EOF
			}
			position += int64(n)
		}
		result := pcmReadResult{buffer: buffer, n: n, err: terminal, generation: request.generation}
		select {
		case <-p.stop:
			return
		case next := <-p.requests:
			p.buffers <- buffer
			request, terminal = next, nil
			continue
		case p.results <- result:
		}
		if !ready {
			close(p.ready)
			ready = true
		}
		if terminal != nil {
			select {
			case <-p.stop:
				return
			case request = <-p.requests:
				terminal = nil
			}
		} else {
			request.position = position
		}
	}
}

func (p *pcmPrefetch) Read(dst []byte) (int, error) {
	select {
	case <-p.stop:
		return 0, io.EOF
	default:
	}
	if len(dst) == 0 {
		return 0, nil
	}
	for p.current.buffer == nil {
		if p.terminal != nil {
			return 0, p.terminal
		}
		select {
		case <-p.stop:
			return 0, io.EOF
		case result := <-p.results:
			if result.generation != p.generation {
				p.buffers <- result.buffer
				continue
			}
			p.current, p.used = result, 0
		}
	}
	n := copy(dst, p.current.buffer[p.used:p.current.n])
	p.used += n
	p.position += int64(n)
	if p.used == p.current.n {
		p.terminal = p.current.err
		p.buffers <- p.current.buffer
		p.current = pcmReadResult{}
	}
	return n, p.terminal
}

func (p *pcmPrefetch) Seek(offset int64, whence int) (int64, error) {
	var position int64
	switch whence {
	case io.SeekStart:
		position = offset
	case io.SeekCurrent:
		position = p.position + offset
	case io.SeekEnd:
		position = p.length + offset
	default:
		return p.position, fmt.Errorf("invalid audio seek origin %d", whence)
	}
	if position < 0 {
		return p.position, fmt.Errorf("negative audio position")
	}
	if position == p.position {
		return position, nil
	}
	p.generation++
	if p.current.buffer != nil {
		p.buffers <- p.current.buffer
		p.current = pcmReadResult{}
	}
	p.terminal = nil
	select {
	case <-p.stop:
		return p.position, io.EOF
	case p.requests <- pcmReadRequest{position: position, generation: p.generation}:
		p.position = position
		return position, nil
	}
}

func (p *pcmPrefetch) Close() error {
	p.closeOnce.Do(func() { close(p.stop) })
	return nil
}

func (s *Stream) StartReadAhead() {
	if s.pcm == nil {
		return
	}
	if _, ok := s.pcm.reader.(*pcmPrefetch); ok {
		return
	}
	s.pcm.reader = newPCMPrefetch(s.pcm.reader, int64(s.position)*8, int64(s.frames)*8)
}

func (s *Stream) Ready() bool {
	if s.decode != nil {
		select {
		case <-s.decode.done:
		default:
			return false
		}
	}
	if s.pcm != nil {
		if reader, ok := s.pcm.reader.(*pcmPrefetch); ok {
			select {
			case <-reader.ready:
			default:
				return false
			}
		}
	}
	return true
}

func (s *Stream) Close() error {
	if s.pcm != nil {
		if reader, ok := s.pcm.reader.(*pcmPrefetch); ok {
			return reader.Close()
		}
	}
	return nil
}

func (s *Stream) playbackClosed() bool {
	if s.pcm == nil {
		return false
	}
	reader, ok := s.pcm.reader.(*pcmPrefetch)
	if !ok {
		return false
	}
	select {
	case <-reader.stop:
		return true
	default:
		return false
	}
}
