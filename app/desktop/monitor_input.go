package desktop

import (
	"image"
	"math"
	"time"
	"unicode/utf8"

	"gowarp/core/vm"
)

type monitorHit struct {
	index, item, count, rows, offset int
	kind                             string
	value                            string
	rect                             image.Rectangle
	x, y, low, high                  float64
	size                             image.Point
	list                             bool
}

type monitorEdit struct {
	index, item     int
	original, text  string
	selected, dirty bool
}

type monitorDrag struct {
	hit   monitorHit
	x, y  float64
	moved bool
}

type monitorUI struct {
	hits       []monitorHit
	offsets    map[int]int
	edit       *monitorEdit
	drag       *monitorDrag
	captured   bool
	clickAt    time.Time
	clickIndex int
	clickPoint image.Point
}

func (u *monitorUI) commit(r *vm.Runtime) {
	e := u.edit
	u.edit = nil
	if e == nil || !e.dirty {
		return
	}
	current, ok := r.MonitorValue(e.index, e.item)
	if !ok || current != e.original {
		return
	}
	if e.item < 0 {
		r.SetMonitorValue(e.index, e.text)
	} else {
		r.EditMonitorList(e.index, "replace", e.item, e.text)
	}
}

func (u *monitorUI) scroll(index, offset int) {
	if u.offsets == nil {
		u.offsets = make(map[int]int)
	}
	u.offsets[index] = max(0, offset)
}

func (u *monitorUI) update(r *vm.Runtime, in *Input, width, height int) {
	x, y := in.MouseX, in.MouseY-ToolbarHeight
	point := image.Pt(int(x), int(y))
	var hit *monitorHit
	for i := len(u.hits) - 1; i >= 0; i-- {
		if point.In(u.hits[i].rect) && r.MonitorVisible(u.hits[i].index) {
			hit = &u.hits[i]
			break
		}
	}
	editing := u.edit != nil
	if e := u.edit; e != nil {
		current, ok := r.MonitorValue(e.index, e.item)
		if !ok || current != e.original || in.Escape {
			u.edit = nil
		} else {
			if in.SelectAll {
				e.selected = true
			}
			if in.Backspace || in.Delete {
				if e.selected {
					e.text = ""
				} else if in.Backspace && e.text != "" {
					_, size := utf8.DecodeLastRuneInString(e.text)
					e.text = e.text[:len(e.text)-size]
				}
				e.selected, e.dirty = false, true
			}
			if in.Text != "" && len(in.Text) <= 4096 {
				if e.selected {
					e.text = ""
				}
				if len(e.text)+len(in.Text) <= 4096 {
					e.text += in.Text
					e.dirty = true
				}
				e.selected = false
			}
			if in.Enter {
				u.commit(r)
			}
		}
	}
	if hit != nil && in.WheelY != 0 && hit.count > 0 {
		u.commit(r)
		step := int(math.Ceil(math.Abs(in.WheelY) * 3))
		if in.WheelY > 0 {
			step = -step
		}
		u.scroll(hit.index, min(max(0, hit.count-hit.rows), max(0, hit.offset+step)))
		in.WheelY = 0
	}
	if in.MousePressed {
		u.commit(r)
		if hit != nil {
			u.captured = true
			now := time.Now()
			delta := point.Sub(u.clickPoint)
			doubleClick := !hit.list && (hit.kind == "value" || hit.kind == "drag" || hit.kind == "panel") &&
				hit.index == u.clickIndex && !u.clickAt.IsZero() && now.Sub(u.clickAt) < 400*time.Millisecond &&
				delta.X*delta.X+delta.Y*delta.Y <= 25
			u.clickAt, u.clickIndex, u.clickPoint = now, hit.index, point
			if doubleClick {
				r.NextMonitorMode(hit.index)
				u.clickAt = time.Time{}
				u.drag = nil
			} else {
				switch hit.kind {
				case "value":
					if value, ok := r.MonitorValue(hit.index, hit.item); ok {
						if hit.item < 0 || value == hit.value {
							u.edit = &monitorEdit{index: hit.index, item: hit.item, original: value, text: value, selected: true}
						}
					}
					if !hit.list {
						u.drag = &monitorDrag{hit: *hit, x: x, y: y}
					}
				case "delete":
					if value, ok := r.MonitorValue(hit.index, hit.item); ok && value == hit.value {
						r.EditMonitorList(hit.index, "delete", hit.item, "")
					}
				case "add":
					count := r.MonitorListLength(hit.index)
					if r.EditMonitorList(hit.index, "add", 0, "") {
						u.scroll(hit.index, max(0, count+1-hit.rows))
						u.edit = &monitorEdit{index: hit.index, item: count, selected: true}
					}
				case "drag", "slider", "scroll", "resize":
					u.drag = &monitorDrag{hit: *hit, x: x, y: y}
				case "panel":
					if !hit.list {
						u.drag = &monitorDrag{hit: *hit, x: x, y: y}
					}
				}
			}
		} else {
			u.clickAt = time.Time{}
		}
	}
	if d := u.drag; d != nil {
		if !r.MonitorVisible(d.hit.index) {
			u.drag = nil
		} else {
			h := d.hit
			dx, dy := x-d.x, y-d.y
			if dx*dx+dy*dy >= 9 {
				d.moved = true
				u.clickAt = time.Time{}
			}
			switch h.kind {
			case "drag", "value", "panel":
				if d.moved {
					u.edit = nil
					r.MoveMonitor(h.index, mathClamp(h.x+dx, 0, float64(max(0, width-h.size.X))), mathClamp(h.y+dy, 0, float64(max(0, height-22))))
				}
			case "resize":
				if d.moved {
					r.ResizeMonitor(h.index, min(float64(width), float64(h.size.X)+dx), min(float64(height), float64(h.size.Y)+dy))
				}
			case "slider":
				fraction := mathClamp((x-float64(h.rect.Min.X))/float64(max(1, h.rect.Dx()-1)), 0, 1)
				r.SetMonitorSlider(h.index, h.low+(h.high-h.low)*fraction)
			case "scroll":
				fraction := mathClamp((y-float64(h.rect.Min.Y))/float64(max(1, h.rect.Dy()-1)), 0, 1)
				u.scroll(h.index, int(fraction*float64(max(0, h.count-h.rows))))
			}
		}
	}
	if editing || u.edit != nil {
		// Typing must not start key hats; key releases still reach the project.
		for _, key := range in.Pressed {
			r.SetKeyState(key, false)
		}
		in.Pressed = nil
		in.Text, in.Enter, in.Backspace, in.Delete = "", false, false, false
	}
	down := in.MouseDown
	if hit != nil || u.captured {
		in.MouseDown, in.MousePressed = false, false
	}
	if !down {
		u.drag = nil
		u.captured = false
	}
}
