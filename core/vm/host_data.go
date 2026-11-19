package vm

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"gowarp/core/project"
)

type ListAppend struct {
	Target, List, Text, Note string
	Number                   float64
}

/*
SetListObserver observes each add-to-list operation, including an add followed
by a clear in the same frame. It does not change list contents or execution.
*/
func (r *Runtime) SetListObserver(observer func(ListAppend)) {
	r.listObserver = observer
	r.listNotes = map[*project.Block]string{}
	if observer == nil {
		return
	}
	for _, t := range r.targets {
		ids := make([]string, 0, len(t.source.Comments))
		for id := range t.source.Comments {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			comment := t.source.Comments[id]
			if b := t.source.Blocks[comment.BlockID]; b != nil {
				r.listNotes[b] = strings.ReplaceAll(comment.Text, "\n", " ")
			}
		}
	}
}

func (r *Runtime) Time() float64 {
	return r.elapsed
}

func (r *Runtime) SetStageSize(width, height float64) error {
	if !validStage(width, height) {
		return fmt.Errorf("stage dimensions must be 1..4096 with at most 16M pixels")
	}
	r.options.View.Width, r.options.View.Height = width, height
	if r.graphics != nil {
		r.graphics.SetView(r.View())
	}
	return nil
}

/*
AppendStageBytes feeds the named terminal input buffer, retaining Scratch's
list limit. Returns false when the project has no such stage list.
*/
func (r *Runtime) AppendStageBytes(name string, data []byte) bool {
	if r.stage == nil {
		return false
	}
	for id, list := range r.stage.state.Lists {
		if list.Name != name {
			continue
		}
		for _, value := range data {
			if len(list.Items) >= listLimit {
				break
			}
			list.Items = append(list.Items, float64(value))
		}
		r.stage.state.Lists[id] = list
		return true
	}
	return false
}

/*
WriteDump emits variables and lists as text, sorted by line and
excluding clones. Structured JSON remains the CLI's normal stdout format.
*/
func (r *Runtime) WriteDump(writer io.Writer) error {
	var lines []string
	for _, t := range r.targets {
		if t.state.CloneID != 0 {
			continue
		}
		for _, v := range t.variables {
			lines = append(lines, t.state.Name+"."+v.Name+" = "+text(v.Value))
		}
		for _, list := range t.state.Lists {
			name := t.state.Name + "." + list.Name
			lines = append(lines, fmt.Sprintf("%s # %d", name, len(list.Items)))
			for i, value := range list.Items {
				lines = append(lines, fmt.Sprintf("%s[%d] = %s", name, i+1, text(value)))
			}
		}
	}
	sort.Strings(lines)
	_, err := fmt.Fprintln(writer, strings.Join(lines, "\n"))
	return err
}

func (r *Runtime) clickAt(x, y float64) {
	r.mouseX, r.mouseY, r.mouseDown = x, y, true
	if r.graphics != nil {
		scene := r.Scene()
		for i := len(r.drawList) - 1; i >= 0; i-- {
			t := r.drawList[i]
			if !t.state.IsStage && t.state.Visible && r.graphics.Touching(scene, display(t), "_mouse_", x, y) {
				if t.state.Draggable {
					r.RaiseSprite(t.sourceIndex, t.state.CloneID)
				}
				r.startHatsFor(t, "event_whenthisspriteclicked", "", true)
				return
			}
		}
	}
	if r.stage != nil {
		r.startHatsFor(r.stage, "event_whenstageclicked", "", true)
	}
}
