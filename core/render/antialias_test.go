package render

import (
	"bytes"
	"image"
	"image/color"
	"slices"
	"testing"

	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestNoAntialiasTextCoverageAndCache(t *testing.T) {
	for _, tc := range []struct {
		color string
		alpha uint8
	}{{"#ff0000", 255}, {"1090453504", 64}} {
		r := New(&media.Library{})
		target := vm.DisplayTarget{State: vm.TargetState{Text: &vm.TextSkin{
			Text: "Ag", Color: tc.color, FontSize: 23, Width: 100, LineHeight: 30, PaddingY: 4,
		}}}
		smooth := r.textCostume(target)
		partial := false
		for i := 3; i < len(smooth.Image.Pix); i += 4 {
			alpha := smooth.Image.Pix[i]
			partial = partial || alpha > 0 && alpha < tc.alpha
		}
		if !partial {
			t.Fatal("default text rendering has no antialiased coverage")
		}
		view := r.DisplayView()
		view.NoAntialias = true
		r.SetView(view)
		sharp := r.textCostume(target)
		if sharp == smooth {
			t.Fatal("antialiasing change reused the smooth text costume")
		}
		if sharp.Width != smooth.Width || sharp.Height != smooth.Height || sharp.CenterY != smooth.CenterY {
			t.Fatal("antialiasing changed text layout")
		}
		visible := false
		for i := 3; i < len(sharp.Image.Pix); i += 4 {
			alpha := sharp.Image.Pix[i]
			if alpha != 0 && alpha != tc.alpha {
				t.Fatalf("sharp text alpha = %d, want 0 or intended opacity %d", alpha, tc.alpha)
			}
			visible = visible || alpha != 0
		}
		if !visible {
			t.Fatal("sharp text is empty")
		}
		view.NoAntialias = false
		r.SetView(view)
		if r.textCostume(target) != smooth {
			t.Fatal("restoring antialiasing did not restore the smooth cached costume")
		}
	}
}

func TestNoAntialiasDisplaySampling(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	im.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 64})
	im.SetNRGBA(1, 0, color.NRGBA{B: 255, A: 64})
	costume := &media.Costume{Image: im, Width: 2, Height: 1, CenterX: 1, CenterY: .5}
	r := New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
	view := r.DisplayView()
	view.Width, view.Height, view.Color = 8, 8, "#000000"
	r.SetView(view)
	target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 200, Direction: 90, StretchX: 100, StretchY: 100}}
	if got := r.DrawAt([]vm.DisplayTarget{target}, 8, 8).RGBAAt(3, 4); got.R == 0 || got.B == 0 {
		t.Fatalf("default display sampling did not interpolate: %v", got)
	}
	view.NoAntialias = true
	r.SetView(view)
	if got := r.DrawAt([]vm.DisplayTarget{target}, 8, 8).RGBAAt(3, 4); got != (color.RGBA{R: 64, A: 255}) {
		t.Fatalf("sharp display pixel = %v, want unchanged translucent red over black", got)
	}
}

func TestNoAntialiasPenMatchesExtensionCoverage(t *testing.T) {
	var sharp []byte
	for _, mode := range [][2]bool{{false, false}, {false, true}, {true, false}, {true, true}} {
		r := New(&media.Library{})
		view := r.DisplayView()
		view.Width, view.Height = 16, 16
		view.NoAntialias, view.PenNoAntialias = mode[0], mode[1]
		r.SetView(view)
		r.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{-5, -3, 5, 3}, Size: 2, Color: color.NRGBA{R: 255, A: 64}})
		partial, visible := false, false
		for i := 3; i < len(r.pen.Pix); i += 4 {
			alpha := r.pen.Pix[i]
			partial = partial || alpha > 0 && alpha < 64
			visible = visible || alpha > 0
		}
		if !visible || partial == (mode[0] || mode[1]) {
			t.Fatalf("mode=%v: visible=%v partial coverage=%v", mode, visible, partial)
		}
		if mode[1] && !mode[0] {
			sharp = append([]byte(nil), r.pen.Pix...)
		}
		if mode[0] && !bytes.Equal(r.pen.Pix, sharp) {
			t.Fatalf("global no-antialiasing mode %v differs from extension coverage", mode)
		}
	}
}

func TestNoAntialiasPenSurfaceSamplingAndDepth(t *testing.T) {
	var sharpPixels []byte
	var sharpDepth []float64
	for _, mode := range [][2]bool{{false, false}, {true, true}, {false, true}} {
		r := New(&media.Library{})
		view := r.DisplayView()
		view.Width, view.Height, view.NoAntialias = 8, 8, mode[0]
		r.SetView(view)
		r.Pen(vm.PenCommand{Kind: "surface-create", Surface: &vm.PenSurfaceCommand{Name: "texture", Width: 2, Height: 1}})
		r.PenSurface("texture").SetNRGBA(0, 0, color.NRGBA{R: 255, A: 64})
		white := color.NRGBA{255, 255, 255, 255}
		s := &vm.PenSurfaceCommand{Name: vm.StageSurface, Texture: "texture", Textured: true, Linear: mode[1],
			Vertices: [3]vm.PenVertex{surfaceVertex(-4, 4, 0, 0, white), surfaceVertex(4, 4, 1, 0, white), surfaceVertex(-4, -4, 0, 1, white)}}
		r.Pen(vm.PenCommand{Kind: "surface-triangle", Surface: s})
		if !mode[1] {
			sharpPixels = append([]byte(nil), r.pen.Pix...)
			sharpDepth = append([]float64(nil), r.penDepth...)
		} else if mode[0] {
			if !bytes.Equal(r.pen.Pix, sharpPixels) || !slices.Equal(r.penDepth, sharpDepth) {
				t.Fatal("global no-antialiasing did not override linear texture sampling")
			}
			r.Pen(vm.PenCommand{Kind: "clear"})
			r.PreservePenDepth(s)
			if !slices.Equal(r.penDepth, sharpDepth) {
				t.Fatal("depth replay used smoothed texture alpha")
			}
		} else if bytes.Equal(r.pen.Pix, sharpPixels) {
			t.Fatal("default linear texture sampling was disabled")
		}
	}
}

func TestNoAntialiasImportedSVGPreservesOpacity(t *testing.T) {
	r := New(&media.Library{Costumes: [][]*media.Costume{{}}})
	data := []byte(`<svg width="32" height="32"><path d="M2.2 1.4L29.4 27.7L4.5 29.1Z" fill="red" opacity="0.5"/></svg>`)
	for _, noAntialias := range []bool{false, true, false} {
		view := r.DisplayView()
		view.NoAntialias = noAntialias
		r.SetView(view)
		meta, err := r.AddCostume(0, project.Costume{DataFormat: "svg"}, data)
		if err != nil {
			t.Fatal(err)
		}
		costumes := r.Assets.Costumes[0]
		imported := costumes[len(costumes)-1]
		if imported.Width != 32 || imported.Height != 32 || imported.CenterX != 16 || imported.CenterY != 16 || meta.RotationCenterX != 16 || meta.RotationCenterY != 16 {
			t.Fatal("import layout changed")
		}
		partial, visible := false, false
		for i := 3; i < len(imported.Image.Pix); i += 4 {
			alpha := imported.Image.Pix[i]
			if alpha > 127 {
				t.Fatalf("import lost intended half opacity: alpha=%d", alpha)
			}
			partial = partial || alpha > 0 && alpha < 127
			visible = visible || alpha != 0
		}
		if !visible || partial == noAntialias {
			t.Fatalf("import noAntialias=%v: visible=%v partial coverage=%v", noAntialias, visible, partial)
		}
	}
}
