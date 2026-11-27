package vm

import (
	"encoding/json"
	"math"
	"math/rand"
	"strings"
	"unicode/utf16"

	"gowarp/core/project"
)

type GraphicEffects struct {
	Color      float64     `json:"color"`
	Fisheye    float64     `json:"fisheye"`
	Whirl      float64     `json:"whirl"`
	Pixelate   float64     `json:"pixelate"`
	Mosaic     float64     `json:"mosaic"`
	Brightness float64     `json:"brightness"`
	Ghost      float64     `json:"ghost"`
	Tint       *[3]float64 `json:"tint,omitempty"`
}

func (e GraphicEffects) MarshalJSON() ([]byte, error) {
	values := map[string]any{}
	for name, v := range map[string]float64{
		"color":      e.Color,
		"fisheye":    e.Fisheye,
		"whirl":      e.Whirl,
		"pixelate":   e.Pixelate,
		"mosaic":     e.Mosaic,
		"brightness": e.Brightness,
		"ghost":      e.Ghost,
	} {
		values[name] = v
		if !finite(v) {
			values[name] = text(v)
		}
	}
	if e.Tint != nil {
		values["tint"] = e.Tint
	}
	return json.Marshal(values)
}

func costumeName(t *target) string {
	if t.state.CurrentCostume >= 0 && t.state.CurrentCostume < len(t.source.Costumes) {
		return t.source.Costumes[t.state.CurrentCostume].Name
	}
	return ""
}

func setCostume(t *target, index float64) {
	changed := t.state.Text != nil && t.state.Text.Visible
	if changed {
		skin := *t.state.Text
		skin.Visible = false
		t.state.Text = &skin
	}
	count := len(t.source.Costumes)
	if count == 0 {
		return
	}
	index = math.Floor(index + .5)
	if !finite(index) {
		index = 0
	}
	index = math.Mod(index, float64(count))
	if index < 0 {
		index += float64(count)
	}
	changed = changed || t.state.CurrentCostume != int(index)
	t.state.CurrentCostume = int(index)
	t.state.CostumeName = costumeName(t)
	if changed && t.owner != nil {
		if preparer, ok := t.owner.graphicsHost().(interface{ PrepareCostume(int, int) }); ok {
			preparer.PrepareCostume(t.sourceIndex, t.state.CurrentCostume)
		}
	}
	t.requestRedraw()
}

func switchCostume(t *target, value any, backdrop bool) {
	if n, ok := value.(float64); ok {
		setCostume(t, n-1)
		return
	}
	name := text(value)
	if i, ok := costumeIndex(t, name); ok {
		setCostume(t, float64(i))
		return
	}
	suffix := "costume"
	if backdrop {
		suffix = "backdrop"
	}
	switch name {
	case "next " + suffix:
		setCostume(t, float64(t.state.CurrentCostume+1))
	case "previous " + suffix:
		setCostume(t, float64(t.state.CurrentCostume-1))
	case "random backdrop":
		if !backdrop {
			return
		}
		n := len(t.source.Costumes)
		if n > 1 {
			pick := rand.Intn(n - 1)
			if pick >= t.state.CurrentCostume {
				pick++
			}
			setCostume(t, float64(pick))
		}
	default:
		if trim(name) != "" {
			if n := numeric(value); !math.IsNaN(n) {
				setCostume(t, n-1)
			}
		}
	}
}

func costumeIndex(t *target, name string) (int, bool) {
	if t.owner == nil || len(t.source.Costumes) < 16 {
		for i, costume := range t.source.Costumes {
			if costume.Name == name {
				return i, true
			}
		}
		return 0, false
	}
	names := t.owner.costumeNames[t.source]
	if names == nil {
		names = make(map[string]int, len(t.source.Costumes))
		for i, c := range t.source.Costumes {
			if _, exists := names[c.Name]; !exists {
				names[c.Name] = i
			}
		}
		if t.owner.costumeNames == nil {
			t.owner.costumeNames = make(map[*project.Target]map[string]int)
		}
		t.owner.costumeNames[t.source] = names
	}
	i, ok := names[name]
	return i, ok
}

