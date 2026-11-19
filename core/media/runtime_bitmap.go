package media

import (
	"encoding/binary"
	"image"
	"io"
	"os"
)

const playbackDiskBudget = 256 << 20

type playbackBitmap struct {
	size     image.Point
	repeated bool
	runs     []uint32 // Pixel count followed by the exact NRGBA value.
	indices  []byte
	palette  []uint32
	stored   *io.SectionReader
	lengths  [2]int
}

/*
PrepareCostumePlayback prepares lossless base rasters within the shared cache budget. Call during
loading, before starting display workers.
*/
func (lib *Library) PrepareCostumePlayback(progress func(LoadProgress)) error {
	c := lib.cache
	if c == nil || c.playback != nil {
		return lib.Err()
	}
	if !c.ready() {
		return c.err
	}
	c.playback = make(map[string]*playbackBitmap)
	budget := min(16<<20, c.limit/4)
	used := 0
	diskBytes := int64(0)
	canStore := true
	seen := make(map[string]bool)
	total, done := 0, 0
	for _, costumes := range lib.Costumes {
		total += len(costumes)
	}
	// Prioritize expensive SVG rasters; bitmaps use the remaining allowance.
	for _, bitmap := range []bool{false, true} {
		for _, costumes := range lib.Costumes {
			for _, costume := range costumes {
				if costume.Bitmap != bitmap {
					continue
				}
				if err := c.ctx.Err(); err != nil {
					return err
				}
				source, ok := c.costumes[costume]
				if ok && !seen[source.key] && (budget-used >= 8 || canStore && diskBytes < playbackDiskBudget) {
					seen[source.key] = true
					asset := c.get(source.key)
					if asset == nil {
						data, err := c.reader.Read(source.name)
						if err == nil {
							decoded, err := decodeCostumeWithAntialias(source.meta, data, lib.customFont, lib.noAntialias)
							if err == nil {
								asset = newCostumeAsset(source.key, decoded.Image, costume.Bitmap)
							}
						}
					}
					if asset != nil {
						available := budget - used
						if canStore {
							available = max(available, int(playbackDiskBudget-diskBytes))
						}
						if bitmap := packPlaybackBitmap(asset, min(16<<20, available)); bitmap != nil {
							size := bitmap.bytes()
							if size <= budget-used {
								used += size
								c.playback[source.key] = bitmap
							} else if canStore && diskBytes+int64(size) <= playbackDiskBudget {
								if c.playbackFile == nil {
									var err error
									c.playbackFile, err = os.CreateTemp("", "gowarp-images-*.cache")
									canStore = err == nil
								}
								if canStore {
									canStore = bitmap.store(c.playbackFile, diskBytes) == nil
									if canStore {
										c.playback[source.key] = bitmap
										diskBytes += int64(size)
									}
								}
							}
						}
					}
				}
				done++
				if progress != nil {
					progress(LoadProgress{Done: done, Total: max(1, total), Label: "Preparing project"})
				}
			}
		}
	}
	c.limit -= used
	for c.bytes > c.limit {
		oldest := c.recent.Back()
		asset := oldest.Value.(*cachedAsset)
		delete(c.entries, asset.key)
		c.bytes -= asset.bytes
		c.recent.Remove(oldest)
	}
	return nil
}

func (bitmap *playbackBitmap) store(file *os.File, offset int64) error {
	data := make([]byte, 0, bitmap.bytes())
	for _, value := range bitmap.runs {
		data = binary.LittleEndian.AppendUint32(data, value)
	}
	for _, value := range bitmap.palette {
		data = binary.LittleEndian.AppendUint32(data, value)
	}
	data = append(data, bitmap.indices...)
	if _, err := file.WriteAt(data, offset); err != nil {
		return err
	}
	bitmap.stored = io.NewSectionReader(file, offset, int64(len(data)))
	bitmap.lengths = [2]int{len(bitmap.runs), len(bitmap.palette)}
	bitmap.runs, bitmap.palette, bitmap.indices = nil, nil, nil
	return nil
}

func (bitmap *playbackBitmap) load() (*playbackBitmap, error) {
	if bitmap.stored == nil {
		return bitmap, nil
	}
	data := make([]byte, int(bitmap.stored.Size()))
	if _, err := bitmap.stored.ReadAt(data, 0); err != nil {
		return nil, err
	}
	loaded := &playbackBitmap{size: bitmap.size, repeated: bitmap.repeated}
	if bitmap.lengths[0] > 0 {
		loaded.runs = make([]uint32, bitmap.lengths[0])
		for i := range loaded.runs {
			loaded.runs[i] = binary.LittleEndian.Uint32(data[i*4:])
		}
	} else {
		loaded.palette = make([]uint32, bitmap.lengths[1])
		for i := range loaded.palette {
			loaded.palette[i] = binary.LittleEndian.Uint32(data[i*4:])
		}
		loaded.indices = data[len(loaded.palette)*4:]
	}
	return loaded, nil
}

