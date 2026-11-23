package render

import (
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"gowarp/core/media"
	"gowarp/core/vm"
)

func surfaceVertex(x, y, u, v float64, c color.NRGBA) vm.PenVertex {
	return vm.PenVertex{X: x, Y: y, U: u, V: v, R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255, A: float64(c.A) / 255, Z: -1, W: 1}
}

func surfaceQuad(r *Renderer, name string, x0, y0, x1, y1 float64, c color.NRGBA, texture string, textured bool) {
	v := [4]vm.PenVertex{surfaceVertex(x0, y1, 0, 0, c), surfaceVertex(x1, y1, 1, 0, c), surfaceVertex(x1, y0, 1, 1, c), surfaceVertex(x0, y0, 0, 1, c)}
	for _, indices := range [][3]int{{0, 1, 2}, {0, 2, 3}} {
		r.Pen(vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{Name: name, Texture: texture, Textured: textured, Vertices: [3]vm.PenVertex{v[indices[0]], v[indices[1]], v[indices[2]]}}})
	}
}

func TestPenSurfaceNamedTextureOrientationAndSharedEdge(t *testing.T) {
	r := New(&media.Library{})
	r.Pen(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "", Width: 4, Height: 4}})
	red, blue := color.NRGBA{R: 255, A: 128}, color.NRGBA{B: 255, A: 128}
	surfaceQuad(r, "", -2, 0, 2, 2, red, "", false)
	surfaceQuad(r, "", -2, -2, 2, 0, blue, "", false)
	surfaceQuad(r, vm.StageSurface, -2, -2, 2, 2, color.NRGBA{255, 255, 255, 255}, "", true)
	for y := 178; y < 182; y++ {
		for x := 238; x < 242; x++ {
			want := red
			if y >= 180 {
				want = blue
			}

			if got := r.pen.NRGBAAt(x, y); got != want {
				t.Fatalf("pixel %d,%d: got %v want %v", x, y, got, want)
			}
		}
	}
}

func TestPenSurfaceLifecycleAndStageClear(t *testing.T) {
	r := New(&media.Library{})
	create := vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "buffer", Width: 4, Height: 4}}
	r.Pen(create)
	surfaceQuad(r, "buffer", -2, -2, 2, 2, color.NRGBA{G: 255, A: 255}, "", false)
	r.Pen(vm.PenCommand{Kind: "clear"})
	surfaceQuad(r, vm.StageSurface, -2, -2, 2, 2, color.NRGBA{255, 255, 255, 255}, "buffer", true)
	if got := r.penAt(0, 0); got.G != 255 {
		t.Fatalf("stage clear lost named surface: %v", got)
	}
	for _, kind := range []string{"surface-clear", "surface-create", "surface-delete", "surface-reset"} {
		r.Pen(create)
		surfaceQuad(r, "buffer", -2, -2, 2, 2, color.NRGBA{G: 255, A: 255}, "", false)
		cmd := create
		cmd.Kind = kind
		r.Pen(cmd)
		r.Pen(vm.PenCommand{Kind: "clear"})
		surfaceQuad(r, vm.StageSurface, -2, -2, 2, 2, color.NRGBA{255, 255, 255, 255}, "buffer", true)
		if got := r.penAt(0, 0); got.A != 0 {
			t.Fatalf("%s retained pixels: %v", kind, got)
		}
	}
}

func TestPenSurfaceCostumeAndSensing(t *testing.T) {
	c := solid(color.NRGBA{G: 255, A: 255})
	r := New(&media.Library{Costumes: [][]*media.Costume{{c}}})
	target := vm.DisplayTarget{Source: 0, State: vm.TargetState{Size: 100, StretchX: 100, StretchY: 100, Direction: 90, Visible: true, Effects: vm.GraphicEffects{Ghost: 100}}}
	white := color.NRGBA{255, 255, 255, 255}
	cmd := vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{Name: vm.StageSurface, Textured: true, Costume: &target, Vertices: [3]vm.PenVertex{surfaceVertex(-5, 5, 0, 0, white), surfaceVertex(5, 5, 1, 0, white), surfaceVertex(0, -5, .5, 1, white)}}}
	r.Pen(cmd)
	if !r.TouchingColor([]vm.DisplayTarget{target}, target, color.NRGBA{G: 255, A: 255}, nil) {
		t.Fatal("raw costume triangle absent from color sensing")
	}
}

