// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"math"

	e "gowarp/core/extension"
)

func New() e.Extension {
	threshold := 33.33
	lastDt := 0.0
	return e.Extension{
		ID: "framedropdetector",
		Blocks: []e.Block{
			{Opcode: "framedropdetector_getFps", Kind: "reporter", Text: "현재 FPS", Handler: func(c *e.Context) (e.Result, error) {
				value, err := c.Call("host.frameMilliseconds")
				if err != nil {
					return e.Result{}, err
				}
				fps := 0.0
				if dt := e.Number(value); dt > 0 {
					fps = math.Round(1000 / dt)
				}
				return e.Result{Value: fps}, nil
			}},
			{Opcode: "framedropdetector_isDropped", Kind: "boolean", Text: "프레임 드랍이 감지되었는가?", Handler: func(c *e.Context) (e.Result, error) {
				value, err := c.Call("host.frameMilliseconds")
				return e.Result{Value: e.Number(value) > threshold}, err
			}},
			{Opcode: "framedropdetector_setThreshold", Kind: "command", Inputs: []string{"MS"}, Handler: func(c *e.Context) (e.Result, error) {
				threshold = c.Number("MS")
				return e.Result{}, nil
			}},
			{Opcode: "framedropdetector_getThreshold", Kind: "reporter", Text: "현재 감지 기준 (ms)", Handler: func(*e.Context) (e.Result, error) {
				return e.Result{Value: threshold}, nil
			}},
			{Opcode: "framedropdetector_getLastDt", Kind: "reporter", Text: "마지막 프레임 지연 시간 (ms)", Handler: func(*e.Context) (e.Result, error) {
				return e.Result{Value: math.Round(lastDt*10) / 10}, nil
			}},
		},
		Events: map[string]e.Handler{
			"before-tick": func(c *e.Context) (e.Result, error) {
				value, err := c.Call("host.frameMilliseconds")
				if err != nil {
					return e.Result{}, err
				}
				if dt := e.Number(value); dt > threshold {
					lastDt = dt
				}
				return e.Result{}, nil
			},
		},
	}
}
