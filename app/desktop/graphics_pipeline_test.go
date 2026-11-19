package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/render"
	"gowarp/core/vm"
)

func pipelineRenderer() *render.Renderer {
	pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < len(pixels.Pix); i += 4 {
		copy(pixels.Pix[i:], []byte{255, 0, 0, 255})
	}
	r := render.New(&media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 8, Height: 8, CenterX: 4, CenterY: 4}}}})
	r.SetView(vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90})
	return r
}

func TestGraphicsPipelineKeepsOverflowTexturesAcrossBatches(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 1600, 640))
		draw.Draw(pixels, pixels.Bounds(), image.NewUniform(color.NRGBA{R: 255, A: 255}), image.Point{}, draw.Src)
		draw.Draw(pixels, image.Rect(0, 0, 2, 2), image.NewUniform(color.NRGBA{G: 255, A: 255}), image.Point{}, draw.Src)
		costumes := make([]*media.Costume, 34)
		for i := range costumes {
			costumes[i] = &media.Costume{CacheKey: fmt.Sprintf("pipeline:%d", i), Image: pixels, Bitmap: true, Width: 8, Height: 8, CenterX: 4, CenterY: 4}
		}
		r := render.New(&media.Library{Costumes: [][]*media.Costume{costumes}})
		r.SetView(vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, HighQualityPen: true})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 32; i++ {
			gpu.texture(r, render.DisplaySprite{Costume: costumes[i], Density: 1})
		}
		p := newGraphicsPipeline(deferredPen{pen}, pen.replayCommands)
		defer p.close()
		gpu.measureUploads = true
		if err := p.run(context.Background(), func(context.Context) error {
			for i := 0; i < 1024; i++ {
				p.Pen(vm.PenCommand{Kind: "stamp", Target: vm.DisplayTarget{State: vm.TargetState{CurrentCostume: i % 34, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}})
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if gpu.uploadCount > 2 {
			t.Fatalf("reuploaded overflow textures across batches: %d uploads", gpu.uploadCount)
		}
		if len(gpu.transientTextures) != 0 {
			t.Fatal("tick retained temporary textures")
		}
	})
}

func pipelineStampProject() *project.Project {
	blocks := map[string]*project.Block{"flag": {Opcode: "event_whenflagclicked", TopLevel: true, Next: "stamp0"}}
	for i := 0; i < 600; i++ {
		next := ""
		if i < 599 {
			next = fmt.Sprintf("stamp%d", i+1)
		}
		blocks[fmt.Sprintf("stamp%d", i)] = &project.Block{Opcode: "pen_stamp", Next: next}
	}
	return &project.Project{Targets: []*project.Target{{Name: "Sprite", Size: 100, Direction: 90, Blocks: blocks}}}
}

func TestControllerPipelineKeepsTickAndPenState(t *testing.T) {
	project := pipelineStampProject()
	var states [][]vm.TargetState
	var pictures [][]byte
	for _, parallel := range []bool{false, true} {
		r := pipelineRenderer()
		c := newController(project, r.Assets, Options{FPS: 60})
		c.renderer = r
		if parallel {
			c.graphics = newGraphicsPipeline(r, nil)
			defer c.graphics.close()
			c.vm.SetGraphics(c.graphics)
		} else {
			c.vm.SetGraphics(r)
		}
		for i := 0; i < 3; i++ {
			c.vm.GreenFlag()
			before := c.vm.Frames()
			if err := c.tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			if c.vm.Frames() != before+1 {
				t.Fatal("tick count changed")
			}
		}
		states = append(states, c.vm.Snapshot())
		pictures = append(pictures, append([]byte(nil), r.Draw(nil).Pix...))
		if parallel && c.graphics.penCount != 600 {
			t.Fatalf("fixture executed %d stamps", c.graphics.penCount)
		}
	}
	if !bytes.Equal(pictures[0], pictures[1]) || states[0][0].X != states[1][0].X {
		t.Fatal("controller pipeline changed final state")
	}
}

func TestControllerPipelineDefersIntermediateCatchupDrawing(t *testing.T) {
	runGPUFrame(t, func() {
		r := pipelineRenderer()
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		c := newController(pipelineStampProject(), r.Assets, Options{FPS: 60})
		c.graphics = newGraphicsPipeline(deferredPen{pen}, pen.replayCommands)
		defer c.graphics.close()
		c.vm.SetGraphics(c.graphics)
		if err := c.tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		c.vm.GreenFlag()
		c.graphics.Pen(vm.PenCommand{Kind: "clear"})
		c.credit, c.tickCount, c.tickLimit = 1, 0, 2
		if err := c.tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(pen.commands) != 600 || len(gpu.textures) != 0 {
			t.Fatal("intermediate catch-up tick rendered stamps that the next tick may clear")
		}
	})
}

func TestGraphicsPipelineGPU(t *testing.T) {
	runGPUFrame(t, func() {
		var pictures [][]byte
		for _, parallel := range []bool{false, true} {
			r := pipelineRenderer()
			var gpu stageGPU
			defer gpu.Close()
			pen, err := gpu.enablePen(r)
			if err != nil {
				t.Fatal(err)
			}
			p := newGraphicsPipeline(deferredPen{pen}, pen.replayCommands)
			defer p.close()
			var g vm.Graphics = deferredPen{pen}
			if parallel {
				g = p
			}
			dst := ebiten.NewImage(64, 48)
			defer dst.Deallocate()
			pixels := make([]byte, 64*48*4)
			work := func(context.Context) error {
				g.Pen(vm.PenCommand{Kind: "clear"})
				points := [4]float64{}
				for i := 0; i < 2048; i++ {
					x := float64(i)
					for j := 0; j < 80; j++ {
						x = math.Sin(x) + 1.25
					}
					points = [4]float64{-25, x, 25, float64(i%20) - 10}
					if i%3 == 0 {
						g.Pen(vm.PenCommand{Kind: "stamp", Target: vm.DisplayTarget{State: vm.TargetState{X: x, Y: float64(i%30) - 15, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, Effects: vm.GraphicEffects{Ghost: 50}}}})
					} else {
						g.Pen(vm.PenCommand{Kind: "line", Line: points, Size: 2, Color: color.NRGBA{R: 255, A: 255}})
					}
				}
				return nil
			}
			n := 2
			if os.Getenv("GOWARP_PIPELINE_BENCH") == "1" {
				n = 100
			}
			started := time.Now()
			for i := 0; i < n; i++ {
				if parallel {
					err = p.run(context.Background(), work)
				} else {
					err = work(context.Background())
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := gpu.Draw(dst, r, nil); err != nil {
					t.Fatal(err)
				}
				dst.ReadPixels(pixels)
			}
			t.Logf("parallel=%v complete frame=%s", parallel, time.Since(started)/time.Duration(n))
			pictures = append(pictures, pixels)
		}
		if !bytes.Equal(pictures[0], pictures[1]) {
			t.Fatal("parallel pen changed GPU pixels")
		}
	})
}

func TestGraphicsPipelineSensingBeforeClear(t *testing.T) {
	runGPUFrame(t, func() {
		r := pipelineRenderer()
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = true
		p := newGraphicsPipeline(deferredPen{pen}, pen.replayCommands)
		defer p.close()
		target := vm.DisplayTarget{State: vm.TargetState{Size: 100, StretchX: 100, StretchY: 100, Direction: 90, Visible: true}}
		green := color.NRGBA{G: 255, A: 255}
		if err := p.run(context.Background(), func(context.Context) error {
			p.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{-10, 0, 10, 0}, Size: 3, Color: green})
			if !p.TouchingColor(nil, target, green, nil) {
				return errors.New("sensing did not see queued pen")
			}
			p.Pen(vm.PenCommand{Kind: "clear"})
			if p.TouchingColor(nil, target, green, nil) {
				return errors.New("sensing did not see clear")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestGraphicsPipelinePreservesPenAndSensing(t *testing.T) {
	actual, expected := pipelineRenderer(), pipelineRenderer()
	p := newGraphicsPipeline(actual, nil)
	defer p.close()
	draw := func(g vm.Graphics) {
		points := [4]float64{-25, -12, 25, 12}
		for i := 0; i < 600; i++ {
			g.Pen(vm.PenCommand{Kind: "line", Line: points, Size: 2, Color: color.NRGBA{R: 255, A: 200}})
			points[1] = float64(i%24) - 12
			g.Pen(vm.PenCommand{Kind: "stamp", Target: vm.DisplayTarget{State: vm.TargetState{X: float64(i%50) - 25, Y: float64(i%30) - 15, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}})
		}
	}
	draw(expected)
	err := p.run(context.Background(), func(context.Context) error {
		draw(p)
		// A synchronous query must finish earlier writes before returning.
		p.Bounds(vm.DisplayTarget{}, false)
		if !bytes.Equal(actual.Draw(nil).Pix, expected.Draw(nil).Pix) {
			return errors.New("sensing barrier missed preceding drawing or borrowed line points changed")
		}
		p.Pen(vm.PenCommand{Kind: "clear"})
		expected.Pen(vm.PenCommand{Kind: "clear"})
		p.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{-15, 0, 15, 0}, Size: 3, Color: color.NRGBA{G: 255, A: 255}})
		expected.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{-15, 0, 15, 0}, Size: 3, Color: color.NRGBA{G: 255, A: 255}})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual.Draw(nil).Pix, expected.Draw(nil).Pix) {
		t.Fatal("tick returned before its final pen commands were applied")
	}
}

func TestGraphicsPipelineFailureDrainsAndRemainsUsable(t *testing.T) {
	r := pipelineRenderer()
	p := newGraphicsPipeline(r, nil)
	defer p.close()
	want := errors.New("tick failed")
	if err := p.run(context.Background(), func(context.Context) error {
		p.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{-10, 0, 10, 0}, Size: 3, Color: color.NRGBA{R: 255, A: 255}})
		return want
	}); !errors.Is(err, want) {
		t.Fatalf("run: %v", err)
	}
	if r.Draw(nil).RGBAAt(32, 24).R != 255 {
		t.Fatal("drawing preceding a failed block was lost")
	}
	if err := p.run(context.Background(), func(context.Context) error {
		p.Pen(vm.PenCommand{Kind: "clear"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGraphicsPipelineSerialQueriesDoNotAddAllocations(t *testing.T) {
	r := pipelineRenderer()
	p := newGraphicsPipeline(r, nil)
	defer p.close()
	target := vm.DisplayTarget{State: vm.TargetState{Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
	query := func(g vm.Graphics) {
		g.SetView(vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90})
		g.Bounds(target, false)
		g.CostumeSize(target)
		g.Touching(nil, target, "_edge_", 0, 0)
		g.TouchingColor(nil, target, color.NRGBA{G: 255, A: 255}, nil)
	}
	direct := testing.AllocsPerRun(100, func() { query(r) })
	wrapped := testing.AllocsPerRun(100, func() { query(p) })
	if wrapped > direct {
		t.Fatalf("serial query allocations: direct=%g pipeline=%g", direct, wrapped)
	}
}

type failingPipelineRenderer struct {
	*render.Renderer
	fail bool
}

type waitingPipelineRenderer struct {
	*render.Renderer
	recorded <-chan struct{}
}

func (r *waitingPipelineRenderer) Pen(cmd vm.PenCommand) {
	if r.recorded != nil {
		select {
		case <-r.recorded:
		case <-time.After(3 * time.Second):
			panic("VM recording waited before reaching the existing pen queue limit")
		}
		r.recorded = nil
	}
	r.Renderer.Pen(cmd)
}

func TestGraphicsPipelineDoesNotShortenPenRecordingWindow(t *testing.T) {
	recorded := make(chan struct{})
	p := newGraphicsPipeline(&waitingPipelineRenderer{Renderer: pipelineRenderer(), recorded: recorded}, nil)
	defer p.close()
	defer func() {
		if failure := recover(); failure != nil {
			t.Fatal(failure)
		}
	}()
	if err := p.run(context.Background(), func(context.Context) error {
		for i := 0; i < 4000; i++ {
			p.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{0, 0, 2, 2}, Size: 1})
		}
		close(recorded)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func (r *failingPipelineRenderer) Pen(cmd vm.PenCommand) {
	if r.fail {
		panic("display failure")
	}
	r.Renderer.Pen(cmd)
}

func TestGraphicsPipelinePanicReleasesWorker(t *testing.T) {
	r := &failingPipelineRenderer{Renderer: pipelineRenderer()}
	p := newGraphicsPipeline(r, nil)
	defer p.close()
	for i := 0; i < 6; i++ {
		r.fail = true
		func() {
			defer func() {
				if got := recover(); got != "display failure" {
					t.Fatalf("panic: %v", got)
				}
			}()
			_ = p.run(context.Background(), func(ctx context.Context) error {
				if i%2 == 0 {
					p.Pen(vm.PenCommand{Kind: "clear"})
				}
				for j := 0; j < 4096; j++ {
					p.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{0, 0, 10, 10}, Size: 1})
				}
				return ctx.Err()
			})
		}()
		r.fail = false
		if err := p.run(context.Background(), func(context.Context) error {
			p.Pen(vm.PenCommand{Kind: "clear"})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func BenchmarkGraphicsPipeline(b *testing.B) {
	for _, parallel := range []bool{false, true} {
		name := "serial"
		if parallel {
			name = "parallel"
		}
		b.Run(name, func(b *testing.B) {
			r := pipelineRenderer()
			p := newGraphicsPipeline(r, nil)
			defer p.close()
			var g vm.Graphics = r
			if parallel {
				g = p
			}
			run := func(context.Context) error {
				g.Pen(vm.PenCommand{Kind: "clear"})
				points := [4]float64{}
				for i := 0; i < 2048; i++ {
					x := float64(i)
					for j := 0; j < 80; j++ {
						x = math.Sin(x) + 1.25
					}
					points = [4]float64{-25, x, 25, float64(i%20) - 10}
					g.Pen(vm.PenCommand{Kind: "line", Line: points, Size: 2, Color: color.NRGBA{R: 255, A: 255}})
				}
				return nil
			}
			b.ReportAllocs()
			for b.Loop() {
				if parallel {
					_ = p.run(context.Background(), run)
				} else {
					_ = run(context.Background())
				}
			}
		})
	}
}

func TestGraphicsPipelineParallelQueriesReuseStorage(t *testing.T) {
	r := pipelineRenderer()
	p := newGraphicsPipeline(r, nil)
	defer p.close()
	target := vm.DisplayTarget{State: vm.TargetState{Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
	run := func(context.Context) error {
		for i := 0; i < 100; i++ {
			target.State.X = float64(i)
			box, ok := p.Bounds(target, false)
			if !ok || box.Left != float64(i)-4 || box.Right != float64(i)+4 {
				return fmt.Errorf("bounds did not read current position: %+v", box)
			}
			w, h := p.CostumeSize(target)
			if w != 8 || h != 8 {
				return fmt.Errorf("costume size=%g,%g", w, h)
			}
		}
		return nil
	}
	allocations := testing.AllocsPerRun(10, func() {
		if err := p.run(context.Background(), run); err != nil {
			t.Fatal(err)
		}
	})
	if allocations > 10 {
		t.Fatalf("200 queries allocated %g objects; only per-tick setup should allocate", allocations)
	}
}

func BenchmarkGraphicsPipelineQueries(b *testing.B) {
	r := pipelineRenderer()
	p := newGraphicsPipeline(r, nil)
	defer p.close()
	target := vm.DisplayTarget{State: vm.TargetState{Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
	run := func(context.Context) error {
		for i := 0; i < 1000; i++ {
			target.State.X = float64(i)
			if _, ok := p.Bounds(target, false); !ok {
				return errors.New("missing geometry")
			}
			p.CostumeSize(target)
		}
		return nil
	}
	if err := p.run(context.Background(), run); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := p.run(context.Background(), run); err != nil {
			b.Fatal(err)
		}
	}
}

func TestGraphicsPipelineQueriesReadCurrentInputs(t *testing.T) {
	p := newGraphicsPipeline(pipelineRenderer(), nil)
	defer p.close()
	target := vm.DisplayTarget{State: vm.TargetState{X: 12, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, Visible: true}}
	other := target
	other.State.Name, other.State.CloneID = "other", 1
	scene := []vm.DisplayTarget{other}
	red := color.NRGBA{R: 255, A: 255}
	green := color.NRGBA{G: 255, A: 255}
	err := p.run(context.Background(), func(context.Context) error {
		for i := 0; i < 4; i++ {
			hit := i%2 == 0
			width := 64.0
			mouseX := 0.0
			scene[0].State.X = -12
			mask := green
			if hit {
				width, mouseX = 24, 12
				scene[0].State.X = 12
				mask = red
			}
			p.SetView(vm.View{Width: width, Height: 48, Zoom: 100, Direction: 90})
			if p.Touching(nil, target, "_edge_", 0, 0) != hit {
				return errors.New("edge query used stale view")
			}
			if p.Touching(nil, target, "_mouse_", mouseX, 0) != hit {
				return errors.New("mouse query used stale coordinates")
			}
			if p.Touching(scene, target, "other", 0, 0) != hit {
				return errors.New("sprite query used stale scene")
			}
			p.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{8, 0, 16, 0}, Size: 3, Color: green})
			if p.TouchingColor(nil, target, green, &mask) != hit {
				return errors.New("color query used stale mask")
			}
			if !p.TouchingColor(nil, target, green, nil) {
				return errors.New("unmasked query reused previous mask")
			}
			p.Pen(vm.PenCommand{Kind: "clear"})
			if p.TouchingColor(nil, target, green, nil) {
				return errors.New("color query missed pen clear")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGraphicsPipelineMeasuresOnlyDiagnosticRuns(t *testing.T) {
	p := newGraphicsPipeline(pipelineRenderer(), nil)
	defer p.close()
	for _, measured := range []bool{true, false} {
		p.measure = measured
		p.stats = graphicsPipelineStats{}
		err := p.run(context.Background(), func(context.Context) error {
			p.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{-4, 0, 4, 0}, Size: 1, Color: color.NRGBA{A: 255}})
			w, h := p.CostumeSize(vm.DisplayTarget{})
			if w != 8 || h != 8 {
				return fmt.Errorf("costume dimensions: %g x %g", w, h)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if measured {
			if p.stats.ParallelTicks != 1 || p.stats.Batches != 1 || p.stats.Queries != 1 ||
				p.stats.QueryWait <= 0 || p.stats.DrawTime <= 0 {
				t.Fatalf("missing graphics timing: %+v", p.stats)
			}
		} else if p.stats != (graphicsPipelineStats{}) {
			t.Fatalf("ordinary play collected graphics timing: %+v", p.stats)
		}
	}
}

func TestPlayerResetsGraphicsTimingEachUpdate(t *testing.T) {
	r := pipelineRenderer()
	c := newController(pipelineStampProject(), r.Assets, Options{FPS: 60})
	c.renderer = r
	c.graphics = newGraphicsPipeline(r, nil)
	defer c.graphics.close()
	c.vm.SetGraphics(c.graphics)
	g := &game{controller: c, ctx: context.Background(), options: Options{Diagnostic: func(string) {}}}
	for range 2 {
		c.graphics.penCount = 600
		c.vm.GreenFlag()
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
		if c.graphics.stats.ParallelTicks != 1 || c.graphics.stats.Batches != 5 {
			t.Fatalf("graphics timings were disabled or accumulated across updates: %+v", c.graphics.stats)
		}
	}
	g.options.Diagnostic = nil
	c.vm.GreenFlag()
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.graphics.measure || c.graphics.stats != (graphicsPipelineStats{}) {
		t.Fatal("non-diagnostic update retained graphics timing")
	}
}
