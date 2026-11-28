package media

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var svgUniformScale = regexp.MustCompile(`\bscale\s*\(\s*([+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?)\s*\)`)

func normalizeSVG(data []byte) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var out bytes.Buffer
	encoder := xml.NewEncoder(&out)
	var width, height float64
	seenRoot := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if start, ok := token.(xml.StartElement); ok {
			if !seenRoot {
				if start.Name.Local != "svg" {
					return nil, fmt.Errorf("expected SVG root")
				}
				seenRoot = true
				var viewBox string
				for _, a := range start.Attr {
					if a.Name.Local == "viewBox" {
						viewBox = a.Value
					}
				}
				if viewBox != "" {
					parts := strings.Fields(strings.ReplaceAll(viewBox, ",", " "))
					if len(parts) != 4 {
						return nil, fmt.Errorf("invalid SVG viewBox")
					}
					values := make([]float64, 4)
					for i, part := range parts {
						values[i], err = svgNumber(part)
						if err != nil {
							return nil, err
						}
					}
					width, height = values[2], values[3]
				} else {
					for _, a := range start.Attr {
						switch a.Name.Local {
						case "width":
							width, err = svgNumber(a.Value)
						case "height":
							height, err = svgNumber(a.Value)
						}
						if err != nil {
							return nil, err
						}
					}
				}
				if !validSize(width, height) && !(width == 0 && height == 0) {
					return nil, fmt.Errorf("invalid or excessive SVG dimensions")
				}
			} else if start.Name.Local == "svg" {
				return nil, fmt.Errorf("nested SVG viewports are unsupported")
			}
			switch start.Name.Local {
			case "svg", "rect", "circle", "ellipse", "line", "image":
				for i, a := range start.Attr {
					basis := float64(0)
					switch a.Name.Local {
					case "x", "x1", "x2", "cx", "width", "rx":
						basis = width
					case "y", "y1", "y2", "cy", "height", "ry":
						basis = height
					case "r":
						basis = math.Hypot(width, height) / math.Sqrt2
					default:
						continue
					}
					value := strings.TrimSpace(a.Value)
					if strings.HasSuffix(value, "%") {
						n, err := svgNumber(strings.TrimSuffix(value, "%"))
						if err != nil {
							return nil, err
						}
						start.Attr[i].Value = strconv.FormatFloat(n*basis/100, 'g', -1, 64)
					} else {
						n, err := svgNumber(value)
						if err != nil {
							return nil, err
						}
						start.Attr[i].Value = strconv.FormatFloat(n, 'g', -1, 64)
					}
				}
			}
			attrs := start.Attr[:0]
			for _, attr := range start.Attr {
				if attr.Name.Local == "transform" {
					attr.Value = svgUniformScale.ReplaceAllString(attr.Value, "scale($1,$1)")
				}
				if attr.Name.Local == "stroke-width" && attr.Value == "none" {
					continue
				}
				attrs = append(attrs, attr)
			}
			start.Attr = attrs

			token = start
		}
		if err := encoder.EncodeToken(token); err != nil {
			return nil, err
		}
	}
	if err := encoder.Flush(); err != nil {
		return nil, err
	}
	if !seenRoot {
		return nil, fmt.Errorf("empty SVG")
	}
	return out.Bytes(), nil
}

func svgNumber(s string) (float64, error) {
	n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "px"), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, fmt.Errorf("invalid or unsupported SVG length %q", s)
	}
	return n, nil
}
