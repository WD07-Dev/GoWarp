package assets

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"

	"golang.org/x/image/draw"
)

func LoadIcon(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 8<<20 {
		return nil, fmt.Errorf("icon image must be at most 8 MiB")
	}
	info, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		return nil, fmt.Errorf("choose a PNG or JPEG icon image")
	}
	if info.Width < 1 || info.Height < 1 || info.Width > 4096 || info.Height > 4096 {
		return nil, fmt.Errorf("icon image dimensions must be 1..4096")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	icon := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	width := max(1, info.Width*256/max(info.Width, info.Height))
	height := max(1, info.Height*256/max(info.Width, info.Height))
	x, y := (256-width)/2, (256-height)/2
	draw.CatmullRom.Scale(icon, image.Rect(x, y, x+width, y+height), source, source.Bounds(), draw.Src, nil)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, icon); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func DecodeIcon(data []byte) (image.Image, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) > 512<<10 {
		return nil, fmt.Errorf("embedded icon exceeds 512 KiB")
	}
	info, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || info.Width < 1 || info.Height < 1 || info.Width > 256 || info.Height > 256 {
		return nil, fmt.Errorf("embedded icon must be a PNG of at most 256×256")
	}
	return png.Decode(bytes.NewReader(data))
}

func IconImages(data []byte) ([]image.Image, error) {
	source, err := DecodeIcon(data)
	if err != nil || source == nil {
		return nil, err
	}
	var images []image.Image
	for _, size := range []int{16, 32, 48, 256} {
		icon := image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.CatmullRom.Scale(icon, icon.Bounds(), source, source.Bounds(), draw.Src, nil)
		images = append(images, icon)
	}
	return images, nil
}

func DefaultWindowIcon() []image.Image {
	images, err := IconImages(DefaultIcon)
	if err != nil {
		panic(fmt.Sprintf("invalid bundled application icon: %v", err))
	}
	return images
}
