package extension

/*
SplitGeometry describes a full-height seam and the opposite translations on
either side, in output pixels.
*/
type SplitGeometry struct {
	TopX, BottomX, MoveX, MoveY float64
}

// ScreenSplit is immutable once submitted to the host.
type ScreenSplit interface {
	Project(width, height, pixelScale float64) SplitGeometry
}

/*
NormalizedSplit uses width fractions for its seam and logical display pixels
for movement. It does not round coordinates.
*/
type NormalizedSplit struct {
	TopX, BottomX, MoveX, MoveY float64
}

func (s NormalizedSplit) Project(width, height, pixelScale float64) SplitGeometry {
	return SplitGeometry{s.TopX * width, s.BottomX * width, s.MoveX * pixelScale, s.MoveY * pixelScale}
}
