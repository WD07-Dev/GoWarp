package render

import (
	"image/color"
	"math"
	"testing"

	"gowarp/core/media"
	"gowarp/core/vm"
)

func TestNamedPenLayersKeepIndependentContents(t *testing.T) {
	r := New(&media.Library{Costumes: [][]*media.Costume{{solid(color.NRGBA{G: 255, A: 255})}}})
	r.Pen(vm.PenCommand{Kind: "layer-create", Layer: "front"})
	r.Pen(vm.PenCommand{Kind: "line", Size: 20, Color: color.NRGBA{R: 255, A: 255}})
	r.Pen(vm.PenCommand{Kind: "line", Layer: "front", Size: 10, Color: color.NRGBA{B: 255, A: 255}})
	sprite := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
	layer := vm.DisplayTarget{Source: -1, State: vm.TargetState{Visible: true, RenderLayer: "front"}}
	frame := r.Draw([]vm.DisplayTarget{sprite, layer})
	if got := frame.RGBAAt(240, 180); got.B != 255 {
		t.Fatalf("front layer = %v", got)
	}
	if !r.TouchingColor([]vm.DisplayTarget{sprite, layer}, sprite, color.NRGBA{B: 255, A: 255}, nil) {
		t.Fatal("named pen layer was invisible to color sensing")
	}
	r.Pen(vm.PenCommand{Kind: "clear", Layer: "front"})
	frame = r.Draw([]vm.DisplayTarget{sprite, layer})
	if got := frame.RGBAAt(240, 180); got.G != 255 {
		t.Fatalf("clear erased sprite or wrong layer = %v", got)
	}
	if got := r.PenSurface(vm.StageSurface).NRGBAAt(246, 180); got.R != 255 {
		t.Fatalf("clear erased default pen = %v", got)
	}
}

func TestCameraTransformDoesNotMoveLogicalTarget(t *testing.T) {
	r := New(&media.Library{Costumes: [][]*media.Costume{{solid(color.NRGBA{R: 255, A: 255})}}})
	target := vm.DisplayTarget{State: vm.TargetState{X: 20, Y: 10, Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90,
		Transform: &vm.RenderTransform{X: 10, Y: 5, Scale: 2, Rotation: 90}}}
	tr, ok := r.transformGeometry(target)
	if !ok || math.Abs(tr.x+10) > 1e-9 || tr.y != 20 || tr.sx != 2 {
		t.Fatalf("camera transform = %+v (%v)", tr, ok)
	}
	if target.State.X != 20 || target.State.Y != 10 {
		t.Fatal("camera changed logical coordinates")
	}
}

func TestNamedPenLayerSamplesRootTexture(t *testing.T) {
	r := New(&media.Library{})
	r.Pen(vm.PenCommand{Kind: "layer-create", Layer: "paper"})
	r.Pen(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "texture", Width: 4, Height: 4}})
	surfaceQuad(r, "texture", -2, -2, 2, 2, color.NRGBA{G: 255, A: 255}, "", false)
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	vertices := [3]vm.PenVertex{surfaceVertex(-8, -8, .5, .5, white), surfaceVertex(8, -8, .5, .5, white), surfaceVertex(0, 8, .5, .5, white)}
	r.Pen(vm.PenCommand{Kind: "surface-triangle", Layer: "paper", Surface: &vm.PenSurfaceCommand{Name: vm.StageSurface, Texture: "texture", Textured: true, Vertices: vertices}})
	r.Pen(vm.PenCommand{Kind: "surface-delete", Surface: &vm.PenSurfaceCommand{Name: "texture"}})
	layer := r.PenLayer("paper")
	if got := layer.penAt(0, 0); got != (color.NRGBA{G: 255, A: 255}) {
		t.Fatalf("paper lost sampled texture: %v", got)
	}
	if len(layer.penSurfaces) != 0 {
		t.Fatal("paper duplicated root texture registry")
	}
}
