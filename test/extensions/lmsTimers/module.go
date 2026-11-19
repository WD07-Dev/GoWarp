// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
)

func New() e.Extension {
	return e.Extension{ID: "lmsTimers", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "lmsTimers_resumeTimer", Kind: "command", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			ts, name := timers(c), c.String("TIMER")
			t, ok := ts[name]
			if !ok || !t.Paused {
				return e.Result{}, nil
			}
			now, err := clock(c)
			if err != nil {
				return e.Result{}, err
			}
			t.Start, t.Paused = now, false
			ts[name] = t
			return e.Result{}, nil
		}},

		{Opcode: "lmsTimers_valueOfTimer", Kind: "reporter", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			t, ok := timers(c)[c.String("TIMER")]
			if !ok {
				return e.Result{Value: ""}, nil
			}
			now, err := clock(c)
			return e.Result{Value: value(t, now)}, err
		}},
		{Opcode: "lmsTimers_removeTimers", Kind: "command", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			c.State["timers"] = map[string]timer{}
			return e.Result{}, nil
		}},
		{Opcode: "lmsTimers_removeTimer", Kind: "command", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			delete(timers(c), c.String("TIMER"))
			return e.Result{}, nil
		}},
		{Opcode: "lmsTimers_startResetTimer", Kind: "command", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			now, err := clock(c)
			if err == nil {
				timers(c)[c.String("TIMER")] = timer{Start: now}
			}
			return e.Result{}, err
		}},
		{Opcode: "lmsTimers_setTimer", Kind: "command", Inputs: []string{"TIMER", "NUM"}, Handler: func(c *e.Context) (e.Result, error) {
			now, err := clock(c)
			if err == nil {
				timers(c)[c.String("TIMER")] = timer{Start: now, Value: c.Number("NUM")}
			}
			return e.Result{}, err
		}},
		{Opcode: "lmsTimers_pauseTimer", Kind: "command", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			now, err := clock(c)
			if err != nil {
				return e.Result{}, err
			}
			ts := timers(c)
			name := c.String("TIMER")
			if t, ok := ts[name]; ok {
				t.Value = value(t, now)
				t.Paused = true
				ts[name] = t
			}
			return e.Result{}, nil
		}},
	}, Events: map[string]e.Handler{"flag": func(c *e.Context) (e.Result, error) {
		c.State["timers"] = map[string]timer{}
		return e.Result{}, nil
	}}}
}

type timer struct {
	Start, Value float64
	Paused       bool
}

func clock(c *e.Context) (float64, error) {
	v, err := c.Call("runtime.monotonicTime")
	return math.Floor(e.Number(v) * 1000), err
}
func timers(c *e.Context) map[string]timer {
	v, ok := c.State["timers"].(map[string]timer)
	if !ok {
		v = map[string]timer{}
		c.State["timers"] = v
	}
	return v
}
func value(t timer, now float64) float64 {
	if t.Paused {
		return t.Value
	}
	return t.Value + (now-t.Start)/1000
}
