package vm

import (
	"context"
	"fmt"

	"gowarp/core/project"
)

func (th *thread) breakControl() {
	for i := len(th.stack) - 1; i >= 0; i-- {
		c := th.stack[i]
		if c.call {
			return
		}
		if c.loop || c.forEach || c.switchBlock {
			th.pc = c.exit
			th.stack = th.stack[:i]
			return
		}
	}
}

func (th *thread) inSwitch() bool {
	for i := len(th.stack) - 1; i >= 0; i-- {
		if th.stack[i].call {
			return false
		}
		if th.stack[i].switchBlock {
			return true
		}
	}
	return false
}

func (r *Runtime) forEach(th *thread, id string, b *project.Block, input func(string) (any, error)) error {
	value, err := input("VALUE")
	if err != nil {
		return err
	}
	current := -1
	if len(th.stack) > 0 {
		top := len(th.stack) - 1
		if th.stack[top].forEach && th.stack[top].header == id {
			current = top
		}
	}
	index := float64(0)
	if current >= 0 {
		index = th.stack[current].index
	}
	if !(index < number(value)) {
		if current >= 0 {
			th.stack = th.stack[:current]
		}
		return nil
	}
	_, v, err := r.variable(th.target, field(b, "VARIABLE", 1))
	if err != nil {
		return err
	}
	body, err := substack(b, "SUBSTACK")
	if err != nil {
		return err
	}
	if current < 0 {
		if len(th.stack) >= 1024 {
			return fmt.Errorf("control nesting limit exceeded")
		}
		th.stack = append(th.stack, continuation{
			forEach: true,
			header:  id,
			exit:    b.Next,
		})
		current = len(th.stack) - 1
	}
	th.stack[current].index = index + 1
	v.Value = index + 1
	th.pc = body
	return nil
}

func (r *Runtime) switchControl(ctx context.Context, th *thread, b *project.Block, budget *int) error {
	value, err := r.input(ctx, th, b, "VALUE", 0, budget)
	if err != nil {
		return err
	}
	id, err := substack(b, "SUBSTACK")
	if err != nil {
		return err
	}
	chosen, fallback := "", ""
	seen := map[string]bool{}
	for id != "" {
		if seen[id] {
			return fmt.Errorf("cyclic switch branches at %q", id)
		}
		seen[id] = true
		if len(seen) > stepsPerFrame {
			return fmt.Errorf("switch branch limit exceeded")
		}
		child := th.target.source.Blocks[id]
		if child == nil {
			return fmt.Errorf("missing switch branch %q", id)
		}
		switch child.Opcode {
		case "control_default":
			fallback = id
		case "control_case", "control_case_fallthrough":
			label, err := r.input(ctx, th, child, "VALUE", 0, budget)
			if err != nil {
				return err
			}
			if text(label) == text(value) {
				chosen = id
			}
		}
		if chosen != "" {
			break
		}
		id = child.Next
	}
	if chosen == "" {
		chosen = fallback
	}
	if chosen == "" {
		return nil
	}
	if len(th.stack) >= 1024 {
		return fmt.Errorf("control nesting limit exceeded")
	}
	th.stack = append(th.stack, continuation{
		next:        b.Next,
		exit:        b.Next,
		switchBlock: true,
	})
	th.pc = chosen
	return nil
}
