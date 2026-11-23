// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import e "gowarp/core/extension"

func New() e.Extension {
	return e.Extension{ID: "cubesterWindowControls", Blocks: []e.Block{
		{Opcode: "cubesterWindowControls_isFullscreen", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) {
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
}
