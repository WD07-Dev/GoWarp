package vm

/*
TextSkin is immutable once published so clones and render snapshots retain
the text they were given when another sprite changes its style.
*/
type TextSkin struct {
	Text, Font, Color, OutlineColor string
	Width, OutlineWidth             float64
	Visible                         bool
	FontSize, LineHeight, PaddingY  float64
	// Line advances multiply by LineHeight before dividing, retaining rational spacing.
	LineHeightDivisor float64
	AnchorY, Align    float64
	ScreenWidth       bool
}

// Defaults supplies usable raster dimensions when a caller omits layout metrics.
func (s TextSkin) Defaults() TextSkin {
	if !finite(s.FontSize) || s.FontSize <= 0 {
		s.FontSize = 16
	}
	if !finite(s.LineHeight) || s.LineHeight <= 0 {
		s.LineHeight = s.FontSize
	}
	if !finite(s.LineHeightDivisor) || s.LineHeightDivisor <= 0 {
		s.LineHeightDivisor = 1
	}
	return s
}
