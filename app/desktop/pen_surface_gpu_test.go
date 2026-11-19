package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"
)

func checkSurfaceStage(t *testing.T, pen *penGPU, reference *render.Renderer) {
	t.Helper()
	pen.flushStamps()
	pen.flushLines()
	pen.flushClear([4]*ebiten.Image{})
	want := reference.DisplayPenComposite()
	if want == nil {
		w, h := reference.Size()
		want = image.NewRGBA(image.Rect(0, 0, w, h))
		if pixels, _ := reference.DisplayPen(); pixels != nil {
			draw.Draw(want, want.Bounds(), pixels, pixels.Bounds().Min, draw.Src)
		}
	}
	if pen.image == nil || want == nil {
		t.Fatal("missing pen image")
	}
	got := image.NewRGBA(pen.image.Bounds())
	pen.image.ReadPixels(got.Pix)
	if got.Bounds() != want.Bounds() {
		t.Fatalf("bounds: %v != %v", got.Bounds(), want.Bounds())
	}
	for i, channel := range want.Pix {
		if delta := int(got.Pix[i]) - int(channel); delta < -3 || delta > 3 {
			t.Fatalf("pixel (%d,%d) channel %d: GPU %d CPU %d", (i/4)%got.Rect.Dx(), (i/4)/got.Rect.Dx(), i%4, got.Pix[i], channel)
		}
	}
}

func surfaceTestTriangle(name string, red, green, blue, alpha float64) vm.PenCommand {
	var vertices [3]vm.PenVertex
	for i, xy := range [3][2]float64{{-8, -8}, {8, -8}, {0, 8}} {
		vertices[i] = vm.PenVertex{X: xy[0], Y: xy[1], R: red, G: green, B: blue, A: alpha, Z: -1, W: 1, U: .25, V: .25}
	}
	return vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{Name: name, Vertices: vertices}}
}

func TestSurfaceHistorySkipsUnreferencedStage(t *testing.T) {
	for _, erase := range []bool{false, true} {
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90})
		commands := []vm.PenCommand{
			{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "effect", Width: 16, Height: 16}},
			{Kind: "line", Line: [4]float64{-8, 0, 8, 0}, Size: 10, Color: color.NRGBA{R: 255, A: 255}},
			surfaceTestTriangle("effect", 0, 1, 0, 1),
		}
		if erase {
			commands[2].Surface.Textured = true
			commands[2].Surface.Texture = vm.StageSurface
			commands = append(commands, vm.PenCommand{Kind: "surface-clear", Surface: &vm.PenSurfaceCommand{Name: "effect"}})
		}
		pen := &penGPU{Renderer: r, pending: commands}
		pen.preserveSurfaceHistory(false)
		if pixels, _ := r.DisplayPen(); pixels != nil {
			t.Fatalf("erase=%v: discarded stage drawing was rasterized on the CPU", erase)
		}
		pixels := r.PenSurface("effect")
		want := color.NRGBA{G: 255, A: 255}
		if erase {
			want = color.NRGBA{}
		}
		if pixels == nil || pixels.NRGBAAt(8, 8) != want {
			t.Fatalf("erase=%v: named surface lost its final contents", erase)
		}
	}
}

func TestSurfaceHistoryKeepsTransitiveStageCapture(t *testing.T) {
	r, reference := render.New(&media.Library{}), render.New(&media.Library{})
	view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90}
	r.SetView(view)
	reference.SetView(view)
	commands := []vm.PenCommand{
		{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "first", Width: 16, Height: 16}},
		{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "second", Width: 16, Height: 16}},
		{Kind: "line", Line: [4]float64{-8, 0, 8, 0}, Size: 10, Color: color.NRGBA{R: 255, A: 255}},
	}
	for _, pair := range [][2]string{{"first", vm.StageSurface}, {"second", "first"}} {
		cmd := surfaceTestTriangle(pair[0], 1, 1, 1, 1)
		cmd.Surface.Textured, cmd.Surface.Texture = true, pair[1]
		for i := range cmd.Surface.Vertices {
			cmd.Surface.Vertices[i].U, cmd.Surface.Vertices[i].V = .5, .5
		}
		commands = append(commands, cmd)
	}
	commands = append(commands, vm.PenCommand{Kind: "surface-delete", Surface: &vm.PenSurfaceCommand{Name: "first"}})
	for _, cmd := range commands {
		reference.Pen(cmd)
	}
	pen := &penGPU{Renderer: r, pending: commands}
	pen.preserveSurfaceHistory(false)
	got, want := r.PenSurface("second"), reference.PenSurface("second")
	if got == nil || want == nil || !bytes.Equal(got.Pix, want.Pix) || got.NRGBAAt(8, 8).R != 255 {
		t.Fatal("deleting the intermediate surface lost the captured stage pixels")
	}
	if r.PenSurface("first") != nil {
		t.Fatal("deleted surface was restored")
	}
}

