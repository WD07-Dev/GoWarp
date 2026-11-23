package extension

import e "gowarp/core/extension"

// Only the FPS reporter used by the project is ported; browser asset import and editor operations remain JavaScript-only.
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
	}}, Blocks: []e.Block{{Opcode: "ShovelUtils_getfps", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) {
		if v, ok := c.State["fps"]; ok {
			return e.Result{Value: v}, nil
		}
		v, err := c.Call("runtime.fps")
		return e.Result{Value: v}, err
	}}}}
}
