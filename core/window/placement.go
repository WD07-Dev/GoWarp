package window

import (
	"fmt"
	"image"
	"slices"
)

type Monitor struct {
	X          int `json:"x"`
	Y          int `json:"y"`
	Width      int `json:"width"`
	Height     int `json:"height"`
	WorkX      int `json:"workX"`
	WorkY      int `json:"workY"`
	WorkWidth  int `json:"workWidth"`
	WorkHeight int `json:"workHeight"`
}

func Center(views []View, outer []image.Rectangle, work image.Rectangle, id string, all bool) ([]View, error) {
	if len(outer) != len(views) || work.Empty() {
		return nil, fmt.Errorf("invalid window or monitor bounds")
	}
	anchor := slices.IndexFunc(views, func(v View) bool { return v.ID == id })
	if anchor < 0 {
		return nil, fmt.Errorf("unknown native window %q", id)
	}
	var bounds image.Rectangle
	selected := make([]bool, len(views))
	for i, v := range views {
		if !all && i != anchor {
			continue
		}
		if v.Fullscreen || v.State.Minimized || v.State.Maximized {
			if !all {
				return nil, fmt.Errorf("restore window %q before centering", id)
			}
			continue
		}
		if all && v.Hidden {
			continue
		}
		if outer[i].Empty() {
			return nil, fmt.Errorf("missing outer bounds for window %q", v.ID)
		}
		selected[i] = true
		bounds = bounds.Union(outer[i])
	}
	result := slices.Clone(views)
	if bounds.Empty() {
		return result, nil
	}
	dx := work.Min.X + max(0, (work.Dx()-bounds.Dx())/2) - bounds.Min.X
	dy := work.Min.Y + max(0, (work.Dy()-bounds.Dy())/2) - bounds.Min.Y
	for i := range result {
		if selected[i] {
			result[i].X += dx
			result[i].Y += dy
		}
	}
	if _, err := Pack(result); err != nil {
		return nil, err
	}
	return result, nil
}
