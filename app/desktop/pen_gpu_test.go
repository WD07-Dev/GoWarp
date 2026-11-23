package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/render"
	"gowarp/core/vm"
)

func TestStageGPUNativePen(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 6))
		for y := 0; y < 6; y++ {
			for x := 0; x < 8; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{uint8(x * 30), 80, uint8(y * 40), 180})
			}
		}
		lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 8, Height: 6, CenterX: 4, CenterY: 3}}}}
		actual, reference := render.New(lib), render.New(lib)
		view := vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, Color: "#ffffff"}
		actual.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		graphics, err := gpu.enablePen(actual)
		if err != nil {
			t.Fatal(err)
		}
		target := vm.DisplayTarget{State: vm.TargetState{Size: 235, StretchX: 100, StretchY: 80, Direction: 37}}
		checkDisplay := func() {
			t.Helper()
			dst := ebiten.NewImage(128, 96)
			defer dst.Deallocate()
			if err := gpu.Draw(dst, actual, nil); err != nil {
				t.Fatal(err)
			}
			want := reference.DrawAt(nil, 128, 96)
			got := make([]byte, len(want.Pix))
			dst.ReadPixels(got)
			bad := 0
			for i, n := range want.Pix {
				delta := int(n) - int(got[i])
				if delta < -3 || delta > 3 {
					bad++
				}
			}
			if bad > 12 {
				t.Fatalf("%d pen channels differ by more than 3", bad)
			}
		}
		pen := func(cmd vm.PenCommand) {
			graphics.Pen(cmd)
			reference.Pen(cmd)
		}
		pen(vm.PenCommand{Kind: "stamp", Target: target})
		if pixels, _ := actual.DisplayPen(); pixels != nil {
			t.Fatal("GPU stamping eagerly rasterized CPU pen pixels")
		}
		checkDisplay()
		for _, effects := range []vm.GraphicEffects{
			{Ghost: 35}, {Color: 43, Brightness: 12},
			{Whirl: 70, Fisheye: 23, Mosaic: 12, Pixelate: 10},
		} {
			pen(vm.PenCommand{Kind: "clear"})
			target.State.Effects = effects
			gpu.measureUploads = true
			gpu.penSubmitTime = 0
			pen(vm.PenCommand{Kind: "stamp", Target: target})
			gpu.measureUploads = false
			if effects.Whirl != 0 && gpu.penSubmitTime <= 0 {
				t.Fatal("direct effect stamp was missing from submission diagnostics")
			}
			checkDisplay()
		}
		/*
			Keep overlapping stamps in one batch, then switch to the effect
			shader without drawing a frame between commands.
		*/
		pen(vm.PenCommand{Kind: "clear"})
		for _, effects := range []vm.GraphicEffects{{Ghost: 15}, {Ghost: 45}, {Color: 43}, {Ghost: 20}} {
			target.State.Effects = effects
			pen(vm.PenCommand{Kind: "stamp", Target: target})
		}
		checkDisplay()
		// Color sensing must see the same exact CPU pixels within the same tick.
		wanted := color.NRGBA{R: 120, G: 80, B: 80, A: 255}
		got := graphics.TouchingColor(nil, target, wanted, nil)
		want := reference.TouchingColor(nil, target, wanted, nil)
		if got != want {
			t.Fatalf("color sensing: got %v, want %v", got, want)
		}
		actualPen, _ := actual.DisplayPen()
		referencePen, _ := reference.DisplayPen()
		if actualPen == nil || !bytes.Equal(actualPen.Pix, referencePen.Pix) {
			t.Fatal("color sensing did not synchronize exact CPU pen pixels")
		}
		pen(vm.PenCommand{Kind: "line", Line: [4]float64{-25, 0, 25, 0}, Size: 7, Color: color.NRGBA{G: 255, A: 200}})
		target.State.Effects = vm.GraphicEffects{Ghost: 20}
		pen(vm.PenCommand{Kind: "stamp", Target: target})
		checkDisplay()
		view.Width, view.Height = 80, 51
		view.X, view.Y, view.Zoom, view.Direction = 4, -3, 125, 47
		graphics.SetView(view)
		reference.SetView(view)
		checkDisplay()
		pen(vm.PenCommand{Kind: "clear"})
		checkDisplay()
	})
}

func TestPenHistoryKeepsRoundingModeAtStampTime(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	pixels.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 1, Height: 1, CenterX: .5, CenterY: .5}}}}
	command := vm.PenCommand{Kind: "stamp", Target: vm.DisplayTarget{State: vm.TargetState{X: .25, Y: .25, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}}
	for _, highQuality := range []bool{false, true} {
		for _, initiallyEnabled := range []bool{false, true} {
			actual, reference := render.New(lib), render.New(lib)
			view := vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90}
			if highQuality {
				view.HighQualityPen = initiallyEnabled
			} else {
				view.DisablePositionRounding = initiallyEnabled
			}
			actual.SetView(view)
			reference.SetView(view)
			reference.Pen(command)
			// CPU color-sensing history may outlive the GPU batch that drew it.
			pen := &penGPU{Renderer: actual, keepHistory: true, scale: 1, pending: []vm.PenCommand{command}}
			if highQuality {
				view.HighQualityPen = !initiallyEnabled
			} else {
				view.DisablePositionRounding = !initiallyEnabled
			}
			pen.SetView(view)
			reference.SetView(view)
			pen.flush()
			got, _ := actual.DisplayPen()
			want, _ := reference.DisplayPen()
			if got == nil || !bytes.Equal(got.Pix, want.Pix) {
				t.Fatalf("HQ=%v initial=%v: changing rounding moved an earlier stamp", highQuality, initiallyEnabled)
			}
		}
	}
}

