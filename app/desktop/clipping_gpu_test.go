package desktop

import (
	"github.com/hajimehoshi/ebiten/v2"
	e "gowarp/core/extension"
	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"
	penplus "gowarp/test/extensions/penP"
	post "gowarp/test/extensions/postprocessingv5"
	"image"
	"image/color"
	"testing"
)

type capturePostShaders struct{ sources []e.ShaderSource }

func (h *capturePostShaders) Call(op string, args ...any) (any, error) {
	if op == "graphics.shader.define" {
		h.sources = append(h.sources, args[1].(e.ShaderSource))
	}
	return nil, nil
}
func TestDusttrustPostprocessingShaders(t *testing.T) {
	x := post.New()
	h := &capturePostShaders{}
	c := &e.Context{State: map[string]any{}, Host: h}
	for _, name := range []string{"chromatic", "fisheye", "pixelwarp", "screensplit", "画中画边框"} {
		c.Args = map[string]any{"Menu": name}
		for _, b := range x.Blocks {
			if b.Opcode == "postprocessingv5_opcodePipelineAdd" {
				if _, err := b.Handler(c); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for _, src := range h.sources {
		program, err := src.Compile()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ebiten.NewShader([]byte(program.Code)); err != nil {
			t.Fatalf("%v\n%s", err, program.Code)
		}
	}
}
func TestDusttrustClippingBlendMaskGPU(t *testing.T) {
	runGPUFrame(t, func() {
		makeCostume := func(c color.NRGBA) *media.Costume {
			im := image.NewNRGBA(image.Rect(0, 0, 8, 8))
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					im.SetNRGBA(x, y, c)
				}
			}
			return &media.Costume{Image: im, Width: 8, Height: 8, CenterX: 4, CenterY: 4, Bitmap: true}
		}
		lib := &media.Library{Costumes: [][]*media.Costume{{makeCostume(color.NRGBA{80, 40, 20, 255})}, {makeCostume(color.NRGBA{20, 70, 160, 255})}}}
		r := render.New(lib)
		view := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90, Color: "#203040", MaskTarget: "base"}
		r.SetView(view)
		base := vm.DisplayTarget{State: vm.TargetState{Name: "base", Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, X: -2}}
		top := base
		top.Source = 1
		top.State.Name = "top"
		top.State.X = 2
		top.State.MaskMode = true
		top.State.Clipbox = &vm.Bounds{Left: 0, Right: 16, Bottom: -12, Top: 12}
		var g stageGPU
		defer g.Close()
		dst := ebiten.NewImage(32, 24)
		defer dst.Deallocate()
		for _, mode := range []string{"default", "additive", "multiply"} {
			top.State.BlendMode = mode
			scene := []vm.DisplayTarget{base, top}
			if err := g.Draw(dst, r, scene); err != nil {
				t.Fatal(err)
			}
			want := r.Draw(scene)
			got := make([]byte, len(want.Pix))
			dst.ReadPixels(got)
			for i, n := range want.Pix {
				delta := int(n) - int(got[i])
				if delta < -1 || delta > 1 {
					t.Fatalf("%s pixel %d: GPU %d CPU %d", mode, i, got[i], n)
				}
			}
		}
		// Dusttrust keeps battle//box shown with ghost=100 as its stencil.
		top.State.BlendMode = "default"
		lib.Costumes[0] = append(lib.Costumes[0], makeCostume(color.NRGBA{}))
		for i, ghost := range []float64{100, 0} {
			base.State.CurrentCostume, base.State.Effects.Ghost = i, ghost
			if err := g.Draw(dst, r, []vm.DisplayTarget{base, top}); err != nil {
				t.Fatal(err)
			}
			if c := color.RGBAModel.Convert(dst.At(16, 12)).(color.RGBA); c != (color.RGBA{20, 70, 160, 255}) {
				t.Fatalf("invisible box %d hid bone: %v", i, c)
			}
			if c := color.RGBAModel.Convert(dst.At(20, 12)).(color.RGBA); c != (color.RGBA{32, 48, 64, 255}) {
				t.Fatalf("bone escaped invisible box: %v", c)
			}
		}
		// A masked drawable earlier than its base has an empty stencil.
		if err := g.Draw(dst, r, []vm.DisplayTarget{top, base}); err != nil {
			t.Fatal(err)
		}
		if c := color.RGBAModel.Convert(dst.At(20, 12)).(color.RGBA); c != (color.RGBA{32, 48, 64, 255}) {
			t.Fatalf("mask draw order: %v", c)
		}
		// Exact additive stamp RGB survives zero alpha, GPU readback and CPU draw.
		graphics, err := g.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		top.State.MaskMode = false
		top.State.BlendMode = "additive"
		top.State.Clipbox = nil
		graphics.Pen(vm.PenCommand{Kind: "stamp", Target: top})
		if err := g.Draw(dst, r, nil); err != nil {
			t.Fatal(err)
		}
		before := color.RGBAModel.Convert(dst.At(18, 12)).(color.RGBA)
		g.nativePen.flush()
		after := r.Draw(nil).RGBAAt(18, 12)
		if before != after {
			t.Fatalf("additive readback: GPU %v CPU %v", before, after)
		}
		if before.B <= 64 {
			t.Fatalf("additive stamp disappeared: %v", before)
		}
	})
}
func TestDusttrustShaderPipelineGPU(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		v := vm.View{Width: 16, Height: 12, Zoom: 100, Direction: 90, Color: "#804020"}
		v.Shader = &vm.StageShader{Compiler: penplus.ShaderCompiler{}, Fragment: "uniform sampler2D u_skin; void main(){gl_FragColor=texture2D(u_skin,gl_FragCoord.xy/vec2(16,12))*vec4(.5,1,1,1);}", Next: &vm.StageShader{Compiler: penplus.ShaderCompiler{}, Fragment: "uniform sampler2D u_skin; void main(){gl_FragColor=texture2D(u_skin,gl_FragCoord.xy/vec2(16,12))*vec4(1,.5,1,1);}"}}
		r.SetView(v)
		var g stageGPU
		defer g.Close()
		dst := ebiten.NewImage(16, 12)
		defer dst.Deallocate()
		for i := 0; i < 3; i++ {
			if err := g.Draw(dst, r, nil); err != nil {
				t.Fatal(err)
			}
			got := color.RGBAModel.Convert(dst.At(8, 6)).(color.RGBA)
			if got != (color.RGBA{64, 32, 32, 255}) {
				t.Fatalf("pipeline frame %d: %v", i, got)
			}
		}
	})
}

