package vm

import "slices"

const cloneLimit = 300

func (r *Runtime) cloneSource(current *target, name string) *target {
	if name == "_myself_" {
		return current
	}
	for _, t := range r.targets {
		if t.state.CloneID == 0 && !t.state.IsStage && t.state.Name == name {
			return t
		}
	}
	return nil
}

func (r *Runtime) createClone(source *target) {
	if source == nil || source.dead || source.state.IsStage || r.cloneCount >= r.options.CloneLimit {
		return
	}
	state := source.state
	state.Volume = 100
	state.Bubble = ""
	state.Think = false
	r.nextCloneID++
	state.CloneID = r.nextCloneID
	variables := make(map[string]*Variable, len(source.variables))
	for id, v := range source.variables {
		copy := *v
		variables[id] = &copy
	}
	state.Lists = make(map[string]List, len(source.state.Lists))
	for id, list := range source.state.Lists {
		if len(list.Items) > 256 {
			if list.sharing == nil {
				list.sharing = &listSharing{}
			}
			list.sharing.shared = true
			source.state.Lists[id] = list
		} else {
			list.Items = append([]any{}, list.Items...)
			list.sharing = nil
		}
		list.textIndex = nil
		state.Lists[id] = list
	}
	clone := &target{
		variables:     variables,
		owner:         r,
		sourceIndex:   source.sourceIndex,
		source:        source.source,
		state:         state,
		hats:          source.hats,
		edgeHats:      source.edgeHats,
		broadcastHats: source.broadcastHats,
		procedures:    source.procedures,
	}
	r.targets = append(r.targets, clone)
	if r.native {
		r.bindNativeVariables(clone)
	}
	r.executable = append(r.executable, clone)
	for i, t := range r.executable {
		if t == source {
			copy(r.executable[i+1:], r.executable[i:len(r.executable)-1])
			r.executable[i] = clone
			break
		}
	}
	layer := source.state.LayerOrder
	r.drawList = append(r.drawList, clone)
	copy(r.drawList[layer+1:], r.drawList[layer:len(r.drawList)-1])
	r.drawList[layer] = clone
	r.drawOrderDirty = true
	for i := layer + 1; i < len(r.drawList); i++ {
		r.drawList[i].state.LayerOrder = i
	}
	r.cloneCount++
	clone.requestRedraw()
	r.startHatsFor(clone, "control_start_as_clone", "", false)
	r.extensionEvent("clone-created", clone)
}

func (r *Runtime) deleteClone(t *target) {
	if t.state.CloneID == 0 || t.dead {
		return
	}
	t.dead = true
	t.requestRedraw()
	r.extensionEvent("clone-deleted", t)
	for _, th := range t.hatThreads {
		th.done = true
	}
	t.hatThreads = nil
	for i, candidate := range r.targets {
		if candidate == t {
			r.targets = slices.Delete(r.targets, i, i+1)
			break
		}
	}
	layer := t.state.LayerOrder
	r.drawList = slices.Delete(r.drawList, layer, layer+1)
	for i := layer; i < len(r.drawList); i++ {
		r.drawList[i].state.LayerOrder = i
	}
	for i, candidate := range r.executable {
		if candidate == t {
			r.executable = slices.Delete(r.executable, i, i+1)
			break
		}
	}
	r.cloneCount--
	t.variables = nil
	t.nativeVariables = nil
	t.state.Lists = nil
	t.state.Text = nil
	t.state.Bubble = ""
	t.bubbleOwner = nil
	t.edgeInputs = nil
	t.edges = nil
	t.owner = nil
}

func (r *Runtime) stopAll() {
	r.stopped = true
	r.stopSounds()
	r.extensionEvent("cancel", r.stage)
	r.askThread = nil
	r.askPrompt = ""
	for _, t := range r.targets {
		t.state.Effects = GraphicEffects{}
		t.state.Bubble = ""
		t.bubbleOwner = nil
	}
	for _, th := range r.threads {
		th.done = true
	}
	for i := len(r.targets) - 1; i >= 0; i-- {
		r.deleteClone(r.targets[i])
	}
}
