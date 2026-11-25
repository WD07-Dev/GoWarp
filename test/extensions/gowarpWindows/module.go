// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"encoding/json"
	"fmt"
	e "gowarp/core/extension"
)

func New() e.Extension {
	return e.Extension{ID: "gowarpWindows", Blocks: []e.Block{
		{Opcode: "gowarpWindows_viewport", Kind: "command", Inputs: []string{"ID", "X", "Y", "W", "H"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("window.viewport", c.String("ID"), c.Number("X"), c.Number("Y"), c.Number("W"), c.Number("H"))
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_moveViewport", Kind: "command", Inputs: []string{"ID", "X", "Y"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("window.viewport", c.String("ID"), c.Number("X"), c.Number("Y"))
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_stageSize", Text: "stage [DIMENSION]", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("camera.get", c.Field("DIMENSION"))
			return e.Result{Value: v}, err
		}},
		{Opcode: "gowarpWindows_boundary", Kind: "command", Inputs: []string{"ID", "COLOR"}, Handler: func(c *e.Context) (e.Result, error) {
			// Boundary guides belong to the browser editor, not the exported game.
			return e.Result{}, nil
		}},
		{Opcode: "gowarpWindows_create", Kind: "command", Inputs: []string{"ID", "X", "Y", "W", "H"}, Handler: func(c *e.Context) (e.Result, error) {
			id := c.String("ID")
			width, height := c.Number("W"), c.Number("H")
			data, err := json.Marshal(map[string]any{
				"id": id, "title": id,
				"x": c.Number("X"), "y": c.Number("Y"),
				"width": width, "height": height,
				"stageWidth": width, "stageHeight": height,
				"border": e.Bool(c.Field("BORDER")), "resizable": true, "opacity": 1,
			})
			if err != nil {
				return e.Result{}, err
			}
			_, err = c.Call("window.create", string(data))
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_monitor", Text: "monitor [PROPERTY] of window [ID]", Kind: "reporter", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("window.monitor", c.String("ID"))
			if err != nil {
				return e.Result{}, err
			}
			var monitor map[string]float64
			if err := json.Unmarshal([]byte(e.String(v)), &monitor); err != nil {
				return e.Result{}, err
			}
			value, ok := monitor[c.Field("PROPERTY")]
			if !ok {
				return e.Result{}, fmt.Errorf("unknown monitor property %q", c.Field("PROPERTY"))
			}
			return e.Result{Value: value}, nil
		}},
		{Opcode: "gowarpWindows_center", Kind: "command", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			scope := c.Field("SCOPE")
			if scope != "window" && scope != "all" {
				return e.Result{}, fmt.Errorf("unknown centering scope %q", scope)
			}
			_, err := c.Call("window.center", c.String("ID"), scope == "all")
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_move", Kind: "command", Inputs: []string{"ID", "X", "Y"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("window.move", c.String("ID"), c.Number("X"), c.Number("Y"))
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_set", Kind: "command", Inputs: []string{"ID", "JSON"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("window.set", c.String("ID"), c.String("JSON"))
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_setBoolean", Kind: "command", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			return setProperty(c, c.Field("PROPERTY"), e.Bool(c.Field("VALUE")))
		}},
		{Opcode: "gowarpWindows_topmost", Kind: "command", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			return setProperty(c, "topmost", e.Bool(c.Field("VALUE")))
		}},
		{Opcode: "gowarpWindows_opacity", Kind: "command", Inputs: []string{"ID", "PERCENT"}, Handler: func(c *e.Context) (e.Result, error) {
			return setProperty(c, "opacity", c.Number("PERCENT")/100)
		}},
		{Opcode: "gowarpWindows_list", Text: "window IDs", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("window.list")
			if err != nil {
				return e.Result{}, err
			}
			data, err := json.Marshal(v)
			return e.Result{Value: string(data)}, err
		}},
		{Opcode: "gowarpWindows_quit", Kind: "command", Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("host.command", "close")
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_remove", Kind: "command", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("window.remove", c.String("ID"))
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_state", Text: "window [ID] is [STATE]?", Kind: "boolean", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("window.state", c.String("ID"), c.Field("STATE"))
			return e.Result{Value: v}, err
		}},
		{Opcode: "gowarpWindows_get", Text: "window [ID] properties", Kind: "reporter", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("window.get", c.String("ID"))
			return e.Result{Value: v}, err
		}},
	}}
}

func setProperty(c *e.Context, property string, value any) (e.Result, error) {
	data, err := json.Marshal(map[string]any{property: value})
	if err != nil {
		return e.Result{}, err
	}
	_, err = c.Call("window.set", c.String("ID"), string(data))
	return e.Result{}, err
}
