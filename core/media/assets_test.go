package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"gowarp/core/project"
)

func BenchmarkDecodeTransparentBitmap(b *testing.B) {
	img := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 200, 80, 40, 128
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		b.Fatal(err)
	}
	meta := project.Costume{DataFormat: "png", BitmapResolution: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		costume, err := DecodeCostume(meta, encoded.Bytes())
		if err != nil {
			b.Fatal(err)
		}
		if costume.Image.NRGBAAt(256, 256) != (color.NRGBA{200, 80, 40, 128}) {
			b.Fatal("decoded pixel changed")
		}
	}
}

func TestBitmapResolutionAndSVGOrigin(t *testing.T) {
	var data bytes.Buffer
	im := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	im.SetNRGBA(3, 1, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&data, im); err != nil {
		t.Fatal(err)
	}
	c, err := DecodeCostume(project.Costume{
		DataFormat:       "png",
		BitmapResolution: 2,
		RotationCenterX:  4,
		RotationCenterY:  2,
	}, data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if c.Width != 4 || c.Height != 2 || c.CenterX != 2 || c.CenterY != 1 {
		t.Fatalf("geometry %+v", c)
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="10 20 8 4"><rect x="10" y="20" width="8" height="4" fill="#ff0000"/></svg>`)
	c, err = DecodeCostume(project.Costume{
		DataFormat:      "svg",
		RotationCenterX: 14,
		RotationCenterY: 22,
	}, svg)
	if err != nil {
		t.Fatal(err)
	}
	if c.Width != 8 || c.Height != 4 || c.CenterX != 4 || c.CenterY != 2 || c.Image.NRGBAAt(2, 2).R != 255 {
		t.Fatalf("SVG geometry/pixels %+v", c)
	}
}

func TestRejectOversizedAndUnsupportedSVG(t *testing.T) {
	for _, svg := range []string{
		`<svg viewBox="0 0 999999 999999"/>`,
		`<svg viewBox="0 0 10 10"><foreignObject>unsupported</foreignObject></svg>`,
	} {
		if _, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(svg)); err == nil {
			t.Fatal("unsafe or unsupported SVG silently accepted")
		}
	}
}

func TestDecodeWAVAndPCMStream(t *testing.T) {
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(44))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint32(8000))
	binary.Write(&b, binary.LittleEndian, uint32(16000))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(8))
	for _, sample := range []int16{16384, 16384, -16384, -16384} {
		binary.Write(&b, binary.LittleEndian, sample)
	}
	sound, err := DecodeSound("wav", b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if sound.Rate != 8000 || len(sound.Samples) != 8 || sound.Duration() != .0005 {
		t.Fatalf("sound %+v", sound)
	}
	stream := NewStream(sound, 8000)
	stream.Set(100, 120, -100)
	out := make([]byte, 16)
	n, err := stream.Read(out)
	if err != nil || n != 16 {
		t.Fatalf("read %d %v", n, err)
	}
	if math.Float32frombits(binary.LittleEndian.Uint32(out[4:8])) != 0 || math.Float32frombits(binary.LittleEndian.Uint32(out[12:16])) != 0 {
		t.Fatal("pan did not silence right channel")
	}
	if binary.LittleEndian.Uint32(out[:4]) != 0x3f000000 || binary.LittleEndian.Uint32(out[8:12]) != 0xbf000000 {
		t.Fatalf("pitch/volume samples %x", out)
	}
}

func TestSVGPercentageGeometry(t *testing.T) {
	c, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(`<svg width="480" height="360"><rect width="100%" height="100%" fill="#102030"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Image.NRGBAAt(479, 359); got != (color.NRGBA{
		R: 16,
		G: 32,
		B: 48,
		A: 255,
	}) {
		t.Fatalf("percentage background pixel: %v", got)
	}
	for _, svg := range []string{
		`<svg width="40" height="40"><rect width="bad" height="10"/></svg>`,
		`<svg width="40" height="40"><circle r="NaN"/></svg>`,
	} {
		if _, err := DecodeCostume(project.Costume{DataFormat: "svg"}, []byte(svg)); err == nil {
			t.Fatal("invalid geometry silently accepted")
		}
	}
}

func TestRejectExcessiveLogicalBitmapSize(t *testing.T) {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeCostume(project.Costume{DataFormat: "png", BitmapResolution: 1e-10}, b.Bytes()); err == nil {
		t.Fatal("tiny bitmap resolution permits unbounded geometry scans")
	}
	if _, err := DecodeCostume(project.Costume{DataFormat: "png", BitmapResolution: .5}, b.Bytes()); err != nil {
		t.Fatalf("ordinary fractional resolution rejected: %v", err)
	}
}
