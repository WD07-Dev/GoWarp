package vm

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gowarp/core/extension"
)

func (c *extensionContext) logicCall(operation string, args []any) (any, error) {
	/*
		Deletion is already guarded against reentry. Let cleanup handlers read
		the departing clone while keeping mutations and stale handles invalid.
	*/
	if c.event == "clone-deleted" {
		switch operation {
		case "target.get", "target.costumes", "target.costumeSize", "variable.get", "variable.exists", "list.get", "list.length", "list.item":
			copy := *c
			copy.readDeletedTarget = true
			c = &copy
		}
	}
	r := c.r
	a := hostArgs(args)
	var current *target
	if c.th != nil {
		current = c.th.target
	}
	resolve := func(i int) (*target, error) {
		return c.logicTarget(current, a.get(i))
	}
	switch operation {
	case "project.storage":
		if len(args) != 1 {
			return nil, fmt.Errorf("project.storage requires an extension ID")
		}
		if c.r.project == nil {
			return "", nil
		}
		return string(c.r.project.ExtensionStorage[extension.String(args[0])]), nil
	case "runtime.greenFlag":
		c.r.GreenFlag()
		return nil, nil
	case "events.pollHats":
		return c.pollHats(args)
	case "events.startHats":
		if len(args) != 2 {
			return nil, fmt.Errorf("events.startHats requires opcode and restart")
		}
		opcode, restart := extension.String(args[0]), extension.Bool(args[1])
		block, ok := c.r.extensionBlocks[opcode]
		if !ok || block.block.Kind != "hat" {
			return nil, fmt.Errorf("unregistered extension hat %q", opcode)
		}
		count := 0
		for i := len(c.r.executable) - 1; i >= 0; i-- {
			count += len(c.r.startHatsFor(c.r.executable[i], opcode, "", restart))
		}
		if count > 0 {
			c.r.stopped = false
		}
		return float64(count), nil
	case "runtime.time":
		return r.elapsed, nil
	case "runtime.monotonicTime":
		return r.monotonicTime(), nil
	case "runtime.frame":
		return float64(r.frame), nil
	case "runtime.fps":
		return r.FPS(), nil
	case "runtime.extensions":
		ids := make([]string, 0, len(r.extensions)+1)
		for _, id := range r.project.Extensions {
			if id == "pen" {
				ids = append(ids, id)
				break
			}
		}
		for _, registered := range r.extensions {
			ids = append(ids, registered.ID)
		}
		return ids, nil
	case "runtime.timer":
		return r.elapsed - r.timerSeconds, nil
	case "runtime.resetTimer":
		r.timerSeconds = r.elapsed
		return nil, nil
	case "host.fullscreen":
		return r.host.fullscreen, nil
	case "host.projectFullscreen":
		return r.host.projectFullscreen, nil
	case "host.window":
		return r.hostWindowProperty(a.string(0))
	case "host.frameMilliseconds":
		dt := r.host.frameMilliseconds
		if dt <= 0 {
			dt = 1000 / r.FPS()
		}
		return dt, nil
	case "host.command":
		kind := a.string(0)
		switch kind {
		case "title", "close", "open-url", "fullscreen":
			if kind == "title" {
				r.host.windowTitle, r.host.windowTitleSet = a.string(1), true
			}
			if kind == "fullscreen" {
				r.host.projectFullscreen = extension.Bool(a.get(1))
			}
			r.host.commands = append(r.host.commands, HostCommand{Kind: kind, Value: a.string(1)})
			return nil, nil
		}
		return nil, fmt.Errorf("unsupported host command %q", kind)
	case "input.keys":
		return append([]string(nil), r.host.keys...), nil
	case "input.key":
		key := normalizeKey(a.string(0))
		return r.keyPressed(key), nil
	case "input.mouse":
		x, y := r.mousePosition()
		return map[string]any{"x": x, "y": y, "down": r.mouseDown}, nil
	case "target.touching":
		t, err := resolve(1)
		if err != nil {
			return nil, err
		}
		if r.graphics == nil {
			return nil, fmt.Errorf("target.touching requires costume geometry")
		}
		return r.touchingObject(t, a.string(0)), nil
	case "target.touchingTarget":
		candidate, err := resolve(0)
		if err != nil {
			return nil, err
		}
		t, err := resolve(1)
		if err != nil {
			return nil, err
		}
		if r.graphics == nil {
			return nil, fmt.Errorf("target.touchingTarget requires costume geometry")
		}
		if candidate == t || candidate.state.IsStage || !candidate.state.Visible {
			return false, nil
		}
		d := display(candidate)
		d.State.Variables, d.State.Lists = nil, nil
		// The candidate is already resolved. An empty query name avoids treating
		// sprites named _edge_ or _mouse_ as special sensing selectors.
		d.State.Name = ""
		x, y := r.mousePosition()
		return r.graphics.Touching([]DisplayTarget{d}, display(t), "", x, y), nil
	case "target.list":
		out := make([]any, 0, len(r.targets))
		for _, t := range r.targets {
			out = append(out, map[string]any{"id": logicTargetID(t), "name": t.state.Name, "source": float64(t.sourceIndex), "cloneId": float64(t.state.CloneID), "isStage": t.state.IsStage})
		}
		return out, nil
	case "target.query":
		q, ok := a.get(0).(extension.TargetQuery)
		if len(args) != 1 || !ok {
			return nil, fmt.Errorf("target.query requires extension.TargetQuery")
		}
		source := -1
		if q.Source != "" {
			t, err := c.logicTarget(current, q.Source)
			if err != nil {
				return nil, err
			}
			source = t.sourceIndex
		}
		var ids []string
		for _, t := range r.targets {
			if t.dead || (source >= 0 && t.sourceIndex != source) || (q.ClonesOnly && t.state.CloneID == 0) {
				continue
			}
			if q.Variable != "" {
				v, found := t.variables[q.Variable]
				if !found && r.stage != nil {
					v, found = r.stage.variables[q.Variable]
				}
				if !found || extension.Compare(v.Value, q.Value) != 0 {
					continue
				}
			}
			ids = append(ids, logicTargetID(t))
			if q.First {
				break
			}
		}
		return ids, nil
	case "target.touchingTargets":
		ids, ok := a.get(0).([]string)
		if !ok || len(args) > 2 {
			return nil, fmt.Errorf("target.touchingTargets requires []string and an optional target")
		}
		t, err := resolve(1)
		if err != nil {
			return nil, err
		}
		if r.graphics == nil {
			return nil, fmt.Errorf("target.touchingTargets requires costume geometry")
		}
		scene := r.sensingScene[:0]
		r.sensingScene = nil
		defer func() { r.releaseSensingScene(scene) }()
		for _, id := range ids {
			candidate, err := c.logicTarget(current, id)
			if err != nil {
				return nil, err
			}
			if candidate == t || candidate.state.IsStage || !candidate.state.Visible {
				continue
			}
			d := display(candidate)
			d.State.Variables, d.State.Lists = nil, nil
			d.State.Name = ""
			scene = append(scene, d)
		}
		if len(scene) == 0 {
			return false, nil
		}
		x, y := r.mousePosition()
		return r.graphics.Touching(scene, display(t), "", x, y), nil
	case "target.get":
		t, err := resolve(1)
		if err != nil {
			return nil, err
		}
		s := &t.state
		switch a.string(0) {
		case "id":
			return logicTargetID(t), nil
		case "x":
			return s.X, nil
		case "y":
			return s.Y, nil
		case "direction":
			return s.Direction, nil
		case "size":
			return s.Size, nil
		case "name":
			return s.Name, nil
		case "cloneId":
			return float64(s.CloneID), nil
		case "source":
			return float64(t.sourceIndex), nil
		case "isStage":
			return s.IsStage, nil
		case "visible":
			return s.Visible, nil
		case "rotationStyle":
			return s.RotationStyle, nil
		case "currentCostume":
			return float64(s.CurrentCostume), nil
		case "volume":
			return s.Volume, nil
		case "layer":
			return float64(s.LayerOrder), nil
		default:
			return nil, fmt.Errorf("unknown target property %q", a.string(0))
		}
	case "target.setXY":
		t, err := resolve(2)
		if err != nil {
			return nil, err
		}
		t.setXY(a.number(0), a.number(1))
		return nil, nil
	case "variable.list", "list.list":
		t, err := resolve(0)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0)
		if operation == "variable.list" {
			for id := range t.variables {
				ids = append(ids, id)
			}
		} else {
			for id := range t.state.Lists {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		out := make([]any, 0, len(ids))
		for _, id := range ids {
			var name string
			if operation == "variable.list" {
				name = t.variables[id].Name
			} else {
				name = t.state.Lists[id].Name
			}
			out = append(out, map[string]any{"id": id, "name": name})
		}
		return out, nil
	case "target.set":
		t, err := resolve(2)
		if err != nil {
			return nil, err
		}
		v := extension.Number(a.get(1))
		switch a.string(0) {
		case "x":
			t.setXY(v, t.state.Y)
		case "y":
			t.setXY(t.state.X, v)
		case "direction":
			t.setDirection(v)
		case "layer":
			r.moveLayer(t, v, false, false)
			t.requestRedraw()
		case "visible":
			t.state.Visible = extension.Bool(a.get(1))
			t.requestRedraw()
		case "rotationStyle":
			t.state.RotationStyle = a.string(1)
			t.requestRedraw()
		case "size":
			r.setTargetSize(t, v)
		case "currentCostume":
			setCostume(t, v)
		default:
			return nil, fmt.Errorf("unknown mutable target property %q", a.string(0))
		}
		return nil, nil
	case "target.clone":
		t, err := resolve(0)
		if err != nil {
			return nil, err
		}
		before := r.nextCloneID
		r.createClone(t)
		if before == r.nextCloneID {
			return nil, nil
		}
		return fmt.Sprintf("%d:%d", t.sourceIndex, r.nextCloneID), nil
	case "target.delete":
		t, err := resolve(0)
		if err != nil {
			return nil, err
		}
		r.deleteClone(t)
		return nil, nil
	case "target.costumeIndex":
		t, err := resolve(1)
		if err != nil {
			return nil, err
		}
		index, found := costumeIndex(t, a.string(0))
		if !found {
			return float64(-1), nil
		}
		return float64(index), nil
	case "target.sounds":
		t, err := resolve(0)
		if err != nil {
			return nil, err
		}
		out := make([]string, len(t.source.Sounds))
		for i, sound := range t.source.Sounds {
			out[i] = sound.Name
		}
		return out, nil
	case "target.costumes":
		t, err := resolve(0)
		if err != nil {
			return nil, err
		}
		out := make([]any, len(t.source.Costumes))
		for i, c := range t.source.Costumes {
			out[i] = map[string]any{"name": c.Name, "rotationCenterX": c.RotationCenterX, "rotationCenterY": c.RotationCenterY, "format": c.DataFormat}
		}
		return out, nil
	case "target.costumeSize":
		t, err := resolve(1)
		if err != nil {
			return nil, err
		}
		i := int(extension.Number(a.get(0)))
		if i < 0 || i >= len(t.source.Costumes) {
			return nil, fmt.Errorf("costume index out of range")
		}
		if r.graphics == nil {
			return nil, fmt.Errorf("costume size requires costume geometry")
		}
		d := display(t)
		d.State.CurrentCostume = i
		w, h := r.graphics.CostumeSize(d)
		/*
			Scratch bitmap metadata uses resolution-2 pixels, including assets
			normalized from resolution 1. Renderer geometry stays in stage units.
		*/
		if !strings.EqualFold(t.source.Costumes[i].DataFormat, "svg") {
			w, h = w*2, h*2
		}
		return []float64{w, h}, nil
	case "events.broadcast":
		return float64(len(r.broadcast(a.string(0)))), nil
	}
	if strings.HasPrefix(operation, "variable.") {
		ref := 1
		if operation == "variable.set" || operation == "variable.change" {
			ref = 2
		}
		if operation == "variable.create" {
			ref = 3
		}
		t, err := resolve(ref)
		if err != nil {
			return nil, err
		}
		id := a.string(0)
		if operation == "variable.create" {
			if _, ok := t.variables[id]; ok {
				return nil, fmt.Errorf("variable already exists %q", id)
			}
			t.variables[id] = &Variable{Name: a.string(1), Value: a.get(2)}
			r.refreshNativeVariables()
			return nil, nil
		}
		if operation == "variable.exists" {
			_, found := t.variables[id]
			if !found && r.stage != nil {
				_, found = r.stage.variables[id]
			}
			return found, nil
		}
		owner, v, err := r.variable(t, id)
		if err != nil {
			return nil, err
		}
		switch operation {
		case "variable.get":
			return v.Value, nil
		case "variable.set":
			v.Value = a.get(1)
		case "variable.change":
			v.Value = extension.Number(v.Value) + extension.Number(a.get(1))
		case "variable.delete":
			delete(owner.variables, id)
			r.refreshNativeVariables()
			return nil, nil
		default:
			return nil, fmt.Errorf("unsupported extension host operation %q", operation)
		}
		return nil, nil
	}
	if strings.HasPrefix(operation, "list.") {
		ref := 1
		switch operation {
		case "list.set", "list.append", "list.delete", "list.item", "list.create":
			ref = 2
		case "list.insert", "list.replace":
			ref = 3
		}
		t, err := resolve(ref)
		if err != nil {
			return nil, err
		}
		id := a.string(0)
		if operation == "list.create" {
			if _, ok := t.state.Lists[id]; ok {
				return nil, fmt.Errorf("list already exists %q", id)
			}
			t.state.Lists[id] = List{Name: a.string(1), Items: []any{}}
			return nil, nil
		}
		owner, l, err := r.list(t, id)
		if err != nil {
			return nil, err
		}
		switch operation {
		case "list.get":
			return append([]any{}, l.Items...), nil
		case "list.length":
			return float64(len(l.Items)), nil
		case "list.item":
			i := logicListIndex(a.get(1), len(l.Items))
			if i < 0 {
				return "", nil
			}
			return l.Items[i], nil
		case "list.set":
			items, ok := a.get(1).([]any)
			if !ok {
				return nil, fmt.Errorf("list.set requires []any")
			}
			if len(items) > listLimit {
				items = items[:listLimit]
			}
			l.Items = append([]any{}, items...)
			l.sharing = nil
			l.textIndex = nil
		case "list.append":
			if len(l.Items) < listLimit {
				l.append(a.get(1))
			}
		case "list.clear":
			l.clear()
		case "list.delete":
			i := logicListIndex(a.get(1), len(l.Items))
			if i >= 0 {
				l.remove(i)
			}
		case "list.remove":
			delete(owner.state.Lists, id)
			return nil, nil
		case "list.replace":
			i := logicListIndex(a.get(1), len(l.Items))
			if i >= 0 {
				l.replace(i, a.get(2))
			}
		case "list.insert":
			i := logicListIndex(a.get(1), len(l.Items)+1)
			if i >= 0 && len(l.Items) < listLimit {
				l.insert(i, a.get(2))
			}
		default:
			return nil, fmt.Errorf("unsupported extension host operation %q", operation)
		}
		owner.state.Lists[id] = l
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported extension host operation %q", operation)
}

func logicTargetID(t *target) string {
	return strconv.Itoa(t.sourceIndex) + ":" + strconv.Itoa(t.state.CloneID)
}

func (c *extensionContext) logicTarget(current *target, ref any) (*target, error) {
	name := extension.String(ref)
	if ref == nil || name == "" || name == "_myself_" {
		if current != nil && (!current.dead || c.readDeletedTarget) {
			return current, nil
		}
		return nil, fmt.Errorf("operation requires a current target")
	}
	sourceText, cloneText, hasID := strings.Cut(name, ":")
	var source, clone int
	if hasID {
		var sourceErr, cloneErr error
		source, sourceErr = strconv.Atoi(sourceText)
		clone, cloneErr = strconv.Atoi(cloneText)
		hasID = sourceErr == nil && cloneErr == nil && source >= 0 && clone >= 0 &&
			sourceText == strconv.Itoa(source) && cloneText == strconv.Itoa(clone)
	}
	// Original names retain precedence over clone handles.
	for _, t := range c.r.targets {
		if t.state.CloneID != 0 {
			break
		}
		if t.dead && !(c.readDeletedTarget && t == current) {
			continue
		}
		if (hasID && clone == 0 && t.sourceIndex == source) ||
			t.state.Name == name || (name == "_stage_" && t.state.IsStage) {
			return t, nil
		}
	}
	if hasID && clone > 0 {
		// Targets keep creation order; deleting a clone preserves this ordering.
		i := sort.Search(len(c.r.targets), func(i int) bool { return c.r.targets[i].state.CloneID >= clone })
		if i < len(c.r.targets) {
			t := c.r.targets[i]
			if t.state.CloneID == clone && t.sourceIndex == source && (!t.dead || (c.readDeletedTarget && t == current)) {
				return t, nil
			}
		}
	}
	return nil, fmt.Errorf("unknown target %q", name)
}

// Preserve named Scratch indexes while accepting native Go numeric arguments.
func logicListIndex(value any, size int) int {
	if _, ok := value.(string); !ok {
		value = extension.Number(value)
	}
	return listIndex(value, size)
}