func TestSurfaceCheckpointPreservesDepthWithoutDrawingColors(t *testing.T) {
	for _, alpha := range []float64{0, 0.5, 1} {
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90})
		pen := &penGPU{Renderer: r, pending: []vm.PenCommand{surfaceTestTriangle(vm.StageSurface, 1, 0, 0, alpha)}}
		pen.preserveSurfaceHistory(true)
		if r.PenSurface(vm.StageSurface) != nil {
			t.Fatalf("alpha=%v: depth checkpoint allocated and painted stage colors", alpha)
		}
		far := surfaceTestTriangle(vm.StageSurface, 0, 0, 1, 1)
		for i := range far.Surface.Vertices {
			far.Surface.Vertices[i].Z = 0
		}
		r.Pen(far)
		pixels := r.PenSurface(vm.StageSurface)
		if alpha == 0 {
			if pixels == nil || pixels.NRGBAAt(16, 12) != (color.NRGBA{B: 255, A: 255}) {
				t.Fatal("transparent checkpoint occluded a later triangle")
			}
		} else if pixels != nil {
			t.Fatal("checkpoint lost depth; farther triangle was drawn")
		}
		r.Pen(vm.PenCommand{Kind: "clear"})
		r.Pen(far)
		if pixels := r.PenSurface(vm.StageSurface); pixels == nil || pixels.NRGBAAt(16, 12).B != 255 {
			t.Fatal("clear retained checkpoint depth")
		}
	}
}

func BenchmarkSurfaceCheckpointDepth(b *testing.B) {
	r := render.New(&media.Library{})
	r.SetView(vm.View{Width: 256, Height: 256, Zoom: 100, Direction: 90})
	cmd := surfaceTestTriangle(vm.StageSurface, 1, 0, 0, 0.5)
	for i := range cmd.Surface.Vertices {
		cmd.Surface.Vertices[i].X *= 16
		cmd.Surface.Vertices[i].Y *= 16
	}
	pen := &penGPU{Renderer: r, pending: make([]vm.PenCommand, 0, 1)}
	b.ReportAllocs()
	for b.Loop() {
		pen.pending = append(pen.pending, cmd)
		pen.preserveSurfaceHistory(true)
	}
}

func TestSurfaceCheckpointKeepsStageCaptureBeforeDepthOnlyDraw(t *testing.T) {
	r := render.New(&media.Library{})
	r.SetView(vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90})
	capture := surfaceTestTriangle("capture", 1, 1, 1, 1)
	capture.Surface.Textured, capture.Surface.Texture = true, vm.StageSurface
	for i := range capture.Surface.Vertices {
		capture.Surface.Vertices[i].U, capture.Surface.Vertices[i].V = 0.5, 0.5
	}
	pen := &penGPU{Renderer: r, pending: []vm.PenCommand{
		{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "capture", Width: 16, Height: 16}},
		surfaceTestTriangle(vm.StageSurface, 1, 0, 0, 1),
		capture,
		surfaceTestTriangle(vm.StageSurface, 0, 0, 1, 1),
	}}
	pen.preserveSurfaceHistory(true)
	if pixels := r.PenSurface("capture"); pixels == nil || pixels.NRGBAAt(8, 8) != (color.NRGBA{R: 255, A: 255}) {
		t.Fatal("checkpoint lost the stage colors needed by a named texture")
	}
	if pixels := r.PenSurface(vm.StageSurface); pixels == nil || pixels.NRGBAAt(16, 12) != (color.NRGBA{R: 255, A: 255}) {
		t.Fatal("checkpoint repainted stage colors after the final capture")
	}
}

