package media

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// Project fonts take precedence over the system's default and fallback fonts.
type svgTextStyle struct {
	size                          float64
	family, weight, style, anchor string
	preserve                      bool
	fonts                         svgFontResolver
}

type svgTextCursor struct {
	x, y           float64
	started, space bool
}

func svgTextInherited(n *svgNode, s svgTextStyle) (svgTextStyle, error) {
	if v := n.property("font-size"); v != "" && v != "none" && v != "inherit" {
		size, err := svgNumber(v)
		if err != nil || size < 0 || size > 4096 {
			return s, fmt.Errorf("invalid or unsupported SVG text font size %q", v)
		}
		s.size = size
	}
	for _, item := range []struct {
		key string
		dst *string
	}{
		{"font-family", &s.family},
		{"font-weight", &s.weight},
		{"font-style", &s.style},
		{"text-anchor", &s.anchor},
	} {
		if v := n.property(item.key); v != "" && v != "none" && v != "inherit" {
			*item.dst = v
		}
	}
	if v := n.attr("space"); v != "" {
		s.preserve = v == "preserve"
	}
	return s, nil
}

func svgTextPaths(n *svgNode, inherited svgTextStyle, budget *int) error {
	style, err := svgTextInherited(n, inherited)
	if err != nil {
		return err
	}
	if n.start.Name.Local == "text" {
		_, err := svgLayoutText(n, style, &svgTextCursor{}, budget)
		return err
	}
	if n.start.Name.Local == "tspan" || n.start.Name.Local == "textPath" {
		return fmt.Errorf("unsupported SVG text element %s outside text", n.start.Name.Local)
	}
	for _, c := range n.children {
		if err := svgTextPaths(c, style, budget); err != nil {
			return err
		}
	}
	return nil
}

func svgLayoutText(n *svgNode, inherited svgTextStyle, cursor *svgTextCursor, budget *int) (float64, error) {
	style, err := svgTextInherited(n, inherited)
	if err != nil {
		return 0, err
	}
	for _, key := range []string{
		"textLength",
		"lengthAdjust",
		"rotate",
		"writing-mode",
		"glyph-orientation-vertical",
		"dominant-baseline",
		"alignment-baseline",
		"baseline-shift",
		"direction",
		"unicode-bidi",
		"font-variant",
		"letter-spacing",
		"word-spacing",
	} {
		if v := n.property(key); v != "" && v != "normal" {
			return 0, fmt.Errorf("unsupported SVG text layout %s", key)
		}
	}
	for _, item := range []struct {
		key string
		dst *float64
		add bool
	}{{"x", &cursor.x, false}, {"y", &cursor.y, false}, {"dx", &cursor.x, true}, {"dy", &cursor.y, true}} {
		if v := n.attr(item.key); v != "" {
			value, err := svgNumber(v)
			if err != nil {
				return 0, fmt.Errorf("unsupported SVG text %s: %w", item.key, err)
			}
			if item.add {
				*item.dst += value
			} else {
				*item.dst = value
			}
		}
	}
	start := cursor.x
	var children []*svgNode
	for _, c := range n.children {
		if c.start.Name.Local != "" {
			if c.start.Name.Local != "tspan" {
				return 0, fmt.Errorf("unsupported SVG text element %s", c.start.Name.Local)
			}
			childStyle := style
			/*
				The outer text anchor applies to the whole uninterrupted text chunk.
				Positioned tspans with a non-start anchor require chunk layout.
			*/
			if style.anchor != "" && style.anchor != "start" && (c.attr("x") != "" || c.attr("y") != "") {
				return 0, fmt.Errorf("positioned SVG tspan with non-start text anchor is unsupported")
			}
			childStyle.anchor = "start"
			if anchor := c.property("text-anchor"); anchor != "" && anchor != "start" {
				return 0, fmt.Errorf("independent SVG tspan text anchor is unsupported")
			}
			if _, err := svgLayoutText(c, childStyle, cursor, budget); err != nil {
				return 0, err
			}
			children = append(children, c)
			continue
		}
		text := c.text
		if !style.preserve {
			var b strings.Builder
			for _, ch := range text {
				if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
					if cursor.started {
						cursor.space = true
					}
					continue
				}
				if cursor.space {
					b.WriteByte(' ')
					cursor.space = false
				}
				b.WriteRune(ch)
				cursor.started = true
			}
			text = b.String()
		}
		if style.size == 0 || text == "" {
			continue
		}
		glyphs, advance, err := svgGlyphNodes(text, style.size, style.family, style.weight, style.style, style.fonts)
		if err != nil {
			return 0, err
		}
		*budget += svgGlyphBytes(glyphs)
		if *budget > 16<<20 {
			return 0, fmt.Errorf("excessive SVG text outlines")
		}
		if len(glyphs) != 0 {
			group := svgElement("g", "transform", fmt.Sprintf("translate(%g,%g)", cursor.x, cursor.y))
			group.children = glyphs
			children = append(children, group)
		}
		cursor.x += advance
	}
	width := cursor.x - start
	anchor := style.anchor
	if anchor == "" {
		anchor = "start"
	}
	if anchor != "start" {
		shift := 0.0
		switch anchor {
		case "middle":
			shift = -width / 2
		case "end":
			shift = -width
		default:
			return 0, fmt.Errorf("unsupported SVG text anchor %q", anchor)
		}
		children = []*svgNode{{
			start: xml.StartElement{
				Name: xml.Name{Local: "g"},
				Attr: []xml.Attr{{Name: xml.Name{Local: "transform"}, Value: fmt.Sprintf("translate(%g,0)", shift)}},
			},
			children: children,
		}}
	}
	n.start.Name = xml.Name{Local: "g"}
	n.children = children
	/*
		Positioning has been baked into each glyph path; the group's transform and
		presentation attributes still apply normally to the generated outlines.
	*/
	return width, nil
}

func svgGlyphBytes(nodes []*svgNode) int {
	size := 0
	for _, node := range nodes {
		for _, attr := range node.start.Attr {
			size += len(attr.Value)
		}
		size += svgGlyphBytes(node.children)
	}
	return size
}
