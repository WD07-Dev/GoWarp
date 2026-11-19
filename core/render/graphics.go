package render

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"gowarp/core/vm"
)

/*
RestorePen imports premultiplied display pixels for CPU-only operations. Color sensing retains
exact CPU history instead.
*/
func (r *Renderer) RestorePen(pixels *image.RGBA) {
	if r.pen == nil {
		r.pen = image.NewNRGBA(r.frame.Bounds())
	}
	clear(r.pen.Pix)
	if r.penComposite == nil || r.penComposite.Bounds() != pixels.Bounds() {
		r.penComposite = image.NewRGBA(pixels.Bounds())
	}
	copy(r.penComposite.Pix, pixels.Pix)
	draw.Draw(r.pen, r.pen.Bounds(), pixels, pixels.Bounds().Min, draw.Src)
	r.penRevision++
}

func (r *Renderer) ScriptDataIndependent() bool {
	return true
}

func (r *Renderer) SetView(v vm.View) {
	if v.Width <= 0 || v.Height <= 0 || v.Width > 4096 || v.Height > 4096 || !finite(v.Width) || !finite(v.Height) {
		return
	}
	changed := r.view.Width != v.Width || r.view.Height != v.Height
	r.view = v
	if changed {
		r.penDepth = nil
		r.frame = image.NewRGBA(image.Rect(0, 0, int(math.Ceil(v.Width)), int(math.Ceil(v.Height))))
	}
	if changed && r.pen != nil {
		r.penRevision++
		old := r.pen
		if r.penComposite != nil {
			oldComposite := r.penComposite
			r.penComposite = image.NewRGBA(image.Rect(0, 0, int(math.Ceil(v.Width)), int(math.Ceil(v.Height))))
			dx, dy := (r.penComposite.Bounds().Dx()-oldComposite.Bounds().Dx())/2, (r.penComposite.Bounds().Dy()-oldComposite.Bounds().Dy())/2
			for y := 0; y < oldComposite.Bounds().Dy(); y++ {
				for x := 0; x < oldComposite.Bounds().Dx(); x++ {
					r.penComposite.SetRGBA(x+dx, y+dy, oldComposite.RGBAAt(x, y))
				}
			}
		}
		r.pen = image.NewNRGBA(image.Rect(0, 0, int(math.Ceil(v.Width)), int(math.Ceil(v.Height))))
		dx, dy := (r.pen.Bounds().Dx()-old.Bounds().Dx())/2, (r.pen.Bounds().Dy()-old.Bounds().Dy())/2
		for y := 0; y < old.Bounds().Dy(); y++ {
			for x := 0; x < old.Bounds().Dx(); x++ {
				r.pen.SetNRGBA(x+dx, y+dy, old.NRGBAAt(x, y))
			}
		}
	}
}

func finite(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0)
}

type screenToWorld struct {
	halfWidth, halfHeight, scaleX, scaleY, zoom float64
	sin, cos, x, y                              float64
}

/*
Cache camera constants, keeping the original arithmetic order so pixels on
texture boundaries do not shift through rounding or accumulated scan steps.
*/
func (r *Renderer) screenToWorld() screenToWorld {
	v := r.view
	zoom := v.Zoom / 100
	if zoom == 0 || !finite(zoom) {
		zoom = math.NaN()
	}
	s, c := math.Sincos((90 - v.Direction) * math.Pi / 180)
	w, h := float64(r.frame.Bounds().Dx()), float64(r.frame.Bounds().Dy())
	return screenToWorld{w / 2, h / 2, v.Width / w, v.Height / h, zoom, s, c, v.X, v.Y}
}

func (s screenToWorld) rowY(y float64) float64 {
	return (s.halfHeight - y) * s.scaleY / s.zoom
}

func (s screenToWorld) point(x, cameraY float64) (float64, float64) {
	x = (x - s.halfWidth) * s.scaleX / s.zoom
	return s.cos*x + s.sin*cameraY + s.x, s.cos*cameraY - s.sin*x + s.y
}

func (r *Renderer) FromScreen(x, y float64) (float64, float64) {
	camera := r.screenToWorld()
	return camera.point(x, camera.rowY(y))
}