func TestPenSurfaceHistoryLimitUsesGPUStage(t *testing.T) {
	runGPUFrame(t, func() {
		for _, scale := range []int{1, 2} {
			r, reference := render.New(&media.Library{}), render.New(&media.Library{})
			view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90, HighQualityPen: true}
			r.SetView(view)
			reference.SetView(view)
			var gpu stageGPU
			pen, err := gpu.enablePen(r)
			if err != nil {
				t.Fatal(err)
			}
			pen.keepHistory = false
			pen.setOutputSize(32*scale, 24*scale)
			draw := func(cmd vm.PenCommand) { pen.Pen(cmd); reference.Pen(cmd) }
			draw(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "capture", Width: 16, Height: 16}})
			draw(surfaceTestTriangle("capture", 0, 1, 0, 1))
			draw(surfaceTestTriangle(vm.StageSurface, 0, 0, 1, .5))
			_, before := r.DisplayPen()
			line := vm.PenCommand{Kind: "line", Line: [4]float64{-8, 0, 8, 0}, Size: 10, Color: color.NRGBA{R: 255, A: 255}}
			for range pendingPenLimit - 2 {
				draw(line)
			}
			_, after := r.DisplayPen()
			if after-before > 2 {
				t.Fatalf("history limit rerasterized %d stage operations instead of copying the GPU result", after-before)
			}
			if len(pen.pending) != 0 {
				t.Fatal("history limit retained the full command queue")
			}
			if scale == 1 {
				checkSurfaceStage(t, pen, reference)
			} else {
				pixels := image.NewRGBA(pen.image.Bounds())
				pen.image.ReadPixels(pixels.Pix)
				if pixels.RGBAAt(16*scale, 12*scale) != (color.RGBA{R: 255, A: 255}) {
					t.Fatal("scaled checkpoint changed the stage")
				}
			}
			if got := r.PenSurface("capture").NRGBAAt(8, 8); got != (color.NRGBA{G: 255, A: 255}) {
				t.Fatalf("named surface after checkpoint: %v", got)
			}

			capture := surfaceTestTriangle("capture", 1, 1, 1, 1)
			capture.Surface.Textured, capture.Surface.Texture = true, vm.StageSurface
			for i := range capture.Surface.Vertices {
				capture.Surface.Vertices[i].U, capture.Surface.Vertices[i].V = .5, .5
			}
			draw(capture)
			depthCapture := surfaceTestTriangle("capture", 1, 1, 1, 1)
			depthCapture.Surface.Textured, depthCapture.Surface.Texture = true, vm.StageSurface
			for i := range depthCapture.Surface.Vertices {
				depthCapture.Surface.Vertices[i].U, depthCapture.Surface.Vertices[i].V = .5, .5
				depthCapture.Surface.Vertices[i].Z = .5
			}
			draw(depthCapture)
			if got := r.PenSurface("capture").NRGBAAt(8, 8); got != (color.NRGBA{R: 255, A: 255}) {
				t.Fatalf("scale=%d: CPU capture after checkpoint: %v", scale, got)
			}
			draw(vm.PenCommand{Kind: "clear"})
			if got := r.PenSurface("capture").NRGBAAt(8, 8); got != (color.NRGBA{R: 255, A: 255}) {
				t.Fatalf("stage capture after checkpoint: %v", got)
			}
			depth := surfaceTestTriangle(vm.StageSurface, 0, 0, 1, 1)
			for i := range depth.Surface.Vertices {
				depth.Surface.Vertices[i].Z = .5
			}
			draw(depth)
			if scale == 1 {
				checkSurfaceStage(t, pen, reference)
			}
			gpu.Close()
		}
	})
}

func TestPenHistoryCheckpointDefersReadback(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		pen.Pen(surfaceTestTriangle(vm.StageSurface, 0, 1, 0, 1))
		line := vm.PenCommand{Kind: "line", Line: [4]float64{-8, 0, 8, 0}, Size: 4, Color: color.NRGBA{R: 255, A: 255}}
		for range pendingPenLimit*2 - 1 {
			pen.Pen(line)
		}
		if len(pen.pending) != 0 {
			t.Fatal("checkpoint retained the command queue")
		}
		if pen.pixels != nil {
			t.Fatal("history limit read GPU pixels without a CPU consumer")
		}
		pen.flush()
		pixels, _ := r.DisplayPen()
		if pixels == nil || pixels.NRGBAAt(16, 12) != (color.NRGBA{R: 255, A: 255}) {
			t.Fatal("CPU consumer did not receive the checkpoint colors")
		}
	})
}

