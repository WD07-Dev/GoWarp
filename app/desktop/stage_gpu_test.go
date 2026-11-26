package desktop

import (
	"bytes"
	"context"
	"gowarp/core/extension"
	penplus "gowarp/test/extensions/penP"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/render"
	"gowarp/core/vm"
)

type stageGPUCheckGame struct{ check func() }

func TestStageSpriteBatchPreservesPixels(t *testing.T) {
	runGPUFrame(t, func() {
		var reference, batched stageGPU
		defer reference.Close()
		defer batched.Close()
		if err := reference.initShader(); err != nil {
			t.Fatal(err)
		}
		if _, err := batched.enablePen(render.New(&media.Library{})); err != nil {
			t.Fatal(err)
		}
		pixels := image.NewNRGBA(image.Rect(0, 0, 12, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 12; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{uint8(x * 21), uint8(y * 31), 103, uint8((x + y) * 17)})
			}
		}
		src := ebiten.NewImage(12, 8)
		defer src.Deallocate()
		stageUpload(src, pixels)
		mask := ebiten.NewImage(64, 48)
		defer mask.Deallocate()
		mask.SubImage(image.Rect(10, 10, 35, 35)).(*ebiten.Image).Fill(color.White)
		for _, nearest := range []bool{false, true} {
			for _, repeated := range []bool{false, true} {
				reference.repeatedPixels = map[*ebiten.Image]bool{src: repeated}
				batched.repeatedPixels = map[*ebiten.Image]bool{src: repeated}
				for _, blend := range []string{"", "additive", "multiply"} {
					got, want := ebiten.NewImage(64, 48), ebiten.NewImage(64, 48)
					got.Fill(color.RGBA{R: 10, G: 30, B: 70, A: 255})
					want.Fill(color.RGBA{R: 10, G: 30, B: 70, A: 255})
					for i, effects := range []vm.GraphicEffects{
						{}, {Ghost: 43}, {Brightness: 20}, {Brightness: -100},
						{Brightness: 100, Ghost: 73}, {Color: 78, Ghost: 37},
						{Color: 200}, {Color: math.NaN()}, {Color: math.Inf(1)},
						{Whirl: 30}, {Mosaic: 25}, {Pixelate: 7}, {Fisheye: 20},
					} {
						x, y := float64(i%4)*11+.25, float64(i/4)*8+.5
						corners := [4][2]float64{{x, y}, {x + 20, y + 3}, {x + 16, y + 18}, {x - 4, y + 15}}
						for _, gpu := range []*stageGPU{&reference, &batched} {
							dst := want
							if gpu == &batched {
								dst = got
							}
							gpu.quadVisual(dst, src, corners, 12, 8, effects, nearest, false, blend, nil)
						}
					}
					// A masked sprite must keep the general shader's clipping path.
					corners := [4][2]float64{{0, 0}, {32, 0}, {32, 24}, {0, 24}}
					reference.quadVisual(want, src, corners, 12, 8, vm.GraphicEffects{}, nearest, false, blend, mask)
					batched.quadVisual(got, src, corners, 12, 8, vm.GraphicEffects{}, nearest, false, blend, mask)
					a, b := make([]byte, 64*48*4), make([]byte, 64*48*4)
					got.ReadPixels(a)
					want.ReadPixels(b)
					got.Deallocate()
					want.Deallocate()
					for i := range a {
						if delta := int(a[i]) - int(b[i]); delta < -1 || delta > 1 {
							t.Fatalf("nearest=%t repeated=%t blend=%s pixel=%d channel=%d: got %d want %d",
								nearest, repeated, blend, i/4, i%4, a[i], b[i])
						}
					}
				}
			}
		}
	})
}

func TestStageSpriteColorBatch(t *testing.T) {
	runGPUFrame(t, func() {
		var gpu stageGPU
		defer gpu.Close()
		if _, err := gpu.enablePen(render.New(&media.Library{})); err != nil {
			t.Fatal(err)
		}
		src := ebiten.NewImage(8, 8)
		defer src.Deallocate()
		src.Fill(color.White)
		dst := ebiten.NewImage(320, 240)
		defer dst.Deallocate()
		t.Log("sprite batch begin")
		for i := 0; i < 300; i++ {
			x, y := float64(i%20)*16, float64(i/20)*16
			gpu.quad(dst, src, [4][2]float64{{x, y}, {x + 8, y}, {x + 8, y + 8}, {x, y + 8}},
				8, 8, vm.GraphicEffects{Color: float64(i % 200), Brightness: float64(i % 80), Ghost: float64(i % 90)}, true, false)
		}
		pixels := make([]byte, 320*240*4)
		dst.ReadPixels(pixels)
		t.Log("sprite batch end")
		if gpu.uniforms != nil {
			t.Fatal("color-only sprites still set per-draw effect uniforms")
		}
	})
}