func (r *Renderer) ToScreen(x, y float64) (float64, float64) {
	return r.DisplayPoint(x, y, r.frame.Bounds().Dx(), r.frame.Bounds().Dy())
}

func (tr transform) worldBounds(crop image.Rectangle) vm.Bounds {
	minU, minV, maxU, maxV := 0.0, 0.0, tr.c.Width, tr.c.Height
	if !crop.Empty() {
		minU = float64(crop.Min.X) / float64(tr.image.Bounds().Dx()) * tr.c.Width
		maxU = float64(crop.Max.X) / float64(tr.image.Bounds().Dx()) * tr.c.Width
		minV = float64(crop.Min.Y) / float64(tr.image.Bounds().Dy()) * tr.c.Height
		maxV = float64(crop.Max.Y) / float64(tr.image.Bounds().Dy()) * tr.c.Height
	}
	box := vm.Bounds{
		Left:   math.Inf(1),
		Right:  math.Inf(-1),
		Bottom: math.Inf(1),
		Top:    math.Inf(-1),
	}
	for _, u := range []float64{minU, maxU} {
		for _, v := range []float64{minV, maxV} {
			lx, ly := (u-tr.c.CenterX)*tr.sx, (tr.c.CenterY-v)*tr.sy
			x, y := tr.x+tr.cos*lx-tr.sin*ly, tr.y+tr.sin*lx+tr.cos*ly
			box.Left = math.Min(box.Left, x)
			box.Right = math.Max(box.Right, x)
			box.Bottom = math.Min(box.Bottom, y)
			box.Top = math.Max(box.Top, y)
		}
	}
	return box
}

func (r *Renderer) Bounds(t vm.DisplayTarget, tight bool) (vm.Bounds, bool) {
	/*
		Fencing needs only costume geometry. Decode pixels only when the
		caller asks for the opaque silhouette.
	*/
	tr, ok := r.transformGeometry(t)
	if ok && tight {
		_, cached := r.hulls[tr.c]
		e := t.State.Effects
		// Reuse a cached silhouette without decoding evicted pixels.
		if !cached || !t.State.Visible || e.Mosaic != 0 || e.Pixelate != 0 || e.Whirl != 0 || e.Fisheye != 0 {
			tr.image = r.Assets.CostumeImage(tr.c)
			ok = tr.image != nil
		}
	}
	if !ok {
		w, h := r.CostumeSize(t)
		if w == 0 && h == 0 && finite(t.State.X) && finite(t.State.Y) {
			x, y := t.State.X, t.State.Y
			if !r.view.HighQualityPen && !r.view.DisablePositionRounding {
				x, y = math.Floor(x+.5), math.Floor(y+.5)
			}
			if t.State.IsStage {
				x, y = 0, 0
			}
			return vm.Bounds{
				Left:   x,
				Right:  x,
				Bottom: y,
				Top:    y,
			}, true
		}
		return vm.Bounds{}, false
	}
	if tight {
		return r.tightBounds(t, tr)
	}
	b := tr.worldBounds(image.Rectangle{})
	return b, finite(b.Left) && finite(b.Right) && finite(b.Top) && finite(b.Bottom)
}

func (r *Renderer) CostumeSize(t vm.DisplayTarget) (float64, float64) {
	if t.State.Text != nil && t.State.Text.Visible {
		if c := r.textCostume(t); c != nil {
			return c.Width, c.Height
		}
		return 0, 0
	}
	if t.Source < 0 || t.Source >= len(r.Assets.Costumes) {
		return 0, 0
	}
	costumes := r.Assets.Costumes[t.Source]
	if t.State.CurrentCostume < 0 || t.State.CurrentCostume >= len(costumes) {
		return 0, 0
	}
	c := costumes[t.State.CurrentCostume]
	return c.Width, c.Height
}

