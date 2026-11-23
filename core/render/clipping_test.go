package render

import (
	"image/color"
	"testing"

	"gowarp/core/media"
	"gowarp/core/vm"
)

func TestHyperClipInvisibleBox(t *testing.T) {
	for _, alpha := range []uint8{255, 0} {
		r := New(&media.Library{
			Costumes: [][]*media.Costume{
				{solid(color.NRGBA{A: alpha})},
				{solid(color.NRGBA{R: 255, A: 255})},
			},
		})
		v := r.DisplayView()
		v.Color, v.MaskTarget = "#000000", "battle//box"
		r.SetView(v)
		base := vm.DisplayTarget{State: vm.TargetState{
			Name:      v.MaskTarget,
			Visible:   true,
			Size:      100,
			StretchX:  100,
			StretchY:  100,
			Direction: 90,
			Effects:   vm.GraphicEffects{Ghost: 100},
		}}
		bone := base
		bone.Source = 1
		bone.State.Name = "bone"
		bone.State.X = 1
		bone.State.Effects.Ghost = 0
		bone.State.MaskMode = true
		frame := r.Draw([]vm.DisplayTarget{base, bone})
		if got := frame.RGBAAt(240, 180); got.R != 255 {
			t.Fatalf("invisible mask (costume alpha %d) hid bone: %v", alpha, got)
		}
		if got := frame.RGBAAt(242, 180); got.R != 0 {
			t.Fatalf("pixel outside mask (costume alpha %d) = %v, want red=0", alpha, got)
		}
		base.State.Visible = false
		if got := r.Draw([]vm.DisplayTarget{base, bone}).RGBAAt(240, 180); got.R != 0 {
			t.Fatalf("hidden mask (costume alpha %d) still drew bone: %v", alpha, got)
		}
	}
}

func TestAdditivePenPreservesRGBAboveAlpha(t *testing.T) {
	r := New(&media.Library{Costumes: [][]*media.Costume{{solid(color.NRGBA{R: 100, A: 255})}}})
	v := r.DisplayView()
	v.Color = "#000000"
	r.SetView(v)
	target := vm.DisplayTarget{State: vm.TargetState{
		Size:      100,
		StretchX:  100,
		StretchY:  100,
		Direction: 90,
		BlendMode: "additive",
	}}
	r.Pen(vm.PenCommand{Kind: "stamp", Target: target})
	c := r.Draw(nil).RGBAAt(240, 180)
	if c.R != 100 {
		t.Fatalf("additive RGB lost over transparent pen: %v", c)
	}
	if got := r.DisplayPenComposite().RGBAAt(240, 180); got.A != 0 || got.R != 100 {
		t.Fatalf("pen pixel = %v, want red=100 and alpha=0", got)
	}
	target.State.BlendMode = "default"
	target.State.Effects.Ghost = 50
	r.Pen(vm.PenCommand{Kind: "stamp", Target: target})
	if got := r.Draw(nil).RGBAAt(240, 180); got.R < 99 || got.R > 101 {
		t.Fatalf("source-over after additive: pixel = %v, want red in [99, 101]", got)
	}
	r.Pen(vm.PenCommand{Kind: "clear"})
	if r.DisplayPenComposite() != nil {
		t.Fatal("clear retained composite")
	}
}

func TestPenScissorTruncatesFlippedOrigin(t *testing.T) {
	view := vm.View{Width: 480, Height: 360}
	box := &vm.Bounds{Left: -1, Right: 1, Bottom: -1.75, Top: .25}
	pen := PenClipRectangle(box, view, 480, 360)
	if pen.Min.Y != 179 || pen.Dy() != 2 {
		t.Fatalf("pen scissor = %v, want top=179 and height=2", pen)
	}
	stage := ClipRectangle(box, view, 480, 360)
	if stage.Min.Y != 180 || stage.Dy() != 2 {
		t.Fatalf("stage scissor = %v, want top=180 and height=2", stage)
	}
}