func TestPenSurfaceTextureAlphaModes(t *testing.T) {
	c := solid(color.NRGBA{R: 255, A: 128})
	white := color.NRGBA{255, 255, 255, 255}
	for _, multiply := range []bool{false, true} {
		r := New(&media.Library{Costumes: [][]*media.Costume{{c}}})
		cmd := vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{
			Name: vm.StageSurface, Textured: true, Costume: &vm.DisplayTarget{Source: 0},
			MultiplyTextureAlpha: multiply,
			Vertices:             [3]vm.PenVertex{surfaceVertex(-5, 5, .5, .5, white), surfaceVertex(5, 5, .5, .5, white), surfaceVertex(0, -5, .5, .5, white)},
		}}
		r.Pen(cmd)
		want := color.NRGBA{R: 255, A: 128}
		if multiply {
			want.R = 128
		}
		if got := r.penAt(0, 0); got != want {
			t.Fatalf("multiply=%v: got %v, want %v", multiply, got, want)
		}
	}
}

func TestPenSurfaceDepthPerspectiveAndNoOp(t *testing.T) {
	r := New(&media.Library{})
	red := color.NRGBA{R: 255, A: 255}
	vertices := [3]vm.PenVertex{surfaceVertex(-5, 5, 0, 0, red), surfaceVertex(5, 5, 1, 0, red), surfaceVertex(0, -5, .5, 1, red)}
	for _, invalid := range []float64{math.NaN(), math.Inf(1)} {
		v := vertices
		v[0].X = invalid
		r.Pen(vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{Name: vm.StageSurface, Vertices: v}})
	}
	v := vertices
	v[2] = v[1]
	r.Pen(vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{Name: vm.StageSurface, Vertices: v}})
	v = vertices
	for i := range v {
		v[i].A = 0
	}
	r.Pen(vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{Name: vm.StageSurface, Vertices: v}})
	if r.pen != nil {
		t.Fatal("no-op triangles allocated stage pixels")
	}
	r.Pen(vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{Name: vm.StageSurface, Vertices: vertices}})
	for i := range vertices {
		vertices[i].R = 0
		vertices[i].B = 1
		vertices[i].Z = 1
	}
	cmd := vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{Name: vm.StageSurface, Vertices: vertices}}
	r.Pen(cmd)
	if got := r.penAt(0, 0); got.R != 255 {
		t.Fatalf("farther triangle bypassed depth: %v", got)
	}
	r.Pen(vm.PenCommand{Kind: "clear"})
	r.Pen(cmd)
	if got := r.penAt(0, 0); got.B != 255 {
		t.Fatalf("stage clear retained depth: %v", got)
	}
}

