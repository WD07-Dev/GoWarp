package vm

import "gowarp/core/extension"

/*
CanvasEffects uses CSS percentages, including 100 for neutral brightness,
contrast and scale. The zero value leaves the canvas unchanged.
*/
type CanvasEffects struct {
	Initialized, Nearest                                                     bool
	Blur, Contrast, Saturation, Hue, Brightness, Invert, Sepia, Transparency float64
	ScaleX, ScaleY, SkewX, SkewY, OffsetX, OffsetY, Rotation, Radius         float64
	RenderWidth, RenderHeight, BorderWidth                                   float64
	BorderStyle, BorderColor, Background                                     string
}

func (c CanvasEffects) Defaults() CanvasEffects {
	if !c.Initialized {
		c.Initialized = true
		c.Contrast, c.Saturation, c.Brightness, c.ScaleX, c.ScaleY = 100, 100, 100, 100, 100
		c.BorderStyle, c.BorderColor, c.Background = "solid", "#000000", "transparent"
	}
	return c
}

func (c *CanvasEffects) slot(name string) *float64 {
	switch name {
	case "blur":
		return &c.Blur
	case "contrast":
		return &c.Contrast
	case "saturation":
		return &c.Saturation
	case "color shift":
		return &c.Hue
	case "brightness":
		return &c.Brightness
	case "invert":
		return &c.Invert
	case "sepia":
		return &c.Sepia
	case "transparency":
		return &c.Transparency
	case "scale", "scale X":
		return &c.ScaleX
	case "scale Y":
		return &c.ScaleY
	case "skew X":
		return &c.SkewX
	case "skew Y":
		return &c.SkewY
	case "offset X":
		return &c.OffsetX
	case "offset Y":
		return &c.OffsetY
	case "rotation":
		return &c.Rotation
	case "border radius":
		return &c.Radius
	case "border width":
		return &c.BorderWidth
	case "renderWidth":
		return &c.RenderWidth
	case "renderHeight":
		return &c.RenderHeight
	}
	return nil
}

/*
StageShader retains the actual project program and its per-program uniforms;
the display backend compiles it without depending on shader or project names.
*/
type StageShader struct {
	// program identifies the registered definition behind a pipeline pass.
	program  *StageShader
	Next     *StageShader
	Fragment string
	Compiler extension.ShaderCompiler
	Uniforms map[string]any
	// Overrides are applied in pass order to the shared program uniforms.
	Overrides map[string]any
}

func (s *StageShader) Source() extension.ShaderSource {
	return extension.ShaderSource{Code: s.Fragment, Compiler: s.Compiler}
}

type visualState struct {
	layers        map[string]*renderLayer
	activeLayer   string
	cameras       map[string]*RenderTransform
	nextLayerID   int
	shaders       map[string]*StageShader
	surfaces      map[string][2]int
	surfacePixels int
}
