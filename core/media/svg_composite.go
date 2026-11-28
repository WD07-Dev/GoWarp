package media

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

type svgNode struct {
	start    xml.StartElement
	children []*svgNode
	text     string
}

type svgBox struct {
	X, Y, W, H float64
}

func (n *svgNode) attr(key string) string {
	for _, a := range n.start.Attr {
		if a.Name.Local == key {
			return a.Value
		}
	}
	return ""
}

func (n *svgNode) property(key string) string {
	v := n.attr(key)
	for _, entry := range strings.Split(n.attr("style"), ";") {
		name, value, ok := strings.Cut(entry, ":")
		if ok && strings.TrimSpace(name) == key {
			v = strings.TrimSpace(value)
		}
	}
	return v
}

func (n *svgNode) without(keys ...string) *svgNode {
	c := *n
	c.start.Attr = nil
	for _, a := range n.start.Attr {
		if slices.Contains(keys, a.Name.Local) {
			continue
		}
		if a.Name.Local == "style" {
			var parts []string
			for _, p := range strings.Split(a.Value, ";") {
				name, _, ok := strings.Cut(p, ":")
				if ok && !slices.Contains(keys, strings.TrimSpace(name)) {
					parts = append(parts, p)
				}
			}
			a.Value = strings.Join(parts, ";")
		}
		c.start.Attr = append(c.start.Attr, a)
	}
	return &c
}

func parseSVGTree(data []byte) (*svgNode, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	var root *svgNode
	var stack []*svgNode
	count := 0
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := t.(type) {
		case xml.StartElement:
			count++
			if count > 100000 || len(stack) >= 128 {
				return nil, fmt.Errorf("excessive SVG complexity")
			}
			n := &svgNode{start: t}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("multiple SVG roots")
				}
				root = n
			} else {
				p := stack[len(stack)-1]
				p.children = append(p.children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.children = append(p.children, &svgNode{text: string(t)})
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("empty SVG")
	}
	return root, nil
}

func encodeSVGNode(e *xml.Encoder, n *svgNode) error {
	if n.start.Name.Local == "" {
		return e.EncodeToken(xml.CharData(n.text))
	}
	if err := e.EncodeToken(n.start); err != nil {
		return err
	}
	for _, c := range n.children {
		if err := encodeSVGNode(e, c); err != nil {
			return err
		}
	}
	return e.EncodeToken(n.start.End())
}