func TestPreservePenDepthMatchesTexturedCoverage(t *testing.T) {
	for _, linear := range []bool{false, true} {
		for _, perspective := range []bool{false, true} {
			for _, multiply := range []bool{false, true} {
				full, depth := New(&media.Library{}), New(&media.Library{})
				for _, r := range []*Renderer{full, depth} {
					view := r.DisplayView()
					view.Width, view.Height = 16, 16
					r.SetView(view)
					r.Pen(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "texture", Width: 2, Height: 2}})
					pixels := r.PenSurface("texture")
					pixels.SetNRGBA(1, 0, color.NRGBA{R: 255, A: 128})
					pixels.SetNRGBA(0, 1, color.NRGBA{G: 255, A: 255})
				}
				white := color.NRGBA{255, 255, 255, 255}
				s := &vm.PenSurfaceCommand{
					Name: vm.StageSurface, Texture: "texture", Textured: true, Linear: linear, MultiplyTextureAlpha: multiply,
					Vertices: [3]vm.PenVertex{surfaceVertex(-8, 8, 0, 0, white), surfaceVertex(8, 8, 2, 0, white), surfaceVertex(-8, -8, 0, 2, white)},
				}
				if perspective {
					s.Vertices[1].W, s.Vertices[2].W = 2, 3
				}
				full.Pen(vm.PenCommand{Kind: "surface-triangle", Surface: s})
				depth.PreservePenDepth(s)
				if depth.pen != nil || depth.penComposite != nil {
					t.Fatal("depth replay allocated color buffers")
				}
				if len(depth.penDepth) != 256 || !slices.Equal(full.penDepth, depth.penDepth) {
					t.Fatalf("coverage differs: linear=%v perspective=%v multiply=%v", linear, perspective, multiply)
				}
				covered, transparent := false, false
				for _, z := range depth.penDepth {
					covered = covered || z < 0
					transparent = transparent || z == 0
				}
				if !covered || !transparent {
					t.Fatal("expected both covered and transparent pixels")
				}
			}
		}
	}
}

func TestPenTextureAlphaMatchesColorSampling(t *testing.T) {
	backing := image.NewNRGBA(image.Rect(-5, 7, 12, 20))
	for y := backing.Rect.Min.Y; y < backing.Rect.Max.Y; y++ {
		for x := backing.Rect.Min.X; x < backing.Rect.Max.X; x++ {
			backing.SetNRGBA(x, y, color.NRGBA{R: 240, G: 90, B: 175, A: uint8(x*29 + y*17)})
		}
	}
	images := []*image.NRGBA{backing, backing.SubImage(image.Rect(-2, 9, 6, 16)).(*image.NRGBA)}
	coordinates := []float64{-3.5, -1, -.01, -1e-20, -math.SmallestNonzeroFloat64, 0, .01, .125, .5, .999, 1, 2.5, math.NaN(), math.Inf(1)}
	for _, im := range images {
		for _, linear := range []bool{false, true} {
			for _, u := range coordinates {
				for _, v := range coordinates {
					_, _, _, want := penTextureSample(im, u, v, linear)
					if got := penTextureAlpha(im, u, v, linear); got != want {
						t.Fatalf("linear=%v uv=(%v,%v): alpha=%v, want %v", linear, u, v, got, want)
					}
				}
			}
		}
	}
}

func TestPenSurfacePerspectiveColor(t *testing.T) {
	r := New(&media.Library{})
	v := [3]vm.PenVertex{
		surfaceVertex(-4, 4, 0, 0, color.NRGBA{R: 255, A: 255}),
		surfaceVertex(4, 4, 1, 0, color.NRGBA{G: 255, A: 255}),
		surfaceVertex(-4, -4, 0, 1, color.NRGBA{B: 255, A: 255}),
	}
	v[1].W, v[2].W = 2, 4
	r.Pen(vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{Name: vm.StageSurface, Vertices: v}})
	/*
		Pixel center (-2.5,2.5) has screen barycentrics (10,3,3)/16.
		Dividing by W and normalizing gives color weights (40,6,3)/49.
	*/
	want := color.NRGBA{R: 208, G: 31, B: 16, A: 255}
	if got := r.penAt(-2.5, 2.5); got != want {
		t.Fatalf("perspective color: got %v want %v", got, want)
	}
	if got := r.penAt(-3.5, -2.5); got.A != 255 {
		t.Fatalf("W incorrectly changed triangle position: %v", got)
	}
}

