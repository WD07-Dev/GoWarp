package vm

import (
	"context"
	"encoding/json"
	"math"
	"math/rand"

	"gowarp/core/project"
)

func (t *target) setXY(x, y float64) {
	if t.state.IsStage {
		return
	}
	oldX, oldY := t.state.X, t.state.Y
	if r := t.owner; r != nil && r.graphics != nil && r.options.Fencing {
		if box, ok := r.graphics.Bounds(display(t), false); ok {
			inset := math.Min(15, math.Floor(math.Min(math.Abs(box.Right-box.Left), math.Abs(box.Top-box.Bottom))/2))
			lx, ly := r.View().Width/2-inset, r.View().Height/2-inset
			originX, originY := oldX, oldY
			if !r.options.HighQualityPen && !r.options.View.DisablePositionRounding {
				originX, originY = math.Floor(oldX+.5), math.Floor(oldY+.5)
			}
			dx, dy := x-originX, y-originY
			if box.Right+dx < -lx {
				x = math.Ceil(originX - lx - box.Right)
			} else if box.Left+dx > lx {
				x = math.Floor(originX + lx - box.Left)
			}
			if box.Top+dy < -ly {
				y = math.Ceil(originY - ly - box.Top)
			} else if box.Bottom+dy > ly {
				y = math.Floor(originY + ly - box.Bottom)
			}
		}
	}
	t.state.X, t.state.Y = x, y
	t.requestRedraw()
	if t.owner != nil && t.state.Pen.Down {
		t.owner.penLine(t, oldX, oldY, x, y)
	}
}

func (t *target) moveSteps(steps float64) {
	radians := (90 - t.state.Direction) * math.Pi / 180
	t.setXY(t.state.X+steps*math.Cos(radians), t.state.Y+steps*math.Sin(radians))
}

func (t *target) setDirection(direction float64) {
	if !finite(direction) {
		return
	}
	t.state.Direction = direction - math.Floor((direction+179)/360)*360
	t.requestRedraw()
}

func limitPrecision(value float64) float64 {
	lower := math.Floor(value)
	if value-lower < 1e-9 {
		if lower == 0 {
			return math.Copysign(0, value)
		}
		return lower
	}
	upper := lower + 1
	if upper-value < 1e-9 {
		if upper == 0 {
			return math.Copysign(0, value)
		}
		return upper
	}
	return value
}

func (r *Runtime) motionDestination(name string) (x, y float64, found bool, err error) {
	switch name {
	case "_mouse_":
		x, y := r.mousePosition()
		return x, y, true, nil
	case "_random_":
		return math.Floor(rand.Float64()*r.View().Width - r.View().Width/2 + 0.5), math.Floor(rand.Float64()*r.View().Height - r.View().Height/2 + 0.5), true, nil
	}
	for _, t := range r.targets {
		if t.state.CloneID == 0 && !t.state.IsStage && t.state.Name == name {
			return t.state.X, t.state.Y, true, nil
		}
	}
	return 0, 0, false, nil
}

func (r *Runtime) motionStatement(th *thread, b *project.Block, input func(string) (any, error)) (bool, error) {
	t := th.target
	num := func(name string) (float64, error) {
		v, err := input(name)
		return number(v), err
	}
	switch b.Opcode {
	case "motion_glidesecstoxy", "motion_glideto":
		duration, err := num("SECS")
		if err != nil {
			return false, err
		}
		startX, startY := t.state.X, t.state.Y
		var x, y float64
		if b.Opcode == "motion_glidesecstoxy" {
			x, err = num("X")
			if err != nil {
				return false, err
			}
			y, err = num("Y")
			if err != nil {
				return false, err
			}
		} else {
			value, err := input("TO")
			if err != nil {
				return false, err
			}
			var found bool
			x, y, found, err = r.motionDestination(text(value))
			if err != nil {
				return false, err
			}
			if !found {
				return false, nil
			}
		}
		th.glide = &glideState{
			startTime: r.elapsed,
			duration:  duration,
			startX:    startX,
			startY:    startY,
			endX:      x,
			endY:      y,
		}
		return !r.advanceGlide(th), nil
	case "motion_movesteps":
		steps, err := num("STEPS")
		if err != nil {
			return false, err
		}
		t.moveSteps(steps)
	case "motion_turnright", "motion_turnleft", "motion_pointindirection":
		name := "DEGREES"
		if b.Opcode == "motion_pointindirection" {
			name = "DIRECTION"
		}
		value, err := num(name)
		if err != nil {
			return false, err
		}
		if b.Opcode == "motion_turnright" {
			value = t.state.Direction + value
		}
		if b.Opcode == "motion_turnleft" {
			value = t.state.Direction - value
		}
		t.setDirection(value)
	case "motion_gotoxy":
		x, err := num("X")
		if err != nil {
			return false, err
		}
		y, err := num("Y")
		if err != nil {
			return false, err
		}
		t.setXY(x, y)
	case "motion_setx", "motion_changexby":
		name := "X"
		if b.Opcode == "motion_changexby" {
			name = "DX"
		}
		x, err := num(name)
		if err != nil {
			return false, err
		}
		if b.Opcode == "motion_changexby" {
			x += t.state.X
		}
		t.setXY(x, t.state.Y)
	case "motion_sety", "motion_changeyby":
		name := "Y"
		if b.Opcode == "motion_changeyby" {
			name = "DY"
		}
		y, err := num(name)
		if err != nil {
			return false, err
		}
		if b.Opcode == "motion_changeyby" {
			y += t.state.Y
		}
		t.setXY(t.state.X, y)
	case "motion_goto", "motion_pointtowards":
		name := "TO"
		if b.Opcode == "motion_pointtowards" {
			name = "TOWARDS"
		}
		value, err := input(name)
		if err != nil {
			return false, err
		}
		if b.Opcode == "motion_pointtowards" && text(value) == "_random_" {
			t.setDirection(math.Floor(rand.Float64()*360+0.5) - 180)
			break
		}
		x, y, found, err := r.motionDestination(text(value))
		if err != nil {
			return false, err
		}
		if !found {
			break
		}
		if b.Opcode == "motion_goto" {
			t.setXY(x, y)
		} else {
			t.setDirection(90 - math.Atan2(y-t.state.Y, x-t.state.X)*180/math.Pi)
		}
	case "motion_setrotationstyle":
		style := field(b, "STYLE", 0)
		switch style {
		case "all around", "left-right", "don't rotate":
			t.state.RotationStyle = style
			t.requestRedraw()
		}
	case "looks_setsizeto", "looks_changesizeby":
		name := "SIZE"
		if b.Opcode == "looks_changesizeby" {
			name = "CHANGE"
		}
		value, err := num(name)
		if err != nil {
			return false, err
		}
		if b.Opcode == "looks_changesizeby" {
			value += t.state.Size
		}
		r.setTargetSize(t, value)
	case "looks_show":
		t.state.Visible = true
		t.requestRedraw()
	case "looks_hide":
		t.state.Visible = false
	}
	return false, nil
}

