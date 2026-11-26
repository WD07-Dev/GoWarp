package desktop

import (
	penplus "gowarp/test/extensions/penP"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gowarp/core/extension"
	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestCanvasBorderBackground(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		v := vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, Color: "#000000"}
		v.Canvas = vm.CanvasEffects{}.Defaults()
		v.Canvas.BorderWidth = 3
		v.Canvas.BorderColor = "#ffffff"
		var g stageGPU
		defer g.Close()
		dst := ebiten.NewImage(64, 48)
		defer dst.Deallocate()
		for _, style := range []string{"dotted", "dashed"} {
			for _, background := range []string{"#ffffff", "#0000ff", "transparent"} {
				v.Canvas.BorderStyle, v.Canvas.Background = style, background
				r.SetView(v)
				if err := g.Draw(dst, r, nil); err != nil {
					t.Fatal(err)
				}
				// Sample the middle of a gap on the top edge.
				x := 4
				if style == "dashed" {
					x = 10
				}
				want := color.RGBA{A: 255}
				if background != "transparent" {
					want = color.RGBAModel.Convert(vm.ParseColor(background)).(color.RGBA)
				}
				got := color.RGBAModel.Convert(dst.At(x, 1)).(color.RGBA)
				if got != want {
					t.Errorf("%s on %s: border gap=%v, want %v", style, background, got, want)
				}
				if got := color.RGBAModel.Convert(dst.At(32, 24)).(color.RGBA); got != (color.RGBA{A: 255}) {
					t.Errorf("border painted over game: %v", got)
				}
			}
		}
		// Exercise the project's white-on-white border at fullscreen resolution.
		v.Canvas.BorderStyle, v.Canvas.Background = "dotted", "#ffffff"
		r.SetView(v)
		fullscreen := ebiten.NewImage(1920, 1080)
		defer fullscreen.Deallocate()
		var drawErr error
		started := time.Now()
		allocs := testing.AllocsPerRun(10, func() {
			drawErr = g.Draw(fullscreen, r, nil)
		})
		if drawErr != nil {
			t.Fatal(drawErr)
		}
		t.Logf("fullscreen border draw: %.0f allocations/frame, %s for 11 submissions", allocs, time.Since(started))
		/*
			The same border must survive the extension's fixed render size,
			which goes through the canvas compositor instead of direct drawing.
		*/
		v.Canvas.RenderWidth, v.Canvas.RenderHeight = 480, 360
		r.SetView(v)
		if err := g.Draw(fullscreen, r, nil); err != nil {
			t.Fatal(err)
		}
		for x := 1; x < 1919; x++ {
			got := color.RGBAModel.Convert(fullscreen.At(x, 1)).(color.RGBA)
			if got != (color.RGBA{255, 255, 255, 255}) {
				t.Fatalf("fullscreen border is interrupted at x=%d: %v", x, got)
			}
		}
	})
}