func TestPenSurfaceTextureFilteringAndWrap(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	im.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 128})
	im.SetNRGBA(1, 0, color.NRGBA{B: 255, A: 255})
	for _, tt := range []struct {
		u      float64
		linear bool
		want   color.NRGBA
	}{
		{.25, false, color.NRGBA{R: 128, A: 128}},
		{-.25, false, color.NRGBA{B: 255, A: 255}},
		{1.25, false, color.NRGBA{R: 128, A: 128}},
		{1, false, color.NRGBA{R: 128, A: 128}},
		{0, true, color.NRGBA{R: 128, A: 128}},
		{.5, true, color.NRGBA{R: 64, B: 128, A: 192}},
		{.99, true, color.NRGBA{B: 255, A: 255}},
	} {
		r, g, b, a := penTextureSample(im, tt.u, .5, tt.linear)
		got := color.NRGBA{penColorByte(r), penColorByte(g), penColorByte(b), penColorByte(a)}
		if got != tt.want {
			t.Fatalf("u=%v linear=%v: got %v want %v", tt.u, tt.linear, got, tt.want)
		}
	}
}

func TestPenSurfaceNoOpAllocations(t *testing.T) {
	r := New(&media.Library{})
	white := color.NRGBA{255, 255, 255, 255}
	v := [3]vm.PenVertex{surfaceVertex(-5, 5, 0, 0, white), surfaceVertex(5, 5, 1, 0, white), surfaceVertex(0, -5, .5, 1, white)}
	cmd := vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{Name: vm.StageSurface, Vertices: v, Textured: true, Texture: "missing"}}
	if allocations := testing.AllocsPerRun(10, func() { r.Pen(cmd) }); allocations != 0 {
		t.Fatalf("missing texture allocated %v objects", allocations)
	}
	cmd.Surface.Textured = false
	for i := range cmd.Surface.Vertices {
		cmd.Surface.Vertices[i].X += 10000
	}
	if allocations := testing.AllocsPerRun(10, func() { r.Pen(cmd) }); allocations != 0 {
		t.Fatalf("offstage triangle allocated %v objects", allocations)
	}
	if r.pen != nil || r.penDepth != nil {
		t.Fatal("no-op allocated stage buffers")
	}
}

func TestPenSurfaceResizeKeepsNamedDimensions(t *testing.T) {
	r := New(&media.Library{})
	r.Pen(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "buffer", Width: 4, Height: 2}})
	surfaceQuad(r, "buffer", -2, -1, 2, 1, color.NRGBA{G: 255, A: 255}, "", false)
	view := r.DisplayView()
	view.Width, view.Height = 100, 100
	r.SetView(view)
	if pixels := r.PenSurface("buffer"); pixels == nil || pixels.Bounds() != image.Rect(0, 0, 4, 2) || pixels.NRGBAAt(0, 0).G != 255 {
		t.Fatal("stage resize changed named surface")
	}
}

func TestPenSurfaceAlphaAndDepthDiscard(t *testing.T) {
	r := New(&media.Library{})
	v := [3]vm.PenVertex{
		surfaceVertex(-5, 5, 0, 0, color.NRGBA{R: 64, A: 255}),
		surfaceVertex(5, 5, 1, 0, color.NRGBA{R: 64, A: 255}),
		surfaceVertex(0, -5, .5, 1, color.NRGBA{R: 64, A: 255}),
	}
	for i := range v {
		v[i].A = 2
	}
	cmd := vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{Name: vm.StageSurface, Vertices: v}}
	r.Pen(cmd)
	if got := r.penAt(0, 0); got != (color.NRGBA{R: 128, A: 255}) {
		t.Fatalf("alpha over one: %v", got)
	}
	r.Pen(vm.PenCommand{Kind: "surface-clear", Surface: &vm.PenSurfaceCommand{Name: vm.StageSurface}})
	for i := range cmd.Surface.Vertices {
		cmd.Surface.Vertices[i].A = 0
	}
	r.Pen(cmd)
	for i := range cmd.Surface.Vertices {
		cmd.Surface.Vertices[i].A = 1
		cmd.Surface.Vertices[i].Z = 1
	}
	r.Pen(cmd)
	if got := r.penAt(0, 0); got != (color.NRGBA{R: 64, A: 255}) {
		t.Fatalf("discarded transparent fragment occluded later triangle: %v", got)
	}
}