func (r *Renderer) screenBounds(box vm.Bounds) image.Rectangle {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, x := range []float64{box.Left, box.Right} {
		for _, y := range []float64{box.Bottom, box.Top} {
			px, py := r.ToScreen(x, y)
			if !finite(px) || !finite(py) {
				return image.Rectangle{}
			}
			minX, minY = math.Min(minX, px), math.Min(minY, py)
			maxX, maxY = math.Max(maxX, px), math.Max(maxY, py)
		}
	}
	limit := func(n, max float64) int {
		return int(math.Max(0, math.Min(max, n)))
	}
	width, height := float64(r.frame.Bounds().Dx()), float64(r.frame.Bounds().Dy())
	return image.Rect(
		limit(math.Floor(minX), width), limit(math.Floor(minY), height),
		limit(math.Ceil(maxX), width), limit(math.Ceil(maxY), height),
	)
}

func blendRGBA(dst *image.RGBA, x, y int, c color.NRGBA) {
	if c.A == 0 {
		return
	}
	at := dst.PixOffset(x, y)
	a := uint32(c.A)
	for i, v := range []uint8{c.R, c.G, c.B} {
		dst.Pix[at+i] = uint8((uint32(v)*a + uint32(dst.Pix[at+i])*(255-a) + 127) / 255)
	}
	dst.Pix[at+3] = 255
}

func blendNRGBA(dst *image.NRGBA, x, y int, c color.NRGBA) {
	if c.A == 0 {
		return
	}
	old := dst.NRGBAAt(x, y)
	if c.A == 255 || old.A == 0 {
		dst.SetNRGBA(x, y, c)
		return
	}
	if old.A == 255 {
		alpha := uint32(c.A)
		keep := 255 - alpha
		dst.SetNRGBA(x, y, color.NRGBA{
			R: uint8((uint32(c.R)*alpha + uint32(old.R)*keep + 127) / 255),
			G: uint8((uint32(c.G)*alpha + uint32(old.G)*keep + 127) / 255),
			B: uint8((uint32(c.B)*alpha + uint32(old.B)*keep + 127) / 255),
			A: 255,
		})
		return
	}
	a, b := float64(c.A)/255, float64(old.A)/255
	out := a + b*(1-a)
	if out == 0 {
		return
	}
	mix := func(n, m uint8) uint8 {
		return uint8(math.Round((float64(n)*a + float64(m)*b*(1-a)) / out))
	}
	dst.SetNRGBA(x, y, color.NRGBA{
		R: mix(c.R, old.R),
		G: mix(c.G, old.G),
		B: mix(c.B, old.B),
		A: uint8(math.Round(out * 255)),
	})
}

func (r *Renderer) penAt(x, y float64) color.NRGBA {
	if r.pen == nil || !finite(x) || !finite(y) {
		return color.NRGBA{}
	}
	return r.pen.NRGBAAt(int(math.Floor(x+r.view.Width/2)), int(math.Floor(r.view.Height/2-y)))
}

func (r *Renderer) drawPen() {
	if r.pen == nil {
		return
	}
	camera := r.screenToWorld()
	for y := 0; y < r.frame.Bounds().Dy(); y++ {
		cameraY := camera.rowY(float64(y) + .5)
		for x := 0; x < r.frame.Bounds().Dx(); x++ {
			wx, wy := camera.point(float64(x)+.5, cameraY)
			if r.penComposite != nil {
				c := r.penComposite.RGBAAt(int(math.Floor(wx+r.view.Width/2)), int(math.Floor(r.view.Height/2-wy)))
				blendPremultiplied(r.frame, x, y, c)
			} else {
				blendRGBA(r.frame, x, y, r.penAt(wx, wy))
			}
		}
	}
}

