package assets

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"io"
	"time"
)

type LoadingImage struct {
	image.Image
	animation *loadingGIFAnimation
}

type loadingGIFFrame struct {
	start, end int
	control    []byte
	bounds     image.Rectangle
	delay      time.Duration
	disposal   byte
}

type loadingGIFAnimation struct {
	data             []byte
	header           int
	frames           []loadingGIFFrame
	loopCount        int
	duration         time.Duration
	maxFramePixels   int
	needsPrevious    bool
	canvas, previous *image.RGBA
	started          time.Time
	cycle            int64
	frame            int
}

// Advance composites only the frames needed for the current playback time.
func (im *LoadingImage) Advance(now time.Time) (bool, error) {
	if im == nil || im.animation == nil || len(im.animation.frames) == 1 {
		return false, nil
	}
	a := im.animation
	if a.started.IsZero() {
		a.started = now
	}
	elapsed := max(time.Duration(0), now.Sub(a.started))
	cycle := int64(elapsed / a.duration)
	within := elapsed % a.duration
	if a.loopCount != 0 && cycle > int64(max(0, a.loopCount)) {
		cycle = int64(max(0, a.loopCount))
		within = a.duration - 1
	}
	index := 0
	for index < len(a.frames)-1 && within >= a.frames[index].delay {
		within -= a.frames[index].delay
		index++
	}
	if a.frame == index && a.cycle == cycle {
		return false, nil
	}
	if a.cycle != cycle || index < a.frame {
		clear(a.canvas.Pix)
		a.frame = -1
	}
	for a.frame < index {
		if err := a.drawFrame(a.frame + 1); err != nil {
			im.animation = nil
			return false, err
		}
	}
	a.cycle = cycle
	return true, nil
}

func (a *loadingGIFAnimation) drawFrame(index int) error {
	f := a.frames[index]
	reader := io.MultiReader(bytes.NewReader(a.data[:a.header]), bytes.NewReader(f.control),
		bytes.NewReader(a.data[f.start:f.end]), bytes.NewReader([]byte{0x3b}))
	frame, err := gif.Decode(reader)
	if err != nil {
		return fmt.Errorf("loading GIF frame %d: %w", index+1, err)
	}
	if index > 0 {
		previous := a.frames[index-1]
		switch previous.disposal {
		case gif.DisposalBackground:
			draw.Draw(a.canvas, previous.bounds, image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			draw.Draw(a.canvas, previous.bounds, a.previous, previous.bounds.Min, draw.Src)
		}
	}
	if f.disposal == gif.DisposalPrevious {
		copy(a.previous.Pix, a.canvas.Pix)
	}
	draw.Draw(a.canvas, f.bounds, frame, f.bounds.Min, draw.Over)
	a.frame = index
	return nil
}

func inspectLoadingGIF(data []byte, canvas image.Rectangle) (*loadingGIFAnimation, error) {
	if len(data) < 13 {
		return nil, fmt.Errorf("truncated loading GIF")
	}
	header := 13
	if data[10]&0x80 != 0 {
		header += 3 * (1 << ((data[10] & 7) + 1))
	}
	if header > len(data) {
		return nil, fmt.Errorf("truncated GIF palette")
	}
	a := &loadingGIFAnimation{data: data, header: header, loopCount: -1}
	var control []byte
	for offset := header; offset < len(data); {
		start := offset
		switch data[offset] {
		case 0x3b:
			if len(a.frames) == 0 {
				return nil, fmt.Errorf("loading GIF has no frames")
			}
			return a, nil
		case 0x21:
			if offset+2 >= len(data) {
				return nil, fmt.Errorf("truncated GIF extension")
			}
			label := data[offset+1]
			end, err := gifBlocksEnd(data, offset+2)
			if err != nil {
				return nil, err
			}
			if label == 0xf9 {
				if end-start != 8 || data[start+2] != 4 {
					return nil, fmt.Errorf("invalid GIF frame control")
				}
				control = data[start:end]
			}
			if label == 0xff && end-start >= 19 && data[start+2] == 11 &&
				(string(data[start+3:start+14]) == "NETSCAPE2.0" || string(data[start+3:start+14]) == "ANIMEXTS1.0") &&
				data[start+14] == 3 && data[start+15] == 1 {
				a.loopCount = int(binary.LittleEndian.Uint16(data[start+16:]))
			}
			offset = end
		case 0x2c:
			if len(a.frames) >= 120 {
				return nil, fmt.Errorf("loading GIF must contain at most 120 frames")
			}
			if offset+10 > len(data) {
				return nil, fmt.Errorf("truncated GIF frame")
			}
			x := int(binary.LittleEndian.Uint16(data[offset+1:]))
			y := int(binary.LittleEndian.Uint16(data[offset+3:]))
			w := int(binary.LittleEndian.Uint16(data[offset+5:]))
			h := int(binary.LittleEndian.Uint16(data[offset+7:]))
			bounds := image.Rect(x, y, x+w, y+h)
			if w == 0 || h == 0 || !bounds.In(canvas) {
				return nil, fmt.Errorf("GIF frame exceeds its canvas")
			}
			offset += 10
			if data[start+9]&0x80 != 0 {
				offset += 3 * (1 << ((data[start+9] & 7) + 1))
			}
			// Skip the LZW code size, then scan the compressed data blocks.
			end, err := gifBlocksEnd(data, offset+1)
			if err != nil {
				return nil, err
			}
			f := loadingGIFFrame{start: start, end: end, control: control, bounds: bounds, delay: 10 * time.Millisecond}
			if control != nil {
				f.delay = max(10*time.Millisecond, time.Duration(binary.LittleEndian.Uint16(control[4:]))*10*time.Millisecond)
				f.disposal = (control[3] >> 2) & 7
			}
			a.frames = append(a.frames, f)
			a.duration += f.delay
			a.maxFramePixels = max(a.maxFramePixels, w*h)
			a.needsPrevious = a.needsPrevious || f.disposal == gif.DisposalPrevious
			control = nil
			offset = end
		default:
			return nil, fmt.Errorf("invalid GIF block")
		}
	}
	return nil, fmt.Errorf("truncated loading GIF")
}

func gifBlocksEnd(data []byte, offset int) (int, error) {
	for offset < len(data) {
		size := int(data[offset])
		offset++
		if size == 0 {
			return offset, nil
		}
		offset += size
	}
	return 0, fmt.Errorf("truncated GIF data")
}
