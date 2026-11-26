package media

import "image"

/*
CostumeImageRegionAtScale uses coordinates relative to the raster's origin.
A compact bitmap expands only the requested pixels, including odd edges.
*/
func (lib *Library) CostumeImageRegionAtScale(costume *Costume, scale float64, region image.Rectangle) *image.NRGBA {
	if cache := lib.cache; cache != nil && costume.Bitmap && costume.Image == nil {
		if !cache.ready() {
			return nil
		}
		if source, ok := cache.costumes[costume]; ok && cache.entries[source.key] == nil {
			if bitmap := cache.playback[source.key]; bitmap != nil {
				if loaded, err := bitmap.load(); err == nil {
					return loaded.decodeRegion(region)
				}
				delete(cache.playback, source.key)
			}
		}
	}
	var pixels *image.NRGBA
	repeated := false
	if costume.Bitmap {
		pixels, repeated = lib.DisplayCostumeImage(costume, scale)
	} else {
		pixels = lib.CostumeImageAtScale(costume, scale)
	}
	if pixels == nil {
		return nil
	}
	size := pixels.Bounds().Size()
	if repeated {
		size = size.Mul(2)
	}
	region = region.Intersect(image.Rectangle{Max: size})
	if region.Empty() {
		return nil
	}
	if !repeated {
		result := *pixels.SubImage(region.Add(pixels.Bounds().Min)).(*image.NRGBA)
		result.Rect = region
		return &result
	}
	cropped := image.NewNRGBA(region)
	origin := pixels.Bounds().Min
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			source := pixels.PixOffset(origin.X+x/2, origin.Y+y/2)
			dest := cropped.PixOffset(x, y)
			copy(cropped.Pix[dest:dest+4], pixels.Pix[source:source+4])
		}
	}
	return cropped
}
