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
	return e.Extension{ID: "lmsSoundExpanded", Blocks: []e.Block{
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
}
