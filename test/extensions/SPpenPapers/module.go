// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"encoding/json"
	e "gowarp/core/extension"
	"strings"
)

const defaultLayer = "Scratch Stage"

func layerName(c *e.Context) string {
	name := strings.TrimSpace(c.String("NAME"))
	if name == "default" {
		return defaultLayer
	}
	return name
}
func invoke(c *e.Context, op string, args ...any) (e.Result, error) {
	value, err := c.Call(op, args...)
	return e.Result{Value: value}, err
}
func New() e.Extension {
	result := e.Extension{ID: "SPpenPapers", State: map[string]any{}, Events: map[string]e.Handler{
		"load": func(c *e.Context) (e.Result, error) {
			c.State["names"] = []string{"default"}
			return invoke(c, "graphics.layer.create", defaultLayer)
		},
	}}
	add := func(op, kind string, inputs []string, handler e.Handler) {
		result.Blocks = append(result.Blocks, e.Block{Opcode: "SPpenPapers_" + op, Kind: kind, Inputs: inputs, Handler: handler, BorrowContext: true})
	}
	for _, key := range []string{"PAPERS", "TARGETS", "VISIBILITY", "PLACEMENT", "RELATIVE_SIDE"} {
		add("menu_"+key, "reporter", nil, func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field(key)}, nil })
	}
	add("createPaper", "command", []string{"NAME"}, func(c *e.Context) (e.Result, error) {
		name := strings.TrimSpace(c.String("NAME"))
		if name == "" {
			return e.Result{}, nil
		}
		found, err := c.Call("graphics.layer.exists", layerName(c))
		if err != nil || e.Bool(found) {
			return e.Result{}, err
		}
		_, err = c.Call("graphics.layer.create", layerName(c))
		if err == nil {
			names, _ := c.State["names"].([]string)
			c.State["names"] = append(names, name)
		}
		return e.Result{}, err
	})
	add("removePaper", "command", []string{"NAME"}, func(c *e.Context) (e.Result, error) {
		if layerName(c) == defaultLayer {
			return e.Result{}, nil
		}
		names, _ := c.State["names"].([]string)
		kept := names[:0]
		for _, name := range names {
			if name != strings.TrimSpace(c.String("NAME")) {
				kept = append(kept, name)
			}
		}
		c.State["names"] = kept
		return invoke(c, "graphics.layer.delete", layerName(c))
	})
	add("allPapers", "reporter", nil, func(c *e.Context) (e.Result, error) {
		raw, err := json.Marshal(c.State["names"])
		return e.Result{Value: string(raw)}, err
	})
	for _, item := range []struct{ opcode, operation, kind string }{
		{"paperExists", "exists", "boolean"}, {"switchPaper", "select", "command"}, {"bindTarget", "bind", "command"}, {"clearPaper", "clear", "command"}, {"paperVisibe", "visible", "boolean"},
	} {
		add(item.opcode, item.kind, []string{"NAME"}, func(c *e.Context) (e.Result, error) { return invoke(c, "graphics.layer."+item.operation, layerName(c)) })
	}
	add("unbindTarget", "command", nil, func(c *e.Context) (e.Result, error) { return invoke(c, "graphics.layer.bind", "") })
	add("currentPaper", "reporter", nil, func(c *e.Context) (e.Result, error) {
		name, err := c.Call("graphics.layer.selected")
		if name == defaultLayer {
			name = "default"
		}
		return e.Result{Value: name}, err
	})
	add("resynchronizePaper", "command", nil, func(c *e.Context) (e.Result, error) {
		name, err := c.Call("graphics.layer.selected")
		if err != nil {
			return e.Result{}, err
		}
		return invoke(c, "graphics.layer.select", name)
	})
	add("clearAllPapers", "command", nil, func(c *e.Context) (e.Result, error) {
		names, _ := c.State["names"].([]string)
		for _, name := range names {
			if name == "default" {
				name = defaultLayer
			}
			if _, err := c.Call("graphics.layer.clear", name); err != nil {
				return e.Result{}, err
			}
		}
		return e.Result{}, nil
	})
	add("setPaperRenderQuality", "command", []string{"NAME", "QUALITY"}, func(c *e.Context) (e.Result, error) {
		return invoke(c, "graphics.layer.quality", layerName(c), c.Number("QUALITY"))
	})
	add("setPaperPlacement", "command", []string{"NAME", "PLACEMENT"}, func(c *e.Context) (e.Result, error) {
		return invoke(c, "graphics.layer.placement", layerName(c), c.String("PLACEMENT"))
	})
	add("movePaperNextToSelf", "command", []string{"NAME", "SIDE"}, func(c *e.Context) (e.Result, error) {
		return invoke(c, "graphics.layer.move", layerName(c), c.String("SIDE"))
	})
	add("paperVisibility", "command", []string{"NAME", "TYPE"}, func(c *e.Context) (e.Result, error) {
		return invoke(c, "graphics.layer.visible", layerName(c), c.String("TYPE") == "show")
	})
	add("paperDrawableId", "reporter", []string{"NAME"}, func(c *e.Context) (e.Result, error) {
		found, err := c.Call("graphics.layer.exists", layerName(c))
		value := ""
		if e.Bool(found) {
			value = layerName(c)
		}
		return e.Result{Value: value}, err
	})
	return result
}