func (r *Renderer) Pen(cmd vm.PenCommand) {
	if r.penSurfaceCommand(cmd) {
		return
	}
	if cmd.Kind == "clear" {
		if r.penClearRevision != r.penRevision {
			clear(r.penDepth)
			if r.pen != nil {
				clear(r.pen.Pix)
			}
		}
		r.penComposite = nil
		r.penRevision++
		r.penClearRevision = r.penRevision
		return
	}
	if cmd.Kind == "stamp" && cmd.Target.State.Effects.Ghost >= 100 {
		return
	}
	for _, n := range cmd.Points {
		if !finite(n) {
			return
		}
	}
	if cmd.Kind == "line" {
		for _, n := range cmd.Line {
			if !finite(n) {
				return
			}
		}
		if !finite(cmd.Size) {
			return
		}
		// TurboWarp's pen renderer falls back to width 1 for a zero diameter.
		if cmd.Size == 0 {
			cmd.Size = 1
		}
	}
	if r.pen == nil {
		r.pen = image.NewNRGBA(image.Rect(0, 0, int(math.Ceil(r.view.Width)), int(math.Ceil(r.view.Height))))
	}
	var sample func(float64, float64) color.NRGBA
	var stamp transform
	box := vm.Bounds{
		Left:   -r.view.Width / 2,
		Right:  r.view.Width / 2,
		Bottom: -r.view.Height / 2,
		Top:    r.view.Height / 2,
	}
	switch cmd.Kind {
	case "stamp":
		// Each stamp starts a fresh stencil pass containing only its drawable.
		if cmd.Target.State.MaskMode && !(cmd.Target.State.Name == r.view.MaskTarget && cmd.Target.State.CloneID == 0) {
			return
		}
		tr, ok := r.transform(cmd.Target)
		if !ok {
			return
		}
		box = tr.worldBounds(image.Rectangle{})
		stamp = tr
		sample = func(x, y float64) color.NRGBA {
			return tr.sample(x, y, cmd.Target.State.Effects, false)
		}
	case "line":
		x0, y0, x1, y1 := cmd.Line[0], cmd.Line[1], cmd.Line[2], cmd.Line[3]
		if cmd.Size == 1 || cmd.Size == 3 {
			x0 += .5
			y0 += .5
			x1 += .5
			y1 += .5
		}
		radius := cmd.Size / 2
		box = vm.Bounds{
			Left:   math.Min(x0, x1) - radius - 1,
			Right:  math.Max(x0, x1) + radius + 1,
			Bottom: math.Min(y0, y1) - radius - 1,
			Top:    math.Max(y0, y1) + radius + 1,
		}
		dx, dy := x1-x0, y1-y0
		length := dx*dx + dy*dy
		sample = func(x, y float64) color.NRGBA {
			fraction := 0.0
			if length > 0 {
				fraction = math.Max(0, math.Min(1, ((x-x0)*dx+(y-y0)*dy)/length))
			}
			distance := math.Hypot(x-x0-fraction*dx, y-y0-fraction*dy)
			coverage := math.Max(0, math.Min(1, radius+.5-distance))
			if r.view.PenNoAntialias || r.view.NoAntialias {
				coverage = 0
				if distance < radius {
					coverage = 1
				}
			}
			c := cmd.Color
			c.A = uint8(math.Round(float64(c.A) * coverage))
			return c
		}
	case "triangle":
		if len(cmd.Points) != 6 {
			return
		}
		p := cmd.Points
		cross := func(x0, y0, x1, y1, x, y float64) float64 {
			return (x1-x0)*(y-y0) - (y1-y0)*(x-x0)
		}
		area := cross(p[0], p[1], p[2], p[3], p[4], p[5])
		if area == 0 {
			return
		}
		box = vm.Bounds{
			Left:   math.Min(p[0], math.Min(p[2], p[4])),
			Right:  math.Max(p[0], math.Max(p[2], p[4])),
			Bottom: math.Min(p[1], math.Min(p[3], p[5])),
			Top:    math.Max(p[1], math.Max(p[3], p[5])),
		}
		sample = func(x, y float64) color.NRGBA {
			a, b, c := cross(p[0], p[1], p[2], p[3], x, y), cross(p[2], p[3], p[4], p[5], x, y), cross(p[4], p[5], p[0], p[1], x, y)
			if (a >= 0 && b >= 0 && c >= 0) || (a <= 0 && b <= 0 && c <= 0) {
				return cmd.Color
			}
			return color.NRGBA{}
		}
	default:
		return
	}
	if !finite(box.Left) || !finite(box.Right) || !finite(box.Bottom) || !finite(box.Top) {
		return
	}
	minX := int(math.Max(0, math.Min(float64(r.pen.Bounds().Dx()), math.Floor(box.Left+r.view.Width/2))))
	maxX := int(math.Max(0, math.Min(float64(r.pen.Bounds().Dx()), math.Ceil(box.Right+r.view.Width/2))))
	minY := int(math.Max(0, math.Min(float64(r.pen.Bounds().Dy()), math.Floor(r.view.Height/2-box.Top))))
	maxY := int(math.Max(0, math.Min(float64(r.pen.Bounds().Dy()), math.Ceil(r.view.Height/2-box.Bottom))))
	clip := PenClipRectangle(cmd.Target.State.Clipbox, r.view, r.pen.Bounds().Dx(), r.pen.Bounds().Dy())
	bounds := image.Rect(minX, minY, maxX, maxY).Intersect(clip)
	minX, minY, maxX, maxY = bounds.Min.X, bounds.Min.Y, bounds.Max.X, bounds.Max.Y
	r.penRevision++
	if cmd.Target.State.BlendMode == "additive" && r.penComposite == nil {
		r.penComposite = image.NewRGBA(r.pen.Bounds())
		draw.Draw(r.penComposite, r.penComposite.Bounds(), r.pen, image.Point{}, draw.Src)
	}
	if r.penComposite == nil && cmd.Kind == "stamp" && (cmd.Target.State.BlendMode == "" || cmd.Target.State.BlendMode == "default") && r.stampAffine(stamp, cmd.Target.State.Effects, image.Rect(minX, minY, maxX, maxY)) {
		return
	}
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			c := sample(float64(x)+.5-r.view.Width/2, r.view.Height/2-float64(y)-.5)
			if r.penComposite != nil {
				blendModeRGBA(r.penComposite, x, y, c, cmd.Target.State.BlendMode)
				copyPenPixel(r.pen, r.penComposite, x, y)
			} else {
				blendModeNRGBA(r.pen, x, y, c, cmd.Target.State.BlendMode)
			}
		}
	}
}

