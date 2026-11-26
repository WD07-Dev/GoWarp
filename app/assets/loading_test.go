package assets

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
	"time"
)

func loadingGIF(t *testing.T, frames int, size image.Point) []byte {
	t.Helper()
	palette := color.Palette{color.Transparent, color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}}
	animation := &gif.GIF{LoopCount: 0, Config: image.Config{Width: size.X, Height: size.Y, ColorModel: palette}}
	for i := range frames {
		frame := image.NewPaletted(image.Rect(0, 0, 1, 1), palette)
		frame.SetColorIndex(0, 0, uint8(i%2+1))
		animation.Image = append(animation.Image, frame)
		animation.Delay = append(animation.Delay, 10)
	}
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, animation); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestDecodeLoadingGIF(t *testing.T) {
	decoded, err := DecodeLoadingImage(loadingGIF(t, 2, image.Pt(4, 3)))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds() != image.Rect(0, 0, 4, 3) {
		t.Fatalf("GIF canvas = %v", decoded.Bounds())
	}
	if got := color.NRGBAModel.Convert(decoded.At(0, 0)); got != (color.NRGBA{255, 0, 0, 255}) {
		t.Fatalf("first frame = %v", got)
	}
	if _, _, _, a := decoded.At(3, 2).RGBA(); a != 0 {
		t.Fatal("transparent canvas became opaque")
	}
}

func TestLoadingGIFTimingAndLoops(t *testing.T) {
	for _, loops := range []int{-1, 0, 1} {
		t.Run(fmt.Sprint(loops), func(t *testing.T) {
			source, err := gif.DecodeAll(bytes.NewReader(loadingGIF(t, 2, image.Pt(1, 1))))
			if err != nil {
				t.Fatal(err)
			}
			source.LoopCount = loops
			source.Delay = []int{5, 15}
			var encoded bytes.Buffer
			if err := gif.EncodeAll(&encoded, source); err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeLoadingImage(encoded.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			start := time.Unix(1, 0)
			for _, ms := range []int{0, 49, 50, 199, 200, 249, 250, 400, 60000} {
				if _, err := decoded.Advance(start.Add(time.Duration(ms) * time.Millisecond)); err != nil {
					t.Fatal(err)
				}
				want := color.NRGBA{255, 0, 0, 255}
				finished := (loops == -1 && ms >= 200) || (loops == 1 && ms >= 400)
				if finished || ms%200 >= 50 {
					want = color.NRGBA{0, 255, 0, 255}
				}
				if got := color.NRGBAModel.Convert(decoded.At(0, 0)); got != want {
					t.Fatalf("at %d ms got %v, want %v", ms, got, want)
				}
			}
		})
	}
}

func TestLoadingGIFDisposal(t *testing.T) {
	palette := color.Palette{color.Transparent, color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}, color.RGBA{0, 0, 255, 255}}
	for _, disposal := range []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalPrevious} {
		source := &gif.GIF{LoopCount: 0, Delay: []int{10, 10, 10}, Disposal: []byte{gif.DisposalNone, disposal, gif.DisposalNone}, Config: image.Config{Width: 3, Height: 1, ColorModel: palette}}
		for i, rect := range []image.Rectangle{image.Rect(0, 0, 3, 1), image.Rect(1, 0, 2, 1), image.Rect(2, 0, 3, 1)} {
			frame := image.NewPaletted(rect, palette)
			for x := rect.Min.X; x < rect.Max.X; x++ {
				frame.SetColorIndex(x, 0, uint8(i+1))
			}
			source.Image = append(source.Image, frame)
		}
		var encoded bytes.Buffer
		if err := gif.EncodeAll(&encoded, source); err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeLoadingImage(encoded.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		start := time.Unix(1, 0)
		decoded.Advance(start)
		if _, err := decoded.Advance(start.Add(200 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		want := color.NRGBA{0, 255, 0, 255}
		if disposal == gif.DisposalBackground {
			want = color.NRGBA{}
		}
		if disposal == gif.DisposalPrevious {
			want = color.NRGBA{255, 0, 0, 255}
		}
		if got := color.NRGBAModel.Convert(decoded.At(1, 0)); got != want {
			t.Fatalf("disposal %d: %v, want %v", disposal, got, want)
		}
		if got := color.NRGBAModel.Convert(decoded.At(2, 0)); got != (color.NRGBA{0, 0, 255, 255}) {
			t.Fatalf("partial frame = %v", got)
		}
		if _, err := decoded.Advance(start.Add(300 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		if got := color.NRGBAModel.Convert(decoded.At(1, 0)); got != (color.NRGBA{255, 0, 0, 255}) {
			t.Fatalf("loop did not reset: %v", got)
		}
	}
}

func TestLoadingGIFLimits(t *testing.T) {
	for _, tc := range []struct {
		frames      int
		size        image.Point
		logo, valid bool
	}{
		{120, image.Pt(512, 512), true, true},
		{121, image.Pt(1, 1), false, false},
		{2, image.Pt(513, 512), true, false},
		{2, image.Pt(1280, 720), false, true},
		{2, image.Pt(1281, 720), false, false},
		{2, image.Pt(1280, 721), false, false},
	} {
		data := loadingGIF(t, tc.frames, tc.size)
		var logo, background []byte
		if tc.logo {
			logo = data
		} else {
			background = data
		}
		if err := ValidateLoadingImages(logo, background); (err == nil) != tc.valid {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	data := loadingGIF(t, 2, image.Pt(4, 3))
	for i := 1; i < len(data); i++ {
		if _, err := DecodeLoadingImage(data[:i]); err == nil {
			t.Fatalf("accepted GIF truncated at byte %d", i)
		}
	}
}

func TestLoadingImagesCombinedMemoryLimit(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	binary.BigEndian.PutUint32(data[16:20], 4096)
	binary.BigEndian.PutUint32(data[20:24], 4096)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if err := ValidateLoadingImages(nil, data); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLoadingImages(data, data); err == nil {
		t.Fatal("accepted two 64 MiB images")
	}
	if err := ValidateLoadingImages(loadingGIF(t, 2, image.Pt(512, 512)), data); err == nil {
		t.Fatal("GIF compositor memory was excluded from the combined limit")
	}
}

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
