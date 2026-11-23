package render

import (
	"image"
	"math"
	"weak"

	"gowarp/core/media"
	"gowarp/core/vm"
)

type hullPoint struct {
	x, y float64
}

/*
Cache the last silhouette within 128 KiB. Weak image identity invalidates replaced pixels without
retaining them.
*/
type shapedHullCache struct {
	image         weak.Pointer[image.NRGBA]
	width, height float64
	effects       [4]float64
	points        []hullPoint
}

/*
tightBounds follows scratch-render's touching hull: first and last touching
skin column of each row, transformed at pixel centers. Transforming a single
rectangular alpha crop would include transparent corners after rotation.
*/
func (r *Renderer) tightBounds(t vm.DisplayTarget, tr transform) (vm.Bounds, bool) {
	fallback := func() (vm.Bounds, bool) {
		box := tr.worldBounds(image.Rectangle{})
		return box, finite(box.Left) && finite(box.Right) && finite(box.Bottom) && finite(box.Top)
	}
	if !t.State.Visible || tr.image != nil && tr.image.Bounds().Empty() {
		return fallback()
	}
	if !finite(tr.c.Width) || !finite(tr.c.Height) || tr.c.Width <= 0 || tr.c.Height <= 0 {
		return fallback()
	}
	e := t.State.Effects
	shaped := e.Mosaic != 0 || e.Pixelate != 0 || e.Whirl != 0 || e.Fisheye != 0
	shapeEffects := [4]float64{e.Mosaic, e.Pixelate, e.Whirl, e.Fisheye}
	cacheShape := shaped && finite(e.Mosaic) && finite(e.Pixelate) && finite(e.Whirl) && finite(e.Fisheye)
	var points []hullPoint
	cached := false
	if !shaped {
		points, cached = r.hulls[tr.c]
	} else if cacheShape && r.shapedHull.width == tr.c.Width && r.shapedHull.height == tr.c.Height &&
		r.shapedHull.effects == shapeEffects && r.shapedHull.image.Value() == tr.image {
		points, cached = r.shapedHull.points, true
	}
	if !cached {
		points = touchingHull(tr, e)
		if !shaped {
			if r.hulls == nil {
				r.hulls = make(map[*media.Costume][]hullPoint)
			}
			r.hulls[tr.c] = points
		} else if cacheShape {
			r.shapedHull = shapedHullCache{}
			if cap(points) <= 8192 {
				r.shapedHull = shapedHullCache{weak.Make(tr.image), tr.c.Width, tr.c.Height, shapeEffects, points}
			}
		}
	}
	// The reference renderer uses fast bounds for a wholly transparent skin.
	if len(points) == 0 {
		return fallback()
	}
	box := vm.Bounds{
		Left:   math.Inf(1),
		Right:  math.Inf(-1),
		Bottom: math.Inf(1),
		Top:    math.Inf(-1),
	}
	for _, p := range points {
		x, y := (p.x+.5-tr.c.CenterX)*tr.sx, (tr.c.CenterY-p.y-.5)*tr.sy
		wx, wy := tr.x+tr.cos*x-tr.sin*y, tr.y+tr.sin*x+tr.cos*y
		box.Left = math.Min(box.Left, wx)
		box.Right = math.Max(box.Right, wx)
		box.Bottom = math.Min(box.Bottom, wy)
		box.Top = math.Max(box.Top, wy)
	}
	return box, finite(box.Left) && finite(box.Right) && finite(box.Bottom) && finite(box.Top)
}

func touchingHull(tr transform, e vm.GraphicEffects) []hullPoint {
	var points []hullPoint
	for y := 0.0; y < tr.c.Height; y++ {
		v := float64(float32(y / tr.c.Height))
		left := -1.0
		for x := 0.0; x < tr.c.Width; x++ {
			if hullTouches(tr, float64(float32(x/tr.c.Width)), v, e) {
				left = x
				break
			}
		}
		if left < 0 {
			continue
		}
		points = append(points, hullPoint{left, y})
		for x := tr.c.Width - 1; x >= 0; x-- {
			if hullTouches(tr, float64(float32(x/tr.c.Width)), v, e) {
				if x != left {
					points = append(points, hullPoint{x, y})
				}
				break
			}
		}
	}
	return points
}

/*
Hull extraction always uses the linear silhouette test, independently of the
displayed costume filter. Color, brightness and ghost do not change its shape.
*/
func hullTouches(tr transform, u, v float64, e vm.GraphicEffects) bool {
	u, v = effectUV(u, v, tr.c.Width, tr.c.Height, e)
	if !finite(u) || !finite(v) {
		return false
	}
	bounds := tr.image.Bounds()
	col, row := math.Floor(u*float64(bounds.Dx()-1)), math.Floor(v*float64(bounds.Dy()-1))
	/*
		Reject before converting huge effect coordinates to integers. A point just
		outside the texture can still touch through its neighboring sample.
	*/
	if col < -1 || row < -1 || col >= float64(bounds.Dx()) || row >= float64(bounds.Dy()) {
		return false
	}
	x, y := int(col)+bounds.Min.X, int(row)+bounds.Min.Y
	return tr.image.NRGBAAt(x, y).A > 0 || tr.image.NRGBAAt(x+1, y).A > 0 || tr.image.NRGBAAt(x, y+1).A > 0 || tr.image.NRGBAAt(x+1, y+1).A > 0
}