func (r *Runtime) setTargetSize(t *target, value float64) {
	if t.state.IsStage {
		return
	}
	t.state.Size = math.Max(0, value)
	t.requestRedraw()
	if r.graphics == nil || !r.options.Fencing {
		return
	}
	width, height := r.graphics.CostumeSize(display(t))
	if width >= 0 && height >= 0 {
		minimum := math.Min(1, math.Max(5/width, 5/height))
		maximum := math.Min(1.5*r.View().Width/width, 1.5*r.View().Height/height)
		t.state.Size = clamp(value/100, minimum, maximum) * 100
	}
}

/*
Preserve numeric state internally while making nonfinite values JSON-safe,
using the same strings already emitted for nonfinite variables/list items.
*/
func (s TargetState) MarshalJSON() ([]byte, error) {
	type plain TargetState
	safe := func(n float64) any {
		if !finite(n) {
			return text(n)
		}
		return n
	}
	return json.Marshal(struct {
		*plain
		X         any `json:"x"`
		Y         any `json:"y"`
		Direction any `json:"direction"`
		Size      any `json:"size"`
		StretchX  any `json:"stretchX"`
		StretchY  any `json:"stretchY"`
	}{
		(*plain)(&s),
		safe(s.X),
		safe(s.Y),
		safe(s.Direction),
		safe(s.Size),
		safe(s.StretchX),
		safe(s.StretchY),
	})
}

// Glide endpoints are captured once; only interpolation advances across frames.
type glideState struct {
	startTime                  float64
	duration                   float64
	startX, startY, endX, endY float64
}

func (r *Runtime) advanceGlide(th *thread) bool {
	g := th.glide
	elapsed := r.elapsed - g.startTime
	if g.duration <= 0 || elapsed+1e-12 >= g.duration {
		th.target.setXY(g.endX, g.endY)
		th.glide = nil
		return true
	}
	fraction := elapsed / g.duration
	th.target.setXY(g.startX+(g.endX-g.startX)*fraction, g.startY+(g.endY-g.startY)*fraction)
	return false
}

// Prepare both pure inputs together; yielding reporters keep the normal path.
type coordinateMove struct {
	x, y *numericExpression
}

func (r *Runtime) prepareCoordinateMove(t *target, b *project.Block) *coordinateMove {
	if move, found := r.coordinateMoves[b]; found {
		return move
	}
	if r.coordinateMoves == nil {
		r.coordinateMoves = make(map[*project.Block]*coordinateMove)
	}
	x, y := r.numericInput(t, b, "X", 0), r.numericInput(t, b, "Y", 0)
	var move *coordinateMove
	if x != nil && y != nil && !x.conditional && !y.conditional &&
		x.height <= 128 && y.height <= 128 && x.blocks+y.blocks <= stepsPerFrame {
		move = &coordinateMove{x: x, y: y}
	}
	r.coordinateMoves[b] = move
	return move
}

func (m *coordinateMove) run(ctx context.Context, r *Runtime, th *thread) error {
	x, err := m.x.evaluate(ctx, r, th)
	if err != nil {
		return err
	}
	y, err := m.y.evaluate(ctx, r, th)
	if err != nil {
		return err
	}
	// Match Scratch's numeric input conversion, including NaN results.
	if math.IsNaN(x) {
		x = 0
	}
	if math.IsNaN(y) {
		y = 0
	}
	th.target.setXY(x, y)
	return nil
}
