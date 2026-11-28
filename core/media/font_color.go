package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"image/png"
	"math"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

func svgElement(name string, attributes ...string) *svgNode {
	node := &svgNode{start: xml.StartElement{Name: xml.Name{Local: name}}}
	for i := 0; i < len(attributes); i += 2 {
		node.start.Attr = append(node.start.Attr, xml.Attr{Name: xml.Name{Local: attributes[i]}, Value: attributes[i+1]})
	}
	return node
}

// COLR v0/CPAL and CBDT/CBLC cover Segoe UI Emoji and bitmap Noto Color Emoji.
func (f *systemFont) colorGlyph(index sfnt.GlyphIndex, size float64) []*svgNode {
	if nodes := f.layerGlyph(index, size); len(nodes) != 0 {
		return nodes
	}
	return f.bitmapGlyph(index, size)
}

func (f *systemFont) layerGlyph(index sfnt.GlyphIndex, size float64) []*svgNode {
	colr, cpal := f.table("COLR"), f.table("CPAL")
	if len(colr) < 14 || len(cpal) < 14 {
		return nil
	}
	count := int(binary.BigEndian.Uint16(colr[2:]))
	bases := fontBytes(colr, int(binary.BigEndian.Uint32(colr[4:])), count*6)
	if bases == nil {
		return nil
	}
	lo, hi := 0, count
	for lo < hi {
		m := (lo + hi) / 2
		if binary.BigEndian.Uint16(bases[m*6:]) < uint16(index) {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo == count || binary.BigEndian.Uint16(bases[lo*6:]) != uint16(index) {
		return nil
	}
	base := bases[lo*6:]
	first, n := int(binary.BigEndian.Uint16(base[2:])), int(binary.BigEndian.Uint16(base[4:]))
	if n == 0 || n > 512 || first+n > int(binary.BigEndian.Uint16(colr[12:])) {
		return nil
	}
	layers := fontBytes(colr, int(binary.BigEndian.Uint32(colr[8:]))+first*4, n*4)
	paletteCount := int(binary.BigEndian.Uint16(cpal[4:]))
	paletteEntries := int(binary.BigEndian.Uint16(cpal[2:]))
	if layers == nil || paletteCount == 0 || 12+paletteCount*2 > len(cpal) {
		return nil
	}
	paletteStart := int(binary.BigEndian.Uint16(cpal[12:]))
	colors := fontBytes(cpal, int(binary.BigEndian.Uint32(cpal[8:])), int(binary.BigEndian.Uint16(cpal[6:]))*4)
	if colors == nil || paletteStart+paletteEntries > len(colors)/4 {
		return nil
	}
	var buffer sfnt.Buffer
	ppem := fixed.Int26_6(math.Round(size * 64))
	nodes := make([]*svgNode, 0, n)
	for i := 0; i < n; i++ {
		glyph := sfnt.GlyphIndex(binary.BigEndian.Uint16(layers[i*4:]))
		palette := binary.BigEndian.Uint16(layers[i*4+2:])
		segments, err := f.face.LoadGlyph(&buffer, glyph, ppem, nil)
		if err != nil {
			return nil
		}
		path, err := glyphOutline(segments, 0)
		if err != nil {
			return nil
		}
		node := svgElement("path", "d", path, "stroke", "none")
		if palette != 0xffff {
			if int(palette) >= paletteEntries {
				return nil
			}
			color := colors[(paletteStart+int(palette))*4:]
			node.start.Attr = append(node.start.Attr,
				xml.Attr{Name: xml.Name{Local: "fill"}, Value: fmt.Sprintf("#%02x%02x%02x", color[2], color[1], color[0])},
				xml.Attr{Name: xml.Name{Local: "fill-opacity"}, Value: fmt.Sprint(float64(color[3]) / 255)})
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func (f *systemFont) bitmapGlyph(index sfnt.GlyphIndex, size float64) []*svgNode {
	cblc, cbdt := f.table("CBLC"), f.table("CBDT")
	if len(cblc) < 8 || len(cbdt) < 4 {
		return nil
	}
	count := int(binary.BigEndian.Uint32(cblc[4:]))
	if count > (len(cblc)-8)/48 {
		return nil
	}
	var best []byte
	distance := math.Inf(1)
	for i := 0; i < count; i++ {
		strike := cblc[8+i*48 : 8+(i+1)*48]
		if uint16(index) < binary.BigEndian.Uint16(strike[40:]) || uint16(index) > binary.BigEndian.Uint16(strike[42:]) || strike[44] == 0 || strike[45] == 0 || strike[47]&1 == 0 {
			continue
		}
		d := math.Abs(float64(strike[45]) - size)
		if d < distance {
			best, distance = strike, d
		}
	}
	if best == nil {
		return nil
	}
	listOffset := int(binary.BigEndian.Uint32(best))
	count = int(binary.BigEndian.Uint32(best[8:]))
	if count > len(cblc)/8 {
		return nil
	}
	entries := fontBytes(cblc, listOffset, count*8)
	if entries == nil {
		return nil
	}
	for i := 0; i < count; i++ {
		entry := entries[i*8:]
		first, last := binary.BigEndian.Uint16(entry), binary.BigEndian.Uint16(entry[2:])
		if uint16(index) < first || uint16(index) > last {
			continue
		}
		offset := listOffset + int(binary.BigEndian.Uint32(entry[4:]))
		header := fontBytes(cblc, offset, 8)
		if header == nil {
			return nil
		}
		sub := fontBytes(cblc, offset, len(cblc)-offset)
		imageFormat := binary.BigEndian.Uint16(header[2:])
		start, end, metrics, ok := bitmapRange(sub, int(uint16(index)-first), int(last-first)+1, uint16(index))
		if !ok || end <= start {
			return nil
		}
		imageOffset := int(binary.BigEndian.Uint32(header[4:]))
		data := fontBytes(cbdt, imageOffset+start, end-start)
		var width, height, bearingX, bearingY int
		switch imageFormat {
		case 17:
			if len(data) < 9 {
				return nil
			}
			metrics, data = data[:5], data[5:]
		case 18:
			if len(data) < 12 {
				return nil
			}
			metrics, data = data[:8], data[8:]
		case 19:
			if len(metrics) < 8 || len(data) < 4 {
				return nil
			}
		default:
			return nil
		}
		if len(metrics) < 5 {
			return nil
		}
		height, width = int(metrics[0]), int(metrics[1])
		bearingX, bearingY = int(int8(metrics[2])), int(int8(metrics[3]))
		length := int(binary.BigEndian.Uint32(data))
		if length > 4<<20 {
			return nil
		}
		image := fontBytes(data, 4, length)
		config, err := png.DecodeConfig(bytes.NewReader(image))
		if err != nil || config.Width != width || config.Height != height {
			return nil
		}
		sx, sy := size/float64(best[44]), size/float64(best[45])
		return []*svgNode{svgElement("image",
			"x", fmt.Sprint(float64(bearingX)*sx), "y", fmt.Sprint(-float64(bearingY)*sy),
			"width", fmt.Sprint(float64(width)*sx), "height", fmt.Sprint(float64(height)*sy),
			"href", "data:image/png;base64,"+base64.StdEncoding.EncodeToString(image))}
	}
	return nil
}

func bitmapRange(sub []byte, position, count int, glyph uint16) (start, end int, metrics []byte, ok bool) {
	if len(sub) < 8 {
		return
	}
	switch binary.BigEndian.Uint16(sub) {
	case 1, 3:
		stride := 4
		if binary.BigEndian.Uint16(sub) == 3 {
			stride = 2
		}
		data := fontBytes(sub, 8, (count+1)*stride)
		if data == nil {
			return
		}
		if stride == 4 {
			start, end = int(binary.BigEndian.Uint32(data[position*4:])), int(binary.BigEndian.Uint32(data[(position+1)*4:]))
		} else {
			start, end = int(binary.BigEndian.Uint16(data[position*2:])), int(binary.BigEndian.Uint16(data[(position+1)*2:]))
		}
	case 2:
		if len(sub) < 20 {
			return
		}
		size := int(binary.BigEndian.Uint32(sub[8:]))
		start, end, metrics = position*size, (position+1)*size, sub[12:20]
	case 4:
		if len(sub) < 12 {
			return
		}
		n := int(binary.BigEndian.Uint32(sub[8:]))
		if n > (len(sub)-12)/4-1 {
			return
		}
		found := false
		for i := 0; i < n; i++ {
			if binary.BigEndian.Uint16(sub[12+i*4:]) == glyph {
				start, end = int(binary.BigEndian.Uint16(sub[14+i*4:])), int(binary.BigEndian.Uint16(sub[18+i*4:]))
				found = true
				break
			}
		}
		if !found {
			return
		}
	case 5:
		if len(sub) < 24 {
			return
		}
		n := int(binary.BigEndian.Uint32(sub[20:]))
		if n > (len(sub)-24)/2 {
			return
		}
		found := -1
		for i := 0; i < n; i++ {
			if binary.BigEndian.Uint16(sub[24+i*2:]) == glyph {
				found = i
				break
			}
		}
		if found < 0 {
			return
		}
		size := int(binary.BigEndian.Uint32(sub[8:]))
		start, end, metrics = found*size, (found+1)*size, sub[12:20]
	default:
		return
	}
	ok = true
	return
}
