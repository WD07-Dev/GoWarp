package vm

import "strings"

func (r *Runtime) CloudValues() map[string]Variable {
	r.readObservedVariables()
	values := map[string]Variable{}
	if r.stage == nil {
		return values
	}
	for id, raw := range r.stage.source.Variables {
		marked := len(raw) > 2 && raw[2] == true
		var v Variable
		if cell := r.stage.variables[id]; cell != nil {
			v = *cell
		}
		if marked || strings.HasPrefix(v.Name, "☁") {
			values[id] = v
		}
	}
	return values
}

func (r *Runtime) ApplyCloud(id, value string) {
	if _, ok := r.CloudValues()[id]; !ok {
		return
	}
	v := r.stage.variables[id]
	if v == nil {
		v = &Variable{}
		r.stage.variables[id] = v
		r.refreshNativeVariables()
	}
	v.Value = value
	r.writeObservedVariables()
}
