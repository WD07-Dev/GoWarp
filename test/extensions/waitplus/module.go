// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
)

func New() e.Extension {
	blocks := []e.Block{}
	for _, name := range []string{"waitSecondsAsync", "waitTicksAsync", "waitSecondsOr", "waitTicksOr", "waitSecondsOrAsync", "waitTicksOrAsync", "waitUntilAsync", "waitSecondsOrCondition", "waitTicksOrCondition", "waitSecondsOrConditionAsync", "waitTicksOrConditionAsync", "waitUntilConditionAsync"} {
		kind := "command"
		loop := name == "waitSecondsOrCondition" || name == "waitTicksOrCondition" || name == "waitSecondsOrConditionAsync" || name == "waitTicksOrConditionAsync" || name == "waitUntilConditionAsync"
		if loop {
			kind = "branch"
		}
		inputs := []string{"N", "M"}
		if name == "waitUntilAsync" || name == "waitUntilConditionAsync" {
			inputs = []string{"M"}
		}
		if name == "waitSecondsAsync" || name == "waitTicksAsync" {
			inputs = []string{"N"}
		}
		blocks = append(blocks, e.Block{Opcode: "waitplus_" + name, Kind: kind, Inputs: inputs, Handler: func(c *e.Context) (e.Result, error) { return wait(c, name, loop) }})
	}
	return e.Extension{ID: "waitplus", Blocks: blocks}
}

func positive(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Max(0, value)
}

func wait(c *e.Context, name string, loop bool) (e.Result, error) {
	if c.Bool("M") {
		return e.Result{}, nil
	}
	seconds := name == "waitSecondsAsync" || name == "waitSecondsOr" || name == "waitSecondsOrAsync" || name == "waitSecondsOrCondition" || name == "waitSecondsOrConditionAsync"
	until := name == "waitUntilAsync" || name == "waitUntilConditionAsync"
	if !until {
		if seconds {
			now, err := c.Call("runtime.monotonicTime")
			if err != nil {
				return e.Result{}, err
			}
			deadline, exists := c.Local["deadline"].(float64)
			if !exists {
				deadline = e.Number(now) + positive(c.Number("N"))
				c.Local["deadline"] = deadline
			}
			if e.Number(now) >= deadline {
				return e.Result{}, nil
			}
		} else if name == "waitTicksAsync" {
			frame, err := c.Call("runtime.frame")
			if err != nil {
				return e.Result{}, err
			}
			deadline, exists := c.Local["frame"].(float64)
			if !exists {
				deadline = e.Number(frame) + math.Ceil(positive(c.Number("N")))
				c.Local["frame"] = deadline
			}
			if e.Number(frame) >= deadline {
				return e.Result{}, nil
			}
		} else {
			remaining, exists := c.Local["remaining"].(float64)
			if !exists {
				remaining = math.Ceil(positive(c.Number("N")))
			}
			if remaining <= 0 {
				return e.Result{}, nil
			}
			c.Local["remaining"] = remaining - 1
		}
	}
	if loop {
		warp, err := c.Call("procedures.warp")
		if err != nil {
			return e.Result{}, err
		}
		// Ordinary branch continuations yield after their body. Warp loops must
		// explicitly wait for a runtime tick before executing the branch.
		return e.Result{Branch: "SUBSTACK", Again: true, ReevaluateInputs: true, YieldTick: e.Bool(warp)}, nil
	}
	force := name != "waitSecondsOr" && name != "waitTicksOr"
	return e.Result{Again: true, Yield: !force, YieldTick: force}, nil
}
