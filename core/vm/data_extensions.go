package vm

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"gowarp/core/project"
)

type dataState struct {
	savePath, status string
	saved            map[string]any
	monitors         []dataMonitor
}

type dataMonitor struct {
	source project.Monitor
	owner  *target
}

// MonitorState contains detached display values in stage pixel coordinates.
type MonitorState struct {
	Index, Count, Offset   int
	SliderMin, SliderMax   float64
	Discrete               bool
	ID, Label, Mode, Value string
	List, Visible          bool
	X, Y, Width, Height    float64
	Items                  []string
}

func (r *Runtime) InitData(p *project.Project) {
	r.data = dataState{saved: map[string]any{}}
	for _, m := range p.Monitors {
		if m.Opcode != "data_variable" && m.Opcode != "data_listcontents" {
			continue
		}
		for _, t := range r.targets {
			if t.state.CloneID != 0 || (m.SpriteName == "" && !t.state.IsStage) || (m.SpriteName != "" && m.SpriteName != t.state.Name) {
				continue
			}
			if m.Opcode == "data_variable" {
				if _, ok := t.variables[m.ID]; !ok {
					break
				}
			} else {
				if _, ok := t.state.Lists[m.ID]; !ok {
					break
				}
			}
			r.data.monitors = append(r.data.monitors, dataMonitor{source: m, owner: t})
			break
		}
	}
}

func (r *Runtime) Monitors() []MonitorState {
	return r.monitors(false, nil, 0)
}

// VisibleMonitors skips hidden values before copying lists or formatting numbers.
func (r *Runtime) VisibleMonitors() []MonitorState {
	return r.monitors(true, nil, 0)
}

// MonitorWindow formats only the list rows needed by the player.
func (r *Runtime) MonitorWindow(offsets map[int]int, rows int) []MonitorState {
	return r.monitors(true, offsets, max(1, rows))
}

func (r *Runtime) monitors(visibleOnly bool, offsets map[int]int, rows int) []MonitorState {
	count := len(r.data.monitors)
	if visibleOnly {
		count = 0
		for _, m := range r.data.monitors {
			if m.source.Visible {
				count++
			}
		}
	}
	result := make([]MonitorState, 0, count)
	for index, m := range r.data.monitors {
		if visibleOnly && !m.source.Visible {
			continue
		}
		s := MonitorState{
			Index:     index,
			SliderMin: 0, SliderMax: 100, Discrete: true,
			ID:      m.source.ID,
			Mode:    m.source.Mode,
			Visible: m.source.Visible,
			List:    m.source.Opcode == "data_listcontents",
			X:       m.source.X,
			Y:       m.source.Y,
			Width:   m.source.Width,
			Height:  m.source.Height,
		}
		if m.source.SliderMin != nil {
			s.SliderMin = *m.source.SliderMin
		}
		if m.source.SliderMax != nil {
			s.SliderMax = *m.source.SliderMax
		}
		if m.source.IsDiscrete != nil {
			s.Discrete = *m.source.IsDiscrete
		}
		if s.List {
			list := m.owner.state.Lists[s.ID]
			s.Label, s.Count = list.Name, len(list.Items)
			end := s.Count
			if rows > 0 {
				height := s.Height
				if height <= 0 || math.IsNaN(height) {
					height = 200
				}
				page := min(rows, max(1, (int(math.Min(height, 4096))-44)/22))
				s.Offset = min(max(0, offsets[index]), max(0, s.Count-page))
				end = min(s.Count, s.Offset+page)
			}
			s.Items = make([]string, end-s.Offset)
			for i := range s.Items {
				s.Items[i] = text(list.Items[s.Offset+i])
			}
		} else {
			v := m.owner.variables[s.ID]
			if v != nil {
				s.Label, s.Value = v.Name, text(v.Value)
				if value, ok := v.Value.(float64); ok && math.Abs(value) < 1e21 {
					s.Value = text(math.Round(value*1e6) / 1e6)
				}
			} else {
				s.Label, s.Value = "", text(nil)
			}
		}
		if !m.owner.state.IsStage {
			s.Label = m.owner.state.Name + ": " + s.Label
		}
		result = append(result, s)
	}
	return result
}

// SetSavePath configures offline storage and loads it without firing a load hat.
func (r *Runtime) SetSavePath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("save path must be absolute")
	}
	r.data.savePath = filepath.Clean(path)
	r.loadSavedData()
	return nil
}