func TestStageSpriteAtlasBatchPreservesOrderAndClipping(t *testing.T) {
	runGPUFrame(t, func() {
		var reference, batched stageGPU
		defer reference.Close()
		defer batched.Close()
		if err := reference.initShader(); err != nil {
			t.Fatal(err)
		}
		if _, err := batched.enablePen(render.New(&media.Library{})); err != nil {
			t.Fatal(err)
		}
		var textures [2]*ebiten.Image
		for i, size := range []image.Point{{7, 9}, {13, 5}} {
			pixels := image.NewNRGBA(image.Rectangle{Max: size})
			for y := 0; y < size.Y; y++ {
				for x := 0; x < size.X; x++ {
					pixels.SetNRGBA(x, y, color.NRGBA{uint8(23 * x), uint8(41 * y), uint8(60 + i*90), 137})
				}
			}
			textures[i] = ebiten.NewImage(size.X, size.Y)
			textures[i].WritePixels(pixels.Pix)
			defer textures[i].Deallocate()
		}
		got, want := ebiten.NewImage(64, 48), ebiten.NewImage(64, 48)
		defer got.Deallocate()
		defer want.Deallocate()
		for i := 0; i < 20; i++ {
			src := textures[i%2]
			x, y := float64(i%5)*7+.25, float64(i/5)*6+.5
			// Mirrored, overlapping quads exercise alpha order and atlas origins.
			corners := [4][2]float64{{x + 20, y}, {x, y + 4}, {x + 3, y + 20}, {x + 23, y + 16}}
			clip := image.Rect(2, 3, 61, 46)
			effects := vm.GraphicEffects{Brightness: float64(i * 3), Ghost: float64(i * 2)}
			reference.quadVisual(clippedImage(want, clip), src, corners, 7, 9, effects, i%3 == 0, false, "", nil)
			batched.quadVisual(clippedImage(got, clip), src, corners, 7, 9, effects, i%3 == 0, false, "", nil)
		}
		a, b := make([]byte, 64*48*4), make([]byte, 64*48*4)
		got.ReadPixels(a)
		want.ReadPixels(b)
		for i := range a {
			if delta := int(a[i]) - int(b[i]); delta < -1 || delta > 1 {
				t.Fatalf("atlas/order/clip mismatch: pixel=%d channel=%d got=%d want=%d", i/4, i%4, a[i], b[i])
			}
		}
	})
}

func TestProjectShaderWarmupReusesPrograms(t *testing.T) {
	runGPUFrame(t, func() {
		var gpu stageGPU
		defer gpu.Close()
		source := "uniform float amount; void main(){gl_FragColor=vec4(amount,0.,0.,1.);}"
		program := extension.ShaderSource{Code: source, Compiler: penplus.ShaderCompiler{}}
		invalid := extension.ShaderSource{Code: "invalid shader", Compiler: penplus.ShaderCompiler{}}
		count := gpu.canvas.warmupPrograms([]extension.ShaderSource{program, program, invalid})
		if count != 1 || len(gpu.canvas.programs) != 1 {
			t.Fatalf("prepared=%d cached=%d", count, len(gpu.canvas.programs))
		}
		cached := gpu.canvas.programs[program.Key()].shader
		if gpu.canvas.warmupPrograms([]extension.ShaderSource{program}) != 0 {
			t.Fatal("cached program was prepared again")
		}
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 4, Height: 4, Zoom: 100, Direction: 90,
			Shader: &vm.StageShader{Compiler: penplus.ShaderCompiler{}, Fragment: source, Uniforms: map[string]any{"amount": float32(1)}}})
		dst := ebiten.NewImage(4, 4)
		defer dst.Deallocate()
		if err := gpu.Draw(dst, r, nil); err != nil {
			t.Fatal(err)
		}
		if gpu.canvas.programs[program.Key()].shader != cached {
			t.Fatal("first real draw compiled the shader again")
		}
		if got := dst.At(2, 2); got != (color.RGBA{R: 255, A: 255}) {
			t.Fatalf("warmup changed live uniforms/pixels: %v", got)
		}
		if _, err := gpu.canvas.compileProgram(invalid); err == nil {
			t.Fatal("unused invalid shader error was lost")
		}
	})
}

func TestProjectShaderWarmupIsBounded(t *testing.T) {
	runGPUFrame(t, func() {
		var gpu stageGPU
		defer gpu.Close()
		source := "void main(){gl_FragColor=vec4(1.,0.,0.,1.);}"
		sources := []extension.ShaderSource{{Code: strings.Repeat(" ", 256<<10) + source, Compiler: penplus.ShaderCompiler{}}}
		for i := 0; i < 20; i++ {
			sources = append(sources, extension.ShaderSource{Code: source + strings.Repeat(" ", i), Compiler: penplus.ShaderCompiler{}})
		}
		if n := gpu.canvas.warmupPrograms(sources); n != 16 {
			t.Fatalf("prepared %d shaders, want bounded 16", n)
		}
		if len(gpu.canvas.programs) != 16 {
			t.Fatal("oversized source was prepared")
		}
	})
}