func TestRepeatedPenClearAfterDrawing(t *testing.T) {
	r := New(&media.Library{})
	for i := 0; i < 3; i++ {
		surfaceQuad(r, vm.StageSurface, -2, -2, 2, 2, color.NRGBA{R: 255, A: 255}, "", false)
		if r.penAt(0, 0).A == 0 {
			t.Fatal("drawing after clear was lost")
		}
		for j := 0; j < 3; j++ {
			before := r.penRevision
			r.Pen(vm.PenCommand{Kind: "clear"})
			if r.penRevision != before+1 || r.penAt(0, 0).A != 0 {
				t.Fatal("clear changed revision or left pixels")
			}
			for _, depth := range r.penDepth {
				if depth != 0 {
					t.Fatal("clear left depth values")
				}
			}
		}
	}
	pixels := image.NewRGBA(image.Rect(0, 0, Width, Height))
	pixels.SetRGBA(Width/2, Height/2, color.RGBA{G: 255, A: 255})
	r.RestorePen(pixels)
	r.Pen(vm.PenCommand{Kind: "clear"})
	if r.penAt(0, 0).A != 0 || r.penComposite != nil {
		t.Fatal("restored pixels survived clear")
	}
}

func BenchmarkRepeatedPenClear(b *testing.B) {
	r := New(&media.Library{})
	view := r.view
	view.Width, view.Height = 800, 600
	r.SetView(view)
	surfaceQuad(r, vm.StageSurface, -2, -2, 2, 2, color.NRGBA{R: 255, A: 255}, "", false)
	r.Pen(vm.PenCommand{Kind: "clear"})
	b.ReportAllocs()
	for b.Loop() {
		r.Pen(vm.PenCommand{Kind: "clear"})
	}
}

func TestRestoredSurfaceDoesNotAllocatePerPixel(t *testing.T) {
	r := New(&media.Library{})
	r.SetView(vm.View{Width: 64, Height: 64, Zoom: 100, Direction: 90})
	r.RestorePen(image.NewRGBA(image.Rect(0, 0, 64, 64)))
	c := color.NRGBA{R: 200, G: 100, A: 128}
	cmd := vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{
		Name: vm.StageSurface,
		Vertices: [3]vm.PenVertex{
			surfaceVertex(-32, 32, 0, 0, c),
			surfaceVertex(32, 32, 1, 0, c),
			surfaceVertex(-32, -32, 0, 1, c),
		},
	}}
	r.Pen(cmd)
	if allocs := testing.AllocsPerRun(5, func() { r.Pen(cmd) }); allocs > 1 {
		t.Fatalf("restored triangle allocated %.0f objects", allocs)
	}
}

func BenchmarkRestoredSurface(b *testing.B) {
	r := New(&media.Library{})
	r.SetView(vm.View{Width: 800, Height: 600, Zoom: 100, Direction: 90})
	r.RestorePen(image.NewRGBA(image.Rect(0, 0, 800, 600)))
	c := color.NRGBA{R: 200, G: 100, A: 128}
	cmd := vm.PenCommand{Kind: "surface-triangle", Surface: &vm.PenSurfaceCommand{
		Name: vm.StageSurface,
		Vertices: [3]vm.PenVertex{
			surfaceVertex(-400, 300, 0, 0, c),
			surfaceVertex(400, 300, 1, 0, c),
			surfaceVertex(-400, -300, 0, 1, c),
		},
	}}
	r.Pen(cmd)
	b.ReportAllocs()
	for b.Loop() {
		r.Pen(cmd)
	}
}

