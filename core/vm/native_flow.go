package vm

import "fmt"

type nativeFlowNode struct {
	ID, Body    string
	Label, Next string
	Work, Depth int
	Complete    bool
	Repeat      bool
	Sequence    bool
	Stop        string
}

// flowEntries selects scripts with branching or nested loops for compilation.
func (g *nativeGenerator) flowEntries(t *target, p *procedure) []string {
	type pendingBlock struct {
		id    string
		loops int
	}
	pending := []pendingBlock{{id: p.body}}
	seen := make(map[string]bool)
	var ids []string
	connected := false
	branchingRepeat := false
	hasWait := false
	for len(pending) > 0 {
		entry := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		b := t.source.Blocks[entry.id]
		if b == nil || seen[entry.id] {
			continue
		}
		seen[entry.id] = true
		ids = append(ids, entry.id)
		if p.warp && len(ids) > 256 {
			return nil
		}
		pending = append(pending, pendingBlock{b.Next, entry.loops})
		loops := entry.loops
		switch b.Opcode {
		case "procedures_call":
			branchingRepeat = branchingRepeat || entry.loops > 0
		case "control_wait":
			hasWait = true
		case "control_if", "control_if_else":
			branchingRepeat = branchingRepeat || entry.loops > 0
		case "control_repeat_until", "control_while":
			connected = true
			loops++
		case "control_repeat", "control_forever":
			connected = connected || loops > 0
			loops++
		}
		for _, name := range []string{"SUBSTACK2", "SUBSTACK"} {
			if child, err := substack(b, name); err == nil && child != "" {
				pending = append(pending, pendingBlock{child, loops})
			}
		}
	}
	if p.warp && !connected {
		if !branchingRepeat && !hasWait {
			return nil
		}
		// A repeat whose entire body compiles can use batched execution.
		for _, id := range ids {
			b := t.source.Blocks[id]
			if b.Opcode != "control_repeat" {
				continue
			}
			body, err := substack(b, "SUBSTACK")
			if err != nil {
				return nil
			}
			probe := nativeEmitter{g: g, t: t, p: p, path: make(map[string]bool), region: true, depth: 1, resumable: !p.warp}
			if _, ok := probe.statements(body, 1); ok && probe.work <= nativeWorkLimit {
				return nil
			}
		}
	}
	return ids
}

func (g *nativeGenerator) compileFlow(t *target, code string) bool {
	return g.compileScript(t, t.procedures[code], code, false)
}

func (g *nativeGenerator) compileScript(t *target, p *procedure, code string, script bool) bool {
	ids := g.flowEntries(t, p)
	if len(ids) == 0 {
		return false
	}
	var nodes []nativeFlowNode
	loopBodies := make(map[string]bool)
	for _, id := range ids {
		b := t.source.Blocks[id]
		switch b.Opcode {
		case "control_repeat", "control_repeat_until", "control_while":
			if body, err := substack(b, "SUBSTACK"); err == nil && body != "" {
				loopBodies[body] = loopBodies[body] || b.Opcode == "control_repeat"
			}
		}
	}
	hasControl := false
	usesBranches := false
	totalWork := 0
	for _, id := range ids {
		probe := nativeEmitter{g: g, t: t, p: p, path: make(map[string]bool), region: true, depth: 1, resumable: !p.warp}
		repeat, loopBody := loopBodies[id]
		complete := false
		if loopBody && p.warp {
			_, complete = probe.statements(id, 1)
			// Keep branches and callbacks at explicit interruption boundaries.
			complete = complete && probe.work <= nativeWorkLimit && probe.depth == 1 && !probe.effects.external
		}
		if !complete {
			probe = nativeEmitter{g: g, t: t, p: p, path: make(map[string]bool), region: true, depth: 1, resumable: !p.warp}
			if _, ok := probe.flowStatement(id); !ok {
				continue
			}
		}
		if probe.work == 0 || probe.depth > 32 {
			continue
		}
		totalWork += probe.work
		if totalWork > nativeWorkLimit {
			if p.warp {
				return false
			}
			break
		}
		nodes = append(nodes, nativeFlowNode{ID: id, Work: probe.work, Depth: probe.depth, Complete: complete, Repeat: complete && repeat})
		switch t.source.Blocks[id].Opcode {
		case "control_repeat", "control_forever", "control_repeat_until", "control_while", "control_if", "control_if_else":
			usesBranches = true
			hasControl = true
		case "control_wait":
			hasControl = true
		}
	}
	if !hasControl && p.warp {
		return false
	}
	g.combineFlowCalculations(t, p, nodes, totalWork)
	e := nativeEmitter{g: g, t: t, p: p, path: make(map[string]bool), region: true, depth: 1, resumable: !p.warp}
	e.flowLabels = make(map[string]string, len(nodes))
	for i := range nodes {
		nodes[i].Label = fmt.Sprintf("flow%d", i)
		e.flowLabels[nodes[i].ID] = nodes[i].Label
	}
	for i := range nodes {
		clear(e.numbers)
		e.work, e.depth = 0, 1
		var body string
		var ok bool
		if nodes[i].Complete || nodes[i].Sequence {
			e.stop = nodes[i].Stop
			body, ok = e.statements(nodes[i].ID, 1)
		} else {
			body, ok = e.flowStatement(nodes[i].ID)
		}
		if !ok {
			return false
		}
		nodes[i].Body = body
		next := t.source.Blocks[nodes[i].ID].Next
		if nodes[i].Complete {
			next = ""
		} else if nodes[i].Sequence {
			next = nodes[i].Stop
		}
		nodes[i].Next = e.flowJump(next)
	}
	name := fmt.Sprintf("nativeCalc%d", g.next)
	g.next++
	body := e.functionBody(name, nativeCode("flow", map[string]any{"Nodes": nodes, "Branches": usesBranches, "Resumable": !p.warp}))
	for _, node := range nodes {
		g.functions = append(g.functions, &nativeFunction{
			name: name, code: code, source: t.sourceIndex, procedure: p,
			script: script, resumable: !p.warp,
			parameters: e.parameters(),
			variables:  e.variables, lists: e.lists, effects: e.effects, region: node.ID,
			body: body, depth: node.Depth, work: node.Work,
		})
		body = ""
	}
	return true
}