func packPlaybackBitmap(asset *cachedAsset, budget int) *playbackBitmap {
	pixels := asset.image
	size := pixels.Bounds().Size()
	limit := min(budget, size.X*size.Y) / 8 // At least a fourfold reduction.
	if limit == 0 {
		return nil
	}
	bitmap := &playbackBitmap{size: size, repeated: asset.repeated}
	var previous, count uint32
	for y := 0; y < size.Y; y++ {
		row := pixels.Pix[y*pixels.Stride : y*pixels.Stride+size.X*4]
		for x := 0; x < len(row); x += 4 {
			pixel := binary.LittleEndian.Uint32(row[x : x+4])
			if count != 0 && pixel != previous {
				if len(bitmap.runs)/2 >= limit {
					return packIndexedBitmap(asset, budget)
				}
				bitmap.runs = append(bitmap.runs, count, previous)
				count = 0
			}
			previous = pixel
			count++
		}
	}
	if count == 0 || len(bitmap.runs)/2 >= limit {
		return packIndexedBitmap(asset, budget)
	}
	runs := make([]uint32, len(bitmap.runs)+2)
	copy(runs, bitmap.runs)
	runs[len(runs)-2], runs[len(runs)-1] = count, previous
	bitmap.runs = runs
	return bitmap
}

func (bitmap *playbackBitmap) bytes() int {
	return len(bitmap.runs)*4 + len(bitmap.indices) + len(bitmap.palette)*4
}

func packIndexedBitmap(asset *cachedAsset, budget int) *playbackBitmap {
	pixels := asset.image
	size := pixels.Bounds().Size()
	count := size.X * size.Y
	limit := min(budget, count*2)
	if count == 0 || count+4 > limit {
		return nil
	}
	indices := make([]byte, count)
	colors := make(map[uint32]byte)
	var palette []uint32
	for y := 0; y < size.Y; y++ {
		row := pixels.Pix[y*pixels.Stride : y*pixels.Stride+size.X*4]
		for x := 0; x < size.X; x++ {
			pixel := binary.LittleEndian.Uint32(row[x*4:])
			index, ok := colors[pixel]
			if !ok {
				if len(palette) == 256 || count+(len(palette)+1)*4 > limit {
					return nil
				}
				index = byte(len(palette))
				colors[pixel] = index
				palette = append(palette, pixel)
			}
			indices[y*size.X+x] = index
		}
	}
	stored := make([]uint32, len(palette))
	copy(stored, palette)
	return &playbackBitmap{size: size, repeated: asset.repeated, indices: indices, palette: stored}
}

// Decode only visible pixels; cropped uploads must not allocate a full canvas.
func (bitmap *playbackBitmap) region(region image.Rectangle) *image.NRGBA {
	scale := 1
	if bitmap.repeated {
		scale = 2
	}
	region = region.Intersect(image.Rectangle{Max: bitmap.size.Mul(scale)})
	if region.Empty() {
		return nil
	}
	pixels := image.NewNRGBA(region)
	if bitmap.indices != nil {
		for y := region.Min.Y; y < region.Max.Y; y++ {
			row := pixels.Pix[(y-region.Min.Y)*pixels.Stride:]
			for x := region.Min.X; x < region.Max.X; x++ {
				index := bitmap.indices[y/scale*bitmap.size.X+x/scale]
				binary.LittleEndian.PutUint32(row[(x-region.Min.X)*4:], bitmap.palette[index])
			}
		}
		return pixels
	}
	runIndex := 0
	runEnd := int(bitmap.runs[0])
	for y := region.Min.Y; y < region.Max.Y; y++ {
		row := pixels.Pix[(y-region.Min.Y)*pixels.Stride : (y-region.Min.Y+1)*pixels.Stride]
		if scale == 2 && y > region.Min.Y && y%2 == 1 {
			copy(row, pixels.Pix[(y-region.Min.Y-1)*pixels.Stride:])
			continue
		}
		sourceRow := y / scale * bitmap.size.X
		for x := region.Min.X; x < region.Max.X; {
			source := sourceRow + x/scale
			for source >= runEnd {
				runIndex += 2
				runEnd += int(bitmap.runs[runIndex])
			}
			end := min(region.Max.X, (runEnd-sourceRow)*scale)
			value := bitmap.runs[runIndex+1]
			if value != 0 {
				run := row[(x-region.Min.X)*4 : (end-region.Min.X)*4]
				binary.LittleEndian.PutUint32(run, value)
				for filled := 4; filled < len(run); {
					filled += copy(run[filled:], run[:filled])
				}
			}
			x = end
		}
	}
	return pixels
}

func (c *runtimeCache) playbackAsset(key string) *cachedAsset {
	bitmap := c.playback[key]
	if bitmap == nil {
		return nil
	}
	bitmap, err := bitmap.load()
	if err != nil {
		// Optional prepared storage may fail; the original asset remains available.
		delete(c.playback, key)
		return nil
	}
	asset := bitmap.asset(key)
	c.put(asset)
	return asset
}

func (bitmap *playbackBitmap) asset(key string) *cachedAsset {
	pixels := image.NewNRGBA(image.Rectangle{Max: bitmap.size})
	if bitmap.indices != nil {
		for i, index := range bitmap.indices {
			binary.LittleEndian.PutUint32(pixels.Pix[i*4:], bitmap.palette[index])
		}
	}
	offset := 0
	for i := 0; i < len(bitmap.runs); i += 2 {
		n := int(bitmap.runs[i]) * 4
		value := bitmap.runs[i+1]
		if value != 0 {
			run := pixels.Pix[offset : offset+n]
			binary.LittleEndian.PutUint32(run, value)
			for filled := 4; filled < len(run); {
				filled += copy(run[filled:], run[:filled])
			}
		}
		offset += n
	}
	return &cachedAsset{key: key, image: pixels, repeated: bitmap.repeated, bytes: len(pixels.Pix)}
}
