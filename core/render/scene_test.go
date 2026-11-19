package render

import (
	"image"
	"image/color"
	"testing"

	"gowarp/core/media"
	"gowarp/core/vm"
)

func solid(c color.NRGBA) *media.Costume {
	im := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			im.SetNRGBA(x, y, c)
		}
	}
	return &media.Costume{
		Image:   im,
		Width:   4,
		Height:  2,
		CenterX: 2,
		CenterY: 1,
	}
}

func TestSceneTransformsLayerAndHit(t *testing.T) {
	lib := &media.Library{Costumes: [][]*media.Costume{{solid(color.NRGBA{R: 255, A: 255})}, {solid(color.NRGBA{B: 255, A: 255})}}}
	renderer := New(lib)
	first := vm.DisplayTarget{
		Source: 0,
		State: vm.TargetState{
			Visible:   true,
			Size:      100,
			StretchX:  100,
			StretchY:  100,
			Direction: 90,
		},
	}
	second := vm.DisplayTarget{
		Source: 1,
		State: vm.TargetState{
			Visible:   true,
			Size:      100,
			StretchX:  100,
			StretchY:  100,
			Direction: 0,
			X:         10,
			Y:         20,
			CloneID:   3,
		},
	}
	scene := []vm.DisplayTarget{first, second}
	im := renderer.Draw(scene)
	if im.RGBAAt(239, 179).R != 255 || im.RGBAAt(250, 159).B != 255 || im.RGBAAt(251, 159).B != 255 || im.RGBAAt(251, 159).R != 255 {
		t.Fatal("sprite transformation incorrect")
	}
	hit, ok := renderer.Hit(scene, 10, 20)
	if !ok || hit.State.CloneID != 3 {
		t.Fatal("hit identity lost")
	}
	second.State.X, second.State.Y = 0, 0
	second.State.Direction = 90
	scene[1] = second
	if got := renderer.Draw(scene).RGBAAt(240, 180); got.B != 255 || got.R != 0 {
		t.Fatal("layer order incorrect")
	}
	scene[1].State.Visible = false
	if got := renderer.Draw(scene).RGBAAt(240, 180); got.R != 255 || got.B != 0 {
		t.Fatal("hidden target rendered")
	}
}

func TestGhostBrightnessAndRotationStyles(t *testing.T) {
	c := solid(color.NRGBA{R: 255, A: 255})
	c.Image.SetNRGBA(0, 0, color.NRGBA{G: 255, A: 255})
	renderer := New(&media.Library{Costumes: [][]*media.Costume{{c}}})
	target := vm.DisplayTarget{
		Source: 0,
		State: vm.TargetState{
			Visible:       true,
			Size:          100,
			StretchX:      100,
			StretchY:      100,
			Direction:     -90,
			RotationStyle: "left-right",
		},
	}
	im := renderer.Draw([]vm.DisplayTarget{target})
	if got := im.RGBAAt(241, 179); got.G != 255 || got.R != 0 {
		t.Fatalf("left-right flip: %v", got)
	}
	target.State.Effects.Ghost = 100
	if got := renderer.Draw([]vm.DisplayTarget{target}).RGBAAt(240, 180); got != (color.RGBA{255, 255, 255, 255}) {
		t.Fatal("ghost not transparent")
	}
	if _, ok := renderer.Hit([]vm.DisplayTarget{target}, 0, 0); !ok {
		t.Fatal("ghost incorrectly disabled hit detection")
	}
}
