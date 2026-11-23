package extension

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"gowarp/core/project"
)

// Project and ProjectBlock describe saved project data, not live VM state.
type Project = project.Project
type ProjectBlock = project.Block

// PrepareProject runs transforms on a private copy, leaving the saved project intact.
func PrepareProject(p *project.Project, extensions []*Extension) (*project.Project, error) {
	prepared := p
	for _, def := range extensions {
		if def == nil {
			return nil, fmt.Errorf("nil extension")
		}
		if def.Prepare == nil {
			continue
		}
		if prepared == p {
			var err error
			prepared, err = copyPreparedProject(p)
			if err != nil {
				return nil, fmt.Errorf("extension %s preparation: %w", def.ID, err)
			}
		}
		if err := def.Prepare(prepared); err != nil {
			return nil, fmt.Errorf("extension %s preparation: %w", def.ID, err)
		}
		normalized, err := copyPreparedProject(prepared)
		if err != nil {
			return nil, fmt.Errorf("extension %s preparation: %w", def.ID, err)
		}
		prepared = normalized
		if err := validatePreparedProject(p, prepared); err != nil {
			return nil, fmt.Errorf("extension %s preparation: %w", def.ID, err)
		}
	}
	return prepared, nil
}

// The JSON boundary also normalizes Go integer literals to Scratch input values.
func copyPreparedProject(p *project.Project) (*project.Project, error) {
	if p == nil {
		return nil, fmt.Errorf("missing project")
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var copyProject project.Project
	type projectData project.Project
	type targetData project.Target
	wire := struct {
		*projectData
		Targets []json.RawMessage `json:"targets"`
	}{projectData: (*projectData)(&copyProject)}
	if err = json.Unmarshal(data, &wire); err != nil {
		return nil, err
	}
	if wire.Targets != nil {
		copyProject.Targets = make([]*project.Target, len(wire.Targets))
	}
	for i, raw := range wire.Targets {
		var target *targetData
		if err = json.Unmarshal(raw, &target); err != nil {
			return nil, err
		}
		if target == nil {
			return nil, fmt.Errorf("nil target")
		}
		target.ScriptOrder = slices.Clone(p.Targets[i].ScriptOrder)
		if p.Targets[i].Blocks == nil {
			target.Blocks = nil
		}
		copyProject.Targets[i] = (*project.Target)(target)
	}
	return &copyProject, nil
}

func validatePreparedProject(source, prepared *project.Project) error {
	if prepared == nil || len(source.Targets) != len(prepared.Targets) {
		return fmt.Errorf("only block graphs may be changed")
	}
	for i, target := range prepared.Targets {
		if target == nil || !slices.Equal(target.ScriptOrder, source.Targets[i].ScriptOrder) {
			return fmt.Errorf("target identity and script order must be preserved")
		}
		original := source.Targets[i].Blocks
		for id, block := range target.Blocks {
			if block == nil || block.Opcode == "" || id == "" {
				return fmt.Errorf("target %q: invalid block %q", target.Name, id)
			}
			changed := !reflect.DeepEqual(block, original[id])
			if block.Next != "" && target.Blocks[block.Next] == nil && (changed || original[block.Next] != nil) {
				return fmt.Errorf("target %q block %q: missing next block %q", target.Name, id, block.Next)
			}
			for _, input := range block.Inputs {
				if len(input) == 0 {
					continue
				}
				for _, value := range input[1:] {
					reference, ok := value.(string)
					if !ok || reference == "" || target.Blocks[reference] != nil {
						continue
					}
					if changed || original[reference] != nil {
						return fmt.Errorf("target %q block %q: missing input block %q", target.Name, id, reference)
					}
				}
			}
		}
	}
	metadata := func(p *project.Project) ([]byte, error) {
		copyProject := *p
		copyProject.Targets = make([]*project.Target, len(p.Targets))
		for i, target := range p.Targets {
			copyTarget := *target
			copyTarget.Blocks = nil
			copyProject.Targets[i] = &copyTarget
		}
		return json.Marshal(&copyProject)
	}
	before, err := metadata(source)
	if err != nil {
		return err
	}
	after, err := metadata(prepared)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, after) {
		return fmt.Errorf("only block graphs may be changed")
	}
	return nil
}
