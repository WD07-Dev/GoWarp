package vm

import (
	"context"
	"math"
)

// DisplayTarget references the original project's asset index, including clones.
type DisplayTarget struct {
	Source int
	State  TargetState
}

/*
UsesColorSensing tells display hosts whether they need an exact CPU pen copy.
Sprite/edge touching only reads costume silhouettes, never the pen layer.
*/
func (r *Runtime) UsesColorSensing() bool {
	if r.project == nil {
		return true
	}
	for _, target := range r.project.Targets {
		for _, block := range target.Blocks {
			if block != nil && (block.Opcode == "sensing_touchingcolor" || block.Opcode == "sensing_coloristouchingcolor") {
				return true
			}
		}
	}
	return false
}

func (r *Runtime) Scene() []DisplayTarget {
	return r.SceneInto(nil)
}

// SceneInto overwrites dst with a display snapshot. Separate snapshots need separate buffers.
func (r *Runtime) SceneInto(dst []DisplayTarget) []DisplayTarget {
	n := len(r.drawList)
	if dst == nil || cap(dst) < n || cap(dst) > max(64, 4*n) {
		dst = make([]DisplayTarget, n)
	} else {
		clear(dst[min(n, len(dst)):])
		dst = dst[:n]
	}
	for i, t := range r.drawList {
		s := t.state
		s.Variables = nil
		s.Lists = nil
		dst[i] = DisplayTarget{Source: t.sourceIndex, State: s}
	}
	return dst
}

// Tick keeps virtual time moving while native input/event scripts are idle.
func (r *Runtime) Tick(ctx context.Context, fps float64, turbo bool) error {
	return r.advance(ctx, 1, fps, turbo, true)
}

func (r *Runtime) Stop() {
	r.stopAll()
	r.extensionEvent("stop", r.stage)
}

func (r *Runtime) Stopped() bool {
	return r.stopped
}

func (r *Runtime) MoveSprite(source, cloneID int, x, y float64) {
	for _, t := range r.targets {
		if t.sourceIndex == source && t.state.CloneID == cloneID {
			t.setXY(x, y)
			return
		}
	}
}

// RaiseSprite keeps drag ordering consistent with the front-layer block.
func (r *Runtime) RaiseSprite(source, cloneID int) {
	for _, t := range r.targets {
		if t.sourceIndex == source && t.state.CloneID == cloneID {
			r.moveLayer(t, math.Inf(1), false, true)
			return
		}
	}
}

// AudioCommand captures transitions, including multiple plays within one frame.
type AudioCommand struct {
	Kind               string
	ID, Source, Sound  int
	Volume, Pitch, Pan float64
	Rate               *float64 // Optional independent playback rate; built-in sounds use Pitch.
	Start, End         float64
	Loop               bool
	URL                string
}

func (r *Runtime) ConfigureAudio(durations [][]float64) {
	r.audioEnabled = true
	r.audioDurations = make([][]float64, len(durations))
	for i, values := range durations {
		r.audioDurations[i] = append([]float64(nil), values...)
	}
}

func (r *Runtime) emitAudio(kind string, v *soundVoice) {
	if !r.audioEnabled {
		return
	}
	if kind == "play" || (kind == "resume" && v.outputPending) {
		v.startPending = true
	}
	s := v.player.state
	volume := s.Volume
	if r.audioExtensions != nil {
		volume *= r.audioExtensions.volume / 100
	}
	var rate *float64
	if v.customRate {
		value := v.rate
		rate = &value
	}
	r.audioCommands = append(r.audioCommands, AudioCommand{
		Kind:   kind,
		ID:     v.id,
		Source: v.player.sourceIndex,
		Sound:  v.sound,
		Volume: volume,
		Start:  v.position,
		End:    v.end,
		Loop:   v.loop,
		URL:    v.url,
		Pitch:  s.Pitch,
		Rate:   rate,
		Pan:    s.Pan,
	})
}

func (r *Runtime) DrainAudio() []AudioCommand {
	commands := r.audioCommands
	r.audioCommands = nil
	return commands
}

func (r *Runtime) audioVoice(id int) *soundVoice {
	if r.audioExtensions != nil {
		if v := r.audioExtensions.urls[id]; v != nil {
			return v
		}
		for _, channel := range r.audioExtensions.channels {
			if v := channel.voice; v != nil && v.id == id {
				return v
			}
		}
	}
	for _, sounds := range r.voices {
		for _, v := range sounds {
			if v.id == id {
				return v
			}
		}
	}
	return nil
}

// AudioStarted anchors the voice clock when the host starts playback, after decoding.
func (r *Runtime) AudioStarted(id int) {
	v := r.audioVoice(id)
	if v == nil || !v.active || !v.outputPending || !v.startPending {
		return
	}
	v.startPending = false
	v.updated = r.audioTime()
	if v.stepClock != nil {
		/*
			BEFORE_EXECUTE-based extensions count from this step's timestamp,
			while output playback starts at the actual host acknowledgement.
		*/
		v.stepClock.updated = r.audioTime()
	}
	if r.realtime && !r.realtimeStart.IsZero() {
		v.updated = max(v.updated, r.monotonicTime())
	}
}

func (r *Runtime) AudioFinished(id int) {
	v := r.audioVoice(id)
	if v == nil {
		return
	}
	if v.startPending {
		v.active = false
		v.startPending = false
	}
	v.outputPending = false
	if r.audioExtensions != nil && r.audioExtensions.urls[id] != nil {
		v.active = false
		delete(r.audioExtensions.urls, id)
	}
}

// GreenFlag restarts scripts without resetting project data or sprite positions.
func (r *Runtime) GreenFlag() {
	r.syncTime()
	r.stopAll()
	r.compactThreads()
	r.stopped = false
	r.timerSeconds = r.elapsed
	for i := len(r.executable) - 1; i >= 0; i-- {
		r.startHatsFor(r.executable[i], "event_whenflagclicked", "", true)
	}
	r.extensionEvent("flag", r.stage)
}

// SetKeyState synchronizes held keys while stopped, without starting event hats.
func (r *Runtime) SetKeyState(key string, down bool) {
	r.trackHostKey(key, down)
	r.keys[normalizeKey(key)] = down
}
