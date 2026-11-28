package vm

import (
	"image/color"
	"math"
	"reflect"
)

/*
Custom graphics may inspect or change script data during a synchronous call.
The built-in renderer opts out and never creates these temporary views.
*/
type variableObservation struct {
	target *target
	values map[string]Variable
	before map[string]Variable
}

func sameObservedVariable(a, b Variable) bool {
	if a.Name != b.Name {
		return false
	}
	if x, ok := a.Value.(float64); ok {
		y, numeric := b.Value.(float64)
		return numeric && math.Float64bits(x) == math.Float64bits(y)
	}
	return reflect.DeepEqual(a.Value, b.Value)
}

func (r *Runtime) readObservedVariables() {
	changed := false
	for _, observation := range r.variableObservations {
		t := observation.target
		if t.dead {
			continue
		}
		for id := range observation.before {
			if _, ok := observation.values[id]; !ok {
				delete(t.variables, id)
				delete(observation.before, id)
				changed = true
			}
		}
		for id, value := range observation.values {
			before, exists := observation.before[id]
			if exists && sameObservedVariable(before, value) {
				continue
			}
			cell := t.variables[id]
			if cell == nil {
				cell = &Variable{}
				t.variables[id] = cell
				changed = true
			}
			cell.Name = value.Name
			cell.set(value.Value)
			observation.before[id] = value
		}
	}
	if changed {
		r.refreshNativeVariables()
	}
}

func (r *Runtime) writeObservedVariables() {
	for _, observation := range r.variableObservations {
		clear(observation.values)
		clear(observation.before)
		for id, cell := range observation.target.variables {
			observation.values[id] = *cell
			observation.before[id] = *cell
		}
	}
}

func (r *Runtime) observeVariables(d DisplayTarget) (DisplayTarget, func()) {
	r.readObservedVariables()
	r.writeObservedVariables()
	for _, t := range r.targets {
		if t.sourceIndex != d.Source || t.state.CloneID != d.State.CloneID || t.state.Name != d.State.Name {
			continue
		}
		observation := &variableObservation{
			target: t,
			values: make(map[string]Variable, len(t.variables)),
			before: make(map[string]Variable, len(t.variables)),
		}
		for id, cell := range t.variables {
			observation.values[id], observation.before[id] = *cell, *cell
		}
		count := len(r.variableObservations)
		r.variableObservations = append(r.variableObservations, observation)
		d.State.Variables = observation.values
		return d, func() {
			r.readObservedVariables()
			r.variableObservations[count] = nil
			r.variableObservations = r.variableObservations[:count]
			r.writeObservedVariables()
		}
	}
	return d, func() {}
}

type observingGraphics struct {
	Graphics
	r *Runtime
}

func (g *observingGraphics) Bounds(d DisplayTarget, tight bool) (Bounds, bool) {
	d, finish := g.r.observeVariables(d)
	defer finish()
	return g.Graphics.Bounds(d, tight)
}

func (g *observingGraphics) CostumeSize(d DisplayTarget) (float64, float64) {
	d, finish := g.r.observeVariables(d)
	defer finish()
	return g.Graphics.CostumeSize(d)
}

func (g *observingGraphics) Touching(scene []DisplayTarget, d DisplayTarget, name string, x, y float64) bool {
	d, finish := g.r.observeVariables(d)
	defer finish()
	return g.Graphics.Touching(scene, d, name, x, y)
}

func (g *observingGraphics) TouchingColor(scene []DisplayTarget, d DisplayTarget, color color.NRGBA, mask *color.NRGBA) bool {
	d, finish := g.r.observeVariables(d)
	defer finish()
	return g.Graphics.TouchingColor(scene, d, color, mask)
}

func (g *observingGraphics) Pen(command PenCommand) {
	d, finish := g.r.observeVariables(command.Target)
	defer finish()
	command.Target = d
	g.Graphics.Pen(command)
}

func (r *Runtime) graphicsHost() Graphics {
	if observer, ok := r.graphics.(*observingGraphics); ok {
		return observer.Graphics
	}
	return r.graphics
}