func TestStageGPUCanvasAndProjectShader(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		v := vm.View{Width: 16, Height: 12, Zoom: 100, Direction: 90, Color: "#804020"}
		v.Canvas = vm.CanvasEffects{}.Defaults()
		v.Canvas.Brightness = 50
		r.SetView(v)
		var g stageGPU
		defer g.Close()
		dst := ebiten.NewImage(16, 12)
		defer dst.Deallocate()
		if err := g.Draw(dst, r, nil); err != nil {
			t.Fatal(err)
		}
		got := color.RGBAModel.Convert(dst.At(8, 6)).(color.RGBA)
		if got.R < 63 || got.R > 65 || got.G < 31 || got.G > 33 || got.B < 15 || got.B > 17 {
			t.Fatalf("canvas brightness produced %v", got)
		}
		v.Canvas.Brightness = 100
		v.Shader = &vm.StageShader{Compiler: penplus.ShaderCompiler{}, Fragment: `uniform sampler2D u_skin; uniform float red;
void main() {gl_FragColor=vec4(red,texture2D(u_skin,vec2(.5)).g,0.,1.);}`, Uniforms: map[string]any{"red": float32(.25)}}
		r.SetView(v)
		if err := g.Draw(dst, r, nil); err != nil {
			t.Fatal(err)
		}
		got = color.RGBAModel.Convert(dst.At(8, 6)).(color.RGBA)
		if got.R < 63 || got.R > 65 || got.G != 64 || got.B != 0 {
			t.Fatalf("shader/uniform output=%v", got)
		}
		v.Shader.Uniforms["red"] = float32(.75)
		if err := g.Draw(dst, r, nil); err != nil {
			t.Fatal(err)
		}
		got = color.RGBAModel.Convert(dst.At(8, 6)).(color.RGBA)
		if got.R < 190 || got.R > 192 {
			t.Fatalf("changed shader uniform did not update: %v", got)
		}
		pixels := image.NewNRGBA(image.Rect(0, 0, 16, 12))
		pixels.SetNRGBA(2, 1, color.NRGBA{255, 0, 0, 255})
		lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 16, Height: 12, CenterX: 8, CenterY: 6}}}}
		r = render.New(lib)
		v.Color = "#000000"
		v.Canvas.Nearest = true
		v.Shader = &vm.StageShader{Compiler: penplus.ShaderCompiler{}, Fragment: `uniform sampler2D u_skin;void main(){gl_FragColor=texture2D(u_skin,v_texCoord);}`}
		r.SetView(v)
		scene := []vm.DisplayTarget{{Source: 0, State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}}
		if err := g.Draw(dst, r, scene); err != nil {
			t.Fatal(err)
		}
		got = color.RGBAModel.Convert(dst.At(2, 1)).(color.RGBA)
		if got.R != 255 {
			t.Fatalf("shader flipped framebuffer: %v", got)
		}
		v.Split = &vm.SplitEffects{Effects: []extension.ScreenSplit{extension.NormalizedSplit{TopX: .5, BottomX: .5, MoveX: 2 / ebiten.Monitor().DeviceScaleFactor()}}}
		r.SetView(v)
		if err := g.Draw(dst, r, scene); err != nil {
			t.Fatal(err)
		}
		got = color.RGBAModel.Convert(dst.At(0, 1)).(color.RGBA)
		if got.R != 255 {
			t.Fatalf("split left half did not move: %v", got)
		}
		got = color.RGBAModel.Convert(dst.At(2, 1)).(color.RGBA)
		if got.R != 0 {
			t.Fatalf("split retained unshifted original: %v", got)
		}
	})
}

func TestStageGPUCanvasMatchingRenderSize(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		view := vm.View{Width: 64, Height: 48, Zoom: 100, Direction: 90, Color: "#804020"}
		view.Canvas = vm.CanvasEffects{}.Defaults()
		view.Canvas.RenderWidth, view.Canvas.RenderHeight = 64, 48
		r.SetView(view)
		var gpu stageGPU
		defer gpu.Close()
		dst := ebiten.NewImage(64, 48)
		defer dst.Deallocate()
		if err := gpu.Draw(dst, r, nil); err != nil {
			t.Fatal(err)
		}
		if got := color.RGBAModel.Convert(dst.At(32, 24)); got != (color.RGBA{128, 64, 32, 255}) {
			t.Fatalf("matching render size changed stage color: %v", got)
		}
		if gpu.canvas.frame != nil || gpu.canvas.filter != nil {
			t.Fatal("matching render size allocated an unnecessary intermediate frame/filter")
		}
		view.Canvas.Nearest = true
		r.SetView(view)
		larger := ebiten.NewImage(128, 96)
		defer larger.Deallocate()
		if err := gpu.Draw(larger, r, nil); err != nil {
			t.Fatal(err)
		}
		if gpu.canvas.frame == nil || gpu.canvas.frame.Bounds().Size() != image.Pt(64, 48) {
			t.Fatal("window resize ignored the extension's fixed render size")
		}
		view.Canvas.Brightness = 50
		r.SetView(view)
		if err := gpu.Draw(dst, r, nil); err != nil {
			t.Fatal(err)
		}
		got := color.RGBAModel.Convert(dst.At(32, 24)).(color.RGBA)
		if got.R < 63 || got.R > 65 || got.G < 31 || got.G > 33 || got.B < 15 || got.B > 17 {
			t.Fatalf("matching render size skipped the active brightness effect: %v", got)
		}
		view.Canvas.Brightness = 100
		view.Canvas.Background = "#ffffff"
		view.Color = "2155888672" // 0x80804020: half-transparent stage.
		r.SetView(view)
		if err := gpu.Draw(dst, r, nil); err != nil {
			t.Fatal(err)
		}
		got = color.RGBAModel.Convert(dst.At(32, 24)).(color.RGBA)
		if got.A != 255 || got.R < 190 || got.R > 192 || got.G < 158 || got.G > 160 || got.B < 142 || got.B > 144 {
			t.Fatalf("matching render size skipped canvas background composition: %v", got)
		}
	})
}

