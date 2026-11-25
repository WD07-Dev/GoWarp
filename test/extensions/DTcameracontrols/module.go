// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
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
	labels := map[string]string{"DTcameracontrols_getX": "camera x", "DTcameracontrols_getY": "camera y", "DTcameracontrols_getZoom": "camera zoom", "DTcameracontrols_getDirection": "camera direction", "DTcameracontrols_getCol": "background color", "DTcameracontrols_blocktx": "stage to world x: [x]", "DTcameracontrols_blockty": "stage to world y: [y]"}
	return e.Block{Opcode: op, Text: labels[op], Kind: "reporter", Inputs: inputs, Handler: fn}
}
func menu(op, key string) e.Block {
	return rep(op, nil, func(c *e.Context) (e.Result, error) {
		return e.Result{Value: c.Field(key)}, nil
	})
}
func New() e.Extension {

	x := e.Extension{ID: "DTcameracontrols", Events: map[string]e.Handler{"load": func(c *e.Context) (e.Result, error) {
		if _, err := c.Call("options.set", "fencing", false); err != nil {
			return e.Result{}, err
		}
		return call(c, "options.set", "interpolation", false)
	}}}
	for _, entry := range []struct{ name, key string }{{"X", "x"}, {"Y", "y"}, {"Zoom", "zoom"}, {"Direction", "direction"}, {"Col", "color"}} {
		entry := entry
		x.Blocks = append(x.Blocks, rep("DTcameracontrols_get"+entry.name, nil, func(c *e.Context) (e.Result, error) {
			return call(c, "camera.get", entry.key)
		}), cmd("DTcameracontrols_set"+entry.name, []string{"val"}, func(c *e.Context) (e.Result, error) {
			return call(c, "camera.set", entry.key, c.Args["val"])
		}))
		if entry.key != "color" {
			x.Blocks = append(x.Blocks, cmd("DTcameracontrols_change"+entry.name, []string{"val"}, func(c *e.Context) (e.Result, error) {
				v, err := c.Call("camera.get", entry.key)
				if err != nil {
					return e.Result{}, err
				}
				return call(c, "camera.set", entry.key, e.Number(v)+c.Number("val"))
			}))
		}
	}
	x.Blocks = append(x.Blocks, cmd("DTcameracontrols_setBoth", []string{"x", "y"}, func(c *e.Context) (e.Result, error) {
		if _, err := c.Call("camera.set", "x", c.Number("x")); err != nil {
			return e.Result{}, err
		}
		return call(c, "camera.set", "y", c.Number("y"))
	}))
	for _, op := range []string{"rotateCW", "rotateCCW"} {
		op := op
		x.Blocks = append(x.Blocks, cmd("DTcameracontrols_"+op, []string{"val"}, func(c *e.Context) (e.Result, error) {
			v, err := c.Call("camera.get", "direction")
			if err != nil {
				return e.Result{}, err
			}
			n := c.Number("val")
			if op == "rotateCCW" {
				n = -n
			}
			return call(c, "camera.set", "direction", e.Number(v)+n)
		}))
	}

	for _, axis := range []string{"x", "y"} {
		axis := axis
		x.Blocks = append(x.Blocks, rep("DTcameracontrols_blockt"+axis, []string{axis}, func(c *e.Context) (e.Result, error) {
			pos, err := c.Call("camera.get", axis)
			if err != nil {
				return e.Result{}, err
			}
			zoom, err := c.Call("camera.get", "zoom")
			return e.Result{Value: c.Number(axis)/(e.Number(zoom)/100) + e.Number(pos)}, err
		}))
	}
	x.Blocks = append(x.Blocks, menu("DTcameracontrols_menu_sprites", "sprites"))
	for _, op := range []string{"moveSteps", "goTo", "pointTowards"} {
		op := op
		inputs := []string{"sprite"}
		if op == "moveSteps" {
			inputs = []string{"val"}
		}
		x.Blocks = append(x.Blocks, cmd("DTcameracontrols_"+op, inputs, func(c *e.Context) (e.Result, error) {
			px, err := c.Call("camera.get", "x")
			if err != nil {
				return e.Result{}, err
			}
			py, err := c.Call("camera.get", "y")
			if err != nil {
				return e.Result{}, err
			}
			cx, cy := e.Number(px), e.Number(py)
			if op == "moveSteps" {
				d, err := c.Call("camera.get", "direction")
				if err != nil {
					return e.Result{}, err
				}
				a := (90 - e.Number(d)) * math.Pi / 180
				cx += c.Number("val") * math.Cos(a)
				cy += c.Number("val") * math.Sin(a)
			} else {
				records, err := c.Call("target.list")
				if err != nil {
					return e.Result{}, err
				}
				ref := ""
				for _, v := range records.([]any) {
					t := v.(map[string]any)
					if e.String(t["name"]) == c.String("sprite") && e.Number(t["cloneId"]) == 0 && !e.Bool(t["isStage"]) {
						ref = e.String(t["id"])
						break
					}
				}
				if ref == "" {
					return e.Result{}, nil
				}
				tx, err := c.Call("target.get", "x", ref)
				if err != nil {
					return e.Result{}, err
				}
				ty, err := c.Call("target.get", "y", ref)
				if err != nil {
					return e.Result{}, err
				}
				if op == "pointTowards" {
					return call(c, "camera.set", "direction", 90-math.Atan2(e.Number(ty)-cy, e.Number(tx)-cx)*180/math.Pi)
				}
				cx, cy = math.Floor(e.Number(tx)+.5), math.Floor(e.Number(ty)+.5)
			}
			if _, err := c.Call("camera.set", "x", cx); err != nil {
				return e.Result{}, err
			}
			return call(c, "camera.set", "y", cy)
		}))
	}
	return x
}
