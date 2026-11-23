// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
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
func New() e.Extension {

	x := e.Extension{ID: "stretch"}
	for _, axis := range []string{"X", "Y"} {
		axis := axis
		x.Blocks = append(x.Blocks, rep("stretch_get"+axis, nil, func(c *e.Context) (e.Result, error) {
			return call(c, "stretch.get", strings.ToLower(axis))
		}))
	}
	for _, op := range []string{"setStretch", "changeStretch", "setStretchX", "setStretchY", "changeStretchX", "changeStretchY"} {
		op := op
		change := strings.HasPrefix(op, "change")
		axes := []string{"X", "Y"}
		if strings.HasSuffix(op, "X") {
			axes = []string{"X"}
		}
		if strings.HasSuffix(op, "Y") {
			axes = []string{"Y"}
		}
		keys := append([]string(nil), axes...)
		if change {
			for i := range keys {
				keys[i] = "D" + keys[i]
			}
		}
		x.Blocks = append(x.Blocks, cmd("stretch_"+op, keys, func(c *e.Context) (e.Result, error) {
			for i, axis := range axes {
				v := rawNumber(c.Args[keys[i]])
				if change {
					old, err := c.Call("stretch.get", strings.ToLower(axis))
					if err != nil {
						return e.Result{}, err
					}
					v += e.Number(old)
				}
				if _, err := c.Call("stretch.set", strings.ToLower(axis), v); err != nil {
					return e.Result{}, err
				}
			}
			return e.Result{}, nil
		}))
	}
	return x
}
func rawNumber(v any) float64 {
	s := strings.TrimSpace(e.String(v))
	if s == "" {
		return 0
	}
	if s == "Infinity" || s == "+Infinity" {
		return math.Inf(1)
	}
	if s == "-Infinity" {
		return math.Inf(-1)
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN()
	}
	return n
}
