package vm

import (
	"encoding/json"
	"fmt"
	"maps"

	"gowarp/core/extension"
	"gowarp/core/project"
)

func (c *extensionContext) proceduresCall(op string, args []any) (any, error) {
	a := hostArgs(args)
	switch op {
	case "procedures.warp":
		return c.th.warp, nil
	case "procedures.setParam":
		if c.event != "" {
			return nil, fmt.Errorf("procedure parameters require a running script")
		}
		if c.th.params == nil {
			c.th.params = make(procedureParameters)
		}
		c.th.params[a.string(0)] = calculationValue{value: a.get(1)}
		return nil, nil
	case "procedures.getParam":
		if value, exists := c.th.params[a.string(0)]; exists {
			return value.boxed(), nil
		}
		return "", nil
	case "procedures.snapshot":
		var targets []any
		for index, target := range c.r.sourceProject.Targets {
			blocks := project.Blocks{}
			for id, b := range target.Blocks {
				if b != nil && (b.Opcode == "procedures_prototype" || b.Opcode == "procedures_call") {
					blocks[id] = b
				}
			}
			targets = append(targets, map[string]any{"target": index, "name": target.Name, "extensionStorage": target.ExtensionStorage, "blocks": blocks})
		}
		encoded, err := json.Marshal(targets)
		if err != nil {
			return nil, err
		}
		var snapshot []any
		err = json.Unmarshal(encoded, &snapshot)
		return snapshot, err
	case "procedures.updateCalls":
		return c.updateProcedureCalls(a.get(0))
	}
	return nil, fmt.Errorf("unsupported extension host operation %q", op)
}

// Validate all call updates before replacing the graph and its compiled caches.
func (c *extensionContext) updateProcedureCalls(value any) (any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var updates []struct {
		Target   int                        `json:"target"`
		ID       string                     `json:"id"`
		Mutation map[string]json.RawMessage `json:"mutation"`
		Inputs   map[string][]any           `json:"inputs"`
	}
	if err = json.Unmarshal(encoded, &updates); err != nil {
		return nil, err
	}
	if len(updates) == 0 {
		return nil, nil
	}
	copyProject := *c.r.sourceProject
	copyProject.Targets = make([]*project.Target, len(c.r.sourceProject.Targets))
	for i, target := range c.r.sourceProject.Targets {
		copyTarget := *target
		copyTarget.Blocks = make(project.Blocks, len(target.Blocks))
		maps.Copy(copyTarget.Blocks, target.Blocks)
		copyProject.Targets[i] = &copyTarget
	}
	for _, update := range updates {
		if update.Target < 0 || update.Target >= len(copyProject.Targets) {
			return nil, fmt.Errorf("invalid procedure target %d", update.Target)
		}
		target := copyProject.Targets[update.Target]
		b := target.Blocks[update.ID]
		if b == nil || b.Opcode != "procedures_call" {
			return nil, fmt.Errorf("invalid procedure call %q", update.ID)
		}
		cloned := *b
		cloned.Mutation, cloned.Inputs = update.Mutation, update.Inputs
		if _, err := mutationText(&cloned, "proccode"); err != nil {
			return nil, err
		}
		if _, err := procedureStrings(&cloned, "argumentids"); err != nil {
			return nil, err
		}
		target.Blocks[update.ID] = &cloned
	}
	p, err := extension.PrepareProject(&copyProject, c.r.extensions)
	if err != nil {
		return nil, err
	}
	c.r.replaceBlockGraph(p, &copyProject)
	return nil, nil
}

func (r *Runtime) replaceBlockGraph(p, source *project.Project) {
	r.project, r.sourceProject, r.native = p, source, false
	r.executionCodes, r.numericExpressions, r.calculations = nil, nil, nil
	r.preparedInputs, r.listWrites, r.variableWrites, r.coordinateMoves = nil, nil, nil, nil
	r.procedureCodeCache, r.procedureArgumentCache, r.controlInputs = nil, nil, nil
	for _, registered := range r.extensionBlocks {
		registered.inputs = nil
	}
	for _, target := range r.targets {
		target.source = p.Targets[target.sourceIndex]
		target.procedures = nil
		target.refreshHats()
	}
	for _, th := range r.threads {
		th.code, th.cursor, th.regions = nil, nil, nil
		for i := range th.stack {
			th.stack[i].nextLink, th.stack[i].bodyLink, th.stack[i].regions = nil, nil, nil
		}
	}
}