func (g *nativeGenerator) combineFlowCalculations(t *target, p *procedure, nodes []nativeFlowNode, totalWork int) {
	entries := make(map[string]int, len(nodes))
	for i, node := range nodes {
		entries[node.ID] = i
	}
	covered := make(map[string]bool)
	// Keep interior entry points for interpreter fallback and budget-limited resumes.
	for i := range nodes {
		if nodes[i].Complete || covered[nodes[i].ID] {
			continue
		}
		probe := nativeEmitter{g: g, t: t, p: p, path: make(map[string]bool), region: true, depth: 1, resumable: !p.warp}
		count := 0
		for id := nodes[i].ID; id != ""; {
			entry, exists := entries[id]
			if !exists || nodes[entry].Complete || covered[id] {
				break
			}
			b := t.source.Blocks[id]
			if b.Next == id || covered[b.Next] {
				break
			}
			if b.Opcode != "data_setvariableto" && b.Opcode != "data_changevariableby" {
				break
			}
			if _, ok := probe.flowStatement(id); !ok || probe.effects.external || probe.work > 32 ||
				totalWork+probe.work-nodes[i].Work > nativeWorkLimit {
				break
			}
			count++
			covered[id] = true
			if count > 1 {
				totalWork += probe.work - nodes[i].Work
				nodes[i].Work, nodes[i].Depth = probe.work, probe.depth
				nodes[i].Sequence, nodes[i].Stop = true, b.Next
			}
			id = b.Next
		}
	}
}

func (e *nativeEmitter) flowStatement(id string) (string, bool) {
	b := e.t.source.Blocks[id]
	e.stop = b.Next
	name := "CONDITION"
	switch b.Opcode {
	case "control_wait":
		e.work++
		value, ok := e.input(b, "DURATION", 0)
		body := nativeCode("flow-wait", map[string]any{"Value": value.num()})
		return nativeCode("statement", map[string]any{"Region": true, "Next": b.Next, "Body": body}), ok
	case "control_wait_until":
		e.work++
		value, ok := e.input(b, "CONDITION", 0)
		body := nativeCode("flow-wait-until", map[string]any{"ID": id, "Value": value.truth()})
		return nativeCode("statement", map[string]any{"Region": true, "Next": b.Next, "Body": body}), ok
	case "procedures_call":
		if e.resumable {
			// The call can change warp mode, suspend, or recurse. Resume via the VM.
			return "", false
		}
		code, err := mutationText(b, "proccode")
		p := e.t.procedures[code]
		if err == nil && p != nil && e.g.compile(e.t, code) == nil {
			planner := &Runtime{}
			args, err := planner.procedureArguments(e.t, b, p.ids)
			if err != nil || !args.synchronous || len(args.arguments) > len(p.names) {
				return "", false
			}
			e.work++
			for _, arg := range args.arguments {
				if arg.plan != nil {
					e.work += arg.plan.blocks
				}
			}
			body := nativeCode("flow-call", map[string]any{"ID": id})
			return nativeCode("statement", map[string]any{"Region": true, "Next": b.Next, "Body": body}), true
		}
		return e.statements(id, 1)
	case "control_repeat":
		name = "TIMES"
	case "control_forever":
	case "control_repeat_until", "control_while", "control_if", "control_if_else":
	default:
		if b.Next == id {
			return "", false
		}
		return e.statements(id, 1)
	}
	e.work++
	value, ok := nativeBoolean("true"), true
	if b.Opcode != "control_forever" {
		value, ok = e.input(b, name, 0)
	}
	if !ok {
		return "", false
	}
	for _, branch := range []string{"SUBSTACK", "SUBSTACK2"} {
		if _, err := substack(b, branch); err != nil {
			return "", false
		}
	}
	expression := value.truth()
	if name == "TIMES" {
		expression = value.num()
	}
	bodyID, _ := substack(b, "SUBSTACK")
	elseID, _ := substack(b, "SUBSTACK2")
	body := nativeCode("flow-control", map[string]any{
		"ID": id, "Opcode": b.Opcode, "Value": expression,
		"Next": e.flowJump(b.Next),
		"Body": e.flowJump(bodyID),
		"Else": e.flowJump(elseID),
	})
	return nativeCode("statement", map[string]any{"Region": true, "Next": b.Next, "Body": body}), true
}

func (e *nativeEmitter) flowJump(id string) string {
	if label := e.flowLabels[id]; label != "" {
		return "goto " + label
	}
	return "goto flowDispatch"
}