func TestCanvasReusesBufferAcrossRenderScaleChanges(t *testing.T) {
	runGPUFrame(t, func() {
		for _, effect := range []string{"plain", "shader", "blur"} {
			var gpu stageGPU
			pixels := image.NewNRGBA(image.Rect(0, 0, 8, 6))
			for y := 0; y < 6; y++ {
				for x := 0; x < 8; x++ {
					pixels.SetNRGBA(x, y, color.NRGBA{uint8(x * 30), uint8(y * 40), 64, 128})
				}
			}
			costume := &media.Costume{Image: pixels, Bitmap: true, Width: 480, Height: 360, CenterX: 240, CenterY: 180}
			r := render.New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
			scene := []vm.DisplayTarget{{Source: 0, State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}}
			view := vm.View{Width: 480, Height: 360, Zoom: 100, Direction: 90, Color: "#ff0000"}
			view.Canvas = vm.CanvasEffects{}.Defaults()
			if effect == "shader" {
				view.Shader = &vm.StageShader{Compiler: penplus.ShaderCompiler{}, Fragment: "uniform sampler2D u_skin;void main(){gl_FragColor=texture2D(u_skin,v_texCoord);}"}
			}
			if effect == "blur" {
				view.Canvas.Blur = 1
			}
			dst := ebiten.NewImage(48, 36)
			var first *ebiten.Image
			for i, scale := range []int{3, 2, 3} {
				view.Canvas.RenderWidth = float64(480 * scale)
				view.Canvas.RenderHeight = float64(360 * scale)
				view.Color = []string{"#ff0000", "#00ff00", "#0000ff"}[i]
				r.SetView(view)
				if err := gpu.Draw(dst, r, scene); err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					first = gpu.canvas.frame
				}
				if i == 2 && gpu.canvas.frame != first {
					t.Fatalf("%s: returning to menu render size allocated a new framebuffer", effect)
				}
				if gpu.canvas.frame.Bounds().Size() != image.Pt(480*scale, 360*scale) {
					t.Fatal("buffer reuse changed the requested render resolution")
				}
				/*
					A fresh compositor is the reference for clipping, sampling, and
					clearing the smaller viewport inside a reused allocation.
				*/
				var reference stageGPU
				want := ebiten.NewImage(48, 36)
				if err := reference.Draw(want, r, scene); err != nil {
					t.Fatal(err)
				}
				actual, expected := make([]byte, 48*36*4), make([]byte, 48*36*4)
				dst.ReadPixels(actual)
				want.ReadPixels(expected)
				for n := range actual {
					if actual[n] != expected[n] {
						t.Fatalf("%s scale %d: pixel byte %d = %d, want %d", effect, scale, n, actual[n], expected[n])
					}
				}
				want.Deallocate()
				reference.Close()
			}
			dst.Deallocate()
			gpu.Close()
		}
	})
}

