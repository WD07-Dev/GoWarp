// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"math"
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
func New() e.Extension {
	x := e.Extension{ID: "lmsSoundExpanded", Blocks: []e.Block{
		cmd("lmsSoundExpanded_pauseSounds", nil, func(c *e.Context) (e.Result, error) {
			return call(c, "audio.contextPaused", true)
		}),
		cmd("lmsSoundExpanded_resumeSounds", nil, func(c *e.Context) (e.Result, error) {
			return call(c, "audio.contextPaused", false)
		}),
		cmd("lmsSoundExpanded_stopOwnSounds", nil, func(c *e.Context) (e.Result, error) {
			return call(c, "audio.stopTarget")
		}),
		cmd("lmsSoundExpanded_startSoundFromAndLoop", []string{"SOUND", "START"}, func(c *e.Context) (e.Result, error) {
			start := c.Number("START")
			if math.IsNaN(start) || math.IsInf(start, 0) {
				start = 0
			}
			return call(c, "audio.targetPlay", c.Args["SOUND"], start, true)
		}),
		cmd("lmsSoundExpanded_setProjectVolume", []string{"VALUE"}, func(c *e.Context) (e.Result, error) {
			return call(c, "audio.projectVolume", math.Max(0, math.Min(100, c.Number("VALUE"))))
		}),
		cmd("lmsSoundExpanded_playSoundAt", []string{"SOUND", "START"}, func(c *e.Context) (e.Result, error) {
			return call(c, "audio.targetPlay", c.Args["SOUND"], c.Number("START"), false)
		}),
		cmd("lmsSoundExpanded_stopSound", []string{"SOUND"}, func(c *e.Context) (e.Result, error) {
			v, err := c.Call("audio.targetVoice", c.Args["SOUND"])
			if err != nil {
				return e.Result{}, err
			}
			return call(c, "audio.voice.stop", v)
		}),
		cmd("lmsSoundExpanded_startLooping", []string{"SOUND"}, func(c *e.Context) (e.Result, error) {
			v, err := c.Call("audio.targetVoice", c.Args["SOUND"])
			if err != nil {
				return e.Result{}, err
			}
			active, err := c.Call("audio.voice.get", v, "active")
			if err != nil {
				return e.Result{}, err
			}
			if !e.Bool(active) {
				return call(c, "audio.targetPlay", c.Args["SOUND"], 0.0, true)
			}
			return call(c, "audio.voice.set", v, "loop", true)
		}),
		rep("lmsSoundExpanded_attributeOfSound", []string{"SOUND"}, func(c *e.Context) (e.Result, error) {
			asset, err := c.Call("audio.asset", c.Args["SOUND"], false)
			if err != nil {
				return e.Result{}, err
			}
			if asset == nil {
				return e.Result{Value: float64(0)}, nil
			}
			switch c.Field("ATTRIBUTE") {
			case "length":
				v, err := c.Call("audio.asset.get", asset, "duration")
				return e.Result{Value: math.Round(e.Number(v)*100) / 100}, err
			case "sample rate":
				return call(c, "audio.asset.get", asset, "rate")
			}
			return e.Result{}, fmt.Errorf("unsupported sound attribute %q", c.Field("ATTRIBUTE"))
		})}}
	x.Blocks = append(x.Blocks,
		rep("lmsSoundExpanded_getProjectVolume", nil, func(c *e.Context) (e.Result, error) {
			value, err := c.Call("audio.projectVolume")
			return e.Result{Value: math.Round(e.Number(value)*100) / 100}, err
		}),
		cmd("lmsSoundExpanded_changeProjectVolume", []string{"VALUE"}, func(c *e.Context) (e.Result, error) {
			value, err := c.Call("audio.projectVolume")
			if err != nil {
				return e.Result{}, err
			}
			return call(c, "audio.projectVolume", math.Max(0, math.Min(100, e.Number(value)+c.Number("VALUE"))))
		}),
		rep("lmsSoundExpanded_getSoundEffect", []string{"TARGET"}, func(c *e.Context) (e.Result, error) {
			return call(c, "audio.effect", c.Field("EFFECT"), c.String("TARGET"))
		}),
		cmd("lmsSoundExpanded_stopAllSpriteSounds", []string{"TARGET"}, func(c *e.Context) (e.Result, error) {
			return call(c, "audio.stopTarget", c.String("TARGET"))
		}),
		cmd("lmsSoundExpanded_stopLooping", []string{"SOUND"}, func(c *e.Context) (e.Result, error) {
			voice, err := c.Call("audio.targetVoice", c.Args["SOUND"])
			if err != nil {
				return e.Result{}, err
			}
			return call(c, "audio.voice.set", voice, "loop", false)
		}),
		cmd("lmsSoundExpanded_playSoundAtAndWait", []string{"SOUND", "START"}, func(c *e.Context) (e.Result, error) {
			voice, started := c.Local["voice"]
			if !started {
				var err error
				voice, err = c.Call("audio.targetPlay", c.Args["SOUND"], c.Number("START"), false)
				if err != nil {
					return e.Result{}, err
				}
				if c.Local == nil {
					c.Local = map[string]any{}
				}
				c.Local["voice"] = voice
			}
			pending, err := c.Call("audio.voice.get", voice, "pending")
			return e.Result{Yield: e.Bool(pending), Again: e.Bool(pending)}, err
		}),
	)
	for _, spec := range []struct{ op, key string }{{"isSoundPlaying", "active"}, {"isLooping", "loop"}} {
		op, key := spec.op, spec.key
		x.Blocks = append(x.Blocks, e.Block{Opcode: "lmsSoundExpanded_" + op, Kind: "boolean", Inputs: []string{"SOUND"}, Handler: func(c *e.Context) (e.Result, error) {
			voice, err := c.Call("audio.targetVoice", c.Args["SOUND"])
			if err != nil {
				return e.Result{}, err
			}
			return call(c, "audio.voice.get", voice, key)
		}})
	}
	for _, name := range []string{"targets", "preciseTargets"} {
		x.Blocks = append(x.Blocks, menu("lmsSoundExpanded_menu_"+name, name))
	}
	labels := map[string]string{
		"lmsSoundExpanded_startLooping":         "start looping [SOUND]",
		"lmsSoundExpanded_startLoopingBegin":    "start looping [SOUND] loop start [START] seconds",
		"lmsSoundExpanded_startLoopingBeginEnd": "start looping [SOUND] loop region [START] to [END] seconds",
		"lmsSoundExpanded_stopLooping":          "end looping [SOUND]",
		"lmsSoundExpanded_isLooping":            "[SOUND] is looping?",
		"lmsSoundExpanded_playSoundAtAndWait":   "play sound [SOUND] from [START] seconds until done",
		"lmsSoundExpanded_playSoundAt":          "start sound [SOUND] from [START] seconds",
		"lmsSoundExpanded_playSoundToAndWait":   "play sound [SOUND] from [START] to [END] seconds until done",
		"lmsSoundExpanded_playSoundTo":          "start sound [SOUND] from [START] to [END] seconds",
		"lmsSoundExpanded_stopSound":            "stop sound [SOUND]",
		"lmsSoundExpanded_stopAllSpriteSounds":  "stop all sounds in [TARGET]",
		"lmsSoundExpanded_pauseSounds":          "pause all sounds",
		"lmsSoundExpanded_resumeSounds":         "resume all sounds",
		"lmsSoundExpanded_isSoundPlaying":       "sound [SOUND] is playing?",
		"lmsSoundExpanded_attributeOfSound":     "[ATTRIBUTE] of [SOUND]",
		"lmsSoundExpanded_getSoundEffect":       "effect [EFFECT] of [TARGET]",
		"lmsSoundExpanded_setProjectVolume":     "set project volume to [VALUE]%",
		"lmsSoundExpanded_changeProjectVolume":  "change project volume by [VALUE]",
		"lmsSoundExpanded_getProjectVolume":     "project volume",
	}
	for i := range x.Blocks {
		x.Blocks[i].Text = labels[x.Blocks[i].Opcode]
	}
	return x
}
