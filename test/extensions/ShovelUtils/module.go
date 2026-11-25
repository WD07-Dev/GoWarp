// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"encoding/json"
	e "gowarp/core/extension"
)

func New() e.Extension {
	return e.Extension{ID: "ShovelUtils", State: map[string]any{}, Events: map[string]e.Handler{"tick": func(c *e.Context) (e.Result, error) {
		v, err := c.Call("runtime.time")
		if err != nil {
			return e.Result{}, err
		}
		now := e.Number(v)
		times, _ := c.State["times"].([]float64)
		start := 0
		for start < len(times) && times[start] <= now-1 {
			start++
		}
		times = append(times[start:], now)
		c.State["times"] = times
		c.State["fps"] = float64(len(times))
		return e.Result{}, nil
	}}, Blocks: []e.Block{
		{Opcode: "ShovelUtils_getAllSprites", Kind: "reporter", Text: "all sprites", Handler: func(c *e.Context) (e.Result, error) {
			value, err := c.Call("target.list")
			if err != nil {
				return e.Result{}, err
			}
			names := []string{}
			for _, item := range value.([]any) {
				target := item.(map[string]any)
				if e.Number(target["cloneId"]) == 0 {
					names = append(names, e.String(target["name"]))
				}
			}
			data, err := json.Marshal(names)
			return e.Result{Value: string(data)}, err
		}},
		{Opcode: "ShovelUtils_getfps", Kind: "reporter", Text: "fps", Handler: func(c *e.Context) (e.Result, error) {
			if v, ok := c.State["fps"]; ok {
				return e.Result{Value: v}, nil
			}
			v, err := c.Call("runtime.fps")
			return e.Result{Value: v}, err
		}}}}
}
