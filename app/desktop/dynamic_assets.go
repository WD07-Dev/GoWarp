package desktop

import (
	"bytes"
	"image"
	"image/png"

	"github.com/hajimehoshi/ebiten/v2"
	"gowarp/core/vm"
)

// Capture the current scene, including pending pen commands and stage shaders, as an importable PNG.
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
