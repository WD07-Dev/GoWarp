package media

import "math"

func (s *Stream) SetRate(rate float64) {
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
		return
	}
	s.parametersMu.Lock()
	s.step = float64(s.sound.Rate) / float64(s.outputRate) * rate
	s.parametersMu.Unlock()
}
