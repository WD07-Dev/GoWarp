package extension

// TriangleVertex contains explicit position, UV, color, depth and perspective data.
// NaN fields are coerced to zero, as with Number; infinite fields or W <= 0
// cause the triangle to be skipped.
type TriangleVertex struct {
	X, Y, U, V, R, G, B, A, Z, W float64
}

// SurfaceTexture selects the source of a SurfaceTriangle. Kind is "" for a
// solid triangle, "surface" for Surface (including an empty name), or "costume"
// for a zero-based Costume index. Target uses the usual target selector and
// defaults to the current target. Selecting a costume does not change it.
type SurfaceTexture struct {
	Kind    string
	Surface string
	Costume float64
	Target  any
}

// SurfaceTriangle draws three vertices to a surface. Vertices and options are
// copied on submission; costume sources are captured as snapshots.
type SurfaceTriangle struct {
	Vertices             [3]TriangleVertex
	Texture              SurfaceTexture
	Linear               bool
	MultiplyTextureAlpha bool
}
