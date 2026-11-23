package vm

type audioExtensionState struct {
	paused   bool
	volume   float64
	channels map[string]*audioChannel
	urls     map[int]*soundVoice
}

func (r *Runtime) eachAudioVoice(visit func(*soundVoice)) {
	for _, sounds := range r.voices {
		for _, voice := range sounds {
			visit(voice)
		}
	}
	if state := r.audioExtensions; state != nil {
		for _, voice := range state.urls {
			visit(voice)
		}
		for _, channel := range state.channels {
			if channel.voice != nil {
				visit(channel.voice)
			}
		}
	}
}

func (r *Runtime) pauseAudioContext(paused bool) {
	state := r.extensionAudio()
	if state.paused == paused {
		return
	}
	if paused {
		r.eachAudioVoice(r.advanceVoice)
	}
	state.paused = paused
	r.eachAudioVoice(func(v *soundVoice) {
		if !v.active && !v.outputPending {
			return
		}
		v.updated = r.audioTime()
		if v.stepClock != nil {
			v.stepClock.updated = r.audioTime()
		}
		if paused {
			r.emitAudio("pause", v)
		} else if !v.paused {
			r.emitAudio("resume", v)
		}
	})
}

type audioChannel struct {
	voice *soundVoice
}

func (r *Runtime) extensionAudio() *audioExtensionState {
	if r.audioExtensions == nil {
		r.audioExtensions = &audioExtensionState{volume: 100, channels: map[string]*audioChannel{}, urls: map[int]*soundVoice{}}
	}
	return r.audioExtensions
}

func (r *Runtime) stopExtensionVoice(v *soundVoice) {
	if v == nil {
		return
	}
	r.advanceVoice(v)
	r.emitAudio("stop", v)
	v.active, v.outputPending, v.paused = false, false, false
}