func TestShaderWarmupExecutesEveryPlayerShader(t *testing.T) {
	runGPUFrame(t, func() {
		var gpu stageGPU
		defer gpu.Close()
		if _, err := gpu.enablePen(render.New(&media.Library{})); err != nil {
			t.Fatal(err)
		}
		dst := ebiten.NewImage(7, 1)
		defer dst.Deallocate()
		if err := gpu.drawShaderWarmup(dst); err != nil {
			t.Fatal(err)
		}
		var pixels [28]byte
		dst.ReadPixels(pixels[:])
		want := [28]byte{
			255, 255, 255, 255, 255, 0, 0, 255, 0, 0, 255, 255,
			255, 0, 0, 255, 255, 0, 0, 255, 255, 0, 0, 255,
			255, 0, 0, 255,
		}
		if pixels != want {
			t.Fatalf("stage/stamp/line/copy/filter/split/blur warmup pixels=%v, want %v", pixels, want)
		}
		if gpu.nativePen.image != nil || len(gpu.textures) != 0 {
			t.Fatal("shader warmup modified game graphics")
		}
	})
}

func TestOversizedTextureKeepsCachedCostumes(t *testing.T) {
	runGPUFrame(t, func() {
		small := &media.Costume{Image: image.NewNRGBA(image.Rect(0, 0, 8, 8)), Width: 8, Height: 8}
		/*
			Power-of-two GPU allocation exceeds the cache budget even though
			the source pixels fit within the decoded-image limit.
		*/
		large := &media.Costume{Image: image.NewNRGBA(image.Rect(0, 0, 4097, 2049)), Width: 4097, Height: 2049}
		small.Image.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		large.Image.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		gpu.measureUploads = true
		cached, temporary := gpu.texture(r, render.DisplaySprite{Costume: small, Density: 1})
		if cached == nil || temporary {
			t.Fatal("small costume was not cached")
		}
		before := gpu.bytes
		oversized, temporary := gpu.texture(r, render.DisplaySprite{Costume: large, Density: 1})
		if oversized == nil || !temporary {
			t.Fatal("oversized costume must remain temporary")
		}
		oversized.Deallocate()
		if gpu.bytes != before || len(gpu.textures) != 1 {
			t.Fatal("uncacheable costume evicted reusable textures")
		}
		again, _ := gpu.texture(r, render.DisplaySprite{Costume: small, Density: 1})
		if again != cached {
			t.Fatal("small costume was uploaded again after an oversized costume")
		}
		if gpu.uploadCount != 2 || gpu.uploadBytes != int64(len(small.Image.Pix)+len(large.Image.Pix)) || gpu.uploadTime <= 0 || gpu.evictions != 0 {
			t.Fatal("texture diagnostics did not distinguish uploads from cache hits")
		}
		if gpu.prepareTime <= 0 || gpu.pixelTime <= 0 || gpu.textureTime <= 0 || gpu.writeTime <= 0 {
			t.Fatal("texture diagnostics did not measure each preparation/upload phase")
		}
		phases := gpu.prepareTime + gpu.pixelTime + gpu.textureTime + gpu.writeTime
		if phases > gpu.uploadTime {
			t.Fatal("texture phases overlap or count work twice")
		}
		gpu.texture(r, render.DisplaySprite{Costume: small, Density: 1})
		if phases != gpu.prepareTime+gpu.pixelTime+gpu.textureTime+gpu.writeTime {
			t.Fatal("GPU cache hit was counted as another texture preparation")
		}
	})
}

func TestNativePenPresentation(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 16, 12))
		for y := 0; y < 12; y++ {
			for x := 0; x < 16; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{uint8(30 + x*13), uint8(20 + y*17), 190, uint8(30 + (x+y)%5*50)})
			}
		}
		pen := ebiten.NewImageFromImage(pixels)
		defer pen.Deallocate()
		dst, reference := ebiten.NewImage(64, 48), ebiten.NewImage(64, 48)
		defer dst.Deallocate()
		defer reference.Deallocate()
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		if err := gpu.initShader(); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct{ direction, zoom float64 }{{90, 50}, {38, 50}, {90, 130}, {38, 130}} {
			direction := tc.direction
			view := vm.View{Width: 64, Height: 48, Zoom: tc.zoom, Direction: direction, X: 3, Y: -2}
			r.SetView(view)
			dst.Fill(color.NRGBA{R: 30, G: 50, B: 90, A: 255})
			reference.Fill(color.NRGBA{R: 30, G: 50, B: 90, A: 255})
			gpu.drawPenImage(dst, pen, r, true)
			var corners [4][2]float64
			for i, uv := range [4][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
				corners[i][0], corners[i][1] = r.DisplayPoint((uv[0]-.5)*view.Width, (.5-uv[1])*view.Height, 64, 48)
			}
			gpu.quad(reference, pen, corners, 16, 12, vm.GraphicEffects{}, tc.zoom >= 100, true)
			got, want := make([]byte, 64*48*4), make([]byte, 64*48*4)
			dst.ReadPixels(got)
			reference.ReadPixels(want)
			for i := range got {
				if delta := int(got[i]) - int(want[i]); delta < -1 || delta > 1 {
					t.Fatalf("camera direction %g: pixel channel %d got %d, want %d", direction, i, got[i], want[i])
				}
			}
		}
		allocations := testing.AllocsPerRun(100, func() {
			gpu.drawPenImage(dst, pen, r, true)
		})
		if allocations > 6 {
			t.Errorf("native pen presentation allocates %.0f objects; limit 6", allocations)
		}
	})
}

