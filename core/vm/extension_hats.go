package vm

import (
	"context"
	"fmt"
	"gowarp/core/extension"
)

// A predicate's Local storage belongs to one hat on one target. Its inputs may
// suspend independently, without borrowing the running body thread's state.
type polledHat struct {
	local      map[string]any
	args       map[string]any
	input      int
	evaluation *thread
	previous   bool
}

func (c *extensionContext) pollHats(args []any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("events.pollHats requires opcode and mode")
	}
	opcode, mode := extension.String(args[0]), extension.String(args[1])
	if mode != "edge" && mode != "level" {
		return nil, fmt.Errorf("unknown hat polling mode %q", mode)
	}
	r := c.r
	registered := r.extensionBlocks[opcode]
	if registered == nil || registered.block.Kind != "hat" || registered.block.Handler == nil {
		return nil, fmt.Errorf("unregistered predicate hat %q", opcode)
	}
	if registered.polling {
		return nil, fmt.Errorf("recursive polling of hat %q", opcode)
	}
	registered.polling = true
	defer func() { registered.polling = false }()
	if registered.hatPolls == nil {
		registered.hatPolls = make(map[*target]map[string]*polledHat)
	}
	for target := range registered.hatPolls {
		if target.dead {
			delete(registered.hatPolls, target)
		}
	}
	count := 0
	// Snapshot executable order: a predicate may delete a target or create a clone.
	targets := append([]*target(nil), r.executable...)
	for i := len(targets) - 1; i >= 0; i-- {
		target := targets[i]
		if target.dead {
			continue
		}
		for _, id := range target.hats {
			b := target.source.Blocks[id]
			if b.Opcode != opcode {
				continue
			}
			states := registered.hatPolls[target]
			if states == nil {
				states = make(map[string]*polledHat)
				registered.hatPolls[target] = states
			}
			state := states[id]
			if state == nil {
				state = &polledHat{local: make(map[string]any)}
				states[id] = state
			}
			if state.args == nil {
				state.args = make(map[string]any, len(registered.block.Inputs))
			}
			ready := true
			for state.input < len(registered.block.Inputs) {
				name := registered.block.Inputs[state.input]
				if state.evaluation == nil {
					state.evaluation = &thread{target: target, inputRequest: &inputRequest{block: b, name: name}}
				}
				if err := r.step(context.Background(), state.evaluation); err != nil {
					return nil, err
				}
				if !state.evaluation.done {
					ready = false
					break
				}
				state.args[name] = state.evaluation.inputRequest.value
				state.evaluation = nil
				state.input++
			}
			if !ready {
				continue
			}
			fields := make(map[string]string, len(b.Fields))
			for name := range b.Fields {
				fields[name] = field(b, name, 0)
			}
			host := &extensionContext{r: r, th: &thread{target: target}}
			ctx := &extension.Context{Args: state.args, Fields: fields, State: registered.owner.State, Local: state.local, Host: host}
			result, err := registered.block.Handler(ctx)
			state.args = nil
			state.input = 0
			if err != nil {
				return nil, fmt.Errorf("extension hat %s: %w", opcode, err)
			}
			if result.Yield || result.YieldTick || result.Again || result.Branch != "" {
				return nil, fmt.Errorf("extension hat %s predicate cannot yield or branch", opcode)
			}
			active := extension.Bool(result.Value)
			if active && (mode == "level" || !state.previous) && !target.dead {
				if r.startHat(target, id, false) != nil {
					count++
				}
			}
			state.previous = active
		}
	}
	if count > 0 {
		r.stopped = false
	}
	return float64(count), nil
}