func TestStageGPUHighQualityPen(t *testing.T) {
	runGPUFrame(t, func() {
		var projectData project.Project
		if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"comments":{"config":{"text":"{\"hq\":true} // _twconfig_"}}}]}`), &projectData); err != nil {
			t.Fatal(err)
		}
		machine := vm.New(&projectData)
		pixels := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		pixels.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		pixels.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
		pixels.SetNRGBA(0, 1, color.NRGBA{B: 255, A: 255})
		pixels.SetNRGBA(1, 1, color.NRGBA{A: 255})
		lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 2, Height: 2, CenterX: 1, CenterY: 1}}}}
		renderer := render.New(lib)
		view := machine.View()
		view.Width, view.Height = 64, 48
		renderer.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		dst := ebiten.NewImage(192, 144)
		defer dst.Deallocate()
		if err := gpu.Draw(dst, renderer, nil); err != nil {
			t.Fatal(err)
		}
		target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 400, StretchX: 100, StretchY: 100, Direction: 90}}
		pen.Pen(vm.PenCommand{Kind: "stamp", Target: target})
		if err := gpu.Draw(dst, renderer, nil); err != nil {
			t.Fatal(err)
		}
		if pen.image.Bounds().Size() != dst.Bounds().Size() {
			t.Fatalf("HQ pen remains at %v for a %v display", pen.image.Bounds().Size(), dst.Bounds().Size())
		}
		/*
			The displayed stamp must retain the same smooth gradients as a
			sprite drawn directly at the output resolution.
		*/
		want := renderer.DrawAt([]vm.DisplayTarget{target}, 192, 144)
		got := make([]byte, len(want.Pix))
		dst.ReadPixels(got)
		for y := 60; y < 84; y++ {
			for x := 84; x < 108; x++ {
				for c := 0; c < 4; c++ {
					i := y*want.Stride + x*4 + c
					delta := int(got[i]) - int(want.Pix[i])
					if delta < -3 || delta > 3 {
						t.Fatalf("HQ stamp channel at %d,%d: got %d want %d", x, y, got[i], want.Pix[i])
					}
				}
			}
		}
		pen.TouchingColor(nil, target, color.NRGBA{R: 255, A: 255}, nil)
		cpu, _ := renderer.DisplayPen()
		if cpu.Bounds().Size() != image.Pt(64, 48) {
			t.Fatal("HQ changed collision resolution")
		}
		pen.Pen(vm.PenCommand{Kind: "clear"})
		line := vm.PenCommand{Kind: "line", Line: [4]float64{-9, -5, 11, 7}, Size: 4, Color: color.NRGBA{R: 180, G: 40, A: 200}}
		pen.Pen(line)
		if err := gpu.Draw(dst, renderer, nil); err != nil {
			t.Fatal(err)
		}
		large := render.New(lib)
		largeView := view
		largeView.Width, largeView.Height = 192, 144
		large.SetView(largeView)
		line.Size *= 3
		for i := range line.Line {
			line.Line[i] *= 3
		}
		large.Pen(line)
		want = large.Draw(nil)
		dst.ReadPixels(got)
		for i, value := range want.Pix {
			delta := int(got[i]) - int(value)
			if delta < -3 || delta > 3 {
				t.Fatalf("HQ line channel %d: got %d want %d", i, got[i], value)
			}
		}
		view.HighQualityPen = false
		pen.SetView(view)
		if pen.image.Bounds().Size() != image.Pt(64, 48) {
			t.Fatal("disabling HQ retained the enlarged pen")
		}
	})
}

func TestStageGPUPenReplay(t *testing.T) {
	path := os.Getenv("GOWARP_PEN_PROJECT")
	if path == "" {
		t.Skip("set GOWARP_PEN_PROJECT and GOWARP_PEN_INPUTS for a 4800-frame pen replay")
	}
	runGPUFrame(t, func() {
		ctx := context.Background()
		p, err := project.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		machine := vm.New(p)
		// Measure the same VM and visual assets without audio or window effects.
		visual := *p
		visual.Targets = make([]*project.Target, len(p.Targets))
		for i, target := range p.Targets {
			copy := *target
			copy.Sounds = nil
			visual.Targets[i] = &copy
		}
		lib, err := media.LoadRuntime(ctx, path, &visual, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer lib.Close()
		renderer := render.New(lib)
		var gpu stageGPU
		defer gpu.Close()
		graphics, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		if os.Getenv("GOWARP_PEN_CPU") == "1" {
			machine.SetGraphics(renderer)
		} else {
			machine.SetGraphics(graphics)
		}
		data, err := os.ReadFile(os.Getenv("GOWARP_PEN_INPUTS"))
		if err != nil {
			t.Fatal(err)
		}
		var events []vm.InputEvent
		if err := json.Unmarshal(data, &events); err != nil {
			t.Fatal(err)
		}
		if err := machine.QueueEvents(events); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		var before, current runtime.MemStats
		runtime.ReadMemStats(&before)
		for frame := 1; frame <= 4800; frame++ {
			if err := machine.Run(ctx, 1, 60, false); err != nil {
				t.Fatal(err)
			}
			if graphics.image != nil {
				// Include GPU completion; don't time only deferred draw submission.
				var pixel [4]byte
				graphics.image.SubImage(image.Rect(0, 0, 1, 1)).(*ebiten.Image).ReadPixels(pixel[:])
			}
			if frame%600 == 0 {
				runtime.ReadMemStats(&current)
				t.Logf("frame=%d elapsed=%v allocated=%d heap=%d", frame, time.Since(start), current.TotalAlloc-before.TotalAlloc, current.HeapAlloc)
			}
		}
		if err := lib.Err(); err != nil {
			t.Fatal(err)
		}
		runtime.GC()
		runtime.ReadMemStats(&current)
		t.Logf("live heap=%d, GPU textures=%d bytes, retained stamps=%d", current.HeapAlloc, gpu.bytes, len(graphics.pending))
		for _, target := range machine.Snapshot() {
			if target.IsStage {
				for _, variable := range target.Variables {
					if variable.Name == "페이즈" {
						t.Logf("phase=%v", variable.Value)
					}
				}
			}
		}
	})
}

func TestStageGPUPenLineBatch(t *testing.T) {
	runGPUFrame(t, func() {
		lib := &media.Library{}
		renderer, reference := render.New(lib), render.New(lib)
		view := vm.View{Width: 256, Height: 192, Zoom: 100, Direction: 90, Color: "#ffffff"}
		renderer.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		dst := ebiten.NewImage(256, 192)
		defer dst.Deallocate()
		commands := make([]vm.PenCommand, 64)
		for i := range commands {
			commands[i] = vm.PenCommand{Kind: "line", Line: [4]float64{-100, float64(i - 32), 100, float64(i - 31)}, Size: 3, Color: color.NRGBA{R: 180, G: 60, B: 20, A: 170}}
			reference.Pen(commands[i])
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for _, command := range commands {
			pen.Pen(command)
		}
		if err := gpu.Draw(dst, renderer, nil); err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("64 pen lines allocated %d bytes", allocated)
		// A short path must not allocate an entire stage image for every segment.
		if allocated > 4<<20 {
			t.Errorf("pen line batch allocated %d bytes; limit is 4 MiB", allocated)
		}
		want := reference.Draw(nil)
		got := make([]byte, len(want.Pix))
		dst.ReadPixels(got)
		for i, value := range want.Pix {
			delta := int(value) - int(got[i])
			if delta < -3 || delta > 3 {
				t.Fatalf("pen pixel channel %d: got %d, want %d", i, got[i], value)
			}
		}
	})
}

func TestClippedPenLinesShareBatch(t *testing.T) {
	runGPUFrame(t, func() {
		for _, scale := range []float64{1, 1.5} {
			var actualGPU, referenceGPU stageGPU
			pixels := image.NewNRGBA(image.Rect(0, 0, 4, 4))
			draw.Draw(pixels, pixels.Bounds(), &image.Uniform{color.NRGBA{G: 255, B: 120, A: 150}}, image.Point{}, draw.Src)
			lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 4, Height: 4, CenterX: 2, CenterY: 2}}}}
			actualRenderer, referenceRenderer := render.New(lib), render.New(lib)
			view := vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90}
			actualRenderer.SetView(view)
			referenceRenderer.SetView(view)
			actual, err := actualGPU.enablePen(actualRenderer)
			if err != nil {
				t.Fatal(err)
			}
			reference, err := referenceGPU.enablePen(referenceRenderer)
			if err != nil {
				t.Fatal(err)
			}
			actual.scale, reference.scale = scale, scale
			clip := &vm.Bounds{Left: -13.5, Right: 20, Bottom: -11.5, Top: 17}
			command := vm.PenCommand{Kind: "line", Target: vm.DisplayTarget{State: vm.TargetState{Clipbox: clip}}, Size: 3, Color: color.NRGBA{R: 180, G: 50, A: 90}}
			for group, mode := range []string{"", "additive", "multiply", ""} {
				command.Target.State.BlendMode = mode
				if group == 3 {
					command.Target.State.Clipbox = nil
				}
				for i := range 64 {
					command.Line = [4]float64{-25, float64(i%16 - 8), 25, float64(i%16 - 7)}
					actual.Pen(command)
					reference.Pen(command)
					reference.flushLines()
				}
				if len(actual.lineVertices) != 64*4 {
					t.Fatalf("scale=%g mode=%q: adjacent lines were submitted individually (%d pending vertices)", scale, mode, len(actual.lineVertices))
				}
				if group == 1 {
					stamp := vm.PenCommand{Kind: "stamp", Target: vm.DisplayTarget{State: vm.TargetState{Size: 200, StretchX: 100, StretchY: 100, Direction: 90}}}
					actual.Pen(stamp)
					reference.Pen(stamp)
					reference.flushStamps()
				}
				// Earlier batches must retain their own clip after subsequent commands change it.
				clip.Left += 2
			}
			actual.flushLines()
			got := make([]byte, actual.image.Bounds().Dx()*actual.image.Bounds().Dy()*4)
			want := make([]byte, len(got))
			actual.image.ReadPixels(got)
			reference.image.ReadPixels(want)
			if !bytes.Equal(got, want) {
				t.Fatalf("scale=%g: batching changed clipped or blended pixels", scale)
			}
			actual.flush()
			reference.flush()
			cpu, _ := actualRenderer.DisplayPen()
			cpuReference, _ := referenceRenderer.DisplayPen()
			if !bytes.Equal(cpu.Pix, cpuReference.Pix) {
				t.Fatal("batching changed color sensing history")
			}
			actualGPU.Close()
			referenceGPU.Close()
		}
	})
}

func TestStageGPUPenLineBatchAllocations(t *testing.T) {
	runGPUFrame(t, func() {
		renderer := render.New(&media.Library{})
		renderer.SetView(vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		dst := ebiten.NewImage(64, 48)
		defer dst.Deallocate()
		command := vm.PenCommand{Kind: "line", Line: [4]float64{-20, -10, 20, 10}, Size: 3, Color: color.NRGBA{R: 100, G: 60, B: 30, A: 170}}
		draw := func() {
			for i := 0; i < 1000; i++ {
				command.Color.R = uint8(i)
				pen.Pen(command)
			}
			if err := gpu.Draw(dst, renderer, nil); err != nil {
				t.Fatal(err)
			}
		}
		draw()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		draw()
		runtime.ReadMemStats(&after)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("1000 pen lines allocated %d bytes", allocated)
		if allocated > 1<<20 {
			t.Fatalf("pen lines allocated %d bytes; limit is 1 MiB", allocated)
		}
	})
}

func TestStageGPUDiagonalPen(t *testing.T) {
	runGPUFrame(t, func() {
		renderer, reference := render.New(&media.Library{}), render.New(&media.Library{})
		view := vm.View{Width: 640, Height: 480, Zoom: 100, Direction: 90, Color: "#ffffff"}
		renderer.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		dst := ebiten.NewImage(640, 480)
		defer dst.Deallocate()
		commands := make([]vm.PenCommand, 100)
		for i := range commands {
			y := float64(i%20) - 10
			commands[i] = vm.PenCommand{Kind: "line", Line: [4]float64{-300, -200 + y, 300, 200 + y}, Size: 3, Color: color.NRGBA{R: uint8(i), G: 130, B: 60, A: 170}}
			reference.Pen(commands[i])
		}
		pixels := make([]byte, 640*480*4)
		draw := func() {
			pen.Pen(vm.PenCommand{Kind: "clear"})
			for _, command := range commands {
				pen.Pen(command)
			}
			if err := gpu.Draw(dst, renderer, nil); err != nil {
				t.Fatal(err)
			}
			dst.ReadPixels(pixels)
		}
		draw()
		start := time.Now()
		for i := 0; i < 5; i++ {
			draw()
		}
		t.Logf("5 batches of 100 diagonal lines, including GPU completion: %s", time.Since(start))
		want := reference.Draw(nil)
		for i, value := range want.Pix {
			delta := int(pixels[i]) - int(value)
			if delta < -3 || delta > 3 {
				t.Fatalf("diagonal pen channel %d: got %d want %d", i, pixels[i], value)
			}
		}
	})
}

func TestStageGPUMixedPenDoesNotRasterizeUntilSensing(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 6))
		for y := 0; y < 6; y++ {
			for x := 0; x < 8; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{R: 120, G: 40, B: 200, A: 180})
			}
		}
		lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 8, Height: 6, CenterX: 4, CenterY: 3}}}}
		actual, reference := render.New(lib), render.New(lib)
		view := vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, Color: "#ffffff"}
		actual.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		graphics, err := gpu.enablePen(actual)
		if err != nil {
			t.Fatal(err)
		}
		target := vm.DisplayTarget{State: vm.TargetState{Size: 135, StretchX: 100, StretchY: 100, Direction: 37}}
		dst := ebiten.NewImage(64, 48)
		defer dst.Deallocate()
		check := func() {
			t.Helper()
			if err := gpu.Draw(dst, actual, nil); err != nil {
				t.Fatal(err)
			}
			got := make([]byte, 64*48*4)
			dst.ReadPixels(got)
			want := reference.Draw(nil)
			for i, v := range want.Pix {
				if delta := int(v) - int(got[i]); delta < -3 || delta > 3 {
					t.Fatalf("channel %d: got %d, want %d", i, got[i], v)
				}
			}
		}
		for _, noAA := range []bool{false, true} {
			view.PenNoAntialias = noAA
			graphics.SetView(view)
			reference.SetView(view)
			for i, size := range []float64{1, 3, 4.5, 8} {
				stamp := vm.PenCommand{Kind: "stamp", Target: target}
				line := vm.PenCommand{Kind: "line", Line: [4]float64{-24.25, float64(i*7 - 14), 23.5, float64(i*7 - 11)}, Size: size, Color: []color.NRGBA{{40, 180, 60, 170}, {255, 255, 255, 170}, {255, 0, 255, 170}, {128, 255, 255, 170}}[i]}
				graphics.Pen(stamp)
				reference.Pen(stamp)
				graphics.Pen(line)
				reference.Pen(line)
				line.Line[0] = 999
			}
			check()
			if !noAA {
				if cpu, _ := actual.DisplayPen(); cpu != nil {
					t.Error("mixed GPU pen eagerly rasterized CPU pixels")
				}
			}
		}
		graphics.TouchingColor(nil, target, color.NRGBA{R: 120, G: 40, B: 200, A: 255}, nil)
		got, _ := actual.DisplayPen()
		want, _ := reference.DisplayPen()
		if got == nil || !bytes.Equal(got.Pix, want.Pix) {
			t.Fatal("deferred sensing changed CPU pen pixels or command order")
		}
		graphics.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{0, 0, 0, 0}, Size: 3, Color: color.NRGBA{R: 255, A: 255}})
		graphics.Pen(vm.PenCommand{Kind: "clear"})
		reference.Pen(vm.PenCommand{Kind: "clear"})
		check()
		graphics.TouchingColor(nil, target, color.NRGBA{R: 255, A: 255}, nil)
		got, _ = actual.DisplayPen()
		want, _ = reference.DisplayPen()
		if !bytes.Equal(got.Pix, want.Pix) {
			t.Fatal("cleared commands reappeared during sensing")
		}
	})
}

func TestStageGPUPenWithoutColorSensingKeepsNoCPUHistory(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		for i := range pixels.Pix {
			pixels.Pix[i] = 255
		}
		lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 2, Height: 2, CenterX: 1, CenterY: 1}}}}
		renderer := render.New(lib)
		var gpu stageGPU
		defer gpu.Close()
		graphics, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		graphics.keepHistory = false
		target := vm.DisplayTarget{State: vm.TargetState{Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
		for i := 0; i < 5000; i++ {
			graphics.Pen(vm.PenCommand{Kind: "stamp", Target: target})
		}
		if cpu, _ := renderer.DisplayPen(); cpu != nil {
			t.Fatal("display-only stamps rasterized an unused CPU pen")
		}
		if len(graphics.pending) != 0 {
			t.Fatalf("retained %d unused pen commands", len(graphics.pending))
		}
		// An uncommon CPU primitive still has to blend over earlier GPU stamps.
		graphics.Pen(vm.PenCommand{Kind: "triangle", Points: []float64{-10, -10, 10, -10, 0, 10}, Color: color.NRGBA{R: 255, A: 128}})
		want := render.New(lib)
		want.Pen(vm.PenCommand{Kind: "stamp", Target: target})
		want.Pen(vm.PenCommand{Kind: "triangle", Points: []float64{-10, -10, 10, -10, 0, 10}, Color: color.NRGBA{R: 255, A: 128}})
		line := vm.PenCommand{Kind: "line", Line: [4]float64{-15, 0, 15, 0}, Size: 3, Color: color.NRGBA{G: 255, A: 150}}
		graphics.Pen(line)
		want.Pen(line)
		view := vm.View{Width: 500, Height: 380, Zoom: 100, Direction: 90, Color: "#ffffff"}
		graphics.SetView(view)
		want.SetView(view)
		dst := ebiten.NewImage(500, 380)
		defer dst.Deallocate()
		if err := gpu.Draw(dst, renderer, nil); err != nil {
			t.Fatal(err)
		}
		actual := make([]byte, 500*380*4)
		dst.ReadPixels(actual)
		expected := want.Draw(nil)
		for i, v := range expected.Pix {
			if d := int(v) - int(actual[i]); d < -3 || d > 3 {
				t.Fatalf("fallback/resize channel %d: got %d, want %d", i, actual[i], v)
			}
		}
	})
}

func TestStageGPUOrdinaryStampBatchAllocations(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		for i := range pixels.Pix {
			pixels.Pix[i] = 255
		}
		renderer := render.New(&media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 2, Height: 2, CenterX: 1, CenterY: 1}}}})
		var gpu stageGPU
		defer gpu.Close()
		graphics, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		graphics.keepHistory = false
		command := vm.PenCommand{Kind: "stamp", Target: vm.DisplayTarget{State: vm.TargetState{Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}}
		dst := ebiten.NewImage(64, 48)
		defer dst.Deallocate()
		graphics.Pen(command)
		if err := gpu.Draw(dst, renderer, nil); err != nil {
			t.Fatal(err)
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for i := 0; i < 1000; i++ {
			command.Target.State.Effects.Ghost = float64(i % 100)
			graphics.Pen(command)
		}
		if err := gpu.Draw(dst, renderer, nil); err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("1000 ordinary stamps allocated %d bytes", allocated)
		if allocated > 1<<20 {
			t.Errorf("ordinary stamps allocated %d bytes; limit is 1 MiB", allocated)
		}
	})
}

func TestStageGPUColorStampBatchAllocations(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 30), G: 80, B: 140, A: 180})
			}
		}
		renderer := render.New(&media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 8, Height: 8, CenterX: 4, CenterY: 4}}}})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		command := vm.PenCommand{Kind: "stamp", Target: vm.DisplayTarget{State: vm.TargetState{Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}}
		dst := ebiten.NewImage(64, 48)
		defer dst.Deallocate()
		draw := func() {
			pen.Pen(vm.PenCommand{Kind: "clear"})
			for i := 0; i < 1000; i++ {
				command.Target.State.X = float64(i%35 - 17)
				command.Target.State.Effects = vm.GraphicEffects{Color: float64(i%201 - 100), Brightness: float64(i%80 - 40), Ghost: float64(i % 45)}
				pen.Pen(command)
			}
			if err := gpu.Draw(dst, renderer, nil); err != nil {
				t.Fatal(err)
			}
		}
		draw()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		started := time.Now()
		draw()
		elapsed := time.Since(started)
		runtime.ReadMemStats(&after)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("1000 color stamps: %v, %d allocated bytes", elapsed, allocated)
		if allocated > 1<<20 {
			t.Errorf("color stamps allocated %d bytes; limit 1 MiB", allocated)
		}
	})
}

func TestStageGPUPenStampTextureBounds(t *testing.T) {
	runGPUFrame(t, func() {
		lib := &media.Library{Costumes: make([][]*media.Costume, 3)}
		for i, size := range []image.Point{{3, 7}, {11, 2}, {5, 13}} {
			pixels := image.NewNRGBA(image.Rectangle{Max: size})
			for y := 0; y < size.Y; y++ {
				for x := 0; x < size.X; x++ {
					pixels.SetNRGBA(x, y, color.NRGBA{uint8(20 + x*20), uint8(30 + y*15), uint8(200 - i*60), uint8(60 + (x+y)%6*30)})
					if x == 0 {
						// Full hue turns must still lift black and grayscale colors.
						gray := []uint8{0, 8, 200}[y%3]
						pixels.SetNRGBA(x, y, color.NRGBA{gray, gray, gray, 180})
					}
				}
			}
			lib.Costumes[i] = []*media.Costume{{Image: pixels, Width: float64(size.X), Height: float64(size.Y),
				CenterX: float64(size.X) / 2, CenterY: float64(size.Y) / 2}}
		}
		renderer := render.New(lib)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		dst, expected := ebiten.NewImage(64, 48), ebiten.NewImage(64, 48)
		defer dst.Deallocate()
		defer expected.Deallocate()
		for _, smooth := range []bool{false, true} {
			view := vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, Color: "#806040", HighQualityPen: smooth}
			pen.SetView(view)
			pen.Pen(vm.PenCommand{Kind: "clear"})
			expected.Fill(vm.ParseColor(view.Color))
			for i := 0; i < 24; i++ {
				effects := vm.GraphicEffects{Ghost: float64(i % 10 * 7)}
				effects.Brightness = []float64{0, -100, 100, -35, 23, 0, 0, 0}[i/3]
				if i >= 9 {
					effects.Color = []float64{43, -57, 200, -200, 400}[i%5]
				}
				target := vm.DisplayTarget{Source: i % 3, State: vm.TargetState{X: float64(i%9*4 - 16), Y: float64(i/9*10 - 10),
					Size: 230, StretchX: 100, StretchY: 100, Direction: float64(23 + i*17), Effects: effects}}
				pen.Pen(vm.PenCommand{Kind: "stamp", Target: target})
				sprite, ok := renderer.PenSprite(target)
				if !ok {
					t.Fatal("missing stamp costume")
				}
				texture, temporary := gpu.texture(renderer, sprite)
				gpu.quad(expected, texture, sprite.Corners, sprite.Costume.Width, sprite.Costume.Height, effects, !smooth, false)
				if temporary {
					texture.Deallocate()
				}
			}
			if err := gpu.Draw(dst, renderer, nil); err != nil {
				t.Fatal(err)
			}
			got, want := make([]byte, 64*48*4), make([]byte, 64*48*4)
			dst.ReadPixels(got)
			expected.ReadPixels(want)
			for i, value := range want {
				if delta := int(got[i]) - int(value); delta < -3 || delta > 3 {
					t.Fatalf("smooth=%v channel %d: got %d, want %d", smooth, i, got[i], value)
				}
			}
		}
	})
}

func TestStageGPUPenSilhouetteBatch(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{uint8(x * 30), 80, uint8(y * 30), uint8(40 + x*20)})
			}
		}
		lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 8, Height: 8, CenterX: 4, CenterY: 4}}}}
		renderer, reference := render.New(lib), render.New(lib)
		view := vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, Color: "#806040"}
		renderer.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		dst := ebiten.NewImage(64, 48)
		defer dst.Deallocate()
		command := vm.PenCommand{Kind: "stamp", Target: vm.DisplayTarget{State: vm.TargetState{
			Size: 230, StretchX: 100, StretchY: 100, Direction: 37,
		}}}
		draw := func(referenceDraw bool) {
			pen.Pen(vm.PenCommand{Kind: "clear"})
			reference.Pen(vm.PenCommand{Kind: "clear"})
			for i := 0; i < 1000; i++ {
				command.Target.State.X = float64(i%35 - 17)
				command.Target.State.Y = float64(i%21 - 10)
				command.Target.State.Effects = vm.GraphicEffects{Brightness: -100, Ghost: float64(i % 45), Color: 22}
				if i%2 == 0 {
					command.Target.State.Effects.Brightness = 100
				}
				pen.Pen(command)
				if referenceDraw {
					reference.Pen(command)
				}
			}
			if err := gpu.Draw(dst, renderer, nil); err != nil {
				t.Fatal(err)
			}
		}
		draw(false)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		draw(false)
		runtime.ReadMemStats(&after)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("1000 silhouette stamps allocated %d bytes", allocated)
		if allocated > 1<<20 {
			t.Errorf("silhouette stamps allocated %d bytes; limit 1 MiB", allocated)
		}
		draw(true)
		got := make([]byte, 64*48*4)
		dst.ReadPixels(got)
		want := reference.Draw(nil)
		for i, value := range want.Pix {
			delta := int(got[i]) - int(value)
			if delta < -3 || delta > 3 {
				t.Fatalf("silhouette channel %d: got %d want %d", i, got[i], value)
			}
		}
		view.HighQualityPen = true
		pen.SetView(view)
		expected := ebiten.NewImage(64, 48)
		defer expected.Deallocate()
		for _, brightness := range []float64{-100, 100} {
			command.Target.State.Effects = vm.GraphicEffects{Brightness: brightness, Color: 57, Ghost: 37.5}
			pen.Pen(vm.PenCommand{Kind: "clear"})
			pen.Pen(command)
			if err := gpu.Draw(dst, renderer, nil); err != nil {
				t.Fatal(err)
			}
			d, ok := renderer.PenSprite(command.Target)
			if !ok {
				t.Fatal("missing silhouette costume")
			}
			texture, temporary := gpu.texture(renderer, d)
			expected.Fill(vm.ParseColor(view.Color))
			gpu.quad(expected, texture, d.Corners, d.Costume.Width, d.Costume.Height, command.Target.State.Effects, false, false)
			if temporary {
				texture.Deallocate()
			}
			got, want := make([]byte, 64*48*4), make([]byte, 64*48*4)
			dst.ReadPixels(got)
			expected.ReadPixels(want)
			for i, value := range want {
				delta := int(got[i]) - int(value)
				if delta < -3 || delta > 3 {
					t.Fatalf("HQ silhouette %g channel %d: got %d want %d", brightness, i, got[i], value)
				}
			}
		}

	})
}

func TestNativePenZeroWidthAndStampWidthIndependence(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		for i := 3; i < len(pixels.Pix); i += 4 {
			pixels.Pix[i] = 255
		}
		r := render.New(&media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 2, Height: 2, CenterX: 1, CenterY: 1}}}})
		view := r.DisplayView()
		view.Width, view.Height, view.HighQualityPen, view.OffscreenTouching = 64, 48, true, true
		r.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		p, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		p.keepHistory = false
		p.setOutputSize(128, 96)
		line := vm.PenCommand{Kind: "line", Size: 0, Line: [4]float64{-10, 0, 10, 0}, Color: color.NRGBA{R: 255, A: 255}}
		p.Pen(line)
		if cpu, _ := r.DisplayPen(); cpu != nil || p.dirty {
			t.Fatal("zero width forced synchronous CPU fallback")
		}
		p.flushLines()
		got := make([]byte, 128*96*4)
		p.image.ReadPixels(got)
		p.Pen(vm.PenCommand{Kind: "clear"})
		line.Size = 1
		p.Pen(line)
		p.flushLines()
		want := make([]byte, len(got))
		p.image.ReadPixels(want)
		if !bytes.Equal(got, want) {
			t.Fatal("GPU zero width does not use the reference default")
		}
		target := vm.DisplayTarget{State: vm.TargetState{Size: 400, StretchX: 100, StretchY: 100, Direction: 90}}
		p.Pen(vm.PenCommand{Kind: "clear"})
		p.Pen(vm.PenCommand{Kind: "stamp", Target: target, Size: 1})
		p.flushStamps()
		p.image.ReadPixels(want)
		for _, width := range []float64{math.Inf(1), math.NaN()} {
			p.Pen(vm.PenCommand{Kind: "clear"})
			p.Pen(vm.PenCommand{Kind: "stamp", Target: target, Size: width})
			p.flushStamps()
			p.image.ReadPixels(got)
			if !bytes.Equal(got, want) {
				t.Errorf("GPU dropped stamp with line width %g", width)
			}
		}
	})
}

func TestNativePenResizeReusesImagesAndPreservesPixels(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, HighQualityPen: true})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		pen.Pen(vm.PenCommand{Kind: "line", Size: 4, Line: [4]float64{-20, -10, 20, 10}, Color: color.NRGBA{R: 255, A: 128}})
		pen.flushLines()
		var menu *ebiten.Image
		for i, scale := range []int{3, 2, 3, 2, 3} {
			old := pen.image
			old.Clear()
			/*
				Keep edges away from pixel centers at both 2/3 and 3/2 scales;
				nearest sampling at an exact boundary varies with atlas placement.
			*/
			old.SubImage(image.Rect((i+1)*6, (i+2)*6, 30+i*6, 36+i*6)).(*ebiten.Image).Fill(color.RGBA{R: uint8(30 + i*10), B: 50, A: 128})
			w, h := 64*scale, 48*scale
			want := ebiten.NewImage(w, h)
			options := ebiten.DrawImageOptions{}
			options.GeoM.Scale(float64(w)/float64(old.Bounds().Dx()), float64(h)/float64(old.Bounds().Dy()))
			want.DrawImage(old, &options)
			pen.setOutputSize(w, h)
			if i == 0 {
				menu = pen.image
			} else if scale == 3 && pen.image != menu {
				t.Fatal("returning to the menu size allocated another pen image")
			}
			if pen.image.Bounds().Size() != image.Pt(w, h) {
				t.Fatal("pen resize changed the requested resolution")
			}
			actual, expected := make([]byte, w*h*4), make([]byte, w*h*4)
			pen.image.ReadPixels(actual)
			want.ReadPixels(expected)
			if !bytes.Equal(actual, expected) {
				for n := range actual {
					if actual[n] != expected[n] {
						t.Fatalf("resize %d pixel (%d,%d) channel %d = %d, want %d", i, (n/4)%w, (n/4)/w, n%4, actual[n], expected[n])
					}
				}
			}
			want.Deallocate()
		}
	})
}

func TestNativePenResizeSpareIsBoundedAndReleased(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		view := vm.View{Width: 64, Height: 1, Zoom: 100, Direction: 90, HighQualityPen: true}
		r.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.image = ebiten.NewImage(64, 1)
		pen.setOutputSize(192, 3)
		pen.setOutputSize(128, 2)
		if pen.resizeSpare == nil || pen.resizeSpare == pen.image {
			t.Fatal("resize must retain only a separate previous destination")
		}
		/*
			Disabling HQ at scale 1 must release the spare even if no resize
			is needed after the option changes.
		*/
		pen.setOutputSize(64, 1)
		view.HighQualityPen = false
		pen.SetView(view)
		if pen.resizeSpare != nil {
			t.Fatal("disabled high quality pen retained its resize spare")
		}
		view.HighQualityPen = true
		pen.SetView(view)
		pen.setOutputSize(4096, 64)
		if pen.resizeSpare != nil {
			t.Fatal("large resize retained an additional pen buffer")
		}
		pen.setOutputSize(128, 2)
		if pen.resizeSpare != nil {
			t.Fatal("shrinking the pen retained its oversized buffer")
		}
		pen.setOutputSize(192, 3)
		spare := pen.resizeSpare
		if spare == nil {
			t.Fatal("small resize did not retain a reusable buffer")
		}
		spare.Fill(color.RGBA{R: 255, A: 255})
		gpu.Close()
		if pen.resizeSpare != nil || pen.image != nil {
			t.Fatal("closing the renderer retained pen buffers")
		}
		if got := color.RGBAModel.Convert(spare.At(0, 0)); got != (color.RGBA{}) {
			t.Fatalf("resize spare was not deallocated: %v", got)
		}
		spare.Deallocate()
	})
}

func TestStageGPUMixedBoneFrame(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for i := range pixels.Pix {
			pixels.Pix[i] = 255
		}
		renderer := render.New(&media.Library{Costumes: [][]*media.Costume{{{CacheKey: "bone", Image: pixels, Bitmap: true, Width: 8, Height: 8, CenterX: 4, CenterY: 4}}}})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = false
		for _, size := range []image.Point{{480, 360}, {1920, 1440}} {
			pen.SetView(vm.View{Width: 480, Height: 360, Zoom: 100, Direction: 90, Color: "#000000", HighQualityPen: true})
			pen.setOutputSize(size.X, size.Y)
			dst := ebiten.NewImage(size.X, size.Y)
			draw := func() {
				pen.Pen(vm.PenCommand{Kind: "clear"})
				for i := 0; i < 100; i++ {
					x, y := float64(i%20*20-190), float64(i/20*45-120)
					pen.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{x, y, x, y + 35}, Size: 4, Color: color.NRGBA{R: 255, G: 255, B: 255, A: 255}})
					for _, yy := range []float64{y, y + 35} {
						pen.Pen(vm.PenCommand{Kind: "stamp", Target: vm.DisplayTarget{State: vm.TargetState{X: x, Y: yy, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}})
					}
				}
				if err := gpu.Draw(dst, renderer, nil); err != nil {
					t.Fatal(err)
				}
				var pixel [4]byte
				dst.SubImage(image.Rect(0, 0, 1, 1)).(*ebiten.Image).ReadPixels(pixel[:])
			}
			draw()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()
			for range 10 {
				draw()
			}
			elapsed := time.Since(start) / 10
			runtime.ReadMemStats(&after)
			t.Logf("%dx%d 100 bones, completed GPU frame: %v, %d bytes/frame, %d allocations/frame", size.X, size.Y, elapsed, (after.TotalAlloc-before.TotalAlloc)/10, (after.Mallocs-before.Mallocs)/10)
			if allocations := (after.Mallocs - before.Mallocs) / 10; allocations > 500 {
				t.Errorf("interleaved bones allocate %d objects per frame; limit 500", allocations)
			}
			dst.Deallocate()
		}
	})
}

func TestPenUploadsDoNotSubmitEarlierDrawing(t *testing.T) {
	runGPUFrame(t, func() {
		red, green := image.NewNRGBA(image.Rect(0, 0, 8, 8)), image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for i := 0; i < len(red.Pix); i += 4 {
			copy(red.Pix[i:], []byte{255, 0, 0, 128})
			copy(green.Pix[i:], []byte{0, 255, 0, 128})
		}
		lib := &media.Library{Costumes: [][]*media.Costume{{
			{Image: red, Bitmap: true, Width: 8, Height: 8, CenterX: 4, CenterY: 4},
			{Image: green, Bitmap: true, Width: 8, Height: 8, CenterX: 4, CenterY: 4},
		}}}
		r, reference := render.New(lib), render.New(lib)
		view := vm.View{Width: 16, Height: 16, Zoom: 100, Direction: 90}
		r.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		target := vm.DisplayTarget{State: vm.TargetState{Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
		commands := []vm.PenCommand{
			{Kind: "line", Line: [4]float64{-6, 6, 6, 6}, Size: 2, Color: color.NRGBA{R: 255, A: 180}},
			{Kind: "stamp", Target: target},
			{Kind: "line", Line: [4]float64{-6, 0, 6, 0}, Size: 2, Color: color.NRGBA{B: 255, A: 255}},
		}
		target.State.CurrentCostume = 1
		commands = append(commands, vm.PenCommand{Kind: "stamp", Target: target})
		for _, command := range commands {
			pen.Pen(command)
			reference.Pen(command)
		}
		got := make([]byte, 16*16*4)
		pen.image.ReadPixels(got)
		if !bytes.Equal(got, make([]byte, len(got))) {
			t.Fatal("uploading the next costume submitted earlier stamps/lines before the frame was ready")
		}
		pen.TouchingColor(nil, target, color.NRGBA{B: 255, A: 255}, nil)
		pen.image.ReadPixels(got)
		want, _ := reference.DisplayPen()
		premultiplied := image.NewRGBA(want.Bounds())
		draw.Draw(premultiplied, premultiplied.Bounds(), want, want.Bounds().Min, draw.Src)
		for i, n := range premultiplied.Pix {
			if delta := int(got[i]) - int(n); delta < -1 || delta > 1 {
				t.Fatalf("queued drawing changed order/pixels at %d: got %d want %d", i, got[i], n)
			}
		}
		cpu, _ := r.DisplayPen()
		if !bytes.Equal(cpu.Pix, want.Pix) {
			t.Fatal("color sensing missed pending commands")
		}
		for _, command := range commands {
			pen.Pen(command)
		}
		pen.Pen(vm.PenCommand{Kind: "clear"})
		pen.flush()
		pen.image.ReadPixels(got)
		if !bytes.Equal(got, make([]byte, len(got))) {
			t.Fatal("clear replayed discarded drawings")
		}
	})
}

func TestPenBatchesStandaloneTextureUploads(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.image = ebiten.NewImage(8, 8)
		pen.image.Fill(color.RGBA{R: 255, A: 255})
		var previous [4]byte
		pen.image.SubImage(image.Rect(0, 0, 1, 1)).(*ebiten.Image).ReadPixels(previous[:])
		pen.Pen(vm.PenCommand{Kind: "clear"})
		corners := [4][2]float64{{0, 0}, {8, 0}, {8, 8}, {0, 8}}
		var textures []*ebiten.Image
		for i, fill := range []color.NRGBA{{255, 0, 0, 128}, {0, 255, 0, 128}, {0, 0, 255, 128}, {255, 255, 0, 128}} {
			pixels := image.NewNRGBA(image.Rect(0, 0, 512+i, 512))
			draw.Draw(pixels, pixels.Bounds(), image.NewUniform(fill), image.Point{}, draw.Src)
			pixels.SetNRGBA(0, 0, color.NRGBA{A: 255}) // Keep a full-size, standalone texture.
			texture, _ := gpu.texture(r, render.DisplaySprite{Costume: &media.Costume{CacheKey: []string{"first", "second", "third", "fourth"}[i], Image: pixels}, Density: 1})
			textures = append(textures, texture)
			pen.stamp(texture, corners, vm.GraphicEffects{}, true)
		}
		pen.flushStamps()
		got := make([]byte, 8*8*4)
		pen.image.ReadPixels(got)
		pen.image.Clear()
		for _, texture := range textures {
			pen.stamp(texture, corners, vm.GraphicEffects{}, true)
			pen.flushStamps()
		}
		want := make([]byte, len(got))
		pen.image.ReadPixels(want)
		if !bytes.Equal(got, want) {
			t.Fatal("upload lookahead changed standalone texture composition")
		}
	})
}

func TestPenDrawQueueIsBounded(t *testing.T) {
	runGPUFrame(t, func() {
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(render.New(&media.Library{}))
		if err != nil {
			t.Fatal(err)
		}
		pen.image = ebiten.NewImage(8, 8)
		a, b := ebiten.NewImage(1, 1), ebiten.NewImage(1, 1)
		defer a.Deallocate()
		defer b.Deallocate()
		a.Fill(color.RGBA{R: 255, A: 255})
		b.Fill(color.RGBA{G: 255, A: 255})
		corners := [4][2]float64{{0, 0}, {8, 0}, {8, 8}, {0, 8}}
		for i := 0; i < penQueuedQuadLimit*3; i++ {
			texture := a
			if i%2 != 0 {
				texture = b
			}
			pen.stamp(texture, corners, vm.GraphicEffects{}, true)
			if len(pen.drawVertices) > penQueuedQuadLimit*4 || cap(pen.drawVertices) > penQueuedQuadLimit*4 || len(pen.draws) > penQueuedQuadLimit {
				t.Fatal("drawing without presentation grew the queue beyond its limit")
			}
		}
		pen.flushStamps()
		var pixel [4]byte
		pen.image.SubImage(image.Rect(0, 0, 1, 1)).(*ebiten.Image).ReadPixels(pixel[:])
		if pixel != [4]byte{0, 255, 0, 255} {
			t.Fatalf("queue limit lost final drawing: %v", pixel)
		}
		if len(pen.draws) != 0 || len(pen.uploads) != 0 {
			t.Fatal("submitted batches retained textures")
		}
	})
}

func TestPenClearWaitsForDrawing(t *testing.T) {
	runGPUFrame(t, func() {
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(render.New(&media.Library{}))
		if err != nil {
			t.Fatal(err)
		}
		pen.image = ebiten.NewImage(8, 8)
		pen.image.Fill(color.RGBA{R: 255, A: 255})
		pen.Pen(vm.PenCommand{Kind: "clear"})
		/*
			Even an eviction flush must leave an empty queue deferred, so the
			incoming texture can upload before any GPU drawing for this frame.
		*/
		pen.flushStamps()
		var pixel [4]byte
		pen.image.SubImage(image.Rect(0, 0, 1, 1)).(*ebiten.Image).ReadPixels(pixel[:])
		if pixel != [4]byte{255, 0, 0, 255} {
			t.Fatalf("clear submitted a GPU draw before the next costume upload: %v", pixel)
		}
		// A clear-only frame must still be transparent when presented.
		dst := ebiten.NewImage(8, 8)
		defer dst.Deallocate()
		gpu.drawPen(dst, pen.Renderer)
		pen.image.SubImage(image.Rect(0, 0, 1, 1)).(*ebiten.Image).ReadPixels(pixel[:])
		if pixel != [4]byte{} {
			t.Fatalf("presentation missed a clear-only frame: %v", pixel)
		}
	})
}

func TestDeferredPenDefersTexturesAndPreservesOrder(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		draw.Draw(pixels, pixels.Bounds(), &image.Uniform{color.NRGBA{R: 255, A: 255}}, image.Point{}, draw.Src)
		lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 4, Height: 4, CenterX: 2, CenterY: 2}}}}
		actual, reference := render.New(lib), render.New(lib)
		view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90}
		actual.SetView(view)
		reference.SetView(view)
		var gpu, referenceGPU stageGPU
		defer gpu.Close()
		defer referenceGPU.Close()
		p, err := gpu.enablePen(actual)
		if err != nil {
			t.Fatal(err)
		}
		direct, err := referenceGPU.enablePen(reference)
		if err != nil {
			t.Fatal(err)
		}
		recorder := deferredPen{p}
		gpu.measureUploads = true
		target := vm.DisplayTarget{State: vm.TargetState{Size: 100, StretchX: 100, StretchY: 100, Direction: 90, X: -5}}
		points := [4]float64{-10, 0, 10, 0}
		commands := []vm.PenCommand{
			{Kind: "stamp", Target: target},
			{Kind: "line", Line: points, Size: 3, Color: color.NRGBA{B: 255, A: 255}},
			{Kind: "stamp", Target: target},
		}
		for _, command := range commands {
			recorder.Pen(command)
			direct.Pen(command)
		}
		commands[1].Line[0] = 1000
		target.State.X = 1000
		if gpu.uploadCount != 0 || len(gpu.textures) != 0 || p.image != nil {
			t.Fatal("recording touched GPU textures before drawing")
		}
		got, want := ebiten.NewImage(32, 24), ebiten.NewImage(32, 24)
		defer got.Deallocate()
		defer want.Deallocate()
		if err := gpu.Draw(got, actual, nil); err != nil {
			t.Fatal(err)
		}
		if err := referenceGPU.Draw(want, reference, nil); err != nil {
			t.Fatal(err)
		}
		a, b := make([]byte, 32*24*4), make([]byte, 32*24*4)
		got.ReadPixels(a)
		want.ReadPixels(b)
		if !bytes.Equal(a, b) {
			t.Fatal("deferred stamp/line order or coordinates changed")
		}
		if gpu.uploadCount == 0 || len(p.commands) != 0 {
			t.Fatal("draw did not consume commands")
		}
	})
}

func TestDeferredPenClearAndQueueLimit(t *testing.T) {
	runGPUFrame(t, func() {
		renderer := render.New(&media.Library{})
		renderer.SetView(vm.View{Width: 16, Height: 16, Zoom: 100, Direction: 90})
		var gpu stageGPU
		defer gpu.Close()
		p, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		recorder := deferredPen{p}
		line := vm.PenCommand{Kind: "line", Line: [4]float64{-2, 0, 2, 0}, Size: 2, Color: color.NRGBA{R: 255, A: 255}}
		for i := 0; i < 20; i++ {
			recorder.Pen(line)
		}
		recorder.Pen(vm.PenCommand{Kind: "clear"})
		if len(p.commands) != 0 || p.image != nil {
			t.Fatal("clear rendered obsolete commands")
		}
		for i := 0; i < pendingPenLimit+1; i++ {
			recorder.Pen(line)
		}
		if len(p.commands) > pendingPenLimit || cap(p.commands) > pendingPenLimit {
			t.Fatal("unbounded command queue")
		}
		p.flush()
		got, _ := renderer.DisplayPen()
		if got == nil || got.NRGBAAt(8, 8).R != 255 {
			t.Fatal("queue overflow lost persistent drawing")
		}
	})
}

func TestDeferredPenSensingAndViewBarriers(t *testing.T) {
	runGPUFrame(t, func() {
		renderer := render.New(&media.Library{})
		reference := render.New(&media.Library{})
		view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90}
		renderer.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		p, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		recorder := deferredPen{p}
		line := vm.PenCommand{Kind: "line", Line: [4]float64{-3.2, 0.3, 4.7, 1.1}, Size: 2, Color: color.NRGBA{G: 255, A: 255}}
		recorder.Pen(line)
		reference.Pen(line)
		view.X = 10
		recorder.SetView(view)
		reference.SetView(view)
		if len(p.commands) != 1 {
			t.Fatal("camera change drained the queue")
		}
		recorder.TouchingColor(nil, vm.DisplayTarget{}, color.NRGBA{G: 255, A: 255}, nil)
		a, _ := renderer.DisplayPen()
		b, _ := reference.DisplayPen()
		if a == nil || !bytes.Equal(a.Pix, b.Pix) {
			t.Fatal("sensing missed preceding commands")
		}
		recorder.Pen(line)
		reference.Pen(line)
		view.PenNoAntialias = true
		view.Width = 40
		recorder.SetView(view)
		reference.SetView(view)
		p.flush()
		a, _ = renderer.DisplayPen()
		b, _ = reference.DisplayPen()
		if a == nil || !bytes.Equal(a.Pix, b.Pix) {
			t.Fatal("view change replayed earlier commands with new settings")
		}
	})
}

func TestDeferredPenOutputResizePreservesRaster(t *testing.T) {
	runGPUFrame(t, func() {
		a, b := render.New(&media.Library{}), render.New(&media.Library{})
		view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90, HighQualityPen: true}
		a.SetView(view)
		b.SetView(view)
		var gpu, ref stageGPU
		defer gpu.Close()
		defer ref.Close()
		p, err := gpu.enablePen(a)
		if err != nil {
			t.Fatal(err)
		}
		direct, err := ref.enablePen(b)
		if err != nil {
			t.Fatal(err)
		}
		cmd := vm.PenCommand{Kind: "line", Line: [4]float64{-5.2, -2.3, 6.4, 4.1}, Size: 1.7, Color: color.NRGBA{R: 255, A: 255}}
		deferredPen{p}.Pen(cmd)
		direct.Pen(cmd)
		got, want := ebiten.NewImage(64, 48), ebiten.NewImage(64, 48)
		defer got.Deallocate()
		defer want.Deallocate()
		if err := gpu.Draw(got, a, nil); err != nil {
			t.Fatal(err)
		}
		if err := ref.Draw(want, b, nil); err != nil {
			t.Fatal(err)
		}
		pixels, expected := make([]byte, 64*48*4), make([]byte, 64*48*4)
		got.ReadPixels(pixels)
		want.ReadPixels(expected)
		if !bytes.Equal(pixels, expected) {
			t.Fatal("resize changed previously recorded pen rasterization")
		}
	})
}

func TestPenTransparentMarginsKeepSmallGeometry(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 512, 512))
		for y := 245; y < 267; y++ {
			for x := 250; x < 262; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{R: 200, A: 255})
			}
		}
		lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 512, Height: 512, CenterX: 256, CenterY: 256}}}}
		renderer := render.New(lib)
		renderer.SetView(vm.View{Width: 512, Height: 512, Zoom: 100, Direction: 90})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		pen.Pen(vm.PenCommand{Kind: "stamp", Target: vm.DisplayTarget{State: vm.TargetState{
			Size: 100, Direction: 90, StretchX: 100, StretchY: 100,
		}}})
		if len(pen.stampVertices) != 4 {
			t.Fatalf("stamp has %d vertices", len(pen.stampVertices))
		}
		v := pen.stampVertices
		area := math.Abs(float64((v[1].DstX-v[0].DstX)*(v[3].DstY-v[0].DstY) - (v[1].DstY-v[0].DstY)*(v[3].DstX-v[0].DstX)))
		if area > 32*32 {
			t.Fatalf("small stamp shades %.0f pixels of geometry; want at most 1024", area)
		}
	})
}

func TestInvisibleStampsKeepBatchAndSensingHistory(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 512, 512))
		draw.Draw(pixels, image.Rect(250, 250, 262, 262), image.NewUniform(color.NRGBA{R: 200, A: 255}), image.Point{}, draw.Src)
		empty := image.NewNRGBA(image.Rect(0, 0, 64, 64))
		lib := &media.Library{Costumes: [][]*media.Costume{{
			{Image: pixels, Width: 512, Height: 512, CenterX: 256, CenterY: 256},
			{Image: empty, Width: 64, Height: 64, CenterX: 32, CenterY: 32},
		}}}
		renderer, reference := render.New(lib), render.New(lib)
		view := vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90}
		renderer.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(renderer)
		if err != nil {
			t.Fatal(err)
		}
		command := vm.PenCommand{Kind: "stamp", Target: vm.DisplayTarget{State: vm.TargetState{
			Size: 100, Direction: 90, StretchX: 100, StretchY: 100,
		}}}
		pen.Pen(command)
		reference.Pen(command)
		for i := 0; i < 100; i++ {
			command.Target.State.X = 100
			command.Target.State.CurrentCostume = 0
			if i%2 == 0 {
				command.Target.State.X = .25
				command.Target.State.CurrentCostume = 1
			}
			pen.Pen(command)
			reference.Pen(command)
		}
		if len(pen.draws) != 0 || len(pen.stampVertices) != 4 {
			t.Fatalf("invisible stamps split the visible batch: queued=%d vertices=%d", len(pen.draws), len(pen.stampVertices))
		}
		if len(pen.pending) != 101 {
			t.Fatalf("color sensing lost stamp history: got %d commands", len(pen.pending))
		}
		pen.flush()
		got, _ := renderer.DisplayPen()
		want, _ := reference.DisplayPen()
		if got == nil || want == nil || !bytes.Equal(got.Pix, want.Pix) {
			t.Fatal("skipped GPU stamps changed CPU sensing pixels")
		}
	})
}

func TestPenHistoryReusesLineStorage(t *testing.T) {
	pen := &penGPU{keepHistory: true}
	points := [4]float64{-3, 0, 3, 0}
	cmd := vm.PenCommand{Kind: "line", Line: points, Size: 1}
	pen.remember(cmd)
	cmd.Line[0] = -8
	pen.remember(cmd)
	if pen.pending[0].Line[0] != -3 || pen.pending[1].Line[0] != -8 {
		t.Fatal("recorded lines share mutable input points")
	}
	pen.preserveSurfaceHistory(false)
	allocs := testing.AllocsPerRun(100, func() {
		for range 64 {
			pen.remember(cmd)
		}
		pen.preserveSurfaceHistory(false)
	})
	if allocs != 0 {
		t.Fatalf("retaining line history allocates each frame: %g", allocs)
	}
}

func BenchmarkPenLineHistory(b *testing.B) {
	pen := &penGPU{keepHistory: true}
	cmd := vm.PenCommand{Kind: "line", Line: [4]float64{-3, 0, 3, 0}, Size: 1}
	for range 64 {
		pen.remember(cmd)
	}
	pen.preserveSurfaceHistory(false)
	b.ReportAllocs()
	for b.Loop() {
		for range 64 {
			pen.remember(cmd)
		}
		pen.preserveSurfaceHistory(false)
	}
}