func (g *stageGPUCheckGame) Update() error {
	g.check()
	return ebiten.Termination
}
func (g *stageGPUCheckGame) Draw(*ebiten.Image) {}
func (g *stageGPUCheckGame) Layout(int, int) (int, int) {
	return 64, 48
}

func runGPUFrame(t *testing.T, check func()) {
	t.Helper()
	runGPUCheck(t, &stageGPUCheckGame{check: check})
}

func runGPUCheck(t *testing.T, game ebiten.Game) {
	t.Helper()
	if os.Getenv("GOWARP_GPU_TEST") != "1" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(executable, "-test.run=^"+t.Name()+"$", "-test.v")
		cmd.Env = append(os.Environ(), "GOWARP_GPU_TEST=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("GPU render check: %v\n%s", err, out)
		} else if len(out) > 0 {
			t.Logf("%s", out)
		}
		return
	}
	if err := ebiten.RunGame(game); err != nil {
		t.Fatal(err)
	}
}

func TestStageGPUThinRotatedSprites(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 200, 200))
		for y := 0; y < 200; y++ {
			for x := 0; x < 200; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
			}
		}
		costume := &media.Costume{Image: pixels, Width: 100, Height: 100, CenterX: 50, CenterY: 50, Bitmap: true}
		r := render.New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
		r.SetView(vm.View{Width: 200, Height: 200, Zoom: 100, Direction: 90, Color: "#000000"})
		var gpu stageGPU
		defer gpu.Close()
		for _, scale := range []int{1, 2, 3} {
			dst := ebiten.NewImage(200*scale, 200*scale)
			got := make([]byte, 200*scale*200*scale*4)
			for _, direction := range []float64{90, -90, 0, 180} {
				/*
					A one-unit-wide strip lands on pixel centers at odd scales.
					Rotation must not discard the covered UV=1 boundary.
				*/
				sprite := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, StretchX: 1, StretchY: 101, Direction: direction}}
				if err := gpu.Draw(dst, r, []vm.DisplayTarget{sprite}); err != nil {
					t.Fatal(err)
				}
				dst.ReadPixels(got)
				visible := 0
				for i := 0; i < len(got); i += 4 {
					if got[i] == 255 {
						visible++
					}
				}
				if want := 101 * scale * scale; visible != want {
					t.Errorf("scale %d direction %g: red pixels = %d, want %d", scale, direction, visible, want)
				}
			}
			dst.Deallocate()
		}
	})
}

func TestStageGPUOutput(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 16, 12))
		for y := 0; y < 12; y++ {
			for x := 0; x < 16; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{uint8(30 + x*13), uint8(20 + y*17), uint8(40 + (x+y)*7), uint8(90 + (x+y)%5*40)})
			}
		}
		for y := 0; y < 12; y++ {
			for x := 0; x < 4; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{uint8(x * 10), uint8(x * 10), uint8(x * 10), 255})
			}
		}
		lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 16, Height: 12, CenterX: 6, CenterY: 5}}}}
		r := render.New(lib)
		view := vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, Color: "#193149"}
		scene := []vm.DisplayTarget{{Source: 0, State: vm.TargetState{Visible: true, Size: 180, StretchX: 100, StretchY: 120, Direction: 90, X: 2, Y: -1}}}
		var gpu stageGPU
		defer gpu.Close()
		cases := []struct {
			name      string
			effects   vm.GraphicEffects
			direction float64
			camera    bool
		}{
			{"plain", vm.GraphicEffects{}, 90, false},
			{"rotation-camera", vm.GraphicEffects{}, 38, true},
			{"mosaic", vm.GraphicEffects{Mosaic: 31}, -60, false},
			{"pixelate", vm.GraphicEffects{Pixelate: 21}, 90, false},
			{"whirl", vm.GraphicEffects{Whirl: 83}, 90, false},
			{"fisheye", vm.GraphicEffects{Fisheye: 73}, 90, false},
			{"color-full-turn", vm.GraphicEffects{Color: 200}, 90, false},
			{"color", vm.GraphicEffects{Color: 81, Brightness: 19, Ghost: 27}, 90, false},
			{"combined", vm.GraphicEffects{Mosaic: 11, Pixelate: 13, Whirl: -40, Fisheye: -30, Color: -51, Brightness: -13, Ghost: 18}, 90, false},
		}
		for _, tc := range cases {
			v := view
			if tc.camera {
				v.X, v.Y, v.Zoom, v.Direction = 3, -2, 137, 61
			}
			r.SetView(v)
			scene[0].State.Effects, scene[0].State.Direction = tc.effects, tc.direction
			dst := ebiten.NewImage(128, 96)
			if err := gpu.Draw(dst, r, scene); err != nil {
				t.Error(err)
				dst.Deallocate()
				continue
			}
			want := r.DrawAt(scene, 128, 96)
			got := make([]byte, len(want.Pix))
			dst.ReadPixels(got)
			bad, maxDelta := 0, 0
			for i, n := range want.Pix {
				d := int(n) - int(got[i])
				if d < 0 {
					d = -d
				}
				if d > maxDelta {
					maxDelta = d
				}
				if d > 3 {
					bad++
				}
			}
			if bad > 12 {
				t.Errorf("%s: %d channels differ by >3 (max %d)", tc.name, bad, maxDelta)
			}
			dst.Deallocate()
		}
	})
}

