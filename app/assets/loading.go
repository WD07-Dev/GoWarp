package assets

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
)

const MaxLoadingImageSize = 8 << 20
const MaxLoadingImageMemory = 64 << 20

// LoadLoadingImage keeps the original encoded image in the settings.
func LoadLoadingImage(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxLoadingImageSize+1))
	if err != nil {
		return nil, err
	}
	if err := ValidateLoadingImage(data); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("choose a PNG, JPEG or GIF loading image")
	}
	return data, nil
}

func DecodeLoadingImage(data []byte) (*LoadingImage, error) {
	info, animation, err := inspectLoadingImage(data)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	if animation != nil {
		animation.canvas = image.NewRGBA(image.Rect(0, 0, info.Width, info.Height))
		if animation.needsPrevious {
			animation.previous = image.NewRGBA(animation.canvas.Bounds())
		}
		if err := animation.drawFrame(0); err != nil {
			return nil, err
		}
		return &LoadingImage{Image: animation.canvas, animation: animation}, nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return &LoadingImage{Image: decoded}, nil
}

// ValidateLoadingImage checks bounds before a renderer allocates decoded pixels.
func ValidateLoadingImage(data []byte) error {
	_, _, err := inspectLoadingImage(data)
	return err
}

func ValidateLoadingImages(logo, background []byte) error {
	var memory int64
	for _, item := range []struct {
		name  string
		data  []byte
		limit image.Point
	}{
		{"logo", logo, image.Pt(512, 512)},
		{"background", background, image.Pt(1280, 720)},
	} {
		info, animation, err := inspectLoadingImage(item.data)
		if err != nil {
			return fmt.Errorf("loading %s: %w", item.name, err)
		}
		if animation != nil {
			if info.Width > item.limit.X || info.Height > item.limit.Y {
				return fmt.Errorf("loading %s GIF must be at most %d×%d", item.name, item.limit.X, item.limit.Y)
			}
		}
		memory += loadingImageMemory(info, animation)
	}
	if memory > MaxLoadingImageMemory {
		return fmt.Errorf("decoded loading images must use at most 64 MiB in total")
	}
	return nil
}

func loadingImageMemory(info image.Config, animation *loadingGIFAnimation) int64 {
	pixels := int64(info.Width) * int64(info.Height)
	memory := pixels * 4
	if info.ColorModel == color.NRGBA64Model || info.ColorModel == color.RGBA64Model {
		memory = pixels * 8
	}
	if animation != nil {
		// Interlaced GIF decoding temporarily needs a second paletted buffer.
		memory += int64(animation.maxFramePixels) * 2
		if animation.needsPrevious {
			memory += pixels * 4
		}
	}
	return memory
}

func inspectLoadingImage(data []byte) (image.Config, *loadingGIFAnimation, error) {
	if len(data) == 0 {
		return image.Config{}, nil, nil
	}
	if len(data) > MaxLoadingImageSize {
		return image.Config{}, nil, fmt.Errorf("loading image must be at most 8 MiB")
	}
	info, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "gif") {
		return image.Config{}, nil, fmt.Errorf("choose a PNG, JPEG or GIF loading image")
	}
	if info.Width < 1 || info.Height < 1 || info.Width > 4096 || info.Height > 4096 {
		return image.Config{}, nil, fmt.Errorf("loading image dimensions must be 1..4096")
	}
	if format == "gif" {
		if info.Width > 1280 || info.Height > 720 {
			return image.Config{}, nil, fmt.Errorf("loading GIF dimensions must be at most 1280×720")
		}
		animation, err := inspectLoadingGIF(data, image.Rect(0, 0, info.Width, info.Height))
		return info, animation, err
	}
	if loadingImageMemory(info, nil) > MaxLoadingImageMemory {
		return image.Config{}, nil, fmt.Errorf("decoded loading image must use at most 64 MiB")
	}
	return info, nil, nil
}
