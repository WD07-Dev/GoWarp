// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
)

func New() e.Extension {
	return e.Extension{ID: "lmsMoreEvents", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "lmsMoreEvents_whenStopClicked", Kind: "hat", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{}, nil
		}},
	}, Events: map[string]e.Handler{"stop": func(c *e.Context) (e.Result, error) {
		_, err := c.Call("events.startHats", "lmsMoreEvents_whenStopClicked", true)
		return e.Result{}, err
	}}}
}
