// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"math"
)

func call(c *e.Context, op string, args ...any) (e.Result, error) {
	v, err := c.Call(op, args...)
	return e.Result{Value: v}, err
}
func cmd(op string, inputs []string, fn e.Handler) e.Block {
	return e.Block{Opcode: op, Kind: "command", Inputs: inputs, Handler: fn}
}
func rep(op string, inputs []string, fn e.Handler) e.Block {
	return e.Block{Opcode: op, Kind: "reporter", Inputs: inputs, Handler: fn}
}
func menu(op, key string) e.Block {
	return rep(op, nil, func(c *e.Context) (e.Result, error) {
		return e.Result{Value: c.Field(key)}, nil
	})
}

type split struct {
	X, Y, Width, Offset, Angle, Start, Duration, Wait float64
	Direction, Easing, ReturnEasing                   string
	Returning                                         bool
}

func splits(c *e.Context) []split {
	v, _ := c.State["splits"].([]split)
	return v
}
func update(c *e.Context) (e.Result, error) {
	now, err := c.Call("runtime.time")
	if err != nil {
		return e.Result{}, err
	}
	rows := []any{}
	for _, s := range splits(c) {
		elapsed := e.Number(now) - s.Start
		factor := float64(1)
		if s.Returning {
			if elapsed < s.Duration {
				factor = ease(elapsed/s.Duration, s.Easing)
			} else if elapsed < s.Duration+s.Wait {
				factor = 1
			} else if s.Duration > 0 {
				factor = 1 - ease((elapsed-s.Duration-s.Wait)/s.Duration, s.ReturnEasing)
			} else {
				factor = 0
			}
		} else if s.Duration > 0 {
			factor = ease(elapsed/s.Duration, s.Easing)
		}
		rows = append(rows, map[string]any{"x": s.X, "y": s.Y, "width": s.Width, "offset": s.Offset * factor, "angle": s.Angle, "direction": s.Direction})
	}
	value, err := setSplits(c, []any{rows, e.Bool(c.State["original"]), e.String(c.State["color"])})
	return e.Result{Value: value}, err
}
func New() e.Extension {
	x := e.Extension{ID: "edgetalesplit", Events: map[string]e.Handler{"tick": update}}
	for _, key := range []string{"directionMenu", "easingMenu", "originalVisibilityMenu", "returnMenu"} {
		x.Blocks = append(x.Blocks, menu("edgetalesplit_menu_"+key, key))
	}
	x.Blocks = append(x.Blocks,
		cmd("edgetalesplit_clearSplits", nil, func(c *e.Context) (e.Result, error) {
			c.State["splits"] = nil
			return update(c)
		}),
		cmd("edgetalesplit_setOriginalVisibility", []string{"MODE"}, func(c *e.Context) (e.Result, error) {
			c.State["original"] = c.String("MODE") == "show"
			return update(c)
		}),
		cmd("edgetalesplit_setSplitColor", []string{"COLOR"}, func(c *e.Context) (e.Result, error) {
			c.State["color"] = c.String("COLOR")
			return update(c)
		}),
		cmd("edgetalesplit_addVerticalSplit", []string{"X", "Y", "WIDTH", "OFFSET", "DURATION", "EASING", "DIRECTION", "RETURN", "WAIT", "RETURN_EASING", "ANGLE"}, func(c *e.Context) (e.Result, error) {
			now, err := c.Call("runtime.time")
			if err != nil {
				return e.Result{}, err
			}
			s := split{X: c.Number("X"), Y: c.Number("Y"), Width: c.Number("WIDTH"), Offset: c.Number("OFFSET"), Angle: c.Number("ANGLE"), Start: e.Number(now), Duration: c.Number("DURATION"), Wait: c.Number("WAIT"), Direction: c.String("DIRECTION"), Easing: c.String("EASING"), ReturnEasing: c.String("RETURN_EASING"), Returning: c.String("RETURN") == "yes"}
			if s.Easing == "" {
				s.Easing = "탄성"
			}
			if s.ReturnEasing == "" {
				s.ReturnEasing = s.Easing
			}
			c.State["splits"] = append(splits(c), s)
			return update(c)
		}))
	return x
}
func ease(x float64, name string) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	switch name {
	case "sin":
		return math.Sin(x * math.Pi)
	case "되돌리기":
		y := x - 1
		return 1 + 2.70158*y*y*y + 1.70158*y*y
	case "바운드":
		switch {
		case x < 1/2.75:
			return 7.5625 * x * x
		case x < 2/2.75:
			x -= 1.5 / 2.75
			return 7.5625*x*x + .75
		case x < 2.5/2.75:
			x -= 2.25 / 2.75
			return 7.5625*x*x + .9375
		default:
			x -= 2.625 / 2.75
			return 7.5625*x*x + .984375
		}
	}
	return math.Pow(2, -10*x)*math.Sin((10*x-.75)*2*math.Pi/3) + 1
}

type part struct {
	x, y, offset, angle float64
	direction           string
}

func setSplits(host e.Host, args []any) (any, error) {
	var input any
	if len(args) > 0 {
		input = args[0]
	}
	rows, ok := input.([]any)
	if !ok && input != nil {
		return nil, fmt.Errorf("edgetalesplit.set requires an array")
	}
	parts := make([]e.ScreenSplit, 0, len(rows))
	for _, row := range rows {
		values, ok := row.(map[string]any)
		if !ok {
			continue
		}
		parts = append(parts, part{
			x: e.Number(values["x"]), y: e.Number(values["y"]),
			offset: e.Number(values["offset"]), angle: e.Number(values["angle"]),
			direction: e.String(values["direction"]),
		})
	}
	original := len(args) > 1 && e.Bool(args[1])
	return host.Call("graphics.screenSplits", parts, original)
}

func (p part) Project(width, height, pixelScale float64) e.SplitGeometry {
	cx, cy := width/2+p.x*width/480, height/2-p.y*height/360
	slope := math.Tan(p.angle * math.Pi / 180)
	geometry := e.SplitGeometry{
		TopX: math.Round(cx + slope*cy), BottomX: math.Round(cx - slope*(height-cy)),
	}
	offset := p.offset * pixelScale
	if math.Abs(p.angle) < .01 {
		if p.direction == "horizontal" || p.direction == "both" {
			geometry.MoveX = math.Round(offset)
		}
		if p.direction == "vertical" || p.direction == "both" {
			geometry.MoveY = math.Round(offset)
		}
	} else {
		length := math.Hypot(geometry.BottomX-geometry.TopX, height)
		geometry.MoveX = math.Round((geometry.BottomX - geometry.TopX) / length * math.Round(offset))
		geometry.MoveY = math.Round(height / length * math.Round(offset))
	}
	return geometry
}
