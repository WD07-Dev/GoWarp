package media

import (
	"encoding/xml"
	"fmt"
	"image"
	"image/draw"
	"strings"

	"github.com/srwiley/rasterx"
)

func (r *svgCompositor) clipped(dst *image.RGBA, n *svgNode, parents []*svgNode, matrix rasterx.Matrix2D) error {
	reference := strings.TrimSpace(n.property("clip-path"))
	if !strings.HasPrefix(reference, "url(") || !strings.HasSuffix(reference, ")") {
		return fmt.Errorf("unsupported SVG clip-path %q", reference)
	}
	id := strings.Trim(strings.TrimSpace(reference[4:len(reference)-1]), "'\"")
	if !strings.HasPrefix(id, "#") || len(id) == 1 {
		return fmt.Errorf("SVG clip-path must reference a local definition")
	}
	clip := findSVGClip(r.root, id[1:])
	if clip == nil {
		return fmt.Errorf("SVG clip-path %q was not found", id)
	}
	if units := clip.attr("clipPathUnits"); units != "" && units != "userSpaceOnUse" {
		return fmt.Errorf("unsupported SVG clipPathUnits %q", units)
	}
	geometry, err := svgClipGeometry(clip)
	if err != nil {
		return err
	}
	transform, err := svgTransform(n.attr("transform"))
	if err != nil {
		return err
	}
	clipMatrix := matrix.Mult(transform)
	if err := svgCheckMatrix(clipMatrix); err != nil {
		return err
	}
	maskBytes := 2 * len(dst.Pix)
	if r.bytes+maskBytes > maxDecodedBytes {
		return fmt.Errorf("excessive SVG clipping memory")
	}
	r.bytes += maskBytes
	defer func() {
		r.bytes -= maskBytes
	}()
	mask := image.NewRGBA(dst.Bounds())
	layer := image.NewRGBA(dst.Bounds())
	group := &svgNode{
		start: xml.StartElement{
			Name: xml.Name{Local: "g"},
			Attr: []xml.Attr{{
				Name:  xml.Name{Local: "transform"},
				Value: fmt.Sprintf("matrix(%g %g %g %g %g %g)", clipMatrix.A, clipMatrix.B, clipMatrix.C, clipMatrix.D, clipMatrix.E, clipMatrix.F),
			}},
		},
		children: []*svgNode{geometry},
	}
	root := &svgNode{
		start: xml.StartElement{
			Name: xml.Name{Local: "svg"},
			Attr: []xml.Attr{
				{Name: xml.Name{Local: "width"}, Value: fmt.Sprint(dst.Bounds().Dx())},
				{Name: xml.Name{Local: "height"}, Value: fmt.Sprint(dst.Bounds().Dy())},
			},
		},
		children: []*svgNode{group},
	}
	data, err := svgXML(root)
	if err != nil {
		return err
	}
	r.vectorBytes += len(data)
	r.vectorPixels += dst.Bounds().Dx() * dst.Bounds().Dy()
	if r.vectorBytes > maxDecodedBytes || r.vectorPixels > maxPixels*16 {
		return fmt.Errorf("excessive SVG clipping work")
	}
	if _, err := vectorSVGWithAntialias(data, mask, false, r.noAntialias); err != nil {
		return err
	}
	if err := r.paint(layer, n.without("clip-path"), parents, matrix); err != nil {
		return err
	}
	draw.DrawMask(dst, dst.Bounds(), layer, image.Point{}, mask, image.Point{}, draw.Over)
	return nil
}

func findSVGClip(n *svgNode, id string) *svgNode {
	if n.start.Name.Local == "clipPath" && n.attr("id") == id {
		return n
	}
	for _, child := range n.children {
		if found := findSVGClip(child, id); found != nil {
			return found
		}
	}
	return nil
}

func svgClipGeometry(n *svgNode) (*svgNode, error) {
	switch n.start.Name.Local {
	case "", "clipPath", "g", "path", "rect", "circle", "ellipse", "line", "polygon", "polyline", "title", "desc":
	default:
		return nil, fmt.Errorf("unsupported SVG clipping element %s", n.start.Name.Local)
	}
	for _, key := range []string{"clip-path", "mask", "filter"} {
		if value := n.property(key); value != "" && value != "none" {
			return nil, fmt.Errorf("unsupported SVG %s inside clipPath", key)
		}
	}
	clean := n.without("fill", "stroke", "opacity", "fill-opacity", "fill-rule")
	if clean.start.Name.Local == "clipPath" {
		clean.start.Name.Local = "g"
	}
	clean.start.Attr = append(clean.start.Attr,
		xml.Attr{Name: xml.Name{Local: "fill"}, Value: "white"},
		xml.Attr{Name: xml.Name{Local: "stroke"}, Value: "none"},
	)
	if rule := n.property("clip-rule"); rule != "" {
		clean.start.Attr = append(clean.start.Attr, xml.Attr{Name: xml.Name{Local: "fill-rule"}, Value: rule})
	}
	clean.children = nil
	for _, child := range n.children {
		geometry, err := svgClipGeometry(child)
		if err != nil {
			return nil, err
		}
		clean.children = append(clean.children, geometry)
	}
	return clean, nil
}