func TestCanvasResizeReleasesLargeBackingAndClosesSubImage(t *testing.T) {
	runGPUFrame(t, func() {
		var canvas canvasGPU
		canvasImage(&canvas.frame, &canvas.frameBacking, 4096, 32)
		large := canvas.frameBacking
		canvasImage(&canvas.frame, &canvas.frameBacking, 64, 48)
		if canvas.frameBacking == large || canvas.frameBacking.Bounds().Size() != image.Pt(64, 48) {
			t.Fatal("shrinking the canvas retained an oversized backing")
		}
		backing := canvas.frameBacking
		backing.Fill(color.RGBA{255, 0, 0, 255})
		canvasImage(&canvas.frame, &canvas.frameBacking, 32, 24)
		if canvas.frame == backing {
			t.Fatal("smaller viewport was not clipped")
		}
		canvas.close()
		/*
			Deallocate on a sub-image does nothing. Closing must clear the
			backing resource even when the last viewport was smaller.
		*/
		if got := color.RGBAModel.Convert(backing.At(0, 0)); got != (color.RGBA{}) {
			t.Fatalf("closing a smaller viewport did not release its backing: %v", got)
		}
		backing.Deallocate()
	})
}

func TestCanvasNearestCopyMatchesColorFilter(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 6))
		for y := 0; y < 6; y++ {
			for x := 0; x < 8; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{uint8(30 * x), uint8(40 * y), 127, uint8(20 + x*25)})
			}
		}
		costume := &media.Costume{Image: pixels, Bitmap: true, Width: 32, Height: 24, CenterX: 16, CenterY: 12}
		r := render.New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
		scene := []vm.DisplayTarget{{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}}
		var gpu stageGPU
		defer gpu.Close()
		filter, err := ebiten.NewShader(canvasShaderSource)
		if err != nil {
			t.Fatal(err)
		}
		defer filter.Deallocate()
		view := vm.View{Width: 32, Height: 24, Zoom: 117, Direction: 83, X: 2.25, Y: -1.75}
		view.Canvas = vm.CanvasEffects{}.Defaults()
		view.Canvas.Nearest = true
		for _, background := range []string{"transparent", "#103050"} {
			view.Color = "2155888672" // Half-transparent stage.
			view.Canvas.Background = background
			for i, size := range []image.Point{{48, 36}, {64, 48}, {63, 47}} {
				w, h := 96, 72
				if i == 1 {
					w, h = 64, 48
				}
				view.Canvas.RenderWidth, view.Canvas.RenderHeight = float64(w), float64(h)
				r.SetView(view)
				dst, want := ebiten.NewImage(size.X, size.Y), ebiten.NewImage(size.X, size.Y)
				if err := gpu.Draw(dst, r, scene); err != nil {
					t.Fatal(err)
				}
				if background != "transparent" {
					want.Fill(vm.ParseColor(background))
				}
				/*
					The existing filter is the reference for sampling and alpha
					composition, including a smaller viewport in a reused buffer.
				*/
				vertices := []ebiten.Vertex{
					{}, {DstX: float32(size.X), SrcX: float32(w)},
					{DstX: float32(size.X), DstY: float32(size.Y), SrcX: float32(w), SrcY: float32(h)},
					{DstY: float32(size.Y), SrcY: float32(h)},
				}
				want.DrawTrianglesShader(vertices, []uint16{0, 1, 2, 0, 2, 3}, filter, &ebiten.DrawTrianglesShaderOptions{
					Images: [4]*ebiten.Image{gpu.canvas.frame},
					Uniforms: map[string]any{"SourceSize": []float32{float32(w), float32(h)}, "Nearest": 1,
						"Filters": []float32{1, 1, 0, 1}, "Filters2": []float32{0, 0, 1, 0}},
				})
				actual, expected := make([]byte, size.X*size.Y*4), make([]byte, size.X*size.Y*4)
				dst.ReadPixels(actual)
				want.ReadPixels(expected)
				for n := range actual {
					if actual[n] != expected[n] {
						t.Fatalf("%s %v: byte %d = %d, want %d", background, size, n, actual[n], expected[n])
					}
				}
				dst.Deallocate()
				want.Deallocate()
			}
		}
	})
}

