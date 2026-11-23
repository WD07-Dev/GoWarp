package render

import (
	"gowarp/core/media"
	"gowarp/core/vm"
	"image/color"
	"testing"
)

func TestTintBeforeScratchEffects(t *testing.T) {
	source := color.NRGBA{R: 200, G: 100, B: 80, A: 255}
	for _, effects := range []vm.GraphicEffects{
		{Tint: &[3]float64{0.5, 1, 0.25}},
		{Tint: &[3]float64{0.5, 1, 0.25}, Color: 40, Brightness: 20, Ghost: 50},
	} {
		neutral := effects
		neutral.Tint = nil
		want := effectColor(color.NRGBA{R: 100, G: 100, B: 20, A: 255}, neutral)
		if got := effectColor(source, effects); got != want {
			t.Errorf("got %v; want %v", got, want)
		}
	}
}

func TestTintAppliesToSceneAndAffinePenStamp(t *testing.T) {
	r := New(&media.Library{Costumes: [][]*media.Costume{{solid(color.NRGBA{R: 200, G: 100, B: 80, A: 255})}}})
	target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, Effects: vm.GraphicEffects{Tint: &[3]float64{0.5, 1, 0.25}}}}
	want := color.RGBA{R: 100, G: 100, B: 20, A: 255}
	if got := r.Draw([]vm.DisplayTarget{target}).RGBAAt(240, 180); got != want {
		t.Fatalf("sprite tint = %v; want %v", got, want)
	}
	r.Pen(vm.PenCommand{Kind: "stamp", Target: target})
	if got := r.Draw(nil).RGBAAt(240, 180); got != want {
		t.Fatalf("stamp tint = %v; want %v", got, want)
	}
}
