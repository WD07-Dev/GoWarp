package media

import (
	"encoding/xml"
	"math"
	"strconv"
	"strings"

	"github.com/srwiley/rasterx"
)

type svgStrokeStyle struct {
	color, width, dash, offset string
}

func svgScaleStrokes(n *svgNode, matrix rasterx.Matrix2D, inherited svgStrokeStyle) error {
	transform, err := svgTransform(n.attr("transform"))
	if err != nil {
		return err
	}
	matrix = matrix.Mult(transform)
	for _, property := range []struct {
		name string
		dst  *string
	}{
		{"stroke", &inherited.color}, {"stroke-width", &inherited.width},
		{"stroke-dasharray", &inherited.dash}, {"stroke-dashoffset", &inherited.offset},
	} {
		if value := n.property(property.name); value != "" && value != "inherit" {
			*property.dst = value
		}
	}
	switch n.start.Name.Local {
	case "path", "rect", "circle", "ellipse", "line", "polyline", "polygon":
		if inherited.color == "" || inherited.color == "none" {
			break
		}
		sx, sy := math.Hypot(matrix.A, matrix.B), math.Hypot(matrix.C, matrix.D)
		uniform := math.Abs(sx-sy) < 1e-9 && math.Abs(matrix.A*matrix.C+matrix.B*matrix.D) < 1e-9
		if !uniform || math.Abs(sx-1) <= 1e-9 {
			break
		}
		for _, property := range []struct{ name, value string }{
			{"stroke-width", inherited.width}, {"stroke-dasharray", inherited.dash}, {"stroke-dashoffset", inherited.offset},
		} {
			if property.value == "" || property.value == "none" {
				continue
			}
			values := strings.Fields(strings.ReplaceAll(property.value, ",", " "))
			for i, value := range values {
				length, err := svgNumber(value)
				if err != nil {
					return err
				}
				values[i] = strconv.FormatFloat(length*sx, 'g', -1, 64)
			}
			*n = *n.without(property.name)
			n.start.Attr = append(n.start.Attr, xml.Attr{Name: xml.Name{Local: property.name}, Value: strings.Join(values, " ")})
		}
	}
	for _, child := range n.children {
		if child.start.Name.Local == "" {
			continue
		}
		if err := svgScaleStrokes(child, matrix, inherited); err != nil {
			return err
		}
	}
	return nil
}