func TestCanvasFilterMatchesReference(t *testing.T) {
	runGPUFrame(t, func() {
		path, err := filepath.Abs("../../test/fixtures/render/canvas_filter_reference.kage")
		if err != nil {
			t.Fatal(err)
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		old, err := ebiten.NewShader(source)
		if err != nil {
			t.Fatal(err)
		}
		defer old.Deallocate()
		current, err := ebiten.NewShader(canvasShaderSource)
		if err != nil {
			t.Fatal(err)
		}
		defer current.Deallocate()
		pixels := image.NewRGBA(image.Rect(0, 0, 16, 12))
		for y := 0; y < 12; y++ {
			for x := 0; x < 16; x++ {
				// Include transparent pixels and RGB greater than alpha, as custom shaders can output both.
				pixels.SetRGBA(x, y, color.RGBA{uint8(x * 17), uint8(y * 23), 127, uint8((x + y) * 9)})
			}
		}
		backing := ebiten.NewImage(32, 24)
		defer backing.Deallocate()
		src := backing.SubImage(image.Rect(3, 4, 19, 16)).(*ebiten.Image)
		src.WritePixels(pixels.Pix)
		got, want := ebiten.NewImage(23, 19), ebiten.NewImage(23, 19)
		defer got.Deallocate()
		defer want.Deallocate()
		vertices := []ebiten.Vertex{{SrcX: 3, SrcY: 4}, {DstX: 23, SrcX: 19, SrcY: 4}, {DstX: 23, DstY: 19, SrcX: 19, SrcY: 16}, {DstY: 19, SrcX: 3, SrcY: 16}}
		actual, expected := make([]byte, 23*19*4), make([]byte, 23*19*4)
		for _, nearest := range []int{0, 1} {
			for _, filters := range [][]float32{{1, 1, 0, 1}, {1.2, .7, .3, .8}} {
				for _, extra := range [][]float32{{0, 0, 1, 0}, {0, 0, .4, .3}, {.2, .6, .8, 0}} {
					for _, blur := range [][]float32{{0, 0}, {1, 0}, {0, 1}} {
						options := &ebiten.DrawTrianglesShaderOptions{Images: [4]*ebiten.Image{src}, Blend: ebiten.BlendCopy, Uniforms: map[string]any{"SourceSize": []float32{16, 12}, "Nearest": nearest, "Filters": filters, "Filters2": extra, "Blur": blur}}
						want.DrawTrianglesShader(vertices, []uint16{0, 1, 2, 0, 2, 3}, old, options)
						got.DrawTrianglesShader(vertices, []uint16{0, 1, 2, 0, 2, 3}, current, options)
						got.ReadPixels(actual)
						want.ReadPixels(expected)
						for i, value := range actual {
							difference := int(value) - int(expected[i])
							if difference < -1 || difference > 1 {
								t.Fatalf("nearest=%d filters=%v extra=%v blur=%v: byte %d=%d, want %d", nearest, filters, extra, blur, i, value, expected[i])
							}
						}
					}
				}
			}
		}
	})
}

func TestCanvasUniformsReuseAndReset(t *testing.T) {
	var canvas canvasGPU
	changed := vm.CanvasEffects{}.Defaults()
	changed.Brightness, changed.Contrast, changed.Radius = 50, 120, 20
	changed.Nearest = true
	values := canvas.uniformValues(640, 480, changed)
	if values["Filters"].([]float32)[0] != 1.2 || values["Filters"].([]float32)[3] != .5 || values["Filters2"].([]float32)[3] != .2 || values["Nearest"] != 1 {
		t.Fatalf("wrong filter values: %v", values)
	}
	values["Blur"].([]float32)[0] = 5
	neutral := vm.CanvasEffects{}.Defaults()
	values = canvas.uniformValues(320, 180, neutral)
	if values["SourceSize"].([]float32)[0] != 320 || values["Filters"].([]float32)[3] != 1 || values["Filters2"].([]float32)[3] != 0 || values["Blur"].([]float32)[0] != 0 || values["Nearest"] != 0 {
		t.Fatalf("stale frame values: %v", values)
	}
	if allocs := testing.AllocsPerRun(1000, func() { canvas.uniformValues(640, 480, changed); canvas.uniformValues(320, 180, neutral) }); allocs != 0 {
		t.Fatalf("canvas uniforms allocated %.0f objects", allocs)
	}
}

func BenchmarkCanvasUniforms(b *testing.B) {
	var canvas canvasGPU
	c := vm.CanvasEffects{}.Defaults()
	canvas.uniformValues(640, 480, c)
	b.ReportAllocs()
	for b.Loop() {
		canvas.uniformValues(640, 480, c)
	}
}

func TestCanvasFadingBlurAppliesColorEffectsOnce(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		v := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90, Color: "#804020"}
		v.Canvas = vm.CanvasEffects{}.Defaults()
		v.Canvas.Brightness = 50
		var g stageGPU
		defer g.Close()
		dst := ebiten.NewImage(32, 24)
		defer dst.Deallocate()
		for _, blur := range []float64{1, 0.001, 1e-40, 1e-50, 0} {
			v.Canvas.Blur = blur
			r.SetView(v)
			for range 3 {
				if err := g.Draw(dst, r, nil); err != nil {
					t.Fatal(err)
				}
				got := color.RGBAModel.Convert(dst.At(16, 12)).(color.RGBA)
				if got.R < 63 || got.R > 65 || got.G < 31 || got.G > 33 || got.B < 15 || got.B > 17 {
					t.Fatalf("blur=%g: color effects repeated: %v", blur, got)
				}
			}
		}
		v.Canvas = vm.CanvasEffects{}.Defaults()
		r.SetView(v)
		if err := g.Draw(dst, r, nil); err != nil {
			t.Fatal(err)
		}
		if got := color.RGBAModel.Convert(dst.At(16, 12)).(color.RGBA); got != (color.RGBA{128, 64, 32, 255}) {
			t.Fatalf("clear did not restore image: %v", got)
		}
	})
}

