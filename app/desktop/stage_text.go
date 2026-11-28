package desktop

import (
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/core/render"
)

const textGlyphTextureBudget = 4 << 20

func (g *stageGPU) drawTextTexture(dst *ebiten.Image, r *render.Renderer, layout *render.TextLayout) {
	dst.Clear()
	r.DrawTextGlyphs(layout, func(mask image.Image, point image.Point, rect image.Rectangle, color color.NRGBA) {
		if rect.Intersect(dst.Bounds()).Empty() {
			return
		}
		alpha, ok := mask.(*image.Alpha)
		cacheable := ok && point == (image.Point{}) && rect.Dx()*rect.Dy() <= 512<<10
		var texture *ebiten.Image
		if cacheable {
			if entry, ok := g.textGlyphs[alpha]; ok {
				entry.used = g.clock
				g.textGlyphs[alpha] = entry
				texture = entry.image
			}
		}
		if texture == nil {
			bytes := mask.Bounds().Dx() * mask.Bounds().Dy() * 4
			if cacheable {
				if g.textGlyphs == nil {
					g.textGlyphs = make(map[*image.Alpha]stageTexture)
				}
				for len(g.textGlyphs) >= 1024 || g.textGlyphBytes+bytes > textGlyphTextureBudget {
					var oldest *image.Alpha
					used := ^uint64(0)
					for key, entry := range g.textGlyphs {
						if entry.used < used {
							oldest, used = key, entry.used
						}
					}
					entry := g.textGlyphs[oldest]
					entry.image.Deallocate()
					g.textGlyphBytes -= entry.bytes
					delete(g.textGlyphs, oldest)
				}
			}
			texture = ebiten.NewImageFromImage(mask)
			if cacheable {
				g.textGlyphs[alpha] = stageTexture{image: texture, bytes: bytes, used: g.clock}
				g.textGlyphBytes += bytes
			}
			if g.measureUploads {
				g.uploadCount++
				g.uploadBytes += int64(bytes)
			}
		}
		options := ebiten.DrawImageOptions{}
		options.GeoM.Translate(float64(rect.Min.X), float64(rect.Min.Y))
		options.ColorScale.ScaleWithColor(color)
		source := texture
		if !cacheable {
			start := point.Sub(mask.Bounds().Min)
			source = texture.SubImage(image.Rectangle{Min: start, Max: start.Add(rect.Size())}).(*ebiten.Image)
		}
		dst.DrawImage(source, &options)
		if !cacheable {
			texture.Deallocate()
		}
	})
}
