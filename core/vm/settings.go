package vm

import "fmt"

// ConfigureRendering overrides project rendering defaults before playback.
func (r *Runtime) ConfigureRendering(highQualityPen, disablePositionRounding bool) {
	r.options.HighQualityPen = highQualityPen
	r.options.View.DisablePositionRounding = disablePositionRounding
	if r.graphics != nil {
		r.graphics.SetView(r.View())
	}
}

// ConfigureLimits applies player overrides without changing clone limits.
func (r *Runtime) ConfigureLimits(fencing, miscLimits bool) {
	r.options.Fencing = fencing
	r.options.MiscLimits = miscLimits
	if r.graphics != nil {
		r.graphics.SetView(r.View())
	}
}

// Configure applies an explicit player configuration before execution starts.
func (r *Runtime) Configure(fps float64, turbo, interpolation bool, width, height int, username, background string) error {
	if !finite(fps) || fps <= 0 || fps > 1000 {
		return fmt.Errorf("invalid FPS")
	}
	if err := r.SetStageSize(float64(width), float64(height)); err != nil {
		return err
	}
	r.fps = fps
	r.fpsOverride = true
	r.turbo = turbo
	r.turboOverride = true
	r.options.Interpolation = interpolation
	r.options.View.Color = background
	r.username = username
	if r.graphics != nil {
		r.graphics.SetView(r.View())
	}
	return nil
}

// ConfigureAntialias controls rendering without changing coordinates or timing.
func (r *Runtime) ConfigureAntialias(enabled bool) {
	r.options.View.NoAntialias = !enabled
	if r.graphics != nil {
		r.graphics.SetView(r.View())
	}
}
