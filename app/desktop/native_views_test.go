package desktop

import (
	"encoding/json"
	"image"
	"image/color"
	"math"
	"slices"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/app/config"
	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/window"
)

func TestNativeWindowEditsAreValidatedBeforeCommit(t *testing.T) {
	alpha := 0.5
	layout, err := window.Pack([]window.View{{ID: "main", Width: 480, Height: 360, StageWidth: 480, StageHeight: 360, Opacity: &alpha}})
	if err != nil {
		t.Fatal(err)
	}
	g := &game{native: &nativeViews{layout: layout}}
	if _, err = g.nativeWindowCall("set", []any{"main", `{"opacity":3}`}); err == nil {
		t.Fatal("invalid opacity accepted")
	}
	if g.native.layout.Views[0].Alpha() != 0.5 {
		t.Fatal("rejected edit changed live opacity")
	}
	if _, err = g.nativeWindowCall("move", []any{"main", -800.0, 50.0}); err != nil {
		t.Fatal(err)
	}
	if g.native.layout.Size != layout.Size || g.native.layout.Views[0].X != -800 {
		t.Fatal(g.native.layout)
	}
	if _, err = g.nativeWindowCall("set", []any{"main", `{"width":640,"height":480}`}); err != nil {
		t.Fatal(err)
	}
	if g.native.layout.Size.X != 640 {
		t.Fatal("resize did not update atlas")
	}
}