func BenchmarkPreserveTexturedPenDepth(b *testing.B) {
	for _, linear := range []bool{false, true} {
		name := "nearest"
		if linear {
			name = "linear"
		}
		b.Run(name, func(b *testing.B) {
			r := New(&media.Library{})
			r.SetView(vm.View{Width: 800, Height: 600, Zoom: 100, Direction: 90})
			r.Pen(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "texture", Width: 64, Height: 64}})
			pixels := r.PenSurface("texture")
			for y := range 64 {
				for x := range 64 {
					pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(y * 4), B: 200, A: uint8((x + y) * 2)})
				}
			}
			white := color.NRGBA{255, 255, 255, 255}
			s := &vm.PenSurfaceCommand{
				Name: vm.StageSurface, Texture: "texture", Textured: true, Linear: linear,
				Vertices: [3]vm.PenVertex{
					surfaceVertex(-400, 300, 0, 0, white),
					surfaceVertex(400, 300, 1, 0, white),
					surfaceVertex(-400, -300, 0, 1, white),
				},
			}
			r.PreservePenDepth(s)
			b.ReportAllocs()
			for b.Loop() {
				r.PreservePenDepth(s)
			}
		})
	}
}

func BenchmarkThinPenDepth(b *testing.B) {
	r := New(&media.Library{})
	r.SetView(vm.View{Width: 800, Height: 600, Zoom: 100, Direction: 90})
	white := color.NRGBA{255, 255, 255, 255}
	s := &vm.PenSurfaceCommand{Name: vm.StageSurface, Vertices: [3]vm.PenVertex{
		surfaceVertex(-390, 290, 0, 0, white), surfaceVertex(390, -290, 1, 1, white), surfaceVertex(380, -290, 0, 1, white),
	}}
	r.PreservePenDepth(s)
	b.ReportAllocs()
	for b.Loop() {
		r.PreservePenDepth(s)
	}
}

func TestPenTriangleRowBounds(t *testing.T) {
	triangles := [][3]vm.PenVertex{
		{{X: 0, Y: 0}, {X: 96, Y: 64}, {X: 94, Y: 64}},
		{{X: 0.5, Y: 0.5}, {X: 96.5, Y: 0.5}, {X: 48.5, Y: 64.5}},
		{{X: -100, Y: 32.5}, {X: 200, Y: 32.5}, {X: 48, Y: 33.5}},
		{{X: 0, Y: 0}, {X: 96, Y: 64}, {X: 48, Y: 32.000001}},
		{{X: -65000, Y: -65000}, {X: 65000, Y: 65000}, {X: 65000, Y: 64990}},
	}
	random := rand.New(rand.NewPCG(37, 61))
	for range 200 {
		var v [3]vm.PenVertex
		for i := range v {
			v[i].X = random.Float64()*250 - 75
			v[i].Y = random.Float64()*180 - 55
		}
		triangles = append(triangles, v)
	}
	for n, v := range triangles {
		if penEdge(v[0], v[1], v[2].X, v[2].Y) < 0 {
			v[1], v[2] = v[2], v[1]
		}
		for y := range 64 {
			lo, hi := penTriangleRowBounds(v, float64(y)+.5, 0, 96)
			if lo < 0 || hi > 96 || lo > hi {
				t.Fatalf("triangle %d row %d: invalid range %d..%d", n, y, lo, hi)
			}
			for x := range 96 {
				inside := true
				for i := range 3 {
					a, b := v[i], v[(i+1)%3]
					edge := penEdge(a, b, float64(x)+.5, float64(y)+.5)
					if edge < 0 || edge == 0 && !penTopLeft(a, b) {
						inside = false
					}
				}
				if inside && (x < lo || x >= hi) {
					t.Fatalf("triangle %d: lost pixel %d,%d, range %d..%d", n, x, y, lo, hi)
				}
			}
		}
	}
	v := [3]vm.PenVertex{{X: 0, Y: 0}, {X: 96, Y: 64}, {X: 94, Y: 64}}
	lo, hi := penTriangleRowBounds(v, 32.5, 0, 96)
	if hi-lo > 8 {
		t.Fatalf("thin triangle still scans %d pixels", hi-lo)
	}
}
