// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"strconv"

	e "gowarp/core/extension"
)

func New() e.Extension {
	var previous, delta float64
	started := false
	return e.Extension{
		ID: "dtbyxeroname",
		Blocks: []e.Block{
			{Opcode: "dtbyxeroname_dt", Kind: "reporter", Text: "ΔT", BorrowContext: true, Handler: func(*e.Context) (e.Result, error) {
				return e.Result{Value: delta}, nil
			}},
			{Opcode: "dtbyxeroname_fps", Kind: "reporter", Text: "fps", BorrowContext: true, Handler: func(*e.Context) (e.Result, error) {
				fps, _ := strconv.ParseFloat(strconv.FormatFloat(1/delta, 'f', 2, 64), 64)
				return e.Result{Value: fps}, nil
			}},
		},
		Events: map[string]e.Handler{
			"before-tick": func(c *e.Context) (e.Result, error) {
				value, err := c.Call("runtime.monotonicTime")
				if err != nil {
					return e.Result{}, err
				}
				now := e.Number(value)
				if !started {
					rate, err := c.Call("runtime.fps")
					if err != nil {
						return e.Result{}, err
					}
					delta = 1 / e.Number(rate)
				} else {
					delta = now - previous
				}
				previous, started = now, true
				return e.Result{}, nil
			},
		},
	}
}
