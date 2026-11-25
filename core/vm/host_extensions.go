package vm

import (
	"fmt"
	"unicode/utf8"
)

// HostCommand keeps window operations on the desktop event loop.
type HostCommand struct {
	Kind, Value string
}

type hostExtensionState struct {
	windowAPI         func(string, []any) (any, error)
	files             filePickerState
	commands          []HostCommand
	keys              []string
	fullscreen        bool
	projectFullscreen bool
	frameMilliseconds float64
	windowProvider    func() (HostWindow, error)
	window            HostWindow
	windowRead        bool
	windowTitle       string
	windowTitleSet    bool
}

func (r *Runtime) SetWindowHandler(handler func(string, []any) (any, error)) {
	r.host.windowAPI = handler
}

// HostWindow contains native properties with matching browser reporter units.
// Screen dimensions are device-independent pixels; outer window geometry is
// intentionally absent because desktop backends do not all expose it.
type HostWindow struct {
	ScreenWidth, ScreenHeight int
	Focused                   bool
	Title                     string
}

// SetHostWindowProvider installs a lazy snapshot reader for this host frame.
// Projects without window reporters never invoke the reader.
func (r *Runtime) SetHostWindowProvider(provider func() (HostWindow, error)) {
	r.host.windowProvider = provider
	r.host.windowRead = false
}

func (r *Runtime) hostWindowProperty(key string) (any, error) {
	switch key {
	case "screenWidth", "screenHeight", "focused", "title":
	default:
		return nil, fmt.Errorf("unknown host window property %q", key)
	}
	if key == "title" && r.host.windowTitleSet {
		return r.host.windowTitle, nil
	}
	if !r.host.windowRead {
		if r.host.windowProvider == nil {
			return nil, fmt.Errorf("host.window requires a desktop window")
		}
		value, err := r.host.windowProvider()
		if err != nil {
			return nil, err
		}
		r.host.window, r.host.windowRead = value, true
	}
	switch key {
	case "screenWidth":
		return float64(r.host.window.ScreenWidth), nil
	case "screenHeight":
		return float64(r.host.window.ScreenHeight), nil
	case "focused":
		return r.host.window.Focused, nil
	default:
		return r.host.window.Title, nil
	}
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
