package vm

import "unicode/utf8"

// HostCommand keeps window operations on the desktop event loop.
type HostCommand struct {
	Kind, Value string
}

type hostExtensionState struct {
	files             filePickerState
	commands          []HostCommand
	keys              []string
	fullscreen        bool
	projectFullscreen bool
	frameMilliseconds float64
}

func (r *Runtime) SetHostFrame(fullscreen bool, milliseconds float64) {
	r.host.fullscreen = fullscreen
	if !fullscreen {
		r.host.projectFullscreen = false
	}
	if finite(milliseconds) && milliseconds > 0 {
		r.host.frameMilliseconds = milliseconds
	}
}

func (r *Runtime) DrainHostCommands() []HostCommand {
	commands := r.host.commands
	r.host.commands = nil
	return commands
}

func (r *Runtime) trackHostKey(key string, down bool) {
	/*
		Extension reporters retain typed case; Scratch sensing and key hats use
		the separately normalized key table. Match releases regardless of case.
	*/
	normalized := normalizeKey(key)
	if utf8.RuneCountInString(key) != 1 || key == " " {
		key = normalized
	}
	for i, held := range r.host.keys {
		if normalizeKey(held) != normalized {
			continue
		}
		if !down {
			r.host.keys = append(r.host.keys[:i], r.host.keys[i+1:]...)
		} else {
			r.host.keys[i] = key
		}
		return
	}
	if down {
		r.host.keys = append(r.host.keys, key)
	}
}