func svgXML(root *svgNode) ([]byte, error) {
	var b bytes.Buffer
	e := xml.NewEncoder(&b)
	if err := encodeSVGNode(e, root); err != nil {
		return nil, err
	}
	if err := e.Flush(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func svgHas(n *svgNode, name string) bool {
	if n.start.Name.Local == name {
		return true
	}
	for _, c := range n.children {
		if svgHas(c, name) {
			return true
		}
	}
	return false
}

func vectorSVG(data []byte, dst *image.RGBA) (svgBox, error) {
	return vectorSVGTarget(data, dst, false)
}

func vectorSVGTarget(data []byte, dst *image.RGBA, scaled bool) (svgBox, error) {
	return vectorSVGWithAntialias(data, dst, scaled, false)
}

func vectorSVGWithAntialias(data []byte, dst *image.RGBA, scaled, noAntialias bool) (svgBox, error) {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.StrictErrorMode)
	if err != nil {
		return svgBox{}, err
	}
	b := icon.ViewBox
	box := svgBox{b.X, b.Y, b.W, b.H}
	if !validSize(b.W, b.H) {
		return box, fmt.Errorf("invalid or excessive SVG dimensions")
	}
	if dst != nil {
		w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
		var scanner rasterx.Scanner = rasterx.NewScannerGV(w, h, dst, dst.Bounds())
		if noAntialias {
			scanner = &hardSVGScanner{ScannerGV: scanner.(*rasterx.ScannerGV)}
		}
		raster := rasterx.NewDasher(w, h, scanner)
		icon.SetTarget(0, 0, float64(w), float64(h))
		if scaled {
			/*
				oksvg.SetTarget leaves the viewBox offset unscaled. Correct the
				display transform without changing existing base/collision pixels.
			*/
			sx, sy := float64(w)/b.W, float64(h)/b.H
			icon.Transform = rasterx.Identity.Scale(sx, sy).Translate(-b.X, -b.Y)
			/*
				DrawTransformed scales geometry but not stroke lengths. Keep the
				dimensionless miter limit and existing element transforms intact.
			*/
			strokeScale := math.Sqrt(sx * sy)
			for i := range icon.SVGPaths {
				path := &icon.SVGPaths[i]
				path.LineWidth *= strokeScale
				path.DashOffset *= strokeScale
				// Inherited styles can share their dash slice across paths.
				path.Dash = append([]float64(nil), path.Dash...)
				for j := range path.Dash {
					path.Dash[j] *= strokeScale
				}
			}
		}
		icon.Draw(raster, 1)
	}
	return box, nil
}

func rasterSVGWithFonts(data []byte, scale float64, fonts svgFontResolver) (*image.NRGBA, svgBox, error) {
	return rasterSVGWithAntialias(data, scale, fonts, false)
}

func rasterSVGWithAntialias(data []byte, scale float64, fonts svgFontResolver, noAntialias bool) (*image.NRGBA, svgBox, error) {
	normalized, err := normalizeSVG(data)
	if err != nil {
		return nil, svgBox{}, err
	}
	root, err := parseSVGTree(normalized)
	if err != nil {
		return nil, svgBox{}, err
	}
	/*
		Scratch exports a zero-size, empty SVG for controller-only sprites.
		Store one transparent pixel while preserving zero logical dimensions.
	*/
	if box, empty := svgEmptyViewport(root); empty {
		if !svgEmptyContent(root) {
			return nil, box, fmt.Errorf("nonempty zero-size SVG is unsupported")
		}
		return image.NewNRGBA(image.Rect(0, 0, 1, 1)), box, nil
	}

	if svgHas(root, "text") || svgHas(root, "tspan") || svgHas(root, "textPath") {
		textBytes := 0
		if err = svgTextPaths(root, svgTextStyle{size: 16, fonts: fonts}, &textBytes); err != nil {
			return nil, svgBox{}, err
		}
	}
	if err := svgScaleStrokes(root, rasterx.Identity, svgStrokeStyle{width: "1"}); err != nil {
		return nil, svgBox{}, err
	}
	// Parse the original viewport separately from the mixed content.
	shell := *root
	shell.children = nil
	header, err := svgXML(&shell)
	if err != nil {
		return nil, svgBox{}, err
	}
	box, err := vectorSVG(header, nil)
	if err != nil {
		return nil, box, err
	}
	w, h := math.Ceil(box.W*scale), math.Ceil(box.H*scale)
	if !validSize(w, h) {
		return nil, box, fmt.Errorf("invalid or excessive scaled SVG dimensions")
	}
	if scale > 1 {
		matrix := rasterx.Identity.Scale(w/box.W, h/box.H).Translate(-box.X, -box.Y)
		if err := svgTransformGradients(root, matrix); err != nil {
			return nil, box, err
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	if !svgHas(root, "image") {
		data, err = svgXML(root)
		if err == nil {
			_, err = vectorSVGWithAntialias(data, dst, scale > 1, noAntialias)
		}
	} else {
		r := svgCompositor{root: root, bytes: len(dst.Pix), scaled: scale > 1, noAntialias: noAntialias}
		for _, n := range root.children {
			if n.start.Name.Local == "defs" {
				if svgHas(n, "image") {
					return nil, box, fmt.Errorf("SVG images in definitions are unsupported")
				}
				r.defs = append(r.defs, n)
			}
		}
		matrix := rasterx.Identity.Translate(-box.X, -box.Y).Scale(float64(dst.Bounds().Dx())/box.W, float64(dst.Bounds().Dy())/box.H)
		if r.scaled {
			matrix = rasterx.Identity.Scale(w/box.W, h/box.H).Translate(-box.X, -box.Y)
		}
		err = r.paint(dst, root, nil, matrix)
	}
	if err != nil {
		return nil, box, err
	}
	out := image.NewNRGBA(dst.Bounds())
	draw.Draw(out, out.Bounds(), dst, image.Point{}, draw.Src)
	return out, box, nil
}

/*
Correct user-space gradient colors when scaling geometry. Object-bounds gradients already use
transformed path extents.
*/
func svgTransformGradients(n *svgNode, matrix rasterx.Matrix2D) error {
	if (n.start.Name.Local == "linearGradient" || n.start.Name.Local == "radialGradient") && strings.TrimSpace(n.attr("gradientUnits")) == "userSpaceOnUse" {
		transform, err := svgTransform(n.attr("gradientTransform"))
		if err != nil {
			return err
		}
		transform = matrix.Mult(transform)
		if err := svgCheckMatrix(transform); err != nil {
			return err
		}
		clean := n.without("gradientTransform")
		clean.start.Attr = append(clean.start.Attr, xml.Attr{
			Name:  xml.Name{Local: "gradientTransform"},
			Value: fmt.Sprintf("matrix(%g %g %g %g %g %g)", transform.A, transform.B, transform.C, transform.D, transform.E, transform.F),
		})
		n.start = clean.start
	}
	for _, child := range n.children {
		if err := svgTransformGradients(child, matrix); err != nil {
			return err
		}
	}
	return nil
}

type svgCompositor struct {
	root                      *svgNode
	defs                      []*svgNode
	bytes                     int
	vectorBytes, vectorPixels int
	scaled                    bool
	noAntialias               bool
}

func svgOpacity(n *svgNode) (float64, error) {
	s := n.property("opacity")
	if s == "" {
		return 1, nil
	}
	v, err := svgNumber(s)
	if err != nil {
		return 0, err
	}
	return math.Max(0, math.Min(1, v)), nil
}

func (r *svgCompositor) paint(dst *image.RGBA, n *svgNode, parents []*svgNode, matrix rasterx.Matrix2D) error {
	name := n.start.Name.Local
	if name == "" || name == "defs" || name == "title" || name == "desc" || name == "metadata" {
		return nil
	}
	if n.property("display") == "none" {
		return nil
	}
	if !svgHas(n, "image") {
		return r.vector(dst, n, parents)
	}
	for _, key := range []string{"mask", "filter"} {
		if v := n.property(key); v != "" && v != "none" {
			return fmt.Errorf("SVG %s on embedded images is unsupported", key)
		}
	}
	if clip := n.property("clip-path"); clip != "" && clip != "none" {
		return r.clipped(dst, n, parents, matrix)
	}
	tr, err := svgTransform(n.attr("transform"))
	if err != nil {
		return err
	}
	matrix = matrix.Mult(tr)
	if err := svgCheckMatrix(matrix); err != nil {
		return err
	}
	if name == "image" {
		return r.image(dst, n, matrix, parents)
	}
	if name != "g" && name != "svg" {
		return fmt.Errorf("SVG embedded image container %s is unsupported", name)
	}
	opacity, err := svgOpacity(n)
	if err != nil {
		return err
	}
	target := dst
	if opacity != 1 {
		if r.bytes+len(dst.Pix) > maxDecodedBytes {
			return fmt.Errorf("excessive SVG compositing memory")
		}
		target = image.NewRGBA(dst.Bounds())
		r.bytes += len(target.Pix)
		defer func() {
			r.bytes -= len(target.Pix)
		}()
	}
	var clean *svgNode
	if n.attr("transform") == "" {
		clean = n.without("opacity")
	} else {
		clean = n.without("opacity", "transform")
		clean.start.Attr = append(clean.start.Attr, xml.Attr{
			Name:  xml.Name{Local: "transform"},
			Value: fmt.Sprintf("matrix(%g %g %g %g %g %g)", tr.A, tr.B, tr.C, tr.D, tr.E, tr.F),
		})
	}
	parents = append(append([]*svgNode{}, parents...), clean)
	var pending []*svgNode
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		batch := &svgNode{start: xml.StartElement{Name: xml.Name{Local: "g"}}, children: pending}
		pending = nil
		return r.vector(target, batch, parents)
	}
	for _, c := range n.children {
		if c.start.Name.Local == "" || c.start.Name.Local == "defs" || c.start.Name.Local == "title" || c.start.Name.Local == "desc" || c.start.Name.Local == "metadata" {
			continue
		}
		if !svgHas(c, "image") {
			pending = append(pending, c)
			continue
		}
		if err := flush(); err != nil {
			return err
		}
		if err := r.paint(target, c, parents, matrix); err != nil {
			return err
		}
	}
	if err := flush(); err != nil {
		return err
	}
	if target != dst {
		draw.DrawMask(dst, dst.Bounds(), target, image.Point{}, image.NewUniform(color.Alpha{uint8(math.Round(opacity * 255))}), image.Point{}, draw.Over)
	}
	return nil
}

func (r *svgCompositor) vector(dst *image.RGBA, n *svgNode, parents []*svgNode) error {
	child := n
	for i := len(parents) - 1; i >= 0; i-- {
		if parents[i].start.Name.Local == "svg" {
			continue
		}
		p := *parents[i]
		p.children = []*svgNode{child}
		child = &p
	}
	root := *r.root.without("opacity")
	if len(parents) > 0 && parents[0].start.Name.Local == "svg" {
		root = *parents[0]
	}
	root.children = append(append([]*svgNode{}, r.defs...), child)
	data, err := svgXML(&root)
	if err != nil {
		return err
	}
	r.vectorBytes += len(data)
	r.vectorPixels += dst.Bounds().Dx() * dst.Bounds().Dy()
	if r.vectorBytes > maxDecodedBytes || r.vectorPixels > maxPixels*16 {
		return fmt.Errorf("excessive SVG compositing work")
	}
	_, err = vectorSVGWithAntialias(data, dst, r.scaled, r.noAntialias)
	return err
}

func svgLength(n *svgNode, key string, fallback float64) (float64, error) {
	if s := n.attr(key); s != "" {
		return svgNumber(s)
	}
	return fallback, nil
}

func (r *svgCompositor) image(dst *image.RGBA, n *svgNode, m rasterx.Matrix2D, parents []*svgNode) error {
	visibility := n.property("visibility")
	if visibility == "" {
		for i := len(parents) - 1; i >= 0; i-- {
			if visibility = parents[i].property("visibility"); visibility != "" {
				break
			}
		}
	}
	if visibility == "hidden" || visibility == "collapse" {
		return nil
	}
	href := n.attr("href")
	comma := strings.IndexByte(href, ',')
	if comma < 0 {
		return fmt.Errorf("SVG image must contain embedded PNG or JPEG data")
	}
	media := strings.ToLower(href[:comma])
	if media != "data:image/png;base64" && media != "data:image/jpeg;base64" && media != "data:image/jpg;base64" {
		return fmt.Errorf("unsupported SVG image URI: only embedded PNG/JPEG are supported")
	}
	raw := href[comma+1:]
	if len(raw) > maxDecodedBytes {
		return fmt.Errorf("excessive SVG embedded image data")
	}
	raw = strings.Map(func(ch rune) rune {
		switch ch {
		case ' ', '\n', '\r', '\t':
			return -1
		}
		return ch
	}, raw)
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return fmt.Errorf("invalid SVG embedded image data: %w", err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return err
	}
	if format != "png" && format != "jpeg" {
		return fmt.Errorf("unsupported SVG embedded image format %q", format)
	}
	if !validSize(float64(config.Width), float64(config.Height)) || r.bytes+config.Width*config.Height*4 > maxDecodedBytes {
		return fmt.Errorf("invalid or excessive SVG embedded image dimensions")
	}
	r.bytes += config.Width * config.Height * 4 // Also bounds cumulative decoding work for repeated images.
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return err
	}
	x, err := svgLength(n, "x", 0)
	if err != nil {
		return err
	}
	y, err := svgLength(n, "y", 0)
	if err != nil {
		return err
	}
	w, err := svgLength(n, "width", float64(config.Width))
	if err != nil {
		return err
	}
	h, err := svgLength(n, "height", float64(config.Height))
	if err != nil {
		return err
	}
	if w < 0 || h < 0 {
		return fmt.Errorf("invalid SVG image dimensions")
	}
	if w == 0 || h == 0 {
		return nil
	}
	sx, sy := w/float64(config.Width), h/float64(config.Height)
	aspect := strings.Fields(n.attr("preserveAspectRatio"))
	if len(aspect) == 0 {
		aspect = []string{"xMidYMid", "meet"}
	}
	if aspect[0] != "none" {
		if len(aspect) > 2 || (len(aspect) == 2 && aspect[1] != "meet") {
			return fmt.Errorf("unsupported SVG image preserveAspectRatio")
		}
		scale := math.Min(sx, sy)
		dx, dy := w-float64(config.Width)*scale, h-float64(config.Height)*scale
		switch aspect[0] {
		case "xMinYMin", "xMinYMid", "xMinYMax", "xMidYMin", "xMidYMid", "xMidYMax", "xMaxYMin", "xMaxYMid", "xMaxYMax":
		default:
			return fmt.Errorf("unsupported SVG image alignment %q", aspect[0])
		}
		if strings.HasPrefix(aspect[0], "xMid") {
			x += dx / 2
		} else if strings.HasPrefix(aspect[0], "xMax") {
			x += dx
		}
		if strings.HasSuffix(aspect[0], "YMid") {
			y += dy / 2
		} else if strings.HasSuffix(aspect[0], "YMax") {
			y += dy
		}
		sx, sy = scale, scale
	} else if len(aspect) > 2 {
		return fmt.Errorf("invalid SVG image preserveAspectRatio")
	}
	m = m.Mult(rasterx.Identity.Translate(x, y).Scale(sx, sy))
	if err := svgCheckMatrix(m); err != nil {
		return err
	}
	if m.A*m.D-m.B*m.C == 0 {
		return nil
	}
	opacity, err := svgOpacity(n)
	if err != nil {
		return err
	}
	var opts *xdraw.Options
	alpha := uint8(math.Round(opacity * 255))
	if alpha != 255 {
		opts = &xdraw.Options{SrcMask: image.NewUniform(color.Alpha{A: alpha})}
	}
	transform := f64.Aff3{m.A, m.C, m.E, m.B, m.D, m.F}
	right, bottom := m.E+m.A*float64(config.Width), m.F+m.D*float64(config.Height)
	scaledWidth, scaledHeight := right-m.E, bottom-m.F
	if m.B == 0 && m.C == 0 && m.A >= 1 && m.D >= 1 &&
		m.E == math.Trunc(m.E) && m.F == math.Trunc(m.F) &&
		right == math.Trunc(right) && bottom == math.Trunc(bottom) &&
		validSize(scaledWidth, scaledHeight) {
		if uniform := svgUniformImage(src); uniform != nil {
			var mask image.Image
			if opts != nil {
				mask = opts.SrcMask
			}
			draw.DrawMask(dst, image.Rect(int(m.E), int(m.F), int(right), int(bottom)), uniform, image.Point{}, mask, image.Point{}, draw.Over)
			return nil
		}
	}
	if r.noAntialias {
		xdraw.NearestNeighbor.Transform(dst, transform, src, src.Bounds(), draw.Over, opts)
	} else {
		xdraw.BiLinear.Transform(dst, transform, src, src.Bounds(), draw.Over, opts)
	}
	return nil
}

func svgUniformImage(src image.Image) *image.Uniform {
	var pixels []byte
	var stride int
	switch src := src.(type) {
	case *image.RGBA:
		pixels, stride = src.Pix, src.Stride
	case *image.NRGBA:
		pixels, stride = src.Pix, src.Stride
	default:
		return nil
	}
	bounds := src.Bounds()
	if bounds.Empty() {
		return nil
	}
	first := pixels[:4]
	for y := 0; y < bounds.Dy(); y++ {
		row := pixels[y*stride : y*stride+bounds.Dx()*4]
		for x := 0; x < len(row); x += 4 {
			if !bytes.Equal(row[x:x+4], first) {
				return nil
			}
		}
	}
	return image.NewUniform(src.At(bounds.Min.X, bounds.Min.Y))
}

var svgTransformNumber = regexp.MustCompile(`[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`)

func svgTransform(s string) (rasterx.Matrix2D, error) {
	m := rasterx.Identity
	for strings.TrimSpace(s) != "" {
		s = strings.TrimLeft(s, " \t\r\n,")
		i := strings.IndexByte(s, '(')
		j := strings.IndexByte(s, ')')
		if i < 1 || j < i {
			return m, fmt.Errorf("invalid SVG transform")
		}
		name := strings.TrimSpace(s[:i])
		raw := s[i+1 : j]
		matches := svgTransformNumber.FindAllStringIndex(raw, -1)
		var v []float64
		pos := 0
		for _, p := range matches {
			if strings.Trim(raw[pos:p[0]], " \t\r\n,") != "" {
				return m, fmt.Errorf("invalid SVG transform parameters")
			}
			n, err := strconv.ParseFloat(raw[p[0]:p[1]], 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return m, fmt.Errorf("invalid SVG transform number")
			}
			v = append(v, n)
			pos = p[1]
		}
		if strings.Trim(raw[pos:], " \t\r\n,") != "" {
			return m, fmt.Errorf("invalid SVG transform parameters")
		}
		t := rasterx.Identity
		switch {
		case name == "matrix" && len(v) == 6:
			t = rasterx.Matrix2D{
				A: v[0],
				B: v[1],
				C: v[2],
				D: v[3],
				E: v[4],
				F: v[5],
			}
		case name == "translate" && (len(v) == 1 || len(v) == 2):
			y := 0.0
			if len(v) == 2 {
				y = v[1]
			}
			t = t.Translate(v[0], y)
		case name == "scale" && (len(v) == 1 || len(v) == 2):
			y := v[0]
			if len(v) == 2 {
				y = v[1]
			}
			t = t.Scale(v[0], y)
		case name == "rotate" && (len(v) == 1 || len(v) == 3):
			if len(v) == 3 {
				t = t.Translate(v[1], v[2])
			}
			t = t.Rotate(v[0] * math.Pi / 180)
			if len(v) == 3 {
				t = t.Translate(-v[1], -v[2])
			}
		case name == "skewX" && len(v) == 1:
			t.C = math.Tan(v[0] * math.Pi / 180)
		case name == "skewY" && len(v) == 1:
			t.B = math.Tan(v[0] * math.Pi / 180)
		default:
			return m, fmt.Errorf("unsupported SVG transform %q", name)
		}
		m = m.Mult(t)
		for _, value := range []float64{m.A, m.B, m.C, m.D, m.E, m.F} {
			if math.IsInf(value, 0) || math.IsNaN(value) || math.Abs(value) > 1e12 {
				return m, fmt.Errorf("excessive SVG transform")
			}
		}
		s = s[j+1:]
	}
	return m, nil
}

func svgCheckMatrix(m rasterx.Matrix2D) error {
	for _, value := range []float64{m.A, m.B, m.C, m.D, m.E, m.F} {
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 1e12 {
			return fmt.Errorf("excessive SVG composed transform")
		}
	}
	det := m.A*m.D - m.B*m.C
	if det != 0 {
		for _, value := range []float64{m.A / det, m.B / det, m.C / det, m.D / det} {
			if math.IsInf(value, 0) || math.IsNaN(value) {
				return fmt.Errorf("invalid SVG inverse transform")
			}
		}
	}
	return nil
}

func svgEmptyViewport(root *svgNode) (svgBox, bool) {
	if parts := strings.Fields(strings.ReplaceAll(root.attr("viewBox"), ",", " ")); len(parts) == 4 {
		values := [4]float64{}
		for i, part := range parts {
			v, err := svgNumber(part)
			if err != nil {
				return svgBox{}, false
			}
			values[i] = v
		}
		return svgBox{values[0], values[1], values[2], values[3]}, values[2] == 0 && values[3] == 0
	}
	w, err := svgNumber(root.attr("width"))
	if err != nil {
		return svgBox{}, false
	}
	h, err := svgNumber(root.attr("height"))
	if err != nil {
		return svgBox{}, false
	}
	return svgBox{W: w, H: h}, w == 0 && h == 0
}

func svgEmptyContent(n *svgNode) bool {
	switch n.start.Name.Local {
	case "":
		return strings.TrimSpace(n.text) == ""
	case "title", "desc", "metadata":
		return true
	case "svg", "g":
		for _, c := range n.children {
			if !svgEmptyContent(c) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
