package vm

import (
	"context"
	"errors"
	"fmt"
	"math"
)

func (r *Runtime) MonitorValue(index, item int) (string, bool) {
	m := r.editableMonitor(index, item >= 0)
	if m == nil {
		return "", false
	}
	if item >= 0 {
		list := m.owner.state.Lists[m.source.ID]
		if item >= len(list.Items) {
			return "", false
		}
		return text(list.Items[item]), true
	}
	v := m.owner.variables[m.source.ID]
	if v == nil {
		return "", false
	}
	return text(v.Value), true
}

func (r *Runtime) MonitorVisible(index int) bool {
	return index >= 0 && index < len(r.data.monitors) && r.data.monitors[index].source.Visible && !r.data.monitors[index].owner.dead
}

func (r *Runtime) MonitorListLength(index int) int {
	m := r.editableMonitor(index, true)
	if m == nil {
		return -1
	}
	return len(m.owner.state.Lists[m.source.ID].Items)
}

func (r *Runtime) editableMonitor(index int, list bool) *dataMonitor {
	if index < 0 || index >= len(r.data.monitors) {
		return nil
	}
	m := &r.data.monitors[index]
	if m.reporter != nil || !m.source.Visible || m.owner.dead || (m.source.Opcode == "data_listcontents") != list {
		return nil
	}
	return m
}

func (r *Runtime) SetMonitorValue(index int, value string) bool {
	m := r.editableMonitor(index, false)
	if m == nil || len(value) > maxGeneratedText {
		return false
	}
	v := m.owner.variables[m.source.ID]
	if v == nil {
		return false
	}
	v.Value = value
	r.writeObservedVariables()
	return true
}

func (r *Runtime) SetMonitorSlider(index int, value float64) bool {
	m := r.editableMonitor(index, false)
	if m == nil || m.source.Mode != "slider" || math.IsNaN(value) || math.IsInf(value, 0) {
		return false
	}
	low, high := 0.0, 100.0
	if m.source.SliderMin != nil {
		low = *m.source.SliderMin
	}
	if m.source.SliderMax != nil {
		high = *m.source.SliderMax
	}
	if math.IsNaN(low) || math.IsNaN(high) || math.IsInf(low, 0) || math.IsInf(high, 0) || high < low {
		return false
	}
	step := 1.0
	if m.source.IsDiscrete != nil && !*m.source.IsDiscrete {
		step = .01
	}
	value = min(high, max(low, value))
	value = min(high, max(low, low+math.Floor((value-low)/step+.5)*step))
	v := m.owner.variables[m.source.ID]
	if v == nil {
		return false
	}
	v.Value = numberValue(value)
	r.writeObservedVariables()
	return true
}

func (r *Runtime) EditMonitorList(index int, operation string, item int, value string) bool {
	m := r.editableMonitor(index, true)
	if m == nil || len(value) > maxGeneratedText {
		return false
	}
	list, ok := m.owner.state.Lists[m.source.ID]
	if !ok {
		return false
	}
	switch operation {
	case "add":
		if len(list.Items) >= listLimit {
			return false
		}
		list.append(value)
	case "replace", "delete":
		if item < 0 || item >= len(list.Items) {
			return false
		}
		if operation == "replace" {
			list.replace(item, value)
		} else {
			list.remove(item)
		}
	default:
		return false
	}
	m.owner.state.Lists[m.source.ID] = list
	return true
}

func (r *Runtime) MoveMonitor(index int, x, y float64) {
	if index < 0 || index >= len(r.data.monitors) || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return
	}
	m := &r.data.monitors[index].source
	m.X, m.Y = x, y
}

func (r *Runtime) NextMonitorMode(index int) bool {
	if !r.MonitorVisible(index) {
		return false
	}
	m := &r.data.monitors[index]
	if m.source.Opcode == "data_listcontents" {
		return false
	}
	if m.reporter != nil {
		if m.source.Mode == "large" {
			m.source.Mode = "default"
		} else {
			m.source.Mode = "large"
		}
		return true
	}
	switch m.source.Mode {
	case "large":
		m.source.Mode = "slider"
	case "slider":
		m.source.Mode = "default"
	default:
		m.source.Mode = "large"
	}
	return true
}

func (r *Runtime) ResizeMonitor(index int, width, height float64) bool {
	m := r.editableMonitor(index, true)
	if m == nil || !finite(width) || !finite(height) {
		return false
	}
	m.source.Width = min(4096, max(100, width))
	m.source.Height = min(4096, max(60, height))
	return true
}

// Read extension reporters during VM execution, never from the drawing thread.
func (r *Runtime) updateMonitorReporters(ctx context.Context) error {
	for i := range r.data.monitors {
		m := &r.data.monitors[i]
		if m.reporter == nil || !m.source.Visible || m.owner.dead || m.thread.wakeFrame > r.frame {
			continue
		}
		registered := r.extensionBlocks[m.source.Opcode]
		if registered == nil || (registered.block.Kind != "reporter" && registered.block.Kind != "boolean") {
			continue
		}
		budget := stepsPerFrame
		result, _, err := r.invokeExtension(ctx, m.thread, m.reporter, func(name string) (any, error) {
			return m.source.Params[name], nil
		}, true, 0, &budget)
		if errors.Is(err, errExtensionYield) {
			continue
		}
		if err != nil {
			return fmt.Errorf("monitor %s: %w", m.source.ID, err)
		}
		m.value = result.Value
	}
	return nil
}