func (r *Runtime) looksStatement(th *thread, b *project.Block, input func(string) (any, error)) (bool, error) {
	t := th.target
	switch b.Opcode {
	case "looks_say", "looks_think", "looks_sayforsecs", "looks_thinkforsecs":
		v, err := input("MESSAGE")
		if err != nil {
			return false, err
		}
		timed := strings.HasSuffix(b.Opcode, "forsecs")
		var delay float64
		if timed {
			secs, err := input("SECS")
			if err != nil {
				return false, err
			}
			delay = math.Max(0, number(secs))
		}
		if !t.state.IsStage {
			message := text(v)
			if n, ok := v.(float64); ok && !math.IsNaN(n) && n != math.Floor(n) {
				message = text(math.Floor(n*100+.5) / 100)
			}
			units := utf16.Encode([]rune(message))
			if len(units) > 330 {
				message = string(utf16.Decode(units[:330]))
			}
			t.state.Bubble = message
			t.state.Think = strings.HasPrefix(b.Opcode, "looks_think")
			t.bubbleOwner = th
			t.requestRedraw()
		}
		if timed {
			if delay == 0 && th.warp {
				if t.bubbleOwner == th {
					t.state.Bubble = ""
					t.bubbleOwner = nil
				}
				return false, nil
			}
			th.wake = r.elapsed + delay
			th.wakeFrame = r.frame + 1
			th.bubbleEnd = true
			return true, nil
		}
	case "looks_nextcostume":
		setCostume(t, float64(t.state.CurrentCostume+1))
	case "looks_switchcostumeto":
		value, err := input("COSTUME")
		if err != nil {
			return false, err
		}
		switchCostume(t, value, false)
	case "looks_nextbackdrop", "looks_switchbackdropto", "looks_switchbackdroptoandwait":
		var value any = "next backdrop"
		if b.Opcode != "looks_nextbackdrop" {
			v, err := input("BACKDROP")
			if err != nil {
				return false, err
			}
			value = v
		}
		if r.stage == nil {
			return false, nil
		}
		switchCostume(r.stage, value, true)
		var started []*thread
		for i := len(r.executable) - 1; i >= 0; i-- {
			started = append(started, r.startHatsFor(r.executable[i], "event_whenbackdropswitchesto", costumeName(r.stage), true)...)
		}
		if b.Opcode == "looks_switchbackdroptoandwait" {
			th.waiting = started
			return anyRunning(started), nil
		}
	case "looks_cleargraphiceffects":
		t.state.Effects = GraphicEffects{}
		t.requestRedraw()
	case "looks_seteffectto", "looks_changeeffectby":
		var slot *float64
		switch strings.ToLower(field(b, "EFFECT", 0)) {
		case "color":
			slot = &t.state.Effects.Color
		case "fisheye":
			slot = &t.state.Effects.Fisheye
		case "whirl":
			slot = &t.state.Effects.Whirl
		case "pixelate":
			slot = &t.state.Effects.Pixelate
		case "mosaic":
			slot = &t.state.Effects.Mosaic
		case "brightness":
			slot = &t.state.Effects.Brightness
		case "ghost":
			slot = &t.state.Effects.Ghost
		}
		if slot == nil {
			return false, nil
		}
		name := "VALUE"
		if b.Opcode == "looks_changeeffectby" {
			name = "CHANGE"
		}
		v, err := input(name)
		if err != nil {
			return false, err
		}
		n := number(v)
		if name == "CHANGE" {
			n += *slot
		}
		if slot == &t.state.Effects.Ghost {
			n = math.Max(0, math.Min(100, n))
		}
		if slot == &t.state.Effects.Brightness {
			n = math.Max(-100, math.Min(100, n))
		}
		*slot = n
		t.requestRedraw()
	case "looks_gotofrontback":
		order := math.Inf(-1)
		if field(b, "FRONT_BACK", 0) == "front" {
			order = math.Inf(1)
		}
		r.moveLayer(t, order, false, true)
	case "looks_goforwardbackwardlayers":
		v, err := input("NUM")
		if err != nil {
			return false, err
		}
		n := number(v)
		if field(b, "FORWARD_BACKWARD", 0) != "forward" {
			n = -n
		}
		r.moveLayer(t, n, true, true)
	}
	return false, nil
}

func (r *Runtime) updateLayers() {
	for i, t := range r.drawList {
		t.state.LayerOrder = i
	}
}

func moveInList(list []*target, t *target, n float64, relative bool) {
	old := -1
	for i, candidate := range list {
		if candidate == t {
			old = i
			break
		}
	}
	if old < 0 {
		return
	}
	if relative {
		n += float64(old)
	}
	if math.IsNaN(n) {
		n = 0
	}
	low := 0
	if len(list) > 1 && list[0].state.IsStage {
		low = 1
	}
	pos := int(math.Max(float64(low), math.Min(float64(len(list)-1), n)))
	if pos < old {
		copy(list[pos+1:old+1], list[pos:old])
	} else {
		copy(list[old:pos], list[old+1:pos+1])
	}
	list[pos] = t
}

func (r *Runtime) moveLayer(t *target, n float64, relative, executable bool) {
	if t.state.IsStage {
		return
	}
	moveInList(r.drawList, t, n, relative)
	if executable {
		moveInList(r.executable, t, n, relative)
	}
	r.updateLayers()
}
