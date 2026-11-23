package assets

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIconNormalizesAndEmbedsImage(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "icon.png")
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := LoadIcon(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	icon, err := DecodeIcon(data)
	if err != nil {
		t.Fatal(err)
	}
	if icon.Bounds() != image.Rect(0, 0, 256, 256) {
		t.Fatal(icon.Bounds())
	}
	_, _, _, alpha := icon.At(128, 10).RGBA()
	red, _, _, centerAlpha := icon.At(128, 128).RGBA()
	if alpha != 0 || red != 65535 || centerAlpha != 65535 {
		t.Fatal("aspect ratio or transparency was lost")
	}
	images, err := IconImages(data)
	if err != nil || len(images) != 4 {
		t.Fatalf("window icons: %d, %v", len(images), err)
	}
}

func TestIconRejectsInvalidOrOversizedData(t *testing.T) {
	for _, data := range [][]byte{[]byte("not an image"), make([]byte, (512<<10)+1)} {
		if _, err := DecodeIcon(data); err == nil {
			t.Fatal("invalid embedded icon accepted")
		}
	}
	var large bytes.Buffer
	if err := png.Encode(&large, image.NewRGBA(image.Rect(0, 0, 257, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeIcon(large.Bytes()); err == nil {
		t.Fatal("oversized embedded dimensions accepted")
	}
}
