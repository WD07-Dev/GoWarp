package extension

/*
TriangleVertex contains explicit position, UV, color, depth and perspective data.
NaN fields are coerced to zero, as with Number; infinite fields or W <= 0
cause the triangle to be skipped.
*/
type TriangleVertex struct {
	X, Y, U, V, R, G, B, A, Z, W float64
}

type SurfaceTexture struct {
	Kind    string
	Surface string
	Costume float64
	Target  any
}

/*
SurfaceTriangle draws three vertices to a surface. Vertices and options are
copied on submission; costume sources are captured as snapshots.
*/
type SurfaceTriangle struct {
	Vertices             [3]TriangleVertex
	Texture              SurfaceTexture
	Linear               bool
	MultiplyTextureAlpha bool
}
