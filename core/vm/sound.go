package vm

import (
	"math"
	"strings"

	"gowarp/core/project"
)

/*
A virtual voice tracks headless timing. PCM output belongs to the host backend.
Original and clone targets share their source's sound slots.
*/
type soundVoice struct {
	id, sound                int
	outputPending            bool
	startPending             bool
	player                   *target
	remaining, updated, rate float64
	active                   bool
	duration, position, end  float64
	loop, paused             bool
	customRate               bool
	url                      string
	stepClock                *soundClock
}

type soundClock struct {
	position, updated float64
}

/*
Audio extensions sample performance.now() once per VM step, rather than
the integer-millisecond clock used by Scratch waits.
*/
func (r *Runtime) audioTime() float64 {
	if r.realtime {
		return r.audioElapsed
	}
	return r.elapsed
}

func (r *Runtime) advanceVoice(v *soundVoice) {
	if !v.active || v.paused || v.startPending {
		return
	}
	if clock := v.stepClock; clock != nil {
		now := math.Max(r.audioTime(), clock.updated)
		clock.position = v.positionAfter(clock.position, now-clock.updated)
		clock.updated = now
	}
	now := math.Max(r.audioTime(), v.updated)
	v.position = v.positionAfter(v.position, now-v.updated)
	v.remaining = math.Max(0, v.end-v.position)
	v.updated = now
	v.active = v.remaining > 1e-12
}

func (v *soundVoice) positionAfter(position, elapsed float64) float64 {
	position += elapsed * v.rate
	if v.loop && v.end > 0 {
		return math.Mod(position, v.end)
	}
	return math.Min(v.end, position)
}

func (r *Runtime) refreshVoices(t *target) {
	if r.audioExtensions != nil {
		for _, v := range r.audioExtensions.urls {
			if v.player == t {
				r.emitAudio("update", v)
			}
		}
	}
	for _, v := range r.voices[t.source] {
		if v.player == t && (v.active || v.outputPending) {
			r.advanceVoice(v)
			v.rate = math.Pow(2, t.state.Pitch/120)
			r.emitAudio("update", v)
		}
	}
}

func soundIndex(t *target, which any) int {
	name := text(which)
	for i, s := range t.source.Sounds {
		if s.Name == name {
			return i
		}
	}
	count := len(t.source.Sounds)
	if count == 0 {
		return -1
	}
	s := trim(name)
	sign := float64(1)
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	n := float64(0)
	digits := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + float64(c-'0')
		digits++
	}
	if digits == 0 || math.IsInf(n, 0) {
		return -1
	}
	n = math.Mod(sign*n-1, float64(count))
	if n < 0 {
		n += float64(count)
	}
	return int(n)
}

func (r *Runtime) playSound(t *target, which any) *soundVoice {
	return r.playSoundAt(t, which, 0, false)
}

func (r *Runtime) soundDuration(t *target, index int) float64 {
	asset := t.source.Sounds[index]
	duration := float64(0)
	if asset.Rate > 0 && asset.SampleCount > 0 {
		duration = asset.SampleCount / asset.Rate
	}
	if t.sourceIndex < len(r.audioDurations) && index < len(r.audioDurations[t.sourceIndex]) {
		duration = r.audioDurations[t.sourceIndex][index]
	}
	return duration
}

func (r *Runtime) playSoundAt(t *target, which any, start float64, loop bool) *soundVoice {
	index := soundIndex(t, which)
	if index < 0 {
		return nil
	}
	if r.voices == nil {
		r.voices = map[*project.Target]map[int]*soundVoice{}
	}
	if r.voices[t.source] == nil {
		r.voices[t.source] = map[int]*soundVoice{}
	}
	if old := r.voices[t.source][index]; old != nil {
		old.active = false
		old.outputPending = false
		r.emitAudio("stop", old)
	}
	duration := r.soundDuration(t, index)
	start = math.Max(0, math.Min(duration, start))
	r.nextVoice++
	v := &soundVoice{
		id:            r.nextVoice,
		sound:         index,
		outputPending: r.audioEnabled && duration > 0,
		player:        t,
		remaining:     duration - start,
		duration:      duration,
		position:      start,
		end:           duration,
		loop:          loop,
		updated:       r.audioTime(),
		rate:          math.Pow(2, t.state.Pitch/120),
		active:        duration > start,
	}
	r.voices[t.source][index] = v
	if duration > 0 {
		r.emitAudio("play", v)
	}
	return v
}

func (r *Runtime) stopSounds() {
	if r.audioExtensions != nil {
		for _, v := range r.audioExtensions.urls {
			r.stopExtensionVoice(v)
		}
		clear(r.audioExtensions.urls)
	}
	for _, sounds := range r.voices {
		for _, v := range sounds {
			r.emitAudio("stop", v)
			v.active = false
			v.outputPending = false
			v.remaining = 0
		}
	}
}

func (r *Runtime) soundStatement(th *thread, b *project.Block, input func(string) (any, error)) (bool, error) {
	t := th.target
	switch b.Opcode {
	case "sound_play", "sound_playuntildone":
		value, err := input("SOUND_MENU")
		if err != nil {
			return false, err
		}
		voice := r.playSound(t, value)
		if b.Opcode == "sound_playuntildone" && voice != nil && voice.active {
			th.soundWait = voice
			return true, nil
		}
	case "sound_stopallsounds":
		r.stopSounds()
	case "sound_setvolumeto", "sound_changevolumeby":
		value, err := input("VOLUME")
		if err != nil {
			return false, err
		}
		n := number(value)
		if b.Opcode == "sound_changevolumeby" {
			n += t.state.Volume
		}
		t.state.Volume = math.Max(0, math.Min(100, n))
		r.refreshVoices(t)
		return r.soundUpdateYield(th), nil
	case "sound_cleareffects":
		t.state.Pitch, t.state.Pan = 0, 0
		r.refreshVoices(t)
	case "sound_seteffectto", "sound_changeeffectby":
		effect := strings.ToLower(field(b, "EFFECT", 0))
		if effect != "pitch" && effect != "pan" {
			return false, nil
		}
		value, err := input("VALUE")
		if err != nil {
			return false, err
		}
		n := number(value)
		slot, limit := &t.state.Pitch, float64(360)
		if !r.options.MiscLimits {
			limit = 1000
		}
		if effect == "pan" {
			slot, limit = &t.state.Pan, 100
		}
		if b.Opcode == "sound_changeeffectby" {
			n += *slot
		}
		*slot = math.Max(-limit, math.Min(limit, n))
		r.refreshVoices(t)
		return r.soundUpdateYield(th), nil
	}
	return false, nil
}

func (r *Runtime) soundUpdateYield(th *thread) bool {
	if r.options.MiscLimits {
		th.wakeFrame = r.frame + 1
		return true
	}
	/*
		TurboWarp removes the promise wait, but still prevents extra passes
		in normal mode so volume/pitch loops do not run at full CPU speed.
	*/
	r.redrawRequested = true
	return false
}
