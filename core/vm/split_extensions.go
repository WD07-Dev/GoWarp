package vm

import "gowarp/core/extension"

type SplitEffects struct {
	Effects      []extension.ScreenSplit
	ShowOriginal bool
}

func (r *Runtime) SplitEffects() []extension.ScreenSplit {
	view := r.splitEffectsView()
	if view == nil {
		return nil
	}
	return append([]extension.ScreenSplit(nil), view.Effects...)
}

func (r *Runtime) SplitShowsOriginal() bool {
	return r.logic.splitOriginal
}

func (r *Runtime) splitEffectsView() *SplitEffects {
	if len(r.logic.splits) == 0 {
		return nil
	}
	if r.logic.splitView != nil && r.logic.splitElapsed == r.elapsed {
		return r.logic.splitView
	}
	view := &SplitEffects{
		ShowOriginal: r.logic.splitOriginal,
		Effects:      append([]extension.ScreenSplit(nil), r.logic.splits...),
	}

	r.logic.splitElapsed = r.elapsed
	r.logic.splitView = view
	return view
}
