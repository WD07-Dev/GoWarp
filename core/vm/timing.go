package vm

import (
	"math"
	"time"
)

/*
TickInterval matches TurboWarp's browser timer: setInterval truncates its
delay to whole milliseconds and repeated timers have a 4 ms minimum.
Zero FPS uses animation frames instead of that timer.
*/
func (r *Runtime) TickInterval() time.Duration {
	if r.fps <= 0 {
		return time.Second / 60
	}
	return time.Duration(math.Max(4, math.Floor(1000/r.fps))) * time.Millisecond
}

/*
UseRealtimeClock anchors playback timers to wall time after loading. Replay and headless runs
retain deterministic frame time.
*/
func (r *Runtime) UseRealtimeClock() {
	if r.realtime {
		return
	}
	r.realtime = true
	r.realtimeBase = r.elapsed
	r.audioElapsed = r.elapsed
}

func (r *Runtime) syncTime() {
	if !r.realtime {
		return
	}
	now := r.now()
	if r.realtimeStart.IsZero() {
		r.realtimeStart = now
	}
	r.audioElapsed = max(r.audioElapsed, r.realtimeBase+now.Sub(r.realtimeStart).Seconds())
	elapsed := r.realtimeElapsed(now)
	if elapsed > r.elapsed {
		r.elapsed = elapsed
	}
}

func (r *Runtime) realtimeElapsed(now time.Time) float64 {
	/*
		Match Scratch's integer-millisecond clock without losing Go's
		monotonic elapsed time when the system clock changes.
	*/
	fraction := time.Duration(r.realtimeStart.Nanosecond()) % time.Millisecond
	elapsed := now.Sub(r.realtimeStart) + fraction
	return r.realtimeBase + float64(elapsed.Milliseconds())/1000
}

/*
Extensions using performance.now() read the clock at the call, without
changing the step timestamp used by Scratch waits and the built-in timer.
*/
func (r *Runtime) monotonicTime() float64 {
	if !r.realtime {
		return r.elapsed
	}
	now := r.now()
	if r.realtimeStart.IsZero() {
		r.realtimeStart = now
	}
	return r.realtimeBase + now.Sub(r.realtimeStart).Seconds()
}

func (r *Runtime) wakePending(th *thread) bool {
	return th.wakeFrame > r.frame || th.wake-r.elapsed > 1e-12
}

func (r *Runtime) advanceTime() {
	if r.realtime {
		return
	}
	/*
		Compensated summation prevents frame-sized waits gaining an extra tick
		through accumulated floating point rounding at fixed frame rates.
	*/
	delta := 1/r.FPS() - r.elapsedError
	next := r.elapsed + delta
	r.elapsedError = (next - r.elapsed) - delta
	r.elapsed = next
}
