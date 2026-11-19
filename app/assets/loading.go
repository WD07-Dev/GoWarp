package assets

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"os"
)

const MaxLoadingImageSize = 8 << 20

// LoadLoadingImage embeds the original PNG or JPEG, preserving transparency and aspect ratio.
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
	if _, err := DecodeLoadingImage(data); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("choose a PNG or JPEG loading image")
	}
	return data, nil
}

func DecodeLoadingImage(data []byte) (image.Image, error) {
	if err := ValidateLoadingImage(data); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	return decoded, err
}

// ValidateLoadingImage checks bounds before a renderer allocates decoded pixels.
func ValidateLoadingImage(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if len(data) > MaxLoadingImageSize {
		return fmt.Errorf("loading image must be at most 8 MiB")
	}
	info, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		return fmt.Errorf("choose a PNG or JPEG loading image")
	}
	if info.Width < 1 || info.Height < 1 || info.Width > 4096 || info.Height > 4096 {
		return fmt.Errorf("loading image dimensions must be 1..4096")
	}
	return nil
}