func checkStageGPUPixels(t *testing.T, gpu *stageGPU, r *render.Renderer, scene []vm.DisplayTarget, width, height int) {
	t.Helper()
	dst := ebiten.NewImage(width, height)
	defer dst.Deallocate()
	if err := gpu.Draw(dst, r, scene); err != nil {
		t.Error(err)
		return
	}
	want := r.DrawAt(scene, width, height)
	got := make([]byte, len(want.Pix))
	dst.ReadPixels(got)
	bad := 0
	for i, n := range want.Pix {
		delta := int(n) - int(got[i])
		if delta > 3 || delta < -3 {
			bad++
		}
	}
	if bad > 12 {
		t.Errorf("%d channels differ by more than 3 at %dx%d", bad, width, height)
	}
}

func TestStageGPUPenAndFlips(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 6))
		for y := 0; y < 6; y++ {
			for x := 0; x < 8; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{uint8(x * 30), 20, uint8(y * 40), 170})
			}
		}
		lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 8, Height: 6, CenterX: 4, CenterY: 3}}}}
		r := render.New(lib)
		r.SetView(vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, Color: "#ffffff"})
		stage := vm.DisplayTarget{State: vm.TargetState{Visible: true, IsStage: true, Size: 800, StretchX: 100, StretchY: 100, Direction: 90}}
		sprite := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 250, StretchX: 100, StretchY: 80, Direction: -90, RotationStyle: "left-right"}}
		scene := []vm.DisplayTarget{stage, sprite}
		var gpu stageGPU
		defer gpu.Close()
		for _, kind := range []string{"line", "stamp", "clear"} {
			r.Pen(vm.PenCommand{Kind: kind, Line: [4]float64{-25, 0, 25, 0}, Size: 7, Color: color.NRGBA{G: 255, A: 200}, Target: sprite})
			checkStageGPUPixels(t, &gpu, r, scene, 128, 96)
			checkStageGPUPixels(t, &gpu, r, scene, 128, 96)
		}
		scene[1].State.StretchX = -130
		scene[1].State.RotationStyle = "don't rotate"
		checkStageGPUPixels(t, &gpu, r, scene, 128, 96)
		r.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{-25, 0, 25, 0}, Size: 7, Color: color.NRGBA{G: 255, A: 200}})
		r.SetView(vm.View{Width: 80, Height: 51, Zoom: 125, Direction: 47, X: 4, Y: -3, Color: "#193149"})
		checkStageGPUPixels(t, &gpu, r, scene, 160, 102)
		if w, h := r.Size(); w != 80 || h != 51 {
			t.Errorf("GPU display changed logical size: %dx%d", w, h)
		}
	})
}

func TestStageGPUAdaptiveSVG(t *testing.T) {
	runGPUFrame(t, func() {
		dir := t.TempDir()
		data := []byte(`<svg viewBox="0 0 8 4"><rect x="2.25" y="0" width="0.25" height="4" fill="red"/></svg>`)
		if err := os.WriteFile(filepath.Join(dir, "fine.svg"), data, 0600); err != nil {
			t.Error(err)
			return
		}
		p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{{MD5Ext: "fine.svg", DataFormat: "svg", RotationCenterX: 4, RotationCenterY: 2}}}}}
		lib, err := media.LoadRuntime(context.Background(), dir, p, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer lib.Close()
		r := render.New(lib)
		r.SetView(vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90, Color: "#ffffff"})
		scene := []vm.DisplayTarget{{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}}
		var gpu stageGPU
		defer gpu.Close()
		checkStageGPUPixels(t, &gpu, r, scene, 128, 96)
		// Cached GPU pixels must remain drawable without decoding an asset again.
		if err := os.Remove(filepath.Join(dir, "fine.svg")); err != nil {
			t.Error(err)
		}
		checkStageGPUPixels(t, &gpu, r, scene, 128, 96)
		if lib.Costumes[0][0].Image != nil {
			t.Error("GPU display pinned a CPU costume raster")
		}
	})
}

