package assets

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestDecodeLoadingImage(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		var data bytes.Buffer
		source := image.NewRGBA(image.Rect(0, 0, 12, 6))
		var err error
		if format == "png" {
			err = png.Encode(&data, source)
		} else {
			err = jpeg.Encode(&data, source, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeLoadingImage(data.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if got.Bounds() != source.Bounds() {
			t.Fatal("aspect ratio changed")
		}
		if _, err := DecodeLoadingImage(data.Bytes()[:len(data.Bytes())/2]); err == nil {
			t.Fatal("truncated image accepted")
		}
	}
	var tooWide bytes.Buffer
	if err := png.Encode(&tooWide, image.NewRGBA(image.Rect(0, 0, 4097, 1))); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{[]byte("invalid"), make([]byte, (8<<20)+1), tooWide.Bytes()} {
		if _, err := DecodeLoadingImage(data); err == nil {
			t.Fatal("invalid loading image accepted")
		}
	}
	if got, err := DecodeLoadingImage(nil); got != nil || err != nil {
		t.Fatal("empty image should use default")
	}
}