func TestPenHistoryCheckpointDiscardedByClear(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		pen.Pen(surfaceTestTriangle(vm.StageSurface, 0, 1, 0, 1))
		line := vm.PenCommand{Kind: "line", Line: [4]float64{-8, 0, 8, 0}, Size: 4, Color: color.NRGBA{R: 255, A: 255}}
		for range pendingPenLimit - 1 {
			pen.Pen(line)
		}
		pen.Pen(vm.PenCommand{Kind: "clear"})
		if pen.pixels != nil {
			t.Fatal("discarded checkpoint was read back")
		}
		pen.flush()
		if pixels, _ := r.DisplayPen(); pixels != nil && pixels.NRGBAAt(16, 12).A != 0 {
			t.Fatal("clear restored old checkpoint colors")
		}
	})
}

func TestPenCheckpointPreservesStageCapture(t *testing.T) {
	runGPUFrame(t, func() {
		for _, clearStage := range []bool{false, true} {
			r := render.New(&media.Library{})
			r.SetView(vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90})
			var gpu stageGPU
			pen, err := gpu.enablePen(r)
			if err != nil {
				t.Fatal(err)
			}
			pen.keepHistory = false
			pen.Pen(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "capture", Width: 16, Height: 16}})
			pen.Pen(surfaceTestTriangle(vm.StageSurface, 0, 1, 0, 1))
			line := vm.PenCommand{Kind: "line", Line: [4]float64{-8, 0, 8, 0}, Size: 4, Color: color.NRGBA{R: 255, A: 255}}
			for range pendingPenLimit - 1 {
				pen.Pen(line)
			}
			capture := surfaceTestTriangle("capture", 1, 1, 1, 1)
			capture.Surface.Textured, capture.Surface.Texture = true, vm.StageSurface
			for i := range capture.Surface.Vertices {
				capture.Surface.Vertices[i].U, capture.Surface.Vertices[i].V = .5, .5
			}
			pen.Pen(capture)
			line.Color = color.NRGBA{B: 255, A: 255}
			pen.Pen(line)
			if clearStage {
				pen.Pen(vm.PenCommand{Kind: "clear"})
			} else {
				for range pendingPenLimit - 2 {
					pen.Pen(vm.PenCommand{Kind: "surface-clear", Surface: &vm.PenSurfaceCommand{Name: "missing"}})
				}
			}
			pixels := r.PenSurface("capture")
			if pixels == nil || pixels.NRGBAAt(8, 8) != (color.NRGBA{R: 255, A: 255}) {
				t.Fatalf("clear=%v: capture lost the stage as it was before the blue line", clearStage)
			}
			pen.flush()
			pixels, _ = r.DisplayPen()
			want := color.NRGBA{B: 255, A: 255}
			if clearStage {
				want = color.NRGBA{}
			}
			if pixels == nil || pixels.NRGBAAt(16, 12) != want {
				t.Fatalf("clear=%v: checkpoint restore lost the final stage", clearStage)
			}
			gpu.Close()
		}
	})
}

func TestPenCheckpointRestoresBeforeStageResize(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90}
		r.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		pen.Pen(surfaceTestTriangle(vm.StageSurface, 1, 0, 0, 1))
		for range pendingPenLimit - 1 {
			pen.Pen(vm.PenCommand{Kind: "surface-clear", Surface: &vm.PenSurfaceCommand{Name: "missing"}})
		}
		view.Width, view.Height = 64, 48
		pen.SetView(view)
		pixels, _ := r.DisplayPen()
		if pixels == nil || pixels.Bounds() != image.Rect(0, 0, 64, 48) || pixels.NRGBAAt(32, 24) != (color.NRGBA{R: 255, A: 255}) {
			t.Fatal("stage resize lost or rescaled the old logical pen")
		}
	})
}

func TestPenSurfaceHistoryLimitKeepsExactCPUState(t *testing.T) {
	runGPUFrame(t, func() {
		for _, depth := range []bool{false, true} {
			r, reference := render.New(&media.Library{}), render.New(&media.Library{})
			view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90}
			r.SetView(view)
			reference.SetView(view)
			var gpu stageGPU
			pen, err := gpu.enablePen(r)
			if err != nil {
				t.Fatal(err)
			}
			pen.keepHistory = !depth
			draw := func(cmd vm.PenCommand) { pen.Pen(cmd); reference.Pen(cmd) }
			cmd := surfaceTestTriangle(vm.StageSurface, 0, 1, 0, .6)
			if depth {
				for i := range cmd.Surface.Vertices {
					cmd.Surface.Vertices[i].Z = -.5
				}
			}
			draw(cmd)
			line := vm.PenCommand{Kind: "line", Line: [4]float64{-8.3, -6.1, 8.8, 6.7}, Size: 3.5, Color: color.NRGBA{R: 255, A: 31}}
			for range pendingPenLimit {
				draw(line)
			}
			pen.flush()
			got, _ := r.DisplayPen()
			want, _ := reference.DisplayPen()
			if got == nil || !bytes.Equal(got.Pix, want.Pix) {
				t.Fatalf("depth=%v: history limit changed exact CPU pixels", depth)
			}
			if depth {
				far := surfaceTestTriangle(vm.StageSurface, 0, 0, 1, 1)
				for i := range far.Surface.Vertices {
					far.Surface.Vertices[i].Z = .5
				}
				draw(far)
				got, _ = r.DisplayPen()
				want, _ = reference.DisplayPen()
				if !bytes.Equal(got.Pix, want.Pix) {
					t.Fatal("history limit discarded the stage depth buffer")
				}
			}
			gpu.Close()
		}
	})
}