func TestStageGPUReplacesPreparedSVGFallback(t *testing.T) {
	runGPUFrame(t, func() {
		dir := t.TempDir()
		data := []byte(`<svg width="8" height="4"><rect x="2.25" width="0.25" height="4" fill="red"/></svg>`)
		if err := os.WriteFile(filepath.Join(dir, "fine.svg"), data, 0600); err != nil {
			t.Error(err)
			return
		}
		p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{{MD5Ext: "fine.svg", DataFormat: "svg"}}}}}
		lib, err := media.LoadRuntime(context.Background(), dir, p, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer lib.Close()
		if err := lib.EnableDisplayPreparation(dir, p); err != nil {
			t.Error(err)
			return
		}
		var gpu stageGPU
		defer gpu.Close()
		r := render.New(lib)
		d := render.DisplaySprite{Costume: lib.Costumes[0][0], Density: 4}
		fallback, _ := gpu.textureRegion(r, d, image.Rectangle{})
		if fallback == nil || fallback.Bounds().Size() != image.Pt(8, 4) {
			t.Error("first display request blocked on high-resolution pixels")
			return
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			texture, _ := gpu.textureRegion(r, d, image.Rectangle{})
			if texture != nil && texture.Bounds().Size() == image.Pt(32, 16) {
				pixels := make([]byte, 32*16*4)
				texture.ReadPixels(pixels)
				if pixels[(8*32+9)*4] != 255 || pixels[(8*32+9)*4+3] != 255 {
					t.Error("prepared SVG lost its subpixel geometry")
				}
				if err := os.Remove(filepath.Join(dir, "fine.svg")); err != nil {
					t.Error(err)
				}
				cached, _ := gpu.textureRegion(r, d, image.Rectangle{})
				if cached != texture || len(gpu.textures) != 2 || lib.Err() != nil {
					t.Error("fallback and completed pixels did not retain separate reusable textures")
				}
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Error("GPU kept the fallback after preparation completed")
	})
}

func TestStageGPUResizeTiming(t *testing.T) {
	if os.Getenv("GOWARP_GPU_TIMING") != "1" {
		t.Skip("set GOWARP_GPU_TIMING=1 for comparative timing")
	}
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 480, 360))
		for y := 0; y < 360; y++ {
			for x := 0; x < 480; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), uint8(x + y), 255})
			}
		}
		r := render.New(&media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 480, Height: 360, CenterX: 240, CenterY: 180}}}})
		scene := []vm.DisplayTarget{{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}}
		var gpu stageGPU
		defer gpu.Close()
		for _, size := range []image.Point{{480, 360}, {960, 720}, {1920, 1440}} {
			dst := ebiten.NewImage(size.X, size.Y)
			out := make([]byte, size.X*size.Y*4)
			if err := gpu.Draw(dst, r, scene); err != nil {
				t.Error(err)
				return
			}
			dst.ReadPixels(out)
			r.DrawAt(scene, size.X, size.Y)
			const repeats = 6
			start := time.Now()
			for i := 0; i < repeats; i++ {
				if err := gpu.Draw(dst, r, scene); err != nil {
					t.Error(err)
					return
				}
				dst.ReadPixels(out)
			}
			gpuTime := time.Since(start) / repeats
			start = time.Now()
			for i := 0; i < repeats; i++ {
				r.DrawAt(scene, size.X, size.Y)
			}
			cpuTime := time.Since(start) / repeats
			t.Logf("%dx%d CPU DrawAt %v, GPU including readback %v", size.X, size.Y, cpuTime, gpuTime)
			dst.Deallocate()
		}
	})
}

func TestStageGPUReusesBitmapTextureAcrossDensities(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 6))
		for i := range pixels.Pix {
			pixels.Pix[i] = 255
		}
		var data bytes.Buffer
		if err := png.Encode(&data, pixels); err != nil {
			t.Error(err)
			return
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "sprite.png"), data.Bytes(), 0600); err != nil {
			t.Error(err)
			return
		}
		p := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{{MD5Ext: "sprite.png", DataFormat: "png", RotationCenterX: 4, RotationCenterY: 3}, {MD5Ext: "sprite.png", DataFormat: "png", RotationCenterX: 5, RotationCenterY: 3}}}}}
		lib, err := media.LoadRuntime(context.Background(), dir, p, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer lib.Close()
		r := render.New(lib)
		r.SetView(vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90, Color: "#ffffff"})
		scene := []vm.DisplayTarget{{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}}
		var gpu stageGPU
		defer gpu.Close()
		for _, scale := range []int{1, 2, 4} {
			checkStageGPUPixels(t, &gpu, r, scene, 32*scale, 24*scale)
		}
		scene[0].State.CurrentCostume = 1
		checkStageGPUPixels(t, &gpu, r, scene, 128, 96)
		if len(gpu.textures) != 1 {
			t.Errorf("unchanged bitmap uploaded %d times across densities/rotation centers", len(gpu.textures))
		}
	})
}