func (r *Runtime) loadSavedData() bool {
	r.data.status = "loading"
	r.data.saved = map[string]any{}
	f, err := os.Open(r.data.savePath)
	if os.IsNotExist(err) {
		r.data.status = "loaded"
		return true
	}
	if err != nil {
		r.data.status = "error: could not read " + r.data.savePath
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		r.data.status = "error: could not read " + r.data.savePath
		return false
	}
	if info.Size() > maxGeneratedText {
		r.data.status = "error: saved data exceeds 16 MiB"
		return false
	}
	// The read limit also protects against files growing after Stat.
	raw, err := io.ReadAll(io.LimitReader(f, maxGeneratedText+1))
	if err != nil {
		r.data.status = "error: could not read " + r.data.savePath
		return false
	}
	if len(raw) > maxGeneratedText {
		r.data.status = "error: saved data exceeds 16 MiB"
		return false
	}
	var saved map[string]any
	if json.Unmarshal(raw, &saved) == nil && saved != nil {
		r.data.saved = saved
	}
	r.data.status = "loaded"
	return true
}

// Use a sibling temporary file so a failed write cannot truncate an existing save.
func writeSavedData(path string, data []byte) error {
	if len(data) > maxGeneratedText {
		return fmt.Errorf("saved data exceeds 16 MiB")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("save path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".gowarp-save-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

type jsonOutput struct {
	text strings.Builder
	err  error
}

func (out *jsonOutput) add(value string) {
	if out.err != nil {
		return
	}
	if len(value) > maxGeneratedText-out.text.Len() {
		out.err = fmt.Errorf("generated JSON exceeds 16 MiB")
		return
	}
	out.text.WriteString(value)
}

func jsonData(value any) (string, error) {
	var out jsonOutput
	out.value(value)
	if out.err != nil {
		return "", out.err
	}
	return out.text.String(), nil
}

func (out *jsonOutput) value(value any) {
	if out.err != nil {
		return
	}
	// Scratch's numeric values include infinities; JSON.stringify emits null.
	switch value := value.(type) {
	case string:
		// Escaping can only expand strings; reject this lower bound before copying.
		if len(value) > maxGeneratedText-out.text.Len()-2 {
			out.err = fmt.Errorf("generated JSON exceeds 16 MiB")
			return
		}
		out.add(`"`)
		start := 0
		for offset, char := range value {
			escaped := ""
			size := utf8.RuneLen(char)
			switch char {
			case '"', '\\':
				escaped = "\\" + string(char)
			case '\n':
				escaped = `\n`
			case '\r':
				escaped = `\r`
			case '\t':
				escaped = `\t`
			case '\b':
				escaped = `\b`
			case '\f':
				escaped = `\f`
			case utf8.RuneError:
				_, size = utf8.DecodeRuneInString(value[offset:])
				if size == 1 {
					escaped = "\ufffd"
				}
			default:
				if char < 32 {
					escaped = fmt.Sprintf(`\u%04x`, char)
				}
			}
			if escaped != "" {
				out.add(value[start:offset])
				out.add(escaped)
				if out.err != nil {
					return
				}
				start = offset + size
			}
		}
		out.add(value[start:])
		out.add(`"`)
	case float64:
		if !finite(value) {
			out.add("null")
		} else {
			out.add(text(value))
		}
	case []any:
		out.add("[")
		for i, item := range value {
			if i != 0 {
				out.add(",")
			}
			out.value(item)
			if out.err != nil {
				return
			}
		}
		out.add("]")
	case map[string]any:
		// Even empty keys with one-character values need at least five bytes each.
		if len(value) > (maxGeneratedText-out.text.Len()-1)/5 {
			out.err = fmt.Errorf("generated JSON exceeds 16 MiB")
			return
		}
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.add("{")
		for i, key := range keys {
			if i != 0 {
				out.add(",")
			}
			out.value(key)
			out.add(":")
			out.value(value[key])
			if out.err != nil {
				return
			}
		}
		out.add("}")
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			out.err = err
			return
		}
		out.add(string(raw))
	}
}

func jsonArrayText(items []any) string {
	parts := make([]string, len(items))
	for i, item := range items {
		switch item := item.(type) {
		case nil:
		case []any:
			parts[i] = jsonArrayText(item)
		case map[string]any:
			parts[i] = "[object Object]"
		default:
			parts[i] = text(item)
		}
	}
	return strings.Join(parts, ",")
}

func (r *Runtime) dataReporter(th *thread, b *project.Block, input func(string) (any, error)) (any, bool, error) {
	switch b.Opcode {
	case "data_get_list_as":
		_, list, err := r.list(th.target, field(b, "LIST", 1))
		if err != nil {
			return nil, true, err
		}
		format := field(b, "FORMAT", 0)
		if format == "" {
			format = field(b, "TYPE", 0)
		}
		if format == "" || strings.EqualFold(format, "JSON") {
			value, err := jsonData(list.Items)
			return value, true, err
		}
		parts := make([]string, len(list.Items))
		for i, item := range list.Items {
			parts[i] = text(item)
		}
		return strings.Join(parts, ", "), true, nil
	case "mistwarpData_get":
		key, err := input("KEY")
		if err != nil {
			return nil, true, err
		}
		value := r.data.saved[text(key)]
		switch value.(type) {
		case nil:
			return "", true, nil
		case string, float64, bool:
			return value, true, nil
		default:
			encoded, err := jsonData(value)
			return encoded, true, err
		}
	case "mistwarpData_status":
		return r.data.status, true, nil
	case "mistwarpData_all":
		value, err := jsonData(r.data.saved)
		return value, true, err
	}
	return nil, false, nil
}

func (r *Runtime) dataStatement(th *thread, b *project.Block, input func(string) (any, error)) (bool, error) {
	switch b.Opcode {
	case "data_set_list_to_array":
		id := field(b, "LIST", 1)
		owner, list, err := r.list(th.target, id)
		if err != nil {
			return true, err
		}
		name := "ARRAY"
		if _, ok := b.Inputs[name]; !ok {
			name = "VALUE"
		}
		value, err := input(name)
		if err != nil {
			return true, err
		}
		var parsed any
		if json.Unmarshal([]byte(text(value)), &parsed) != nil {
			return true, nil
		}
		items, ok := parsed.([]any)
		if !ok {
			return true, nil
		}
		if len(items) > listLimit {
			items = items[:listLimit]
		}
		for i, item := range items {
			switch item := item.(type) {
			case []any:
				items[i] = jsonArrayText(item)
			case map[string]any:
				items[i] = "[object Object]"
			}
		}
		list.Items = items
		list.sharing = nil
		list.textIndex = nil
		owner.state.Lists[id] = list
		return true, nil
	case "data_showvariable", "data_hidevariable", "data_showlist", "data_hidelist":
		isList := strings.HasSuffix(b.Opcode, "list")
		name := "VARIABLE"
		if isList {
			name = "LIST"
		}
		id := field(b, name, 1)
		var owner *target
		var err error
		if isList {
			owner, _, err = r.list(th.target, id)
		} else {
			owner, _, err = r.variable(th.target, id)
		}
		if err != nil {
			return true, err
		}
		visible := strings.HasPrefix(b.Opcode, "data_show")
		for i := range r.data.monitors {
			m := &r.data.monitors[i]
			if m.source.ID == id && (m.source.Opcode == "data_listcontents") == isList && m.owner.source == owner.source {
				m.source.Visible = visible
				return true, nil
			}
		}
		if visible {
			for _, original := range r.targets {
				if original.source != owner.source || original.state.CloneID != 0 {
					continue
				}
				m := project.Monitor{ID: id, Opcode: "data_variable", Mode: "default", Visible: true,
					X: 5, Y: float64(5 + (len(r.data.monitors)%10)*28)}
				if isList {
					m.Opcode, m.Mode, m.Width, m.Height = "data_listcontents", "list", 120, 200
				}
				if !original.state.IsStage {
					m.SpriteName = original.state.Name
				}
				r.data.monitors = append(r.data.monitors, dataMonitor{source: m, owner: original})
				break
			}
		}
		return true, nil
	case "mistwarpData_set":
		rawKey, err := input("KEY")
		if err != nil {
			return true, err
		}
		value, err := input("VALUE")
		if err != nil {
			return true, err
		}
		key := trim(text(rawKey))
		if key == "" || strings.HasPrefix(key, "$") || utf16Length(key) > 64 {
			return true, nil
		}
		if s, ok := value.(string); ok && len(s) <= maxGeneratedText {
			var parsed any
			if json.Unmarshal([]byte(s), &parsed) == nil && parsed != nil {
				value = parsed
			}
		}
		r.data.saved[key] = value
		return true, nil
	case "mistwarpData_load":
		if r.loadSavedData() {
			r.startDataHats("mistwarpData_whenLoaded")
		}
		return true, nil
	case "mistwarpData_save":
		r.data.status = "saving"
		encoded, err := jsonData(r.data.saved)
		if err != nil {
			r.data.status = "error: " + err.Error()
			return true, nil
		}
		if err := writeSavedData(r.data.savePath, []byte(encoded)); err != nil {
			r.data.status = "error: could not write " + r.data.savePath
		} else {
			r.data.status = "saved"
			r.startDataHats("mistwarpData_whenSaved")
		}
		return true, nil
	}
	return false, nil
}

func (r *Runtime) startDataHats(opcode string) {
	for i := len(r.executable) - 1; i >= 0; i-- {
		r.startHatsFor(r.executable[i], opcode, "", false)
	}
}