/*
Keep sampleFiltered's arithmetic order while reusing row and column terms.
Texture-warping effects use the general sampler below.
*/
func (r *Renderer) stampAffine(tr transform, effects vm.GraphicEffects, bounds image.Rectangle) bool {
	if tr.sx == 0 || tr.sy == 0 ||
		effects.Mosaic != 0 || effects.Pixelate != 0 || effects.Whirl != 0 || effects.Fisheye != 0 ||
		tr.image.Rect.Min != (image.Point{}) {
		return false
	}
	var columns [4096]int
	var columnCos, columnSin [4096]float64
	axisAligned := tr.sin == 0 && tr.cos == 1
	width, height := float64(tr.image.Rect.Dx()), float64(tr.image.Rect.Dy())
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		dx := float64(x) + .5 - r.view.Width/2 - tr.x
		if !axisAligned {
			columnCos[x], columnSin[x] = tr.cos*dx, tr.sin*dx
			continue
		}
		u := (dx/tr.sx + tr.c.CenterX) / tr.c.Width
		columns[x] = -1
		if u >= 0 && u < 1 {
			columns[x] = int(u*width) * 4
		}
	}
	var alpha [256]uint8
	ghostOnly := effects.Color == 0 && effects.Brightness == 0
	if ghostOnly && effects.Ghost != 0 {
		for i := range alpha {
			alpha[i] = effectColor(color.NRGBA{A: uint8(i)}, effects).A
		}
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		dy := r.view.Height/2 - float64(y) - .5 - tr.y
		row := tr.image.Pix
		rowSin, rowCos := tr.sin*dy, tr.cos*dy
		if axisAligned {
			v := (tr.c.CenterY - dy/tr.sy) / tr.c.Height
			if !(v >= 0 && v < 1) {
				continue
			}
			row = row[int(v*height)*tr.image.Stride:]
		}
		dst := r.pen.Pix[y*r.pen.Stride:]
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			at := columns[x]
			if !axisAligned {
				u := ((columnCos[x]+rowSin)/tr.sx + tr.c.CenterX) / tr.c.Width
				v := (tr.c.CenterY - (-columnSin[x]+rowCos)/tr.sy) / tr.c.Height
				if !(u >= 0 && u < 1 && v >= 0 && v < 1) {
					continue
				}
				at = int(v*height)*tr.image.Stride + int(u*width)*4
			}
			if at < 0 {
				continue
			}
			pixel := row[at : at+4]
			if pixel[3] == 0 {
				continue
			}
			c := color.NRGBA{pixel[0], pixel[1], pixel[2], pixel[3]}
			if ghostOnly {
				if effects.Ghost != 0 {
					c.A = alpha[c.A]
				}
			} else {
				c = effectColor(c, effects)
			}
			if c.A == 0 {
				continue
			}
			out := dst[x*4 : x*4+4]
			if c.A == 255 || out[3] == 0 {
				out[0], out[1], out[2], out[3] = c.R, c.G, c.B, c.A
			} else {
				blendNRGBA(r.pen, x, y, c)
			}
		}
	}
	return true
}

