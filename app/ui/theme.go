package ui

import (
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"gowarp/app/assets"
)

var FontLicense = assets.FontLicense

var Background = color.RGBA{31, 31, 31, 255}

var Field = color.RGBA{19, 19, 19, 255}

var Text = color.RGBA{245, 245, 245, 255}

var Muted = color.RGBA{170, 170, 170, 255}

var Accent = color.RGBA{255, 75, 81, 255}

func Font(size float64) font.Face {
	f, err := opentype.Parse(assets.PretendardRegular)
	if err != nil {
		panic(err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		panic(err)
	}
	return face
}

func Fill(dst draw.Image, r image.Rectangle, c color.Color) {
	draw.Draw(dst, r, image.NewUniform(c), image.Point{}, draw.Src)
}

func Label(dst draw.Image, face font.Face, x, y int, s string, c color.Color) {
	d := font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}
