package vm

import "strings"

func anyRunning(threads []*thread) bool {
	for _, th := range threads {
		if !th.done && !th.replaced {
			return true
		}
	}
	return false
}

/*
Restarting replaces the scheduled thread, preserving waiters' references to
the old instance. An instance currently executing can finish its current slice.
*/
func (r *Runtime) startHat(t *target, id string, restart bool) *thread {
	old := t.hatThreads[id]
	running := old != nil && !old.done && !old.replaced
	if running && !restart {
		return nil
	}
	fresh := &thread{
		target: t,
		hat:    id,
		pc:     t.source.Blocks[id].Next,
		slot:   len(r.threads),
	}
	if r.native {
		fresh.regions = nativeScripts[t.sourceIndex][id]
	}
	if running {
		old.replaced = true
		fresh.slot = old.slot
		r.threads[old.slot] = fresh
	} else {
		r.threads = append(r.threads, fresh)
	}
	// Search only this clone's running hats.
	if t.hatThreads == nil {
		t.hatThreads = make(map[string]*thread)
	}
	t.hatThreads[id] = fresh
	return fresh
}

func (r *Runtime) startHatsFor(t *target, opcode, key string, restart bool) []*thread {
	var started []*thread
	if t.dead {
		return started
	}
	hats := t.hats
	if opcode == "event_whenbroadcastreceived" {
		if t.broadcastHats == nil {
			t.broadcastHats = make(map[string][]string)
			for _, id := range t.hats {
				block := t.source.Blocks[id]
				if block.Opcode == opcode {
					name := strings.ToUpper(field(block, "BROADCAST_OPTION", 0))
					t.broadcastHats[name] = append(t.broadcastHats[name], id)
				}
			}
		}
		/*
			Index only project receiver names, never arbitrary broadcast inputs.
			Slices retain the existing hat order, including duplicate names.
		*/
		hats = t.broadcastHats[key]
	}
	for _, id := range hats {
		b := t.source.Blocks[id]
		if b.Opcode != opcode {
			continue
		}
		if opcode == "event_whenbackdropswitchesto" && !strings.EqualFold(field(b, "BACKDROP", 0), key) {
			continue
		}
		if opcode == "event_whenkeypressed" && !strings.EqualFold(field(b, "KEY_OPTION", 0), key) {
			continue
		}
		if th := r.startHat(t, id, restart); th != nil {
			started = append(started, th)
		}
	}
	return started
}

func (r *Runtime) broadcast(name string) []*thread {
	key := strings.ToUpper(name)
	if len(r.broadcastNames) > 0 {
		registered := false
		for _, message := range r.broadcastNames {
			if strings.EqualFold(message, name) {
				key = strings.ToUpper(message)
				registered = true
				break
			}
		}
		if !registered {
			return nil
		}
	}
	var started []*thread
	for i := len(r.executable) - 1; i >= 0; i-- {
		started = append(started, r.startHatsFor(r.executable[i], "event_whenbroadcastreceived", key, true)...)
	}
	return started
}

func (r *Runtime) compactThreads() {
	kept := r.threads[:0]
	for _, th := range r.threads {
		if !th.done && !th.replaced {
			th.slot = len(kept)
			kept = append(kept, th)
		} else if th.target.hatThreads[th.hat] == th {
			delete(th.target.hatThreads, th.hat)
		}
	}
	clear(r.threads[len(kept):])
	r.threads = kept
}
