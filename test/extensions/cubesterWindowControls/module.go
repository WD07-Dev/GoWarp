// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import e "gowarp/core/extension"

func New() e.Extension {
	x := e.Extension{ID: "cubesterWindowControls", Blocks: []e.Block{
		{Opcode: "cubesterWindowControls_isFullscreen", Text: "is window fullscreen?", Kind: "boolean", Handler: func(c *e.Context) (e.Result, error) {
			value, err := c.Call("host.projectFullscreen")
			return e.Result{Value: value}, err
		}},
		{Opcode: "cubesterWindowControls_enterFullscreen", Kind: "command", Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("host.command", "fullscreen", "true")
			return e.Result{}, err
		}},
		{Opcode: "cubesterWindowControls_exitFullscreen", Kind: "command", Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("host.command", "fullscreen", "false")
			return e.Result{}, err
		}},

		{Opcode: "cubesterWindowControls_changeTitleTo", Kind: "command", Inputs: []string{"TITLE"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("host.command", "title", c.String("TITLE"))
			return e.Result{}, err
		}},
		{Opcode: "cubesterWindowControls_closeWindow", Kind: "command", Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("host.command", "close")
			return e.Result{}, err
		}},
	}}
	for _, entry := range []struct{ op, key, text, kind string }{
		{"screenW", "screenWidth", "screen width", "reporter"},
		{"screenH", "screenHeight", "screen height", "reporter"},
		{"isFocused", "focused", "is window focused?", "boolean"},
		{"windowTitle", "title", "window title", "reporter"},
	} {
		x.Blocks = append(x.Blocks, e.Block{Opcode: "cubesterWindowControls_" + entry.op, Text: entry.text, Kind: entry.kind, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("host.window", entry.key)
			return e.Result{Value: v}, err
		}})
	}
	return x
}