func TestStageGPUEnlargedBitmapKeepsSharpPixels(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		for y := 0; y < 2; y++ {
			pixels.SetNRGBA(0, y, color.NRGBA{R: 255, A: 255})
			pixels.SetNRGBA(1, y, color.NRGBA{B: 255, A: 255})
		}
		var data bytes.Buffer
		if err := png.Encode(&data, pixels); err != nil {
			t.Fatal(err)
		}
		meta := project.Costume{MD5Ext: "sprite.png", DataFormat: "png", RotationCenterX: 1, RotationCenterY: 1}
		costume, err := media.DecodeCostume(meta, data.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "sprite.png"), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		projectData := &project.Project{Targets: []*project.Target{{Costumes: []project.Costume{meta}}}}
		runtimeLibrary, err := media.LoadRuntime(context.Background(), dir, projectData, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer runtimeLibrary.Close()
		for name, lib := range map[string]*media.Library{
			"decoded": {Costumes: [][]*media.Costume{{costume}}},
			"runtime": runtimeLibrary,
		} {
			r := render.New(lib)
			view := vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, Color: "#ffffff", HighQualityPen: true}
			r.SetView(view)
			var gpu stageGPU
			defer gpu.Close()
			pen, err := gpu.enablePen(r)
			if err != nil {
				t.Fatal(err)
			}
			dst := ebiten.NewImage(192, 144)
			defer dst.Deallocate()
			target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 400, StretchX: 100, StretchY: 100, Direction: 90}}
			for _, brightness := range []float64{0, 20} {
				target.State.Effects.Brightness = brightness
				for _, stamp := range []bool{false, true} {
					pen.Pen(vm.PenCommand{Kind: "clear"})
					scene := []vm.DisplayTarget{target}
					if stamp {
						pen.Pen(vm.PenCommand{Kind: "stamp", Target: target})
						scene = nil
					}
					if err := gpu.Draw(dst, r, scene); err != nil {
						t.Fatal(err)
					}
					got := make([]byte, 192*144*4)
					dst.ReadPixels(got)
					low := byte(0)
					if brightness == 20 {
						low = 51
					}
					for x, want := range map[int][4]byte{95: {255, low, low, 255}, 96: {low, low, 255, 255}} {
						for channel, value := range want {
							actual := got[(72*192+x)*4+channel]
							if delta := int(actual) - int(value); delta < -1 || delta > 1 {
								t.Errorf("%s stamp=%v brightness=%g pixel %d channel %d: got %d, want %d", name, stamp, brightness, x, channel, actual, value)
							}
						}
					}
				}
			}
		}
	})
}

func TestStageGPUTransparentSpriteDoesNotUploadTexture(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for i := range pixels.Pix {
			pixels.Pix[i] = 255
		}
		renderer := render.New(&media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 8, Height: 8, CenterX: 4, CenterY: 4}}}})
		scene := []vm.DisplayTarget{{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, Effects: vm.GraphicEffects{Ghost: 100}}}}
		var gpu stageGPU
		defer gpu.Close()
		dst := ebiten.NewImage(48, 36)
		defer dst.Deallocate()
		if err := gpu.Draw(dst, renderer, scene); err != nil {
			t.Fatal(err)
		}
		if gpu.bytes != 0 {
			t.Errorf("invisible sprite uploaded %d texture bytes", gpu.bytes)
		}
		got := make([]byte, 48*36*4)
		dst.ReadPixels(got)
		for _, value := range got {
			if value != 255 {
				t.Fatal("invisible sprite changed the white stage")
			}
		}
	})
}

func TestStageGPUClippedSpriteDoesNotUploadTexture(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for i := range pixels.Pix {
			pixels.Pix[i] = 255
		}
		r := render.New(&media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 8, Height: 8, CenterX: 4, CenterY: 4}}}})
		target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90,
			Clipbox: &vm.Bounds{Left: 1000, Right: 1100, Bottom: 1000, Top: 1100}}}
		dst := ebiten.NewImage(48, 36)
		defer dst.Deallocate()
		for _, stamp := range []bool{false, true} {
			var gpu stageGPU
			gpu.measureUploads = true
			pen := &penGPU{Renderer: r, gpu: &gpu, scale: 1}
			gpu.nativePen = pen
			if stamp {
				pen.Pen(vm.PenCommand{Kind: "stamp", Target: target})
			} else if err := gpu.Draw(dst, r, []vm.DisplayTarget{target}); err != nil {
				t.Fatal(err)
			}
			if gpu.uploadCount != 0 {
				t.Errorf("stamp=%v: fully clipped costume uploaded %d textures", stamp, gpu.uploadCount)
			}
			gpu.Close()
		}
	})
}

func TestStageGPUCloneDrawAllocations(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		pixels.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		r := render.New(&media.Library{Costumes: [][]*media.Costume{{{Image: pixels, CacheKey: "clone-costume", Width: 8, Height: 8, CenterX: 4, CenterY: 4}}}})
		scene := make([]vm.DisplayTarget, 300)
		for i := range scene {
			scene[i].State = vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, CloneID: i + 1}
		}
		var gpu stageGPU
		defer gpu.Close()
		dst := ebiten.NewImage(48, 36)
		defer dst.Deallocate()
		allocs := testing.AllocsPerRun(10, func() {
			if err := gpu.Draw(dst, r, scene); err != nil {
				t.Fatal(err)
			}
		})
		t.Logf("300-clone frame: %.0f allocations", allocs)
		if allocs > 100 {
			t.Errorf("300-clone frame allocates %.0f objects; want at most 100 after warmup", allocs)
		}
	})
}