func TestDusttrustPipelineSharedUniforms(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		uniforms := map[string]any{"amount": float32(.5)}
		fragment := "uniform sampler2D u_skin; uniform float amount; void main(){gl_FragColor=texture2D(u_skin,gl_FragCoord.xy/vec2(16,12))*vec4(amount,1,1,1);}"
		v := vm.View{Width: 16, Height: 12, Zoom: 100, Direction: 90, Color: "#804020", Shader: &vm.StageShader{Compiler: penplus.ShaderCompiler{}, Fragment: fragment, Uniforms: uniforms, Next: &vm.StageShader{Compiler: penplus.ShaderCompiler{}, Fragment: fragment, Uniforms: uniforms, Overrides: map[string]any{"amount": float32(.25)}}}}
		r.SetView(v)
		var g stageGPU
		defer g.Close()
		dst := ebiten.NewImage(16, 12)
		defer dst.Deallocate()
		for i, want := range []uint8{16, 8} {
			if err := g.Draw(dst, r, nil); err != nil {
				t.Fatal(err)
			}
			got := color.RGBAModel.Convert(dst.At(8, 6)).(color.RGBA)
			if got.R != want {
				t.Fatalf("frame %d shared shader uniform: %v, want R=%d", i, got, want)
			}
		}
	})
}
func TestDusttrustDeferredPenClipBlend(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		v := vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90, Color: "#000000"}
		r.SetView(v)
		var g stageGPU
		defer g.Close()
		graphics, err := g.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		command := vm.PenCommand{Kind: "line", Target: vm.DisplayTarget{State: vm.TargetState{BlendMode: "additive", Clipbox: &vm.Bounds{Left: 0, Right: 16, Bottom: -12, Top: .25}}}, Line: [4]float64{-12, 0, 12, 0}, Size: 4, Color: color.NRGBA{R: 80, A: 255}}
		graphics.Pen(command)
		reference := render.New(&media.Library{})
		reference.SetView(v)
		reference.Pen(command)
		dst := ebiten.NewImage(32, 24)
		defer dst.Deallocate()
		if err := g.Draw(dst, r, nil); err != nil {
			t.Fatal(err)
		}
		want := reference.Draw(nil)
		for y := 0; y < 24; y++ {
			for x := 0; x < 32; x++ {
				got := color.RGBAModel.Convert(dst.At(x, y)).(color.RGBA)
				w := want.RGBAAt(x, y)
				if got != w {
					t.Fatalf("clipped additive deferred line (%d,%d): %v want %v", x, y, got, w)
				}
			}
		}
	})
}
