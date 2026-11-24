package vm

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"gowarp/core/extension"
	"gowarp/core/project"
)

type EditorBlocks struct {
	Target      int            `json:"target"`
	Blocks      project.Blocks `json:"blocks"`
	ScriptOrder []string       `json:"scriptOrder"`
}

// UpdateEditorBlocks applies edits on the VM thread without restarting the player.
func (r *Runtime) UpdateEditorBlocks(edits []EditorBlocks) error {
	if len(edits) == 0 {
		return nil
	}
	source := *r.sourceProject
	source.Targets = slices.Clone(source.Targets)
	seen := make(map[int]bool, len(edits))
	for _, edit := range edits {
		if edit.Target < 0 || edit.Target >= len(source.Targets) || seen[edit.Target] {
			return fmt.Errorf("invalid editor target %d", edit.Target)
		}
		seen[edit.Target] = true
		target := *source.Targets[edit.Target]
		target.Blocks, target.ScriptOrder = edit.Blocks, edit.ScriptOrder
		source.Targets[edit.Target] = &target
	}
	prepared, err := extension.PrepareProject(&source, r.extensions)
	if err != nil {
		return err
	}
	changed := make(map[int]bool)
	for index, target := range prepared.Targets {
		for id, b := range target.Blocks {
			if b == nil || b.Opcode == "" {
				return fmt.Errorf("invalid editor block %q", id)
			}
			prefix, _, _ := strings.Cut(b.Opcode, "_")
			if !extension.IsBuiltin(prefix) && r.extensionBlocks[b.Opcode] == nil {
				return fmt.Errorf("block %s requires an extension rebuild", b.Opcode)
			}
			if b.Next != "" && target.Blocks[b.Next] == nil {
				return fmt.Errorf("block %q refers to missing next block %q", id, b.Next)
			}
		}
		old := r.project.Targets[index]
		changed[index] = !reflect.DeepEqual(old.Blocks, target.Blocks) || !slices.Equal(old.ScriptOrder, target.ScriptOrder)
	}
	// Threads in an edited target may hold removed blocks or suspended inputs.
	for _, th := range r.threads {
		if changed[th.target.sourceIndex] {
			th.done = true
			if th.editorReport != nil {
				th.editorReport("", fmt.Errorf("script changed during execution"))
				th.editorReport = nil
			}
		}
	}
	r.replaceBlockGraph(prepared, &source)
	for _, target := range r.targets {
		if changed[target.sourceIndex] {
			target.edgeInputs, target.edges, target.hatThreads = nil, nil, nil
		}
	}
	r.cleanQuestion()
	return nil
}

// ToggleScript follows editor stack clicks without restarting the project.
// Call it on the VM's execution thread, like Tick and GreenFlag.
func (r *Runtime) ToggleScript(source int, id string, reporter bool, report func(string, error)) (bool, error) {
	var owner *target
	for _, t := range r.targets {
		if t.sourceIndex == source && t.state.CloneID == 0 && !t.dead {
			owner = t
			break
		}
	}
	if owner == nil || owner.source.Blocks[id] == nil {
		return false, fmt.Errorf("block is not in this preview; run the updated project first")
	}
	b := owner.source.Blocks[id]
	hat := strings.HasPrefix(b.Opcode, "event_when") || b.Opcode == "control_start_as_clone"
	edge := b.Opcode == "event_whengreaterthan"
	if registered := r.extensionBlocks[b.Opcode]; registered != nil && registered.block.Kind == "hat" {
		hat = true
		edge = registered.block.Handler != nil
	}
	for _, th := range r.threads {
		if th.target == owner && th.hat == id && !th.done && !th.replaced {
			if edge && !th.stackClick {
				continue
			}
			th.done = true
			r.cleanQuestion()
			return false, nil
		}
	}
	th := &thread{target: owner, hat: id, pc: id, slot: len(r.threads), stackClick: true}
	if reporter {
		th.pc = ""
		th.editorReport = report
		th.inputRequest = &inputRequest{
			block: &project.Block{Inputs: map[string][]any{"VALUE": {float64(2), id}}},
			name:  "VALUE",
		}
	} else if hat || b.Opcode == "procedures_definition" {
		th.pc = b.Next
	}
	if r.native && hat {
		th.regions = nativeScripts[source][id]
	}
	r.stopped = false
	r.threads = append(r.threads, th)
	if hat && !edge {
		if owner.hatThreads == nil {
			owner.hatThreads = make(map[string]*thread)
		}
		owner.hatThreads[id] = th
	}
	return true, nil
}