func TestStageGPUQueuedSpritesKeepIndependentEffects(t *testing.T) {
	runGPUFrame(t, func() {
		var gpu stageGPU
		defer gpu.Close()
		if err := gpu.initShader(); err != nil {
			t.Fatal(err)
		}
		src := ebiten.NewImage(1, 1)
		defer src.Deallocate()
		src.WritePixels([]byte{80, 40, 20, 255})
		dst := ebiten.NewImage(32, 8)
		defer dst.Deallocate()
		dst.Fill(color.Black)
		cases := []struct {
			effects vm.GraphicEffects
			want    color.RGBA
		}{
			{vm.GraphicEffects{}, color.RGBA{80, 40, 20, 255}},
			{vm.GraphicEffects{Brightness: 20}, color.RGBA{131, 91, 71, 255}},
			{vm.GraphicEffects{}, color.RGBA{80, 40, 20, 255}},
			{vm.GraphicEffects{Ghost: 50}, color.RGBA{40, 20, 10, 255}},
		}
		for i, tc := range cases {
			x := float64(i * 8)
			gpu.quad(dst, src, [4][2]float64{{x, 0}, {x + 8, 0}, {x + 8, 8}, {x, 8}}, 1, 1, tc.effects, true, false)
		}
		// Read only after all draws are queued: later uniforms must not alter earlier sprites.
		pixels := make([]byte, 32*8*4)
		dst.ReadPixels(pixels)
		for i, tc := range cases {
			offset := (4*32 + i*8 + 4) * 4
			for channel, want := range []byte{tc.want.R, tc.want.G, tc.want.B, tc.want.A} {
				if delta := int(pixels[offset+channel]) - int(want); delta < -1 || delta > 1 {
					t.Fatalf("sprite %d channel %d = %d; want %d", i, channel, pixels[offset+channel], want)
				}
			}
		}
	})
}

func TestClippedStampPreservesLogicalPenAtFractionalScale(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for i := range pixels.Pix {
			pixels.Pix[i] = 255
		}
		lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 8, Height: 8, CenterX: 4, CenterY: 4, Bitmap: true}}}}
		r, reference := render.New(lib), render.New(lib)
		view := vm.View{Width: 480, Height: 360, Zoom: 100, Direction: 90, HighQualityPen: true}
		r.SetView(view)
		reference.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.keepHistory = true
		pen.setOutputSize(720, 540)
		gpu.measureUploads = true
		command := vm.PenCommand{Kind: "stamp", Target: vm.DisplayTarget{State: vm.TargetState{
			Size: 100, StretchX: 100, StretchY: 100, Direction: 90,
			Clipbox: &vm.Bounds{Left: .9, Right: 1.1, Bottom: -180, Top: 180},
		}}}
		pen.Pen(command)
		reference.Pen(command)
		pen.flush()
		got, _ := r.DisplayPen()
		want, _ := reference.DisplayPen()
		if want == nil || want.NRGBAAt(240, 180).A == 0 {
			t.Fatal("fixture has no logical pen pixel")
		}
		if got == nil || !bytes.Equal(got.Pix, want.Pix) {
			t.Fatal("empty display clip discarded logical color-sensing pixels")
		}
		if gpu.uploadCount != 0 {
			t.Fatal("empty display clip uploaded a texture")
		}
	})
}

func TestPenPresentationFiltersOnlyWhenShrinking(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x += 2 {
				pixels.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
			}
		}
		pen := ebiten.NewImageFromImage(pixels)
		defer pen.Deallocate()
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		if err := gpu.initShader(); err != nil {
			t.Fatal(err)
		}
		for _, premultiplied := range []bool{false, true} {
			for _, tc := range []struct {
				size       int
				zoom       float64
				x          int
				red, alpha uint8
			}{
				{4, 100, 1, 128, 128},
				{8, 50, 3, 128, 128},
				{8, 100, 2, 255, 255},
				{8, 100, 3, 0, 0},
				{16, 100, 4, 255, 255},
				{16, 100, 6, 0, 0},
			} {
				r.SetView(vm.View{Width: 8, Height: 8, Zoom: tc.zoom, Direction: 90})
				dst := ebiten.NewImage(tc.size, tc.size)
				gpu.drawPenImage(dst, pen, r, premultiplied)
				got := image.NewRGBA(dst.Bounds())
				dst.ReadPixels(got.Pix)
				dst.Deallocate()
				pixel := got.RGBAAt(tc.x, tc.size/2)
				if d := int(pixel.R) - int(tc.red); d < -1 || d > 1 {
					t.Fatalf("size=%d zoom=%g premultiplied=%t: red=%d, want %d", tc.size, tc.zoom, premultiplied, pixel.R, tc.red)
				}
				if d := int(pixel.A) - int(tc.alpha); d < -1 || d > 1 {
					t.Fatalf("size=%d zoom=%g premultiplied=%t: alpha=%d, want %d", tc.size, tc.zoom, premultiplied, pixel.A, tc.alpha)
				}
			}
		}
	})
}
