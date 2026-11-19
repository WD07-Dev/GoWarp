package vm

import (
	"fmt"
	"math"

	"gowarp/core/extension"
)

// mediaAsset keeps a sound's source alive independently of its importing clone.
type mediaAsset struct {
	target *target
	index  int
}

func (c *extensionContext) audioCall(operation string, args []any) (any, error) {
	r, t := c.r, c.th.target
	a := hostArgs(args)
	switch operation {
	case "audio.asset":
		index := soundIndex(t, a.get(0))
		if extension.Bool(a.get(1)) {
			index = -1
			for i, s := range t.source.Sounds {
				if s.Name == a.string(0) {
					index = i
					break
				}
			}
		}
		if index < 0 {
			return nil, nil
		}
		/*
			A sound asset outlives its importing clone. Keep only the source
			identity, not the clone's variables, lists, or execution state.
		*/
		source := &target{source: t.source, sourceIndex: t.sourceIndex}
		return &mediaAsset{source, index}, nil
	case "audio.asset.get":
		asset, ok := a.get(0).(*mediaAsset)
		if !ok || asset == nil {
			return float64(0), nil
		}
		s := asset.target.source.Sounds[asset.index]
		switch a.string(1) {
		case "duration":
			return r.soundDuration(asset.target, asset.index), nil
		case "rate":
			return s.Rate, nil
		case "name":
			return s.Name, nil
		case "format":
			return s.DataFormat, nil
		case "target":
			return asset.target.source.Name, nil
		}
		return nil, fmt.Errorf("unknown asset property %q", a.string(1))
	case "audio.projectVolume":
		state := r.extensionAudio()
		state.volume = a.number(0)
		for _, voices := range r.voices {
			for _, v := range voices {
				if v.active || v.outputPending {
					r.emitAudio("update", v)
				}
			}
		}
		for _, v := range state.urls {
			r.emitAudio("update", v)
		}
		for _, channel := range state.channels {
			if v := channel.voice; v != nil && (v.active || v.outputPending) {
				r.emitAudio("update", v)
			}
		}
		return nil, nil
	case "audio.targetVoice":
		v := r.voices[t.source][soundIndex(t, a.get(0))]
		if v == nil {
			return nil, nil
		}
		return v, nil
	case "audio.targetPlay":
		v := r.playSoundAt(t, a.get(0), a.number(1), extension.Bool(a.get(2)))
		if v == nil {
			return nil, nil
		}
		return v, nil
	case "audio.play":
		asset, ok := a.get(0).(*mediaAsset)
		if !ok || asset == nil {
			return nil, nil
		}
		duration := r.soundDuration(asset.target, asset.index)
		start, end := clamp(a.number(1), 0, duration), a.number(2)
		if end <= 0 || end > duration {
			end = duration
		}
		player := *asset.target
		player.state.Volume, player.state.Pitch, player.state.Pan = a.number(3), 0, 0
		rate := float64(1)
		if len(args) > 5 {
			rate = a.number(5)
			if !finite(rate) || rate < 0 {
				return nil, fmt.Errorf("voice rate must be finite and nonnegative")
			}
			player.state.Pitch = 120 * math.Log2(rate)
		}
		if len(args) > 6 {
			player.state.Pan = clamp(a.number(6), -100, 100)
		}
		r.nextVoice++
		v := &soundVoice{
			id:            r.nextVoice,
			sound:         asset.index,
			player:        &player,
			duration:      duration,
			position:      start,
			end:           end,
			remaining:     math.Max(0, end-start),
			updated:       r.audioTime(),
			rate:          rate,
			customRate:    len(args) > 5,
			active:        end > start,
			outputPending: r.audioEnabled && end > start,
			loop:          extension.Bool(a.get(4)),
		}
		r.extensionAudio().channels[fmt.Sprintf("voice:%d", v.id)] = &audioChannel{voice: v}
		if v.active {
			r.emitAudio("play", v)
		}
		return v, nil
	case "audio.url":
		if a.string(0) == "" {
			return nil, nil
		}
		r.nextVoice++
		v := &soundVoice{
			id:            r.nextVoice,
			sound:         -1,
			player:        t,
			url:           a.string(0),
			active:        true,
			outputPending: r.audioEnabled,
			rate:          1,
			end:           math.Inf(1),
			updated:       r.audioTime(),
		}
		if r.audioEnabled {
			r.extensionAudio().urls[v.id] = v
			r.emitAudio("play", v)
		}
		return v, nil
	case "audio.voice.get", "audio.voice.set", "audio.voice.stop", "audio.voice.dispose":
		v, ok := a.get(0).(*soundVoice)
		if !ok || v == nil {
			if operation == "audio.voice.get" {
				return false, nil
			}
			return nil, nil
		}
		r.advanceVoice(v)
		if operation == "audio.voice.stop" || operation == "audio.voice.dispose" {
			r.stopExtensionVoice(v)
			if operation == "audio.voice.dispose" {
				delete(r.extensionAudio().channels, fmt.Sprintf("voice:%d", v.id))
				delete(r.extensionAudio().urls, v.id)
			}
			return nil, nil
		}
		key := a.string(1)
		if operation == "audio.voice.get" {
			switch key {
			case "active":
				return v.active, nil
			case "pending":
				return v.active || v.outputPending, nil
			case "paused":
				return v.paused, nil
			case "position":
				if v.stepClock != nil {
					return v.stepClock.position, nil
				}
				return v.position, nil
			case "clock":
				if v.stepClock != nil {
					return "step", nil
				}
				return "output", nil
			case "duration":
				return v.duration, nil
			case "loop":
				return v.loop, nil
			}
			return nil, fmt.Errorf("unknown voice property %q", key)
		}
		switch key {
		case "loop":
			v.loop = extension.Bool(a.get(2))
			r.emitAudio("loop", v)
		case "rate":
			rate := a.number(2)
			if !finite(rate) || rate < 0 {
				return nil, fmt.Errorf("voice rate must be finite and nonnegative")
			}
			v.rate = rate
			v.customRate = true
			v.player.state.Pitch = 120 * math.Log2(rate)
			r.emitAudio("update", v)
		case "pan":
			v.player.state.Pan = clamp(a.number(2), -100, 100)
			r.emitAudio("update", v)
		case "volume":
			v.player.state.Volume = a.number(2)
			r.emitAudio("update", v)
		case "position":
			v.position = a.number(2)
			if v.stepClock != nil {
				v.stepClock.position = v.position
			}
		case "clock":
			switch a.string(2) {
			case "output":
				v.stepClock = nil
			case "step":
				if v.stepClock == nil {
					v.stepClock = &soundClock{position: v.position, updated: r.audioTime()}
				}
			default:
				return nil, fmt.Errorf("unknown voice clock %q", a.string(2))
			}
		case "paused":
			paused := extension.Bool(a.get(2))
			if v.active && v.paused != paused {
				v.paused = paused
				v.updated = r.audioTime()
				if v.stepClock != nil {
					v.stepClock.updated = r.audioTime()
				}
				kind := "resume"
				if paused {
					kind = "pause"
				}
				r.emitAudio(kind, v)
			}
		default:
			return nil, fmt.Errorf("unknown voice property %q", key)
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported extension host operation %q", operation)
}