func TestPenSurfaceHistoryLimitPreservesLaterDepthTest(t *testing.T) {
	runGPUFrame(t, func() {
		r, reference := render.New(&media.Library{}), render.New(&media.Library{})
		view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90}
		r.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		draw := func(cmd vm.PenCommand) { pen.Pen(cmd); reference.Pen(cmd) }
		draw(surfaceTestTriangle(vm.StageSurface, 0, 1, 0, 1))
		for range pendingPenLimit - 1 {
			draw(vm.PenCommand{Kind: "surface-clear", Surface: &vm.PenSurfaceCommand{Name: "missing"}})
		}
		far := surfaceTestTriangle(vm.StageSurface, 0, 0, 1, 1)
		for i := range far.Surface.Vertices {
			far.Surface.Vertices[i].Z = .5
		}
		draw(far)
		got, _ := r.DisplayPen()
		if got == nil || got.NRGBAAt(16, 12) != (color.NRGBA{G: 255, A: 255}) {
			t.Fatal("later depth test lost the earlier GPU triangle")
		}
		checkSurfaceStage(t, pen, reference)
	})
}

func TestPenSurfaceHistoryLimitKeepsUnuploadedCPUWork(t *testing.T) {
	runGPUFrame(t, func() {
		r, reference := render.New(&media.Library{}), render.New(&media.Library{})
		view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90}
		r.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		draw := func(cmd vm.PenCommand) { pen.Pen(cmd); reference.Pen(cmd) }
		draw(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "effect", Width: 16, Height: 16}})
		draw(surfaceTestTriangle("effect", 0, 1, 0, 1))
		draw(vm.PenCommand{Kind: "line", Line: [4]float64{-8, 0, 8, 0}, Size: 10, Color: color.NRGBA{G: 255, A: 255}})
		draw(vm.PenCommand{Kind: "line", Line: [4]float64{-100000, 0, 100000, 0}, Size: 4, Color: color.NRGBA{R: 255, A: 255}})
		for range pendingPenLimit {
			draw(vm.PenCommand{Kind: "surface-clear", Surface: &vm.PenSurfaceCommand{Name: "effect"}})
		}
		got, _ := r.DisplayPen()
		if got == nil || got.NRGBAAt(16, 12) != (color.NRGBA{R: 255, A: 255}) {
			t.Fatal("history checkpoint overwrote unuploaded CPU drawing")
		}
		draw(vm.PenCommand{Kind: "line", Line: [4]float64{-8, -8, 8, -8}, Size: 2, Color: color.NRGBA{B: 255, A: 255}})
		checkSurfaceStage(t, pen, reference)
	})
}

func BenchmarkSurfaceHistoryWithUnreferencedStage(b *testing.B) {
	r := render.New(&media.Library{})
	r.SetView(vm.View{Width: 800, Height: 600, Zoom: 100, Direction: 90})
	r.Pen(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "effect", Width: 16, Height: 16}})
	commands := []vm.PenCommand{
		{Kind: "line", Line: [4]float64{-400, -300, 400, 300}, Size: 100, Color: color.NRGBA{R: 255, A: 255}},
		{Kind: "surface-clear", Surface: &vm.PenSurfaceCommand{Name: "effect"}},
		surfaceTestTriangle("effect", 0, 1, 0, 1),
	}
	pen := &penGPU{Renderer: r, pending: make([]vm.PenCommand, 0, len(commands))}
	b.ReportAllocs()
	for b.Loop() {
		pen.pending = append(pen.pending[:0], commands...)
		pen.preserveSurfaceHistory(false)
		r.Pen(vm.PenCommand{Kind: "clear"})
	}
}

