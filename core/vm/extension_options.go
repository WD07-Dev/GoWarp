package vm

import (
	"fmt"
	"math"

	"gowarp/core/extension"
)

func (c *extensionContext) optionsCall(operation string, args []any) (any, error) {
	r := c.r
	a := hostArgs(args)
	if operation != "options.get" && operation != "options.set" {
		return nil, fmt.Errorf("unsupported extension host operation %q", operation)
	}
	key := a.string(0)
	set := operation == "options.set"
	switch key {
	case "framerate":
		if !set {
			return r.fps, nil
		}
		n := a.number(1)
		if !finite(n) || n > 1000 {
			return nil, fmt.Errorf("framerate must be finite and at most 1000; nonpositive values use 60 FPS")
		}
		r.fps = n
		r.fpsOverride = true
	case "cloneLimit":
		if !set {
			return float64(r.options.CloneLimit), nil
		}
		r.options.CloneLimit = int(clamp(math.Floor(a.number(1)), -1, 1e9))
	case "username":
		if !set {
			return r.username, nil
		}
		r.username = a.string(1)
	case "turbo":
		if !set {
			return r.turbo, nil
		}
		r.turbo = extension.Bool(a.get(1))
		r.turboOverride = true
	case "interpolation":
		if !set {
			return r.options.Interpolation, nil
		}
		r.options.Interpolation = extension.Bool(a.get(1))
	case "fencing":
		if !set {
			return r.options.Fencing, nil
		}
		r.options.Fencing = extension.Bool(a.get(1))
		r.extensionViewChanged()
	case "miscLimits":
		if !set {
			return r.options.MiscLimits, nil
		}
		r.options.MiscLimits = extension.Bool(a.get(1))
	case "highQualityPen":
		if !set {
			return r.options.HighQualityPen, nil
		}
		r.options.HighQualityPen = extension.Bool(a.get(1))
		r.extensionViewChanged()
	default:
		return nil, fmt.Errorf("unknown runtime option %q", key)
	}
	return nil, nil
}