func sameTarget(a, b vm.DisplayTarget) bool {
	return a.Source == b.Source && a.State.CloneID == b.State.CloneID
}

func (r *Renderer) Touching(scene []vm.DisplayTarget, t vm.DisplayTarget, name string, mx, my float64) bool {
	if t.State.IsStage {
		return false
	}
	if name == "_edge_" {
		b, ok := r.Bounds(t, true)
		return ok && (b.Left < -r.view.Width/2 || b.Right > r.view.Width/2 || b.Bottom < -r.view.Height/2 || b.Top > r.view.Height/2)
	}
	tr, ok := r.transformGeometry(t)
	if !ok {
		return false
	}
	if name == "_mouse_" {
		tr.image = r.Assets.CostumeImage(tr.c)
		if tr.image == nil {
			return false
		}
		return tr.sample(mx, my, t.State.Effects, true).A > 0
	}
	if !t.State.Visible {
		return false
	}
	mine := tr.worldBounds(image.Rectangle{})
	if !finite(mine.Left) || !finite(mine.Right) || !finite(mine.Bottom) || !finite(mine.Top) {
		return false
	}
	for _, other := range scene {
		if sameTarget(t, other) || other.State.Name != name || !other.State.Visible || other.State.IsStage {
			continue
		}
		ot, ok := r.transformGeometry(other)
		if !ok {
			continue
		}
		box := ot.worldBounds(image.Rectangle{})
		box.Left = math.Max(box.Left, mine.Left)
		box.Right = math.Min(box.Right, mine.Right)
		box.Bottom = math.Max(box.Bottom, mine.Bottom)
		box.Top = math.Min(box.Top, mine.Top)
		if !finite(box.Left) || !finite(box.Right) || !finite(box.Bottom) || !finite(box.Top) {
			continue
		}
		if !r.view.OffscreenTouching {
			box.Left = math.Max(-r.view.Width/2, box.Left)
			box.Right = math.Min(r.view.Width/2, box.Right)
			box.Bottom = math.Max(-r.view.Height/2, box.Bottom)
			box.Top = math.Min(r.view.Height/2, box.Top)
		}
		if box.Left > box.Right || box.Bottom > box.Top {
			continue
		}
		// Decode pixels only for intersecting candidates.
		if tr.image == nil {
			tr.image = r.Assets.CostumeImage(tr.c)
			if tr.image == nil {
				return false
			}
		}
		ot.image = r.Assets.CostumeImage(ot.c)
		if ot.image == nil {
			continue
		}
		if r.scan(box, func(x, y float64) bool {
			return tr.sample(x, y, t.State.Effects, true).A > 0 && ot.sample(x, y, other.State.Effects, true).A > 0
		}) {
			return true
		}
	}
	return false
}

func (r *Renderer) scan(box vm.Bounds, match func(float64, float64) bool) bool {
	if !finite(box.Left) || !finite(box.Right) || !finite(box.Top) || !finite(box.Bottom) {
		return false
	}
	if !r.view.OffscreenTouching {
		box.Left = math.Max(-r.view.Width/2, box.Left)
		box.Right = math.Min(r.view.Width/2, box.Right)
		box.Bottom = math.Max(-r.view.Height/2, box.Bottom)
		box.Top = math.Min(r.view.Height/2, box.Top)
	}
	if box.Left > box.Right || box.Bottom > box.Top {
		return false
	}
	left, right := math.Floor(box.Left), math.Ceil(box.Right)
	bottom, top := math.Floor(box.Bottom), math.Ceil(box.Top)
	for y := bottom; y <= top; y++ {
		for x := left; x <= right; x++ {
			if match(x, y) {
				return true
			}
			// Beyond float64's integer precision, adding one may not advance.
			if x+1 == x {
				break
			}
		}
		if y+1 == y {
			break
		}
	}
	return false
}