func TestNativeWindowCreateAndRemove(t *testing.T) {
	g := &game{native: &nativeViews{}}
	value := `{"id":"view","width":200,"height":100,"stageWidth":200,"stageHeight":100}`
	if _, err := g.nativeWindowCall("create", []any{value}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.nativeWindowCall("create", []any{value}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := g.nativeWindowCall("remove", []any{"view"}); err != nil {
		t.Fatal(err)
	}
	if len(g.native.layout.Views) != 0 {
		t.Fatal("view not removed")
	}
}

func TestNativeViewportMovesWithoutChangingWindowOrAtlas(t *testing.T) {
	v := window.View{ID: "main", X: 30, Y: 50, Width: 480, Height: 360, StageWidth: 480, StageHeight: 360}
	layout, err := window.Pack([]window.View{v})
	if err != nil {
		t.Fatal(err)
	}
	g := &game{native: &nativeViews{layout: layout}}
	if _, err := g.nativeWindowCall("viewport", []any{"main", -40.5, 80.0}); err != nil {
		t.Fatal(err)
	}
	got := g.native.layout.Views[0]
	if got.StageX != -40.5 || got.StageY != 80 || got.StageWidth != 480 || got.StageHeight != 360 || got.X != 30 || got.Y != 50 || g.native.layout.Size != layout.Size {
		t.Fatalf("viewport move changed window geometry: %+v", got)
	}
	if _, err := g.nativeWindowCall("viewport", []any{"main", 20.0, 10.0, 240.0, 180.0}); err != nil {
		t.Fatal(err)
	}
	got = g.native.layout.Views[0]
	if got.StageWidth != 240 || got.StageHeight != 180 || g.native.layout.Size != layout.Size {
		t.Fatal(got)
	}
	for _, args := range [][]any{{"main", 1.0}, {"main", 1.0, 2.0, 3.0}, {"main", 0.0, 0.0, 0.0, 180.0}} {
		if _, err := g.nativeWindowCall("viewport", args); err == nil {
			t.Fatalf("invalid viewport accepted: %v", args)
		}
		if g.native.layout.Views[0].StageX != got.StageX || g.native.layout.Views[0].StageWidth != got.StageWidth {
			t.Fatal("invalid edit changed viewport")
		}
	}
}

func TestNativeWindowJSONRejectsTrailingData(t *testing.T) {
	var v window.View
	if err := decodeView(`{"id":"a"} {"id":"b"}`, &v); err == nil {
		t.Fatal("trailing data accepted")
	}
}

func TestNativeWindowControls(t *testing.T) {
	g := &game{native: &nativeViews{}}
	_, err := g.nativeWindowCall("create", []any{`{"id":"main","width":480,"height":360,"stageWidth":480,"stageHeight":360,"border":false,"resizable":true,"disableClose":true,"disableMinimize":true,"disableMaximize":true,"fullscreen":false,"opacity":0.4}`})
	if err != nil {
		t.Fatal(err)
	}
	value, err := g.nativeWindowCall("get", []any{"main"})
	if err != nil {
		t.Fatal(err)
	}
	var properties map[string]any
	if err := json.Unmarshal([]byte(value.(string)), &properties); err != nil {
		t.Fatal(err)
	}
	if properties["disableClose"] != true || properties["border"] != false || properties["opacity"] != 0.4 {
		t.Fatal(properties)
	}
	// Explicit removal is allowed even when user closing is disabled.
	if _, err := g.nativeWindowCall("remove", []any{"main"}); err != nil {
		t.Fatal(err)
	}
	if len(g.native.layout.Views) != 0 {
		t.Fatal("explicit removal was blocked")
	}
}

func TestMainWindowAPIControlsWithoutMultiwindow(t *testing.T) {
	alpha := 1.0
	g := &game{windowReady: true, windowDirty: true, mainWindow: window.View{ID: "main", Width: 480, Height: 360, StageWidth: 480, StageHeight: 360, Opacity: &alpha, Border: true}}
	if _, err := g.windowCall("set", []any{"main", `{"border":false,"disableClose":true,"opacity":0.5,"resizable":true,"disableMinimize":true,"disableMaximize":true,"fullscreen":true}`}); err != nil {
		t.Fatal(err)
	}
	if g.mainWindow.Border || !g.mainWindow.DisableClose || !g.mainWindow.Fullscreen || g.mainWindow.Alpha() != 0.5 {
		t.Fatal(g.mainWindow)
	}
	if _, err := g.windowCall("set", []any{"main", `{"opacity":2}`}); err == nil {
		t.Fatal("invalid opacity accepted")
	}
	if g.mainWindow.Alpha() != 0.5 {
		t.Fatal("rejected edit changed live alpha")
	}
	if _, err := g.windowCall("create", []any{`{"id":"extra"}`}); err == nil {
		t.Fatal("additional window created without multiwindow")
	}
	if _, err := g.windowCall("set", []any{"main", `{"stageX":12}`}); err == nil {
		t.Fatal("unsupported crop accepted")
	}
	if _, err := g.windowCall("set", []any{"main", `{"hidden":true}`}); err == nil {
		t.Fatal("unsupported hiding accepted")
	}
	if _, err := g.windowCall("set", []any{"main", `{"topmost":true,"passthrough":true}`}); err != nil {
		t.Fatal(err)
	}
	if !g.mainWindow.Topmost || !g.mainWindow.Passthrough {
		t.Fatal("main window flags were not stored")
	}
	if _, err := g.windowCall("remove", []any{"main"}); err != nil {
		t.Fatal(err)
	}
	if !g.windowCloseRequested {
		t.Fatal("explicit close blocked by disableClose")
	}
}

func TestMainWindowPropertiesApplyAndRestore(t *testing.T) {
	runGPUFrame(t, func() {
		g := &game{}
		for _, edit := range []struct {
			patch    string
			border   bool
			resizing ebiten.WindowResizingModeType
		}{
			{`{"border":false,"resizable":false,"disableMinimize":true,"disableMaximize":true,"disableClose":true,"opacity":0.5}`, false, ebiten.WindowResizingModeDisabled},
			{`{"border":true,"resizable":true,"disableMinimize":false,"disableMaximize":false,"disableClose":false,"opacity":1}`, true, ebiten.WindowResizingModeEnabled},
		} {
			if _, err := g.windowCall("set", []any{"main", edit.patch}); err != nil {
				t.Fatal(err)
			}
			if err := g.applyWindowChanges(); err != nil {
				t.Fatal(err)
			}
			if ebiten.IsWindowDecorated() != edit.border || ebiten.WindowResizingMode() != edit.resizing {
				t.Fatal("window appearance did not follow the API")
			}
		}
	})
}

func TestNativeSettingsUseDisplayScale(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 2} {
		s := config.Default(nil)
		s.Window.Width, s.Window.Height = 801, 601
		views, restore := nativeWindowViews(&s, scale)
		v := views[0]
		if v.Width != int(math.Round(801*scale)) || v.Height != int(math.Round(601*scale)) || restore != (image.Point{}) {
			t.Fatalf("scale %g: view=%+v restore=%v", scale, v, restore)
		}
		if v.StageWidth != 480 || v.StageHeight != 360 || s.Window.Width != 801 {
			t.Fatal("display scaling changed stage or settings")
		}
		s.Loading.Width, s.Loading.Height = 640, 360
		views, restore = nativeWindowViews(&s, scale)
		if views[0].Width != int(math.Round(640*scale)) || views[0].Height != int(math.Round(360*scale)) || restore != image.Pt(v.Width, v.Height) {
			t.Fatalf("scale %g: loading=%+v restore=%v", scale, views[0], restore)
		}
		s.Platform.Fullscreen = true
		views, restore = nativeWindowViews(&s, scale)
		if !views[0].Fullscreen || views[0].Width != v.Width || views[0].Height != v.Height || restore != (image.Point{}) {
			t.Fatal("fullscreen must keep game geometry and ignore loading size")
		}
	}
}

func TestNativeExplicitViewsKeepPixelSizes(t *testing.T) {
	s := config.Default(nil)
	s.Platform.Views = []window.View{
		{ID: "main", Width: 321, Height: 201, StageWidth: 480, StageHeight: 360},
		{ID: "second", Width: 123, Height: 99, StageWidth: 200, StageHeight: 100},
	}
	views, _ := nativeWindowViews(&s, 2)
	if views[0].Width != 321 || views[1].Width != 123 || views[1].Height != 99 {
		t.Fatal("explicit pixel dimensions were scaled")
	}
	s.Loading.Width = 300
	views, restore := nativeWindowViews(&s, 2)
	if views[0].Width != 600 || views[0].Height != 201 || restore != image.Pt(321, 201) || s.Platform.Views[0].Width != 321 {
		t.Fatal("loading override must scale only the configured axis without mutating saved views")
	}
}

type pollingWindowBackend struct {
	window.Backend
	views []window.View
}

func (b *pollingWindowBackend) Views() []window.View        { return slices.Clone(b.views) }
func (b *pollingWindowBackend) Poll() (window.Input, error) { return window.Input{Focused: true}, nil }

func TestNativeViewPolling(t *testing.T) {
	backend := &pollingWindowBackend{views: []window.View{{ID: "main", Width: 480, Height: 360, StageWidth: 480, StageHeight: 360}}}
	layout, err := window.Pack(backend.views)
	if err != nil {
		t.Fatal(err)
	}
	g := &game{native: &nativeViews{backend: backend, layout: layout, prepared: true, iconsReady: true}}
	allocs := testing.AllocsPerRun(100, func() {
		if err := g.pollNativeViews(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 1 {
		t.Errorf("unchanged views allocated beyond the backend snapshot: %g", allocs)
	}
	backend.views[0].X = 70
	backend.views[0].StageX = 20
	if err := g.pollNativeViews(); err != nil {
		t.Fatal(err)
	}
	if g.native.layout.Views[0].X != 70 || g.native.layout.Views[0].StageX != 20 || !g.native.input.Focused {
		t.Fatal("window or input changes were not reflected")
	}
	backend.views[0].Width = 0
	if err := g.pollNativeViews(); err == nil {
		t.Fatal("invalid resized window accepted")
	}
}

func BenchmarkNativeViewPolling(b *testing.B) {
	backend := &pollingWindowBackend{views: []window.View{{ID: "main", Width: 480, Height: 360, StageWidth: 480, StageHeight: 360}}}
	layout, err := window.Pack(backend.views)
	if err != nil {
		b.Fatal(err)
	}
	g := &game{native: &nativeViews{backend: backend, layout: layout, prepared: true, iconsReady: true}}
	b.ReportAllocs()
	for b.Loop() {
		if err := g.pollNativeViews(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestWindowStateQueries(t *testing.T) {
	views := []window.View{{ID: "main", Width: 480, Height: 360, StageWidth: 480, StageHeight: 360, DisableMinimize: true, DisableMaximize: true}}
	for _, id := range []string{"main", "removed"} {
		for _, state := range []string{"open", "minimized", "maximized", "focused"} {
			_, value, err := editWindows(views, "state", []any{id, state})
			want := id == "main" && state == "open"
			if err != nil || value != want {
				t.Fatalf("%s %s: %v, %v", id, state, value, err)
			}
		}
	}
	for _, args := range [][]any{nil, {"main"}, {"main", "bad"}, {"removed", "bad"}, {"main", "open", true}} {
		if _, _, err := editWindows(views, "state", args); err == nil {
			t.Fatalf("invalid query accepted: %v", args)
		}
	}
}

func TestWindowStateIsReadOnlyAndTracksSnapshot(t *testing.T) {
	layout, err := window.Pack([]window.View{{ID: "main", Width: 480, Height: 360, StageWidth: 480, StageHeight: 360}})
	if err != nil {
		t.Fatal(err)
	}
	backend := &pollingWindowBackend{views: layout.Views}
	g := &game{native: &nativeViews{backend: backend, layout: layout, prepared: true, iconsReady: true}}
	for _, state := range []window.State{{Minimized: true}, {Maximized: true}, {Focused: true}, {}} {
		backend.views = slices.Clone(backend.views)
		backend.views[0].State = state
		if err := g.pollNativeViews(); err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string]bool{"open": true, "minimized": state.Minimized, "maximized": state.Maximized, "focused": state.Focused} {
			got, err := g.nativeWindowCall("state", []any{"main", name})
			if err != nil || got != want || g.native.dirty {
				t.Fatalf("%s: %v %v dirty=%v", name, got, err, g.native.dirty)
			}
		}
	}
	if _, err := g.nativeWindowCall("set", []any{"main", `{"State":{"Minimized":true}}`}); err == nil {
		t.Fatal("runtime state accepted as window configuration")
	}
	g = &game{windowCloseRequested: true}
	got, err := g.windowCall("state", []any{"main", "open"})
	if err != nil || got != false {
		t.Fatalf("closing main: %v %v", got, err)
	}
}

func nativeRenderTestGame(t *testing.T, views []window.View, lib *media.Library) *game {
	t.Helper()
	s := config.Default(nil)
	s.Platform.MultiWindow = true
	s.Player.Controls = false
	options := Options{Settings: &s, FPS: 60}
	p := &project.Project{Targets: []*project.Target{{IsStage: true, Name: "Stage", Visible: true, Size: 100, Direction: 90}}}
	c := newController(p, lib, options)
	if c.initErr != nil {
		t.Fatal(c.initErr)
	}
	layout, err := window.Pack(views)
	if err != nil {
		t.Fatal(err)
	}
	return &game{controller: c, options: options, native: &nativeViews{layout: layout, input: window.Input{Visible: true}}}
}

func TestNativeCameraBackgroundCoversCanvasBackground(t *testing.T) {
	runGPUFrame(t, func() {
		g := nativeRenderTestGame(t, []window.View{{ID: "main", Width: 64, Height: 48, StageWidth: 64, StageHeight: 48}}, &media.Library{})
		defer g.close()
		if err := g.controller.vm.Configure(60, false, false, 64, 48, "player", "#000000"); err != nil {
			t.Fatal(err)
		}
		v := g.controller.vm.View()
		v.X = 16
		v.Canvas = v.Canvas.Defaults()
		v.Canvas.Background = "#0000ff"
		v.Canvas.RenderWidth, v.Canvas.RenderHeight = 32, 24
		g.controller.renderer.SetView(v)
		dst := ebiten.NewImage(64, 48)
		defer dst.Deallocate()
		g.drawNativeViews(dst)
		if g.drawErr != nil {
			t.Fatal(g.drawErr)
		}
		if got := color.RGBAModel.Convert(dst.At(2, 24)).(color.RGBA); got != (color.RGBA{A: 255}) {
			t.Fatalf("camera background exposed canvas background: %v, want opaque black", got)
		}
	})
}

func TestNativeEmptyStageRemainsTransparent(t *testing.T) {
	runGPUFrame(t, func() {
		g := nativeRenderTestGame(t, []window.View{{ID: "main", Width: 480, Height: 360, StageWidth: 480, StageHeight: 360}}, &media.Library{})
		defer g.close()
		dst := ebiten.NewImage(480, 360)
		defer dst.Deallocate()
		g.drawNativeViews(dst)
		if g.drawErr != nil {
			t.Fatal(g.drawErr)
		}
		if _, _, _, a := dst.At(0, 0).RGBA(); a != 0 {
			t.Fatal("empty native stage is opaque")
		}
		if got := g.controller.renderer.Draw(nil).RGBAAt(0, 0); got.A != 0 {
			t.Fatalf("CPU/GPU stage backgrounds disagree: %v", got)
		}
	})
}

func TestNativeResolutionAndPixelFiltering(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 64, 48))
		for y := 0; y < 48; y++ {
			for x := 0; x < 64; x++ {
				c := color.NRGBA{R: 255, A: 255}
				if x%2 == 1 {
					c = color.NRGBA{G: 255, A: 255}
				}
				pixels.SetNRGBA(x, y, c)
			}
		}
		costume := &media.Costume{Image: pixels, Width: 64, Height: 48, CenterX: 32, CenterY: 24, Bitmap: true}
		views := []window.View{
			{ID: "main", Width: 100, Height: 75, StageWidth: 64, StageHeight: 48},
			{ID: "quarter", Width: 96, Height: 72, StageX: 32, StageY: 24, StageWidth: 32, StageHeight: 24},
			{ID: "hidden", Width: 640, Height: 480, StageWidth: 64, StageHeight: 48, Hidden: true},
		}
		g := nativeRenderTestGame(t, views, &media.Library{Costumes: [][]*media.Costume{{costume}}})
		defer g.close()
		if err := g.controller.vm.Configure(60, false, false, 64, 48, "player", "#000000"); err != nil {
			t.Fatal(err)
		}
		v := g.controller.vm.View()
		v.Canvas.Nearest = true
		g.controller.renderer.SetView(v)
		dst := ebiten.NewImage(g.native.layout.Size.X, g.native.layout.Size.Y)
		defer dst.Deallocate()
		g.drawNativeViews(dst)
		if g.drawErr != nil {
			t.Fatal(g.drawErr)
		}
		if got := g.stageCanvas.Bounds().Size(); got != image.Pt(192, 144) {
			t.Errorf("canvas=%v, want 192x144 for the enlarged quarter", got)
		}
		for i, tile := range g.native.layout.Tiles[:2] {
			for x := tile.Min.X + 2; x < tile.Max.X-2; x++ {
				got := color.RGBAModel.Convert(dst.At(x, tile.Min.Y+tile.Dy()/2)).(color.RGBA)
				if got != (color.RGBA{R: 255, A: 255}) && got != (color.RGBA{G: 255, A: 255}) {
					t.Errorf("window %d blurred pixel at %d: %v", i, x, got)
					break
				}
			}
		}
		canvas := g.stageCanvas
		g.drawNativeViews(dst)
		if g.stageCanvas != canvas {
			t.Fatal("unchanged views reallocated the canvas")
		}
		if w, h := g.controller.renderer.Size(); w != 64 || h != 48 {
			t.Fatal("display resolution changed logical coordinates")
		}
	})
}

func TestNativeCanvasSizeIgnoresInactiveWindowsAndBoundsZoom(t *testing.T) {
	views := []window.View{
		{Width: 400, Height: 300, StageWidth: 240, StageHeight: 180},
		{Width: 4000, Height: 3000, StageWidth: 240, StageHeight: 180, Hidden: true},
		{Width: 4000, Height: 3000, StageWidth: 240, StageHeight: 180, State: window.State{Minimized: true}},
	}
	if w, h := nativeCanvasSize(480, 360, views); w != 800 || h != 600 {
		t.Fatalf("inactive windows increased render size: %dx%d", w, h)
	}
	views[0].Width, views[0].Height = 120, 90
	if w, h := nativeCanvasSize(480, 360, views); w != 240 || h != 180 {
		t.Fatalf("shrinking windows kept oversized canvas: %dx%d", w, h)
	}
	views[0].StageWidth, views[0].StageHeight = math.SmallestNonzeroFloat64, math.SmallestNonzeroFloat64
	w, h := nativeCanvasSize(480, 360, views)
	if w < 1 || h < 1 || w > 8192 || h > 8192 || w*h > 16<<20 {
		t.Fatalf("unbounded canvas for extreme zoom: %dx%d", w, h)
	}
}

type centeringWindowBackend struct{ pollingWindowBackend }

func (b *centeringWindowBackend) SetViews(views []window.View) error {
	b.views = slices.Clone(views)
	return nil
}
func (b *centeringWindowBackend) Monitor(string) (window.Monitor, error) {
	return window.Monitor{X: -1920, Width: 1920, Height: 1080, WorkX: -1920, WorkY: 40, WorkWidth: 1920, WorkHeight: 1040}, nil
}
func (b *centeringWindowBackend) Center(id string, all bool) error {
	outer := make([]image.Rectangle, len(b.views))
	for i, v := range b.views {
		outer[i] = image.Rect(v.X, v.Y, v.X+v.Width+16, v.Y+v.Height+39)
	}
	views, err := window.Center(b.views, outer, image.Rect(-1920, 40, 0, 1080), id, all)
	if err != nil {
		return err
	}
	b.views = views
	return nil
}
func TestNativeCenterFlushesPendingWindowsAndUpdatesQueries(t *testing.T) {
	runGPUFrame(t, func() {
		layout, err := window.Pack([]window.View{{ID: "main", X: 60, Y: 60, Width: 400, Height: 300, StageWidth: 240, StageHeight: 180}})
		if err != nil {
			t.Fatal(err)
		}
		backend := &centeringWindowBackend{pollingWindowBackend{views: layout.Views}}
		g := &game{native: &nativeViews{layout: layout, backend: backend}}
		if _, err := g.nativeWindowCall("create", []any{`{"id":"right","x":480,"y":60,"width":400,"height":300,"stageWidth":240,"stageHeight":180}`}); err != nil {
			t.Fatal(err)
		}
		if _, err := g.nativeWindowCall("center", []any{"main", true}); err != nil {
			t.Fatal(err)
		}
		for i, want := range []int{-1378, -958} {
			if got := g.native.layout.Views[i]; got.X != want || got.Y != 390 {
				t.Fatalf("centered view %d: %+v", i, got)
			}
		}
		if g.native.dirty {
			t.Fatal("centered positions will be overwritten by pending edits")
		}
		value, err := g.nativeWindowCall("get", []any{"right"})
		if err != nil {
			t.Fatal(err)
		}
		var v window.View
		if err := json.Unmarshal([]byte(value.(string)), &v); err != nil {
			t.Fatal(err)
		}
		if v.X != -958 {
			t.Fatal("get returned stale position", v)
		}
		value, err = g.nativeWindowCall("monitor", []any{"right"})
		if err != nil {
			t.Fatal(err)
		}
		var monitor window.Monitor
		if err := json.Unmarshal([]byte(value.(string)), &monitor); err != nil {
			t.Fatal(err)
		}
		if monitor.WorkX != -1920 || monitor.WorkY != 40 || monitor.WorkWidth != 1920 {
			t.Fatal(monitor)
		}
		for _, op := range []string{"center", "monitor"} {
			for _, args := range [][]any{nil, {"missing"}, {"main", true, 123}} {
				if _, err := g.nativeWindowCall(op, args); err == nil {
					t.Fatal("bad arguments accepted", op, args)
				}
			}
			if _, err := (&game{}).windowCall(op, []any{"main"}); err == nil {
				t.Fatal("operation accepted without native backend")
			}
		}
	})
}