func TestCanvasBlurMatchesReference(t *testing.T) {
	runGPUFrame(t, func() {
		path, err := filepath.Abs("../../test/fixtures/render/canvas_filter_reference.kage")
		if err != nil {
			t.Fatal(err)
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		reference, err := ebiten.NewShader(source)
		if err != nil {
			t.Fatal(err)
		}
		defer reference.Deallocate()
		current, err := ebiten.NewShader(canvasShaderSource)
		if err != nil {
			t.Fatal(err)
		}
		defer current.Deallocate()
		for _, size := range []image.Point{{1, 1}, {16, 12}, {63, 47}} {
			backing := ebiten.NewImage(size.X+7, size.Y+9)
			src := backing.SubImage(image.Rect(3, 4, 3+size.X, 4+size.Y)).(*ebiten.Image)
			pixels := make([]byte, size.X*size.Y*4)
			for i := range pixels {
				pixels[i] = uint8(i*73 + i/11)
			}
			src.WritePixels(pixels)
			got, want := ebiten.NewImage(size.X, size.Y), ebiten.NewImage(size.X, size.Y)
			actual, expected := make([]byte, len(pixels)), make([]byte, len(pixels))
			for _, radius := range []float32{1e-6, .001, .05, .16, 1.0 / 6, .2, .75, 1, 1.5, 2, 4, 5, 12} {
				for axis := range 2 {
					blur := []float32{0, 0}
					blur[axis] = radius
					for _, nearest := range []int{0, 1} {
						options := &ebiten.DrawRectShaderOptions{
							Images: [4]*ebiten.Image{src}, Blend: ebiten.BlendCopy,
							Uniforms: map[string]any{"SourceSize": []float32{float32(size.X), float32(size.Y)},
								"Blur": blur, "Nearest": nearest},
						}
						got.DrawRectShader(size.X, size.Y, current, options)
						want.DrawRectShader(size.X, size.Y, reference, options)
						got.ReadPixels(actual)
						want.ReadPixels(expected)
						for i, value := range actual {
							if delta := int(value) - int(expected[i]); delta < -1 || delta > 1 {
								t.Fatalf("size=%v blur=%v nearest=%d: byte %d=%d, want %d", size, blur, nearest, i, value, expected[i])
							}
						}
					}
				}
			}
			backing.Deallocate()
			got.Deallocate()
			want.Deallocate()
		}
	})
}