func TestPenSurfaceGPUNativeTriangle(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.Pen(surfaceTestTriangle(vm.StageSurface, 1, 0, 0, .5))
		if pixels, _ := r.DisplayPen(); pixels != nil {
			t.Fatal("ordinary triangle eagerly rasterized CPU pixels")
		}
		pen.flushStamps()
		pen.flushLines()
		if pen.image == nil {
			t.Fatal("triangle did not create a GPU pen layer")
		}
		pixels := image.NewRGBA(pen.image.Bounds())
		pen.image.ReadPixels(pixels.Pix)
		got := pixels.RGBAAt(16, 12)
		if got.R < 127 || got.R > 128 || got.A < 127 || got.A > 128 || got.G != 0 || got.B != 0 {
			t.Fatalf("triangle center = %v", got)
		}
	})
}

func TestPenSurfaceGPUHistoryAndLifecycle(t *testing.T) {
	runGPUFrame(t, func() {
		for _, keepHistory := range []bool{true, false} {
			r, reference := render.New(&media.Library{}), render.New(&media.Library{})
			view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90}
			r.SetView(view)
			reference.SetView(view)
			var gpu stageGPU
			pen, err := gpu.enablePen(r)
			if err != nil {
				t.Fatal(err)
			}
			pen.keepHistory = keepHistory
			recorder := deferredPen{pen}
			draw := func(cmd vm.PenCommand) { recorder.Pen(cmd); reference.Pen(cmd) }
			draw(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "", Width: 16, Height: 16}})
			draw(surfaceTestTriangle("", 1, 0, 0, .5))
			draw(vm.PenCommand{Kind: "clear"})
			cmd := surfaceTestTriangle(vm.StageSurface, 1, 1, 1, 1)
			cmd.Surface.Textured, cmd.Surface.Texture = true, ""
			cmd.Surface.MultiplyTextureAlpha = true
			for i := range cmd.Surface.Vertices {
				cmd.Surface.Vertices[i].U, cmd.Surface.Vertices[i].V = .5, .5
			}
			draw(cmd)
			checkSurfaceStage(t, pen, reference)
			got := image.NewRGBA(pen.image.Bounds())
			pen.image.ReadPixels(got.Pix)
			center := got.RGBAAt(16, 12)
			if center.R < 63 || center.R > 65 || center.A < 127 || center.A > 128 {
				t.Fatalf("reference framebuffer alpha multiplication lost: %v", center)
			}
			pen.flush()
			a, _ := r.DisplayPen()
			b, _ := reference.DisplayPen()
			if a == nil || !bytes.Equal(a.Pix, b.Pix) {
				t.Fatalf("sensing differs with keepHistory=%v", keepHistory)
			}
			draw(vm.PenCommand{Kind: "surface-clear", Surface: &vm.PenSurfaceCommand{Name: ""}})
			draw(vm.PenCommand{Kind: "clear"})
			draw(cmd)
			checkSurfaceStage(t, pen, reference)
			draw(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "", Width: 8, Height: 6}})
			if pen.surfaces[""].Bounds().Size() != image.Pt(8, 6) {
				t.Fatal("recreate did not resize")
			}
			draw(vm.PenCommand{Kind: "surface-delete", Surface: &vm.PenSurfaceCommand{Name: ""}})
			if pen.surfaces[""] != nil {
				t.Fatal("delete retained texture")
			}
			draw(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "other", Width: 4, Height: 4}})
			draw(vm.PenCommand{Kind: "surface-reset"})
			if len(pen.surfaces) != 0 {
				t.Fatal("reset retained textures")
			}
			gpu.Close()
		}
	})
}

func TestPenSurfaceGPUOrderedStageTexture(t *testing.T) {
	runGPUFrame(t, func() {
		r, reference := render.New(&media.Library{}), render.New(&media.Library{})
		view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90}
		r.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		recorder := deferredPen{pen}
		draw := func(cmd vm.PenCommand) { recorder.Pen(cmd); reference.Pen(cmd) }
		draw(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "capture", Width: 16, Height: 16}})
		draw(vm.PenCommand{Kind: "line", Line: [4]float64{-8, 0, 8, 0}, Size: 10, Color: color.NRGBA{G: 255, A: 255}})
		cmd := surfaceTestTriangle("capture", 1, 1, 1, 1)
		cmd.Surface.Textured = true
		cmd.Surface.Texture = vm.StageSurface
		for i := range cmd.Surface.Vertices {
			cmd.Surface.Vertices[i].U, cmd.Surface.Vertices[i].V = .5, .5
		}
		draw(cmd)
		draw(vm.PenCommand{Kind: "clear"})
		copyCmd := *cmd.Surface
		copyCmd.Name, copyCmd.Texture = vm.StageSurface, "capture"
		draw(vm.PenCommand{Kind: "surface-triangle", Surface: &copyCmd})
		checkSurfaceStage(t, pen, reference)
		pen.flush()
		a, _ := r.DisplayPen()
		b, _ := reference.DisplayPen()
		if a == nil || !bytes.Equal(a.Pix, b.Pix) {
			t.Fatal("clear lost captured stage history")
		}
	})
}