type colorCandidate struct {
	tr    transform
	state vm.TargetState
}

func (r *Renderer) TouchingColor(scene []vm.DisplayTarget, t vm.DisplayTarget, wanted color.NRGBA, mask *color.NRGBA) bool {
	if t.State.IsStage {
		return false
	}
	tr, ok := r.transform(t)
	if !ok {
		return false
	}
	box := tr.worldBounds(image.Rectangle{})
	query := box
	if !r.view.OffscreenTouching {
		query.Left = math.Max(query.Left, -r.view.Width/2)
		query.Right = math.Min(query.Right, r.view.Width/2)
		query.Bottom = math.Max(query.Bottom, -r.view.Height/2)
		query.Top = math.Min(query.Top, r.view.Height/2)
	}
	if !finite(query.Left) || !finite(query.Right) || !finite(query.Bottom) || !finite(query.Top) ||
		query.Left > query.Right || query.Bottom > query.Top {
		return false
	}
	query.Left, query.Right = math.Floor(query.Left), math.Ceil(query.Right)
	query.Bottom, query.Top = math.Floor(query.Bottom), math.Ceil(query.Top)
	others := r.colorCandidates[:0]
	r.colorCandidates = nil
	defer func() {
		clear(others)
		if cap(others) <= 4096 {
			r.colorCandidates = others[:0]
		}
	}()
	hasOthers := false
	for _, o := range scene {
		if sameTarget(t, o) || !o.State.Visible {
			continue
		}
		ot, ok := r.transformGeometry(o)
		if !ok {
			continue
		}
		bounds := ot.worldBounds(image.Rectangle{})
		if math.Ceil(bounds.Right) < query.Left || math.Floor(bounds.Left) > query.Right ||
			math.Ceil(bounds.Top) < query.Bottom || math.Floor(bounds.Bottom) > query.Top {
			// Keep the white background even when every drawable is distant.
			hasOthers = true
			continue
		}
		ot.image = r.Assets.CostumeImage(ot.c)
		if ot.image != nil {
			hasOthers = true
			others = append(others, colorCandidate{ot, o.State})
		}
	}
	if !hasOthers && r.pen == nil {
		return false
	}
	e := t.State.Effects
	e.Ghost = 0
	return r.scan(box, func(x, y float64) bool {
		c := tr.sample(x, y, e, false)
		if c.A == 0 {
			return false
		}
		if mask != nil && (uint8(uint16(c.R)*uint16(c.A)/255)&252 != mask.R&252 ||
			uint8(uint16(c.G)*uint16(c.A)/255)&252 != mask.G&252 ||
			uint8(uint16(c.B)*uint16(c.A)/255)&252 != mask.B&252) {
			return false
		}
		rr, gg, bb := 255.0, 255.0, 255.0
		blend := func(c color.NRGBA) {
			a := float64(c.A) / 255
			rr = math.Floor(float64(c.R)*a + rr*(1-a))
			gg = math.Floor(float64(c.G)*a + gg*(1-a))
			bb = math.Floor(float64(c.B)*a + bb*(1-a))
		}
		penDrawn := false
		for _, o := range others {
			if !o.state.IsStage && !penDrawn {
				blend(r.penAt(x, y))
				penDrawn = true
			}
			blend(o.tr.sample(x, y, o.state.Effects, false))
		}
		if !penDrawn {
			blend(r.penAt(x, y))
		}
		return uint8(rr)&248 == wanted.R&248 && uint8(gg)&248 == wanted.G&248 && uint8(bb)&240 == wanted.B&240
	})
}

func (r *Renderer) Size() (int, int) {
	return r.frame.Bounds().Dx(), r.frame.Bounds().Dy()
}
