package vm

import "gowarp/core/extension"

// Renderer-owned split snapshots; animation policy lives in the Go module.
type logicState struct {
	splits        []extension.ScreenSplit
	splitOriginal bool
	splitView     *SplitEffects
	splitElapsed  float64
}
