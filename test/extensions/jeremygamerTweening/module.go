// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
)

var tweenProperties = map[string]string{
	"x position": "x",
	"y position": "y",
	"direction":  "direction",
	"size":       "size",
}

func New() e.Extension {
	return e.Extension{ID: "jeremygamerTweening", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "jeremygamerTweening_tweenValue", Kind: "reporter", Inputs: []string{"MODE", "DIRECTION", "START", "END", "AMOUNT"}, Handler: func(c *e.Context) (e.Result, error) {
			amount, ok := extensionEase(c.String("MODE"), c.String("DIRECTION"), c.Number("AMOUNT")/100)
			if !ok {
				return e.Result{Value: c.Number("START")}, nil
			}
			return e.Result{Value: c.Number("START") + (c.Number("END")-c.Number("START"))*amount}, nil
		}},
		{Opcode: "jeremygamerTweening_tweenVariable", Kind: "command", Inputs: []string{"VALUE", "SEC", "MODE", "DIRECTION"}, Handler: func(c *e.Context) (e.Result, error) {
			return step(c, true)
		}},
		{Opcode: "jeremygamerTweening_tweenProperty", Kind: "command", Inputs: []string{"PROPERTY", "VALUE", "SEC", "MODE", "DIRECTION"}, Handler: func(c *e.Context) (e.Result, error) {
			return step(c, false)
		}},
		{Opcode: "jeremygamerTweening_menu_direction", Kind: "reporter", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: c.Field("direction")}, nil
		}},
		{Opcode: "jeremygamerTweening_menu_modes", Kind: "reporter", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: c.Field("modes")}, nil
		}},
		{Opcode: "jeremygamerTweening_menu_properties", Kind: "reporter", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: c.Field("properties")}, nil
		}},
	}}
}

type tween struct {
	start, duration, from, to           float64
	mode, direction, property, variable string
}

func step(c *e.Context, variable bool) (e.Result, error) {
	nowValue, err := c.Call("runtime.time")
	if err != nil {
		return e.Result{}, err
	}
	now := e.Number(nowValue)
	t, ok := c.Local["tween"].(*tween)
	if !ok {
		t = &tween{start: now, duration: math.Max(0, c.Number("SEC")), to: c.Number("VALUE"), mode: c.String("MODE"), direction: c.String("DIRECTION")}
		var from any
		if variable {
			t.variable = c.Field("VAR")
			from, err = c.Call("variable.get", t.variable)
			if err != nil {
				return e.Result{}, nil
			}
		} else {
			t.property = tweenProperties[c.String("PROPERTY")]
			if t.property != "" {
				from, err = c.Call("target.get", t.property)
				if err != nil {
					return e.Result{}, err
				}
			}
		}
		t.from = e.Number(from)
		if _, ok := extensionEase(t.mode, t.direction, 0); !ok {
			t.mode = "linear"
		}
		c.Local["tween"] = t
		if !variable && t.property != "" {
			_, err = c.Call("target.set", t.property, t.from)
		}
		return e.Result{Again: true, Yield: true}, err
	}
	elapsed := now - t.start
	done := elapsed+1e-12 >= t.duration
	value := t.to
	if !done {
		amount, _ := extensionEase(t.mode, t.direction, elapsed/t.duration)
		value = t.from + (t.to-t.from)*amount
	}
	if variable {
		_, err = c.Call("variable.set", t.variable, value)
	} else if t.property != "" {
		_, err = c.Call("target.set", t.property, value)
	}
	return e.Result{Again: !done, Yield: !done}, err
}
func extensionEase(mode, dir string, x float64) (float64, bool) {
	if mode == "linear" {
		return x, true
	}
	switch mode {
	case "sine", "quad", "cubic", "quart", "quint", "expo", "circ", "back", "elastic", "bounce":
	default:
		return 0, false
	}
	if dir != "in" && dir != "out" && dir != "in out" {
		return 0, true
	}
	if mode == "back" && dir == "in out" {
		c := 1.70158 * 1.525
		if x < .5 {
			return math.Pow(2*x, 2) * ((c+1)*2*x - c) / 2, true
		}
		return (math.Pow(2*x-2, 2)*((c+1)*(2*x-2)+c) + 2) / 2, true
	}
	if mode == "elastic" && dir == "in out" {
		if x == 0 || x == 1 {
			return x, true
		}
		c := 2 * math.Pi / 4.5
		if x < .5 {
			return -math.Pow(2, 20*x-10) * math.Sin((20*x-11.125)*c) / 2, true
		}
		return math.Pow(2, -20*x+10)*math.Sin((20*x-11.125)*c)/2 + 1, true
	}
	if dir == "out" {
		v, _ := extensionEase(mode, "in", 1-x)
		return 1 - v, true
	}
	if dir == "in out" {
		if x < .5 {
			v, _ := extensionEase(mode, "in", 2*x)
			return v / 2, true
		}
		v, _ := extensionEase(mode, "in", 2-2*x)
		return 1 - v/2, true
	}
	switch mode {
	case "sine":
		return 1 - math.Cos(x*math.Pi/2), true
	case "quad":
		return x * x, true
	case "cubic":
		return x * x * x, true
	case "quart":
		return x * x * x * x, true
	case "quint":
		return x * x * x * x * x, true
	case "expo":
		if x == 0 {
			return 0, true
		}
		return math.Pow(2, 10*x-10), true
	case "circ":
		return 1 - math.Sqrt(1-x*x), true
	case "back":
		return 2.70158*x*x*x - 1.70158*x*x, true
	case "elastic":
		if x == 0 || x == 1 {
			return x, true
		}
		return -math.Pow(2, 10*x-10) * math.Sin((x*10-10.75)*2*math.Pi/3), true
	case "bounce":
		y := 1 - x
		var b float64
		switch {
		case y < 1/2.75:
			b = 7.5625 * y * y
		case y < 2/2.75:
			y -= 1.5 / 2.75
			b = 7.5625*y*y + .75
		case y < 2.5/2.75:
			y -= 2.25 / 2.75
			b = 7.5625*y*y + .9375
		default:
			y -= 2.625 / 2.75
			b = 7.5625*y*y + .984375
		}
		return 1 - b, true
	}
	return 0, false
}
