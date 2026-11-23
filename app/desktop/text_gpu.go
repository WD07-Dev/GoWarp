package desktop

import (
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font"
)

type textCacheKey struct {
	face   font.Face
	text   string
	bounds image.Rectangle
}

type textTexture struct {
	image *ebiten.Image
	bytes int
	used  uint64
}

// Bound changing monitor values and release font faces and textures on resize.
type gpuTextCache struct {
	entries map[textCacheKey]textTexture
	bytes   int
	clock   uint64
}

func (c *gpuTextCache) clear() {
	for _, entry := range c.entries {
		entry.image.Deallocate()
	}
	*c = gpuTextCache{}
}

func (c *gpuTextCache) draw(dst *ebiten.Image, face font.Face, x, y int, sx, sy float64, s string, ink color.Color) {
	box, _ := font.BoundString(face, s)
	bounds := image.Rect(box.Min.X.Floor(), box.Min.Y.Floor(), box.Max.X.Ceil(), box.Max.Y.Ceil())
	clip := dst.Bounds().Sub(image.Pt(x, y))
	bounds = bounds.Intersect(image.Rect(
		int(math.Floor(float64(clip.Min.X)/sx)),
		int(math.Floor(float64(clip.Min.Y)/sy)),
		int(math.Ceil(float64(clip.Max.X)/sx)),
		int(math.Ceil(float64(clip.Max.Y)/sy)),
	))
	if bounds.Empty() {
		return
	}
	key := textCacheKey{face, s, bounds}
	c.clock++
	entry, found := c.entries[key]
	temporary := false
	if !found {
		bytes := bounds.Dx() * bounds.Dy() * 4
		const budget = 8 << 20
		for len(c.entries) > 0 && (len(c.entries) >= 128 || c.bytes+bytes > budget) {
			var oldest textCacheKey
			age := ^uint64(0)
			for k, candidate := range c.entries {
				if candidate.used < age {
					oldest, age = k, candidate.used
				}
			}
			c.entries[oldest].image.Deallocate()
			c.bytes -= c.entries[oldest].bytes
			delete(c.entries, oldest)
		}
		pixels := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		writeText(pixels, face, -bounds.Min.X, -bounds.Min.Y, s, color.White)
		entry = textTexture{image: ebiten.NewImageFromImage(pixels), bytes: bytes}
		temporary = bytes > budget
		if !temporary {
			if c.entries == nil {
				c.entries = make(map[textCacheKey]textTexture)
			}
			c.bytes += bytes
		}
	}
	entry.used = c.clock
	if !temporary {
		c.entries[key] = entry
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(sx, sy)
	op.GeoM.Translate(float64(x)+float64(bounds.Min.X)*sx, float64(y)+float64(bounds.Min.Y)*sy)
	op.ColorScale.ScaleWithColor(ink)
	dst.DrawImage(entry.image, op)
	if temporary {
		entry.image.Deallocate()
	}
}
