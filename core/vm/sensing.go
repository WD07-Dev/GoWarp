package vm

import (
	"math"
	"strings"

	"gowarp/core/project"
)

func (r *Runtime) sensingReporter(t *target, b *project.Block, input func(string) (any, error)) (any, error) {
	switch b.Opcode {
	case "sensing_mousex":
		x, _ := r.mousePosition()
		return x, nil
	case "sensing_mousey":
		_, y := r.mousePosition()
		return y, nil
	case "sensing_mousedown":
		return r.mouseDown, nil
	case "sensing_answer":
		return r.answer, nil
	case "sensing_username":
		return r.username, nil
	case "tw_getLastKeyPressed":
		return r.lastKey, nil
	case "sensing_loudness":
		return r.loudness, nil
	case "sensing_loud":
		return r.loudness > 10, nil
	case "sensing_keypressed":
		value, err := input("KEY_OPTION")
		if err != nil {
			return nil, err
		}
		key := normalizeKey(text(value))
		return r.keyPressed(key), nil
	case "sensing_dayssince2000":
		return float64(r.now().UnixMilli()-946684800000) / 86400000, nil
	case "sensing_current":
		now := r.now()
		n := 0
		switch strings.ToLower(field(b, "CURRENTMENU", 0)) {
		case "year":
			n = now.Year()
		case "month":
			n = int(now.Month())
		case "date":
			n = now.Day()
		case "dayofweek":
			n = int(now.Weekday()) + 1
		case "hour":
			n = now.Hour()
		case "minute":
			n = now.Minute()
		case "second":
			n = now.Second()
		}
		return float64(n), nil
	case "sensing_distanceto":
		if t.state.IsStage {
			return float64(10000), nil
		}
		name, err := input("DISTANCETOMENU")
		if err != nil {
			return nil, err
		}
		x, y := r.mousePosition()
		if text(name) != "_mouse_" {
			other := r.cloneSource(t, text(name))
			if other == nil {
				return float64(10000), nil
			}
			x, y = other.state.X, other.state.Y
		}
		dx, dy := t.state.X-x, t.state.Y-y
		return math.Sqrt(dx*dx + dy*dy), nil
	case "sensing_of":
		name, err := input("OBJECT")
		if err != nil {
			return nil, err
		}
		other := r.cloneSource(t, text(name))
		if text(name) == "_stage_" {
			other = r.stage
		}
		if other == nil {
			return float64(0), nil
		}
		property := field(b, "PROPERTY", 0)
		if other.state.IsStage {
			switch property {
			case "backdrop #", "background #":
				return float64(other.state.CurrentCostume + 1), nil
			case "backdrop name":
				return costumeName(other), nil
			case "volume":
				return other.state.Volume, nil
			}
		} else {
			switch property {
			case "x position":
				return other.state.X, nil
			case "y position":
				return other.state.Y, nil
			case "direction":
				return other.state.Direction, nil
			case "size":
				return other.state.Size, nil
			case "costume #":
				return float64(other.state.CurrentCostume + 1), nil
			case "costume name":
				return costumeName(other), nil
			case "volume":
				return other.state.Volume, nil
			}
		}
		var value any
		var firstID string
		found := false
		for id, v := range other.variables {
			if v.Name == property && (!found || id < firstID) {
				firstID, value, found = id, v.Value, true
			}
		}
		if found {
			return value, nil
		}
	}
	return float64(0), nil
}
