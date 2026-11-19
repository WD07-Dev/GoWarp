package desktop

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
)

func clippedImage(dst *ebiten.Image, clip image.Rectangle) *ebiten.Image {
	if clip == dst.Bounds() {
		return dst
	}
	return dst.SubImage(clip).(*ebiten.Image)
}

func spriteBlend(mode string) ebiten.Blend {
	switch mode {
	case "additive":
		return ebiten.Blend{BlendFactorSourceRGB: ebiten.BlendFactorOne, BlendFactorDestinationRGB: ebiten.BlendFactorOne, BlendFactorSourceAlpha: ebiten.BlendFactorZero, BlendFactorDestinationAlpha: ebiten.BlendFactorOne}
	case "multiply":
		return ebiten.Blend{BlendFactorSourceRGB: ebiten.BlendFactorDestinationColor, BlendFactorDestinationRGB: ebiten.BlendFactorOneMinusSourceAlpha, BlendFactorSourceAlpha: ebiten.BlendFactorDestinationColor, BlendFactorDestinationAlpha: ebiten.BlendFactorOneMinusSourceAlpha}
	default:
		return ebiten.BlendSourceOver
	}
}
