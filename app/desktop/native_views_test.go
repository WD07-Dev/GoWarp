package desktop

import (
	"encoding/json"
	"image"
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/app/config"
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
