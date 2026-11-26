package desktop

import (
	"bytes"
	"image"
	"image/png"

	"gowarp/core/vm"

	"github.com/hajimehoshi/ebiten/v2"
)

func (p *penGPU) Snapshot(scene []vm.DisplayTarget) ([]byte, error) {
	width, height := p.Renderer.Size()
	frame := ebiten.NewImage(width, height)
	defer frame.Deallocate()
	if err := p.gpu.Draw(frame, p.Renderer, scene); err != nil {
		return nil, err
	}
	pixels := image.NewRGBA(image.Rect(0, 0, width, height))
	frame.ReadPixels(pixels.Pix)
	var out bytes.Buffer
	if err := png.Encode(&out, pixels); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
