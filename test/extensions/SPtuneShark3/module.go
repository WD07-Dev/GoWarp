// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"encoding/json"
	"fmt"
	e "gowarp/core/extension"
	"math"
	"sort"
	"strconv"
	"strings"
)

func call(c *e.Context, op string, args ...any) (e.Result, error) {
	v, err := c.Call(op, args...)
	return e.Result{Value: v}, err
}
func cmd(op string, inputs []string, fn e.Handler) e.Block {
	return e.Block{Opcode: op, Kind: "command", Inputs: inputs, Handler: fn}
}
func rep(op string, inputs []string, fn e.Handler) e.Block {
	return e.Block{Opcode: op, Kind: "reporter", Inputs: inputs, Handler: fn}
}
func menu(op, key string) e.Block {
	return rep(op, nil, func(c *e.Context) (e.Result, error) {
		return e.Result{Value: c.Field(key)}, nil
	})
}

type channel struct {
	Order                           uint64
	Asset, Voice                    any
	Volume                          float64
	Loop                            bool
	Overlap                         bool
	Overlays                        []any
	Pitch, Speed, Detune, Gain, Pan float64
}

func channels(c *e.Context) map[string]*channel {
	m, _ := c.State["channels"].(map[string]*channel)
	if m == nil {
		m = map[string]*channel{}
		c.State["channels"] = m
	}
	return m
}
func stop(c *e.Context) (e.Result, error) {
	if enabled, ok := c.State["flagControl"].(bool); ok && !enabled {
		return e.Result{}, nil
	}
	for _, ch := range channels(c) {
		if err := control(c, ch, "stop"); err != nil {
			return e.Result{}, err
		}
	}
	return e.Result{}, nil
}
func start(c *e.Context, ch *channel, from, to float64) (any, error) {
	active, err := c.Call("audio.voice.get", ch.Voice, "active")
	if err != nil {
		return nil, err
	}
	paused, err := c.Call("audio.voice.get", ch.Voice, "paused")
	if err != nil {
		return nil, err
	}
	if e.Bool(active) && !e.Bool(paused) {
		if !ch.Overlap {
			return ch.Voice, nil
		}
		live := ch.Overlays[:0]
		for _, voice := range ch.Overlays {
			pending, err := c.Call("audio.voice.get", voice, "pending")
			if err != nil {
				return nil, err
			}
			if e.Bool(pending) {
				live = append(live, voice)
			} else {
				if _, err := c.Call("audio.voice.dispose", voice); err != nil {
					return nil, err
				}
			}
		}
		ch.Overlays = live
		voice, err := c.Call("audio.play", ch.Asset, from, to, ch.Volume*ch.Gain, ch.Loop, ch.Pitch*ch.Speed*math.Pow(2, ch.Detune/1200), ch.Pan)
		if err != nil {
			return nil, err
		}
		if _, err := c.Call("audio.voice.set", voice, "clock", "step"); err != nil {
			return nil, err
		}
		ch.Overlays = append(ch.Overlays, voice)
		return voice, nil
	}
	if _, err := c.Call("audio.voice.dispose", ch.Voice); err != nil {
		return nil, err
	}
	v, err := c.Call("audio.play", ch.Asset, from, to, ch.Volume*ch.Gain, ch.Loop, ch.Pitch*ch.Speed*math.Pow(2, ch.Detune/1200), ch.Pan)
	if err == nil {
		_, err = c.Call("audio.voice.set", v, "clock", "step")
	}
	ch.Voice = v
	return v, err
}
func applyVoice(c *e.Context, ch *channel, voice any) error {
	for _, property := range []struct {
		name  string
		value float64
	}{{"rate", ch.Pitch * ch.Speed * math.Pow(2, ch.Detune/1200)}, {"volume", ch.Volume * ch.Gain}, {"pan", ch.Pan}} {
		if _, err := c.Call("audio.voice.set", voice, property.name, property.value); err != nil {
			return err
		}
	}
	return nil
}
func dispose(c *e.Context, ch *channel) error {
	for _, voice := range append([]any{ch.Voice}, ch.Overlays...) {
		if _, err := c.Call("audio.voice.dispose", voice); err != nil {
			return err
		}
	}
	ch.Voice = nil
	ch.Overlays = nil
	return nil
}
func control(c *e.Context, ch *channel, op string) error {
	for _, voice := range ch.Overlays {
		var err error
		switch op {
		case "stop":
			_, err = c.Call("audio.voice.stop", voice)
		case "pause", "unpause":
			_, err = c.Call("audio.voice.set", voice, "paused", op == "pause")
		}
		if err != nil {
			return err
		}
	}

	switch op {
	case "start":
		_, err := start(c, ch, 0, 0)
		return err
	case "stop":
		_, err := c.Call("audio.voice.stop", ch.Voice)
		return err
	case "pause", "unpause":
		_, err := c.Call("audio.voice.set", ch.Voice, "paused", op == "pause")
		return err
	}
	return nil
}
func New() e.Extension {

	x := e.Extension{ID: "SPtuneShark3", State: map[string]any{"flagControl": true}, Events: map[string]e.Handler{"stop": stop, "flag": stop, "cancel": stop, "dispose": func(c *e.Context) (e.Result, error) {
		for _, ch := range channels(c) {
			if err := dispose(c, ch); err != nil {
				return e.Result{}, err
			}
		}
		c.State["channels"] = nil
		return e.Result{}, nil
	}}}
	for _, key := range []string{"soundBools", "soundProps", "singleEffectNew", "effectMenu"} {
		x.Blocks = append(x.Blocks, menu("SPtuneShark3_menu_"+key, key))
	}
	x.Blocks = append(x.Blocks,
		cmd("SPtuneShark3_enableControl", nil, func(c *e.Context) (e.Result, error) {
			c.State["flagControl"] = c.Field("ON_OFF") == "on"
			return e.Result{}, nil
		}),
		cmd("SPtuneShark3_importMenu", []string{"SOUND", "NAME"}, func(c *e.Context) (e.Result, error) {
			asset, err := c.Call("audio.asset", c.String("SOUND"), true)
			if err != nil || asset == nil {
				return e.Result{}, err
			}
			m := channels(c)
			name := c.String("NAME")
			if old := m[name]; old != nil {
				if err := dispose(c, old); err != nil {
					return e.Result{}, err
				}
			}
			order, _ := c.State["channelOrder"].(uint64)
			c.State["channelOrder"] = order + 1
			m[name] = &channel{Order: order, Asset: asset, Volume: 100, Pitch: 1, Speed: 1, Gain: 1}
			return e.Result{}, nil
		}),
		cmd("SPtuneShark3_deleteAllSounds", nil, func(c *e.Context) (e.Result, error) {
			for _, ch := range channels(c) {
				if err := dispose(c, ch); err != nil {
					return e.Result{}, err
				}
			}
			c.State["channels"] = nil
			return e.Result{}, nil
		}),
		cmd("SPtuneShark3_ctrlSounds", nil, func(c *e.Context) (e.Result, error) {
			op := c.Field("CONTROL")
			for _, ch := range channels(c) {
				if err := control(c, ch, op); err != nil {
					return e.Result{}, err
				}
				if op == "stop" {
					duration, err := c.Call("audio.asset.get", ch.Asset, "duration")
					if err != nil {
						return e.Result{}, err
					}
					if _, err := c.Call("audio.voice.set", ch.Voice, "position", duration); err != nil {
						return e.Result{}, err
					}
				}
			}
			return e.Result{}, nil
		}))
	for _, op := range []string{"pauseSound", "deleteSound", "toggleOverlap", "setThingNew", "resetEffect"} {
		op := op
		inputs := []string{"NAME"}
		if op == "setThingNew" {
			inputs = append(inputs, "TYPE", "VALUE")
		}
		if op == "resetEffect" {
			inputs = append(inputs, "EFFECT")
		}
		x.Blocks = append(x.Blocks, cmd("SPtuneShark3_"+op, inputs, func(c *e.Context) (e.Result, error) {
			ch := channels(c)[c.String("NAME")]
			if ch == nil {
				return e.Result{}, nil
			}
			switch op {
			case "pauseSound":
				return e.Result{}, control(c, ch, c.Field("UN_PAUSE"))
			case "deleteSound":
				err := dispose(c, ch)
				if err == nil {
					delete(channels(c), c.String("NAME"))
				}
				return e.Result{}, err
			case "toggleOverlap":
				ch.Overlap = c.Field("TYPE") == "on"
				return e.Result{}, nil
			case "setThingNew":
				value := c.Number("VALUE")
				switch c.String("TYPE") {
				case "pitch":
					ch.Pitch = math.Max(0, 1+value/100)
				case "speed":
					ch.Speed = math.Max(0, value/100)
				case "detune":
					ch.Detune = value * 10
				case "gain":
					ch.Gain = value / 100
				case "pan":
					ch.Pan = math.Max(-100, math.Min(100, value))
				default:
					return e.Result{}, fmt.Errorf("unsupported Tune Shark basic effect %q", c.String("TYPE"))
				}
			case "resetEffect":
				switch c.String("EFFECT") {
				case "pitch":
					ch.Pitch = 1
				case "speed":
					ch.Speed = 1
				case "detune":
					ch.Detune = 0
				case "gain":
					ch.Gain = 1
				case "pan":
					ch.Pan = 0
				case "all effects":
					ch.Pitch, ch.Speed, ch.Gain = 1, 1, 1
					ch.Detune, ch.Pan = 0, 0
				default:
					return e.Result{}, fmt.Errorf("unsupported Tune Shark effect %q", c.String("EFFECT"))
				}
			}
			return e.Result{}, applyVoice(c, ch, ch.Voice)
		}))
	}
	for _, op := range []string{"startSound", "startSoundAt", "playAndStop", "stopSound", "toggleLoop", "setVol"} {
		op := op
		inputs := []string{"NAME"}
		if op == "startSoundAt" || op == "playAndStop" {
			inputs = append(inputs, "TIME")
		}
		if op == "playAndStop" {
			inputs = append(inputs, "MAX")
		}
		if op == "setVol" {
			inputs = append(inputs, "NUM")
		}
		x.Blocks = append(x.Blocks, cmd("SPtuneShark3_"+op, inputs, func(c *e.Context) (e.Result, error) {
			ch := channels(c)[c.String("NAME")]
			if ch == nil {
				return e.Result{}, nil
			}
			switch op {

			case "startSound", "startSoundAt", "playAndStop":
				voice, exists := c.Local["voice"]
				if !exists {
					from := float64(0)
					if op != "startSound" {
						from = c.Number("TIME")
					}
					var err error
					voice, err = start(c, ch, from, 0)
					if err != nil {
						return e.Result{}, err
					}
					c.Local["voice"] = voice
					if op == "playAndStop" {
						return e.Result{Yield: true, Again: true}, nil
					}
				}
				if op == "playAndStop" {
					position, err := c.Call("audio.voice.get", voice, "position")
					if err != nil {
						return e.Result{}, err
					}
					pending, err := c.Call("audio.voice.get", voice, "pending")
					if err != nil {
						return e.Result{}, err
					}
					if e.Number(position) >= c.Number("MAX") || !e.Bool(pending) {
						return call(c, "audio.voice.stop", voice)
					}
					return e.Result{Yield: true, Again: true}, nil
				}
				return e.Result{}, nil
			case "stopSound":
				return e.Result{}, control(c, ch, "stop")
			case "toggleLoop":
				ch.Loop = c.Field("TYPE") == "on"
				if _, err := c.Call("audio.voice.set", ch.Voice, "loop", ch.Loop); err != nil {
					return e.Result{}, err
				}
				if !ch.Loop {
					return call(c, "audio.voice.stop", ch.Voice)
				}
			case "setVol":
				ch.Volume = math.Max(0, math.Min(100, c.Number("NUM")))
				return call(c, "audio.voice.set", ch.Voice, "volume", ch.Volume*ch.Gain)

			}
			return e.Result{}, nil
		}))
	}
	for _, op := range []string{"soundProperty", "soundCheck"} {
		op := op
		key := "PROP"
		kind := "reporter"
		if op == "soundCheck" {
			key = "CONTROL"
			kind = "boolean"
		}
		x.Blocks = append(x.Blocks, e.Block{Opcode: "SPtuneShark3_" + op, Kind: kind, Inputs: []string{"NAME", key}, Handler: func(c *e.Context) (e.Result, error) {
			ch := channels(c)[c.String("NAME")]
			if ch == nil {
				if op == "soundCheck" {
					return e.Result{Value: false}, nil
				}
				return e.Result{Value: float64(0)}, nil
			}
			prop := c.String(key)
			if op == "soundCheck" {
				switch prop {
				case "exists":
					return e.Result{Value: true}, nil
				case "looped":
					return e.Result{Value: ch.Loop}, nil
				case "paused":
					return call(c, "audio.voice.get", ch.Voice, "paused")
				case "playing":
					a, err := c.Call("audio.voice.get", ch.Voice, "active")
					if err != nil {
						return e.Result{}, err
					}
					p, err := c.Call("audio.voice.get", ch.Voice, "paused")
					return e.Result{Value: e.Bool(a) && !e.Bool(p)}, err
				case "overlaped":
					return e.Result{Value: ch.Overlap}, nil
				case "reversed", "binded":
					return e.Result{Value: false}, nil
				}
			} else {
				switch prop {
				case "volume":
					return e.Result{Value: ch.Volume}, nil
				case "current time":
					if ch.Voice == nil {
						return e.Result{Value: float64(0)}, nil
					}
					return call(c, "audio.voice.get", ch.Voice, "position")
				case "length":
					return call(c, "audio.asset.get", ch.Asset, "duration")
				case "samples":
					return call(c, "audio.asset.get", ch.Asset, "rate")
				case "source":
					target, err := c.Call("audio.asset.get", ch.Asset, "target")
					if err != nil {
						return e.Result{}, err
					}
					name, err := c.Call("audio.asset.get", ch.Asset, "name")
					if err != nil {
						return e.Result{}, err
					}
					format, err := c.Call("audio.asset.get", ch.Asset, "format")
					return e.Result{Value: "/" + strings.ReplaceAll(e.String(target), "/", "") + "/" + strings.ReplaceAll(e.String(name), "/", "") + "." + e.String(format)}, err
				}
			}
			return e.Result{}, fmt.Errorf("unsupported Tune Shark property %q", prop)
		}})
	}

	for _, op := range []string{"allSounds", "allPlaySounds"} {
		op := op
		label := "all sounds"
		if op == "allPlaySounds" {
			label = "all playing sounds"
		}
		x.Blocks = append(x.Blocks, e.Block{Opcode: "SPtuneShark3_" + op, Text: label, Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) {
			bank := channels(c)
			names := make([]string, 0, len(bank))
			for name := range bank {
				names = append(names, name)
			}
			arrayIndex := func(name string) (uint64, bool) {
				n, err := strconv.ParseUint(name, 10, 32)
				return n, err == nil && n < 4294967295 && strconv.FormatUint(n, 10) == name
			}
			sort.Slice(names, func(i, j int) bool {
				a, ai := arrayIndex(names[i])
				b, bi := arrayIndex(names[j])
				if ai || bi {
					return ai && (!bi || a < b)
				}
				return bank[names[i]].Order < bank[names[j]].Order
			})
			if op == "allPlaySounds" {
				playing := names[:0]
				for _, name := range names {
					voice := bank[name].Voice
					active, err := c.Call("audio.voice.get", voice, "active")
					if err != nil {
						return e.Result{}, err
					}
					paused, err := c.Call("audio.voice.get", voice, "paused")
					if err != nil {
						return e.Result{}, err
					}
					if e.Bool(active) && !e.Bool(paused) {
						playing = append(playing, name)
					}
				}
				names = playing
			}
			data, err := json.Marshal(names)
			return e.Result{Value: string(data)}, err
		}})
	}
	return x
}