func TestPenSurfaceGPUCostumeUVAndFiltering(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x / 2 * 60), G: uint8(y / 2 * 60), B: 120, A: uint8(80 + x/2*40)})
			}
		}
		lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 8, Height: 8, Bitmap: true}}}}
		r, reference := render.New(lib), render.New(lib)
		view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90}
		r.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []struct{ linear, multiply, noAntialias bool }{
			{false, false, false}, {true, false, false}, {false, true, false}, {true, true, false},
			{true, false, true}, {true, true, true},
		} {
			view.NoAntialias = mode.noAntialias
			pen.SetView(view)
			reference.SetView(view)
			pen.Pen(vm.PenCommand{Kind: "clear"})
			reference.Pen(vm.PenCommand{Kind: "clear"})
			cmd := surfaceTestTriangle(vm.StageSurface, .8, .6, 1, .7)
			cmd.Surface.Textured, cmd.Surface.Linear = true, mode.linear
			cmd.Surface.MultiplyTextureAlpha = mode.multiply
			cmd.Surface.Costume = &vm.DisplayTarget{State: vm.TargetState{Size: 0, Effects: vm.GraphicEffects{Ghost: 100}}}
			for i, uv := range [3][2]float64{{-.2, .1}, {1.2, .1}, {.4, 1.3}} {
				cmd.Surface.Vertices[i].U, cmd.Surface.Vertices[i].V = uv[0], uv[1]
			}
			pen.Pen(cmd)
			reference.Pen(cmd)
			checkSurfaceStage(t, pen, reference)
		}
	})
}

func TestPenSurfaceGPUDepthPerspectiveFallback(t *testing.T) {
	runGPUFrame(t, func() {
		r, reference := render.New(&media.Library{}), render.New(&media.Library{})
		view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90}
		r.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		draw := func(cmd vm.PenCommand) { pen.Pen(cmd); reference.Pen(cmd) }
		draw(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "depth", Width: 16, Height: 16}})
		cmd := surfaceTestTriangle("depth", 1, 0, 0, .7)
		for i := range cmd.Surface.Vertices {
			cmd.Surface.Vertices[i].Z = .5
			cmd.Surface.Vertices[i].W = float64(i + 1)
			cmd.Surface.Vertices[i].G = float64(i) / 2
		}
		draw(cmd)
		far := surfaceTestTriangle("depth", 0, 0, 1, 1)
		for i := range far.Surface.Vertices {
			far.Surface.Vertices[i].Z = 1
		}
		draw(far)
		draw(surfaceTestTriangle("depth", 0, 1, 0, .3))
		copyCmd := surfaceTestTriangle(vm.StageSurface, 1, 1, 1, 1)
		copyCmd.Surface.Textured, copyCmd.Surface.Texture = true, "depth"
		for i := range copyCmd.Surface.Vertices {
			copyCmd.Surface.Vertices[i].U, copyCmd.Surface.Vertices[i].V = .5, .5
		}
		draw(copyCmd)
		checkSurfaceStage(t, pen, reference)
		pen.flush()
		a, _ := r.DisplayPen()
		b, _ := reference.DisplayPen()
		if a == nil || !bytes.Equal(a.Pix, b.Pix) {
			t.Fatal("depth fallback lost CPU mirror")
		}
		draw(vm.PenCommand{Kind: "clear"})
		stageDepth := surfaceTestTriangle(vm.StageSurface, 1, .5, 0, .7)
		for i := range stageDepth.Surface.Vertices {
			stageDepth.Surface.Vertices[i].W = float64(i + 1)
			stageDepth.Surface.Vertices[i].R = float64(i) / 2
		}
		draw(stageDepth)
		checkSurfaceStage(t, pen, reference)
		draw(surfaceTestTriangle(vm.StageSurface, 0, 0, 1, .3))
		checkSurfaceStage(t, pen, reference)
	})
}

