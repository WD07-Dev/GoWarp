// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
)

func call(c *e.Context, op string, args ...any) (e.Result, error) {
	v, err := c.Call(op, args...)
	return e.Result{Value: v}, err
}
func cmd(op string, inputs []string, fn e.Handler) e.Block {
	return e.Block{Opcode: op, Kind: "command", Inputs: inputs, Handler: fn}
}
func New() e.Extension {
	x := e.Extension{ID: "notSound", Blocks: []e.Block{cmd("notSound_play", []string{"path"}, func(c *e.Context) (e.Result, error) {
		return call(c, "audio.url", c.String("path"))
	}), cmd("notSound_playUntilDone", []string{"path"}, func(c *e.Context) (e.Result, error) {
		voice, started := c.Local["voice"]
		if !started {
			var err error
			voice, err = c.Call("audio.url", c.String("path"))
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
	})}}
	labels := map[string]string{
		"notSound_play":          "start sound from url: [path]",
		"notSound_playUntilDone": "play sound from url: [path] until done",
	}
	for i := range x.Blocks {
		x.Blocks[i].Text = labels[x.Blocks[i].Opcode]
	}
	return x
}
