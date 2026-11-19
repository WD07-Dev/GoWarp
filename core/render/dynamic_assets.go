package render

import (
	"bytes"
	"fmt"
	"image/png"

	"gowarp/core/project"
	"gowarp/core/vm"
)

func (r *Renderer) AddCostume(source int, meta project.Costume, data []byte) (project.Costume, error) {
	if source < 0 || source >= len(r.Assets.Costumes) {
		return meta, fmt.Errorf("costume source out of range")
	}
	costume, err := r.Assets.DecodeCostume(meta, data, !r.view.NoAntialias)
	if err != nil {
		return meta, err
	}
	// Imports without an explicit rotation center are centered on the image.
	meta.RotationCenterX = costume.Width/2 - costume.CenterX
	meta.RotationCenterY = costume.Height/2 - costume.CenterY
	costume.CenterX, costume.CenterY = costume.Width/2, costume.Height/2
	r.Assets.Costumes[source] = append(r.Assets.Costumes[source], costume)
	return meta, nil
}

func (r *Renderer) DeleteCostume(source, index int) error {
	if source < 0 || source >= len(r.Assets.Costumes) || index < 0 || index >= len(r.Assets.Costumes[source]) {
		return fmt.Errorf("costume index out of range")
	}
	costumes := r.Assets.Costumes[source]
	delete(r.hulls, costumes[index])
	copy(costumes[index:], costumes[index+1:])
	costumes[len(costumes)-1] = nil
	r.Assets.Costumes[source] = costumes[:len(costumes)-1]
	return nil
}

func (r *Renderer) Snapshot(scene []vm.DisplayTarget) ([]byte, error) {
	var out bytes.Buffer
	if err := png.Encode(&out, r.Draw(scene)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
