package vm

type audioExtensionState struct {
	volume   float64
	channels map[string]*audioChannel
	urls     map[int]*soundVoice
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
