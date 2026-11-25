// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
	"sort"
	"strconv"
	"strings"
)

func New() e.Extension {
	return e.Extension{ID: "lmsTimers", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "lmsTimers_whenTimerOp", Kind: "hat", Inputs: []string{"TIMER", "NUM"}, Handler: func(c *e.Context) (e.Result, error) {
			t, ok := timers(c)[c.String("TIMER")]
			if !ok {
				return e.Result{Value: false}, nil
			}
			now, err := clock(c)
			v := value(t, now)
			op := c.Field("OP")
			return e.Result{Value: op == ">" && v > c.Number("NUM") || op == "<" && v < c.Number("NUM")}, err
		}},
		{Opcode: "lmsTimers_changeTimer", Kind: "command", Inputs: []string{"TIMER", "NUM"}, Handler: func(c *e.Context) (e.Result, error) {
			ts, name := timers(c), c.String("TIMER")
			t, ok := ts[name]
			if !ok {
				now, err := clock(c)
				if err != nil {
					return e.Result{}, err
				}
				t.Start = now
				remember(c, name)
			}
			t.Value += c.Number("NUM")
			ts[name] = t
			return e.Result{}, nil
		}},
		{Opcode: "lmsTimers_timerExists", Text: "timer [TIMER] exists?", Kind: "boolean", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			_, ok := timers(c)[c.String("TIMER")]
			return e.Result{Value: ok}, nil
		}},
		{Opcode: "lmsTimers_listExistingTimers", Text: "list existing timers", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) {
			order, _ := c.State["order"].([]string)
			indices := []string{}
			names := []string{}
			for _, name := range order {
				if _, ok := timers(c)[name]; !ok {
					continue
				}
				n, err := strconv.ParseUint(name, 10, 32)
				if err == nil && n < 4294967295 && strconv.FormatUint(n, 10) == name {
					indices = append(indices, name)
				} else {
					names = append(names, name)
				}
			}
			sort.Slice(indices, func(i, j int) bool {
				a, _ := strconv.ParseUint(indices[i], 10, 32)
				b, _ := strconv.ParseUint(indices[j], 10, 32)
				return a < b
			})
			return e.Result{Value: strings.Join(append(indices, names...), ",")}, nil
		}},

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

		{Opcode: "lmsTimers_valueOfTimer", Text: "value of timer [TIMER]", Kind: "reporter", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			t, ok := timers(c)[c.String("TIMER")]
			if !ok {
				return e.Result{Value: ""}, nil
			}
			now, err := clock(c)
			return e.Result{Value: value(t, now)}, err
		}},
		{Opcode: "lmsTimers_removeTimers", Kind: "command", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			c.State["timers"] = map[string]timer{}
			c.State["order"] = []string{}
			return e.Result{}, nil
		}},
		{Opcode: "lmsTimers_removeTimer", Kind: "command", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			name := c.String("TIMER")
			delete(timers(c), name)
			order, _ := c.State["order"].([]string)
			for i, n := range order {
				if n == name {
					c.State["order"] = append(order[:i], order[i+1:]...)
					break
				}
			}
			return e.Result{}, nil
		}},
		{Opcode: "lmsTimers_startResetTimer", Kind: "command", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			now, err := clock(c)
			if err == nil {
				remember(c, c.String("TIMER"))
				timers(c)[c.String("TIMER")] = timer{Start: now}
			}
			return e.Result{}, err
		}},
		{Opcode: "lmsTimers_setTimer", Kind: "command", Inputs: []string{"TIMER", "NUM"}, Handler: func(c *e.Context) (e.Result, error) {
			now, err := clock(c)
			if err == nil {
				remember(c, c.String("TIMER"))
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
	}, Events: map[string]e.Handler{"tick": func(c *e.Context) (e.Result, error) {
		_, err := c.Call("events.pollHats", "lmsTimers_whenTimerOp", "edge")
		return e.Result{}, err
	}, "flag": func(c *e.Context) (e.Result, error) {
		c.State["timers"] = map[string]timer{}
		c.State["order"] = []string{}
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

func remember(c *e.Context, name string) {
	if _, ok := timers(c)[name]; !ok {
		order, _ := c.State["order"].([]string)
		c.State["order"] = append(order, name)
	}
}
