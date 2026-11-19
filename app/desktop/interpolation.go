package desktop

import (
	"math"

	"gowarp/core/vm"
)

// Interpolation owns its output; authoritative VM snapshots are never modified.
type interpolationBuffer struct {
	indices map[[2]int]int
	scene   []vm.DisplayTarget
}

func (buffer *interpolationBuffer) interpolate(previous, current []vm.DisplayTarget, alpha float64) []vm.DisplayTarget {
	alpha = math.Max(0, math.Min(1, alpha))
	if cap(buffer.scene) < len(current) || cap(buffer.scene) > max(64, 4*len(current)) {
		buffer.scene = make([]vm.DisplayTarget, len(current))
	} else {
		clear(buffer.scene[min(len(current), len(buffer.scene)):])
		buffer.scene = buffer.scene[:len(current)]
	}
	copy(buffer.scene, current)
	if len(buffer.indices) > max(64, 4*len(previous)) {
		buffer.indices = nil
	}
	indexed := false
	for i := range buffer.scene {
		s := &buffer.scene[i].State
		if !s.Visible || s.IsStage {
			continue
		}
		key := [2]int{buffer.scene[i].Source, s.CloneID}
		index := i
		// Reuse stable layer order; build a lookup only after reordering.
		if index >= len(previous) || previous[index].Source != key[0] || previous[index].State.CloneID != key[1] {
			if !indexed {
				if buffer.indices == nil {
					buffer.indices = make(map[[2]int]int, len(previous))
				} else {
					clear(buffer.indices)
				}
				for j := range previous {
					t := &previous[j]
					buffer.indices[[2]int{t.Source, t.State.CloneID}] = j
				}
				indexed = true
			}
			var ok bool
			index, ok = buffer.indices[key]
			if !ok {
				continue
			}
		}
		p := &previous[index].State
		if !p.Visible || p.CurrentCostume != s.CurrentCostume || math.Hypot(s.X-p.X, s.Y-p.Y) > 200 {
			continue
		}
		s.X = p.X + (s.X-p.X)*alpha
		s.Y = p.Y + (s.Y-p.Y)*alpha
		s.Size = p.Size + (s.Size-p.Size)*alpha
		delta := math.Mod(s.Direction-p.Direction+540, 360) - 180
		s.Direction = p.Direction + delta*alpha
	}
	return buffer.scene
}
