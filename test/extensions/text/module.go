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
	x := e.Extension{ID: "text"}
	for _, entry := range []struct{ op, key, input string }{{"setFont", "font", ""}, {"setText", "text", "TEXT"}, {"setColor", "color", "COLOR"}, {"setOutlineColor", "outlineColor", "COLOR"}, {"setOutlineWidth", "outlineWidth", "WIDTH"}, {"clearText", "visible", ""}} {
		entry := entry
		var inputs []string
		if entry.input != "" {
			inputs = []string{entry.input}
		}
		x.Blocks = append(x.Blocks, cmd("text_"+entry.op, inputs, func(c *e.Context) (e.Result, error) {
			raw, err := textCall(c, "get", nil)
			if err != nil {
				return e.Result{}, err
			}
			skin := raw.(map[string]any)
			if len(skin) == 0 {
				width, err := c.Call("camera.get", "width")
				if err != nil {
					return e.Result{}, err
				}
				skin = map[string]any{"width": width, "font": "Handwriting", "color": "#575e75", "outlineColor": "#000000"}
			}
			switch entry.op {
			case "setFont":
				skin[entry.key] = c.Field("FONT")
			case "clearText":
				skin[entry.key] = false
			case "setOutlineWidth":
				skin[entry.key] = math.Max(0, math.Min(100, c.Number(entry.input)))
			default:
				skin[entry.key] = c.String(entry.input)
			}
			if entry.op == "setText" {
				skin["visible"] = true
			}
			value, err := textCall(c, "set", []any{skin})
			return e.Result{Value: value}, err
		}))
	}
	return x
}

func textCall(host e.Host, operation string, args []any) (any, error) {
	switch operation {
	case "get":
		value, err := host.Call("graphics.text.get")
		if err != nil {
			return nil, err
		}
		properties, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("graphics.text.get did not return a property map")
		}
		out := make(map[string]any)
		for key, value := range properties {
			if property(key) {
				out[key] = value
			}
		}
		return out, nil
	case "set":
		if len(args) == 0 {
			return nil, fmt.Errorf("animatedtext.set requires a property map")
		}
		properties, ok := args[0].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("animatedtext.set requires a property map")
		}
		const fontSize = 24.0
		out := map[string]any{
			// Retain multiplication before division to preserve glyph raster positions.
			"fontSize":          fontSize,
			"lineHeight":        fontSize * 8,
			"lineHeightDivisor": 7.0,
			"paddingY":          fontSize / 7,
			"anchorY":           fontSize*.9 + fontSize/7,
			"align":             .5,
			"screenWidth":       true,
		}
		for key, value := range properties {
			if !property(key) {
				return nil, fmt.Errorf("unknown text property %q", key)
			}
			out[key] = value
		}
		return host.Call("graphics.text.set", out)
	}
	return nil, fmt.Errorf("unsupported text operation %q", operation)
}

func property(key string) bool {
	switch key {
	case "text", "font", "color", "outlineColor", "width", "outlineWidth", "visible":
		return true
	}
	return false
}
