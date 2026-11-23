package desktop

import (
	"github.com/hajimehoshi/ebiten/v2"
	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"
	"image/color"
	"testing"
)

func TestStageGPUNamedPenLayers(t *testing.T) {
	runGPUFrame(t, func() {
		renderer, reference := render.New(&media.Library{}), render.New(&media.Library{})
		view := vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, Color: "#ffffff"}
		renderer.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		graphics, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		pen := func(command vm.PenCommand) { graphics.Pen(command); reference.Pen(command) }
		dst := ebiten.NewImage(64, 48)
		defer dst.Deallocate()
		stage := vm.DisplayTarget{Source: -1, State: vm.TargetState{Visible: true, RenderLayer: vm.StageSurface}}
		front := vm.DisplayTarget{Source: -2, State: vm.TargetState{Visible: true, RenderLayer: "front"}}
		check := func(scene []vm.DisplayTarget, x, y int, want color.RGBA) {
			t.Helper()
			if err := gpu.Draw(dst, renderer, scene); err != nil {
				t.Fatal(err)
			}
			got := color.RGBAModel.Convert(dst.At(x, y)).(color.RGBA)
			if got != want {
				t.Fatalf("GPU pixel (%d,%d)=%v, want %v", x, y, got, want)
			}
			if got := reference.DrawAt(scene, 64, 48).RGBAAt(x, y); got != want {
				t.Fatalf("CPU pixel (%d,%d)=%v, want %v", x, y, got, want)
			}
		}
		red, blue := color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}
		pen(vm.PenCommand{Kind: "line", Line: [4]float64{-20, 0, 20, 0}, Size: 8, Color: color.NRGBA{R: 255, A: 255}})
		pen(vm.PenCommand{Kind: "layer-create", Layer: "front"})
		pen(vm.PenCommand{Kind: "line", Layer: "front", Line: [4]float64{-20, 0, 20, 0}, Size: 8, Color: color.NRGBA{B: 255, A: 255}})
		check([]vm.DisplayTarget{stage, front}, 32, 24, blue)
		check([]vm.DisplayTarget{front, stage}, 32, 24, red)
		front.State.Visible = false
		check([]vm.DisplayTarget{stage, front}, 32, 24, red)
		front.State.Visible = true
		front.State.Transform = &vm.RenderTransform{X: -10, Y: -10, Scale: 1}
		check([]vm.DisplayTarget{stage, front}, 32, 24, red)
		check([]vm.DisplayTarget{stage, front}, 42, 14, blue)
		pen(vm.PenCommand{Kind: "layer-quality", Layer: "front", Size: 2})
		check([]vm.DisplayTarget{stage, front}, 42, 14, blue)
		pen(vm.PenCommand{Kind: "clear", Layer: "front"})
		check([]vm.DisplayTarget{stage, front}, 32, 24, red)
		check([]vm.DisplayTarget{stage, front}, 42, 14, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		pen(vm.PenCommand{Kind: "layer-delete", Layer: "front"})
		if gpu.nativePen.layers["front"] != nil {
			t.Fatal("deleted GPU layer retained")
		}
		pen(vm.PenCommand{Kind: "layer-create", Layer: "front"})
		front.State.Transform = nil
		check([]vm.DisplayTarget{stage, front}, 32, 24, red)
	})
}

func TestStageGPUPenLayerSamplesRootTextureBeforeMutation(t *testing.T) {
	runGPUFrame(t, func() {
		for _, history := range []bool{false, true} {
			for _, depth := range []bool{false, true} {
				renderer := render.New(&media.Library{})
				renderer.SetView(vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90, Color: "#ffffff"})
				var gpu stageGPU
				pen, err := gpu.enablePen(renderer)
				if err != nil {
					t.Fatal(err)
				}
				pen.keepHistory = history
				pen.Pen(vm.PenCommand{Kind: "layer-create", Layer: "paper"})
				pen.Pen(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "texture", Width: 16, Height: 16}})
				pen.Pen(surfaceTestTriangle("texture", 0, 1, 0, 1))
				triangle := surfaceTestTriangle(vm.StageSurface, 1, 1, 1, 1)
				triangle.Layer = "paper"
				triangle.Surface.Textured = true
				triangle.Surface.Texture = "texture"
				for i := range triangle.Surface.Vertices {
					triangle.Surface.Vertices[i].U = .5
					triangle.Surface.Vertices[i].V = .5
					if depth {
						triangle.Surface.Vertices[i].Z = 0
					}
				}
				pen.Pen(triangle)
				pen.Pen(vm.PenCommand{Kind: "surface-clear", Surface: &vm.PenSurfaceCommand{Name: "texture"}})
				pen.Pen(vm.PenCommand{Kind: "surface-delete", Surface: &vm.PenSurfaceCommand{Name: "texture"}})
				child := pen.layers["paper"]
				child.flush()
				want := color.NRGBA{G: 255, A: 255}
				pixels, _ := child.DisplayPen()
				if pixels == nil || pixels.NRGBAAt(16, 12) != want {
					t.Fatalf("history=%v depth=%v CPU replay lost original texture", history, depth)
				}
				if got := color.RGBAModel.Convert(child.image.At(16, 12)); got != (color.RGBA{G: 255, A: 255}) {
					t.Fatalf("history=%v depth=%v GPU sample=%v", history, depth, got)
				}
				if len(child.surfaces) != 0 {
					t.Fatal("paper duplicated root GPU texture registry")
				}
				pen.Pen(vm.PenCommand{Kind: "layer-delete", Layer: "paper"})
				// A deleted child must not release shaders shared by root or sibling layers.
				pen.Pen(surfaceTestTriangle(vm.StageSurface, 1, 0, 0, 1))
				pen.flush()
				if got := color.RGBAModel.Convert(pen.image.At(16, 12)); got != (color.RGBA{R: 255, A: 255}) {
					t.Fatal("child deletion broke root shader", got)
				}
				gpu.Close()
			}
		}
	})
}