func TestPenSurfaceGPUHighQualityResolution(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90, HighQualityPen: true})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.setOutputSize(64, 48)
		pen.Pen(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "fixed", Width: 7, Height: 5}})
		pen.Pen(surfaceTestTriangle(vm.StageSurface, 1, 0, 0, 1))
		if pen.image.Bounds().Size() != image.Pt(64, 48) || pen.surfaces["fixed"].Bounds().Size() != image.Pt(7, 5) {
			t.Fatal("stage scaling changed fixed surface resolution")
		}
		pixels := image.NewRGBA(pen.image.Bounds())
		pen.image.ReadPixels(pixels.Pix)
		if pixels.RGBAAt(32, 24).R != 255 || pixels.RGBAAt(32, 5).A != 0 {
			t.Fatal("high-quality triangle position or size changed")
		}
	})
}

func TestPenSurfaceGPUAlphaAboveOne(t *testing.T) {
	runGPUFrame(t, func() {
		r, reference := render.New(&media.Library{}), render.New(&media.Library{})
		view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90}
		r.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		cmd := surfaceTestTriangle(vm.StageSurface, .2, .3, .4, 2)
		pen.Pen(cmd)
		reference.Pen(cmd)
		checkSurfaceStage(t, pen, reference)
	})
}

func TestPenSurfaceGPUResetKeepsOrdinaryPenFastPath(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		pen.Pen(vm.PenCommand{Kind: "surface-reset"})
		if pen.surfaceHistory {
			t.Fatal("loading Pen+ enabled CPU triangle history before any surface draw")
		}
		pen.Pen(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "unused", Width: 4, Height: 4}})
		if pen.surfaceHistory {
			t.Fatal("unused surface enabled CPU triangle history")
		}
		pen.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{-8, 0, 8, 0}, Size: 5, Color: color.NRGBA{R: 255, A: 255}})
		if len(pen.pending) != 0 {
			t.Fatal("ordinary line began retaining CPU history after module reset")
		}
		pen.Pen(vm.PenCommand{Kind: "clear"})
		pen.Pen(surfaceTestTriangle(vm.StageSurface, 1, 0, 0, 1))
		_, before := r.DisplayPen()
		pen.Pen(vm.PenCommand{Kind: "clear"})
		_, after := r.DisplayPen()
		if after != before+1 {
			t.Fatal("stage clear replayed discarded stage-only triangle history")
		}
	})
}

func TestPenSurfaceLifecycleKeepsQueuedStageDraws(t *testing.T) {
	runGPUFrame(t, func() {
		r, reference := render.New(&media.Library{}), render.New(&media.Library{})
		view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90}
		r.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		draw := func(cmd vm.PenCommand) { pen.Pen(cmd); reference.Pen(cmd) }
		draw(surfaceTestTriangle(vm.StageSurface, 0, 0, 1, 1))
		draw(vm.PenCommand{Kind: "line", Line: [4]float64{-8, 0, 8, 0}, Size: 5, Color: color.NRGBA{R: 255, A: 255}})
		for _, kind := range []string{"surface-create", "surface-clear", "surface-delete", "surface-reset"} {
			draw(vm.PenCommand{Kind: kind, Surface: &vm.PenSurfaceCommand{Name: "effect", Width: 16, Height: 16}})
			if len(pen.lineVertices) == 0 {
				t.Fatalf("%s unnecessarily submitted unrelated stage drawing", kind)
			}
		}
		checkSurfaceStage(t, pen, reference)
		pen.flush()
		got, _ := r.DisplayPen()
		want, _ := reference.DisplayPen()
		if got == nil || want == nil || !bytes.Equal(got.Pix, want.Pix) {
			t.Fatal("resource changes lost stage pixels needed for sensing")
		}
	})
}

func BenchmarkDiscardedPenHistory(b *testing.B) {
	commands := make([]vm.PenCommand, pendingPenLimit)
	for i := range commands {
		commands[i] = vm.PenCommand{Kind: "line", Line: [4]float64{-10, 0, 10, 0}, Size: 2}
	}
	pen := &penGPU{pending: make([]vm.PenCommand, len(commands))}
	b.ReportAllocs()
	for b.Loop() {
		pen.pending = pen.pending[:len(commands)]
		copy(pen.pending, commands)
		pen.preserveSurfaceHistory(false)
	}
}
