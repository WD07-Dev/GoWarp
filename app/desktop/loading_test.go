package desktop

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"gowarp/app/assets"
	"gowarp/app/config"
	"gowarp/app/ui"
	"gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestLoadingGIFUpdatesExistingTexture(t *testing.T) {
	palette := color.Palette{color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}}
	first := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	second := image.NewPaletted(first.Bounds(), palette)
	for i := range second.Pix {
		second.Pix[i] = 1
	}
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{10, 10}, LoopCount: 0}); err != nil {
		t.Fatal(err)
	}
	picture, err := assets.DecodeLoadingImage(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	runGPUFrame(t, func() {
		var texture *ebiten.Image
		start := time.Unix(1, 0)
		if err := updateLoadingTexture(picture, &texture, start); err != nil {
			t.Fatal(err)
		}
		defer texture.Deallocate()
		original := texture
		if err := updateLoadingTexture(picture, &texture, start.Add(100*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		if texture != original {
			t.Fatal("GIF frame replaced the GPU texture")
		}
		pixels := make([]byte, 16)
		texture.ReadPixels(pixels)
		if !bytes.Equal(pixels[:4], []byte{0, 255, 0, 255}) {
			t.Fatalf("second GIF frame = %v", pixels[:4])
		}
	})
}

func TestLoadingPanelIsTransparent(t *testing.T) {
	runGPUFrame(t, func() {
		g := &game{face: ui.Font(14)}
		defer g.face.Close()
		defer g.releaseLoadingImages()
		screen := ebiten.NewImage(480, 360)
		defer screen.Deallocate()
		g.drawLoading(screen)
		defer g.canvas.Deallocate()
		pixels := make([]byte, g.canvas.Bounds().Dx()*g.canvas.Bounds().Dy()*4)
		g.canvas.ReadPixels(pixels)
		if pixels[3] != 0 {
			t.Fatal("loading panel still has an opaque background")
		}
	})
}

func TestLoadingProgressHiddenKeepsCustomTextAndErrors(t *testing.T) {
	runGPUFrame(t, func() {
		s := config.Default(nil)
		s.Loading.ShowProgress = false
		s.Loading.Text = "Please wait"
		g := &game{options: Options{Settings: &s}}
		defer g.close()
		screen := ebiten.NewImage(480, 360)
		defer screen.Deallocate()
		g.drawLoading(screen)
		bounds := g.canvas.Bounds()
		pixels := make([]byte, bounds.Dx()*bounds.Dy()*4)
		g.canvas.ReadPixels(pixels)
		messageVisible := false
		for y := range bounds.Dy() {
			for x := range bounds.Dx() {
				alpha := pixels[(y*bounds.Dx()+x)*4+3]
				if y >= bounds.Dy()/2 && alpha != 0 {
					t.Fatal("hidden progress still draws its bar or status text")
				}
				messageVisible = messageVisible || alpha != 0
			}
		}
		if !messageVisible {
			t.Fatal("custom loading message was hidden")
		}
		s.Loading.Text = ""
		g.drawLoading(screen)
		g.canvas.ReadPixels(pixels)
		if !bytes.Equal(pixels, make([]byte, len(pixels))) {
			t.Fatal("empty message and hidden progress still draw an overlay")
		}
		g.failure = errors.New("test error")
		g.drawLoading(screen)
		pixels = make([]byte, g.canvas.Bounds().Dx()*g.canvas.Bounds().Dy()*4)
		g.canvas.ReadPixels(pixels)
		if bytes.Equal(pixels, make([]byte, len(pixels))) {
			t.Fatal("hiding loading progress also hid the runtime error")
		}
	})
}

func TestLoadingWaitsForFirstDrawAndCloseJoinsPrepare(t *testing.T) {
	entered := make(chan struct{})
	finished := make(chan struct{})
	g := &game{
		ctx: context.Background(),
		options: Options{Prepare: func(ctx context.Context) (*project.Project, error) {
			close(entered)
			<-ctx.Done()
			defer close(finished)
			return nil, ctx.Err()
		}},
	}
	if err := g.updateLoading(); err != nil {
		t.Fatal(err)
	}
	if g.loader != nil {
		t.Fatal("project started loading before the first window draw")
	}
	g.drawn = true
	if err := g.updateLoading(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("project preparation did not start after drawing")
	}
	g.close()
	select {
	case <-finished:
	default:
		t.Fatal("window closed before its project loader finished")
	}
	if g.controller != nil || g.updates != 0 {
		t.Fatal("loading advanced the runtime")
	}
}

func TestLoadingReturnsPrepareFailure(t *testing.T) {
	want := errors.New("invalid project")
	l := startProjectLoader(context.Background(), "", nil, Options{
		Prepare: func(context.Context) (*project.Project, error) {
			return nil, want
		},
	}, nil)
	defer l.cancel()
	select {
	case <-l.done:
	case <-time.After(5 * time.Second):
		t.Fatal("loader did not return its preparation error")
	}
	result := <-l.result
	if !errors.Is(result.err, want) || result.controller != nil {
		t.Fatalf("loading result = %+v", result)
	}
}

func TestLoadingPreparesRuntimeWithoutExecutingProject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(path, []byte(`{"targets":[{"isStage":true,"name":"Stage","variables":{},"lists":{},"blocks":{}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := config.Default(nil)
	s.Player.AutoStart = false
	configured := false
	l := startProjectLoader(context.Background(), path, nil, Options{
		Settings: &s, FPS: 30,
		Configure: func(r *vm.Runtime) error {
			configured = true
			if r.Frames() != 0 {
				t.Error("runtime advanced before Configure")
			}
			return nil
		},
	}, []vm.InputEvent{{Kind: "key", Key: "space", Down: true}})
	defer l.cancel()
	select {
	case <-l.done:
	case <-time.After(5 * time.Second):
		t.Fatal("loader did not finish")
	}
	result := <-l.result
	if result.err != nil {
		t.Fatal(result.err)
	}
	defer result.controller.assets.Close()
	if !configured || result.controller.vm.Frames() != 0 || !result.controller.vm.Stopped() {
		t.Fatal("loaded runtime did not preserve AutoStart=false without advancing frames")
	}
}

func TestLoadingSelectsClockForPlaybackAndReplay(t *testing.T) {
	for _, mode := range []string{"player", "replay", "headless", "capture"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "project.json")
			if err := os.WriteFile(path, []byte(`{"targets":[{"isStage":true,"name":"Stage","blocks":{},"costumes":[{"md5ext":"fine.svg","dataFormat":"svg"}]}]}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(filepath.Dir(path), "fine.svg"), []byte(`<svg width="8" height="4"><rect width="8" height="4" fill="red"/></svg>`), 0600); err != nil {
				t.Fatal(err)
			}
			settings := config.Default(nil)
			options := Options{Settings: &settings, FPS: 30, Replay: mode == "replay"}
			if mode == "headless" {
				options.Settings = nil
			}
			if mode == "capture" {
				options.Screenshot = filepath.Join(filepath.Dir(path), "capture.png")
			}
			l := startProjectLoader(context.Background(), path, nil, options, nil)
			defer l.cancel()
			select {
			case <-l.done:
			case <-time.After(5 * time.Second):
				t.Fatal("loader did not finish")
			}
			result := <-l.result
			if result.err != nil {
				t.Fatal(result.err)
			}
			defer result.controller.assets.Close()
			defer result.controller.vm.CloseExtensions()
			lib := result.controller.assets
			_, density := lib.DisplayCostumeImageKey(lib.Costumes[0][0], 4, nil)
			if mode == "player" && density != 1 {
				t.Fatal("interactive player waited for display preparation")
			}
			if mode != "player" && density != 4 {
				t.Fatal("replay or capture can receive timing-dependent fallback pixels")
			}
			r := result.controller.vm
			if err := r.Tick(context.Background(), 30, false); err != nil {
				t.Fatal(err)
			}
			want := 1.0 / 30
			if mode == "player" || mode == "capture" {
				want = 0 // The first real-time tick anchors time after loading.
			}
			if r.Time() != want {
				t.Fatalf("%s first tick time=%g, want %g", mode, r.Time(), want)
			}
		})
	}
}

func TestLoadingKeepsConfiguredWindowDimensions(t *testing.T) {
	s := config.Default(nil)
	s.Width, s.Height = 480, 360
	s.Window.Width, s.Window.Height = 640, 360
	s.Player.Controls = true
	g := &game{options: Options{Settings: &s}}
	if w, h := playerWindowSize(&s); w != 640 || h != 408 {
		t.Fatalf("loading window = %dx%d", w, h)
	}
	if w, h := g.Layout(1280, 816); w != 1280 || h != 816 {
		t.Fatalf("loading layout = %dx%d", w, h)
	}
}

func TestLoadingWarmsGraphicsBeforeFirstTick(t *testing.T) {
	runGPUFrame(t, func() {
		for _, autoStart := range []bool{true, false} {
			path := filepath.Join(t.TempDir(), "project.json")
			if err := os.WriteFile(path, []byte(`{"targets":[{"isStage":true,"name":"Stage","blocks":{}}]}`), 0600); err != nil {
				t.Fatal(err)
			}
			s := config.Default(nil)
			s.Player.AutoStart = autoStart
			warmed := false
			g := &game{ctx: context.Background(), drawn: true}
			g.options = Options{Settings: &s, FPS: 60, Diagnostic: func(message string) {
				if strings.HasPrefix(message, "graphics warmup ") {
					warmed = true
					if !strings.Contains(message, "project_shaders=1") {
						t.Errorf("project shader was not prepared during loading: %s", message)
					}
					if g.controller.vm.Frames() != 0 {
						t.Error("game advanced before graphics preparation finished")
					}
				}
			}}
			g.options.Extensions = extension.Bundle{func() extension.Extension {
				return extension.Extension{ID: "example", Events: map[string]extension.Handler{
					"load": func(c *extension.Context) (extension.Result, error) {
						_, err := c.Call("graphics.shader.define", "effect",
							"package main\nfunc Fragment(p vec4, uv vec2, color vec4) vec4 {return vec4(1,0,0,1)}", map[string]any{})
						return extension.Result{}, err
					},
				}}
			}}
			g.loader = startProjectLoader(g.ctx, path, nil, g.options, nil)
			<-g.loader.done
			if err := g.updateLoading(); err != nil {
				g.close()
				t.Fatal(err)
			}
			if !warmed {
				g.close()
				t.Fatal("loading completed without graphics warmup")
			}
			if g.controller.vm.Frames() != 0 || g.waitingStart != !autoStart ||
				g.controller.vm.Stopped() != !autoStart {
				t.Error("graphics preparation changed project start state")
			}
			if p := g.gpu.nativePen; p.image != nil || len(p.pending) != 0 || len(p.commands) != 0 {
				t.Error("graphics preparation drew into the game's pen layer")
			}
			g.close()
		}
	})
}

func TestLoadingFailureDisposesExtensions(t *testing.T) {
	for _, failure := range []string{"load cancel", "load error", "flag error", "events error"} {
		t.Run(failure, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "project.json")
			if err := os.WriteFile(path, []byte(`{"targets":[{"isStage":true,"name":"Stage","blocks":{}}]}`), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := errors.New("extension setup failed")
			disposed := 0
			var runtime *vm.Runtime
			bundle := extension.Bundle{func() extension.Extension {
				return extension.Extension{ID: "cleanup", Events: map[string]extension.Handler{
					"load": func(*extension.Context) (extension.Result, error) {
						if failure == "load cancel" {
							cancel()
						}
						if failure == "load error" {
							return extension.Result{}, want
						}
						return extension.Result{}, nil
					},
					"flag": func(*extension.Context) (extension.Result, error) {
						if failure == "flag error" {
							return extension.Result{}, want
						}
						return extension.Result{}, nil
					},
					"dispose": func(*extension.Context) (extension.Result, error) {
						disposed++
						return extension.Result{}, nil
					},
				}}
			}}
			var events []vm.InputEvent
			if failure == "events error" {
				events = []vm.InputEvent{{Kind: "invalid"}}
			}
			loader := startProjectLoader(ctx, path, nil, Options{FPS: 30, Extensions: bundle, Configure: func(r *vm.Runtime) error {
				runtime = r
				return nil
			}}, events)
			defer loader.cancel()
			select {
			case <-loader.done:
			case <-time.After(5 * time.Second):
				t.Fatal("loader did not finish")
			}
			result := <-loader.result
			if result.controller != nil || result.err == nil {
				t.Fatalf("loading unexpectedly succeeded: %+v", result)
			}
			if failure == "load cancel" && !errors.Is(result.err, context.Canceled) {
				t.Fatalf("cancellation error=%v", result.err)
			}
			if (failure == "load error" || failure == "flag error") && !errors.Is(result.err, want) {
				t.Fatalf("extension error=%v", result.err)
			}
			if runtime == nil {
				t.Fatal("failure happened before runtime creation")
			}
			if disposed != 1 {
				t.Errorf("failed loading disposed extension %d times, want 1", disposed)
			}
			if err := runtime.CloseExtensions(); err != nil {
				t.Fatal(err)
			}
			if disposed != 1 {
				t.Errorf("repeated close disposed extension %d times, want 1", disposed)
			}
		})
	}
}

func TestLoadingAbandonedResultDisposesExtensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(path, []byte(`{"targets":[{"isStage":true,"name":"Stage","blocks":{}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	disposed := 0
	bundle := extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "cleanup", Events: map[string]extension.Handler{
			"dispose": func(*extension.Context) (extension.Result, error) {
				disposed++
				return extension.Result{}, nil
			},
		}}
	}}
	loader := startProjectLoader(context.Background(), path, nil, Options{FPS: 30, Extensions: bundle}, nil)
	select {
	case <-loader.done:
	case <-time.After(5 * time.Second):
		loader.cancel()
		t.Fatal("loader did not finish")
	}
	result := <-loader.result
	if result.err != nil || result.controller == nil {
		loader.cancel()
		t.Fatalf("loading failed: %+v", result)
	}
	t.Cleanup(func() {
		result.controller.vm.CloseExtensions()
		result.controller.assets.Close()
	})
	loader.result <- result
	g := &game{loader: loader}
	g.close()
	if g.controller != nil {
		t.Fatal("closed window adopted prepared controller")
	}
	if disposed != 1 {
		t.Errorf("abandoned result disposed extension %d times, want 1", disposed)
	}
	g.close()
	if disposed != 1 {
		t.Errorf("repeated close disposed extension %d times, want 1", disposed)
	}
}

func TestLoadedPlayerCoalescesMissedHostTicks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.json")
	data := `{"targets":[{"isStage":true,"name":"Stage","variables":{"score":["score",0]},"blocks":{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait"},
 "wait":{"opcode":"control_repeat","next":"done","inputs":{"TIMES":[1,[4,"12"]]}},
 "done":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
 "drawing":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
 "loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"draw"]}},
 "draw":{"opcode":"pen_clear"}
 }}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	settings := config.Default(nil)
	settings.FPS = 60
	settings.Player.AutoStart = true
	l := startProjectLoader(context.Background(), path, nil, Options{Settings: &settings, FPS: 60}, nil)
	defer l.cancel()
	select {
	case <-l.done:
	case <-time.After(5 * time.Second):
		t.Fatal("loader did not finish")
	}
	result := <-l.result
	if result.err != nil {
		t.Fatal(result.err)
	}
	c := result.controller
	defer c.assets.Close()
	defer c.vm.CloseExtensions()
	if c.cadenceNow == nil {
		t.Fatal("loaded player did not enable real-time cadence")
	}
	now := c.cadenceNow()
	c.cadenceNow = func() time.Time { return now }
	c.tickLimit = 2
	if err := c.advance(context.Background(), Input{}); err != nil {
		t.Fatal(err)
	}
	// A long draw must not turn later callbacks into a replay of the whole pause.
	now = now.Add(250 * time.Millisecond)
	for range 7 {
		if err := c.advance(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
		if c.tickCount > 2 {
			t.Fatal("recovery monopolized a host callback")
		}
	}
	if c.vm.Frames() != 3 || c.vm.Snapshot()[0].Variables["score"].Value != float64(0) {
		t.Fatalf("recovery replayed the pause: frames=%d score=%#v", c.vm.Frames(), c.vm.Snapshot()[0].Variables["score"].Value)
	}
	for range 10 {
		now = now.Add(16 * time.Millisecond)
		if err := c.advance(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Frames() != 13 || c.vm.Snapshot()[0].Variables["score"].Value != "1" {
		t.Fatalf("coalescing skipped repeat instructions: frames=%d score=%#v", c.vm.Frames(), c.vm.Snapshot()[0].Variables["score"].Value)
	}
}

func TestLoadingAppliesAntialiasBeforePreparingSVG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project.json")
	projectData := []byte(`{"targets":[{"isStage":true,"blocks":{},"costumes":[{"md5ext":"edge.svg","dataFormat":"svg"}]}]}`)
	if err := os.WriteFile(path, projectData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "edge.svg"), []byte(`<svg width="8" height="8"><path d="M .2 .4 L 7.1 .6 L 2.4 7.3 Z" fill="red" opacity=".5"/></svg>`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false} {
		s := config.Default(nil)
		s.Canvas.Antialias = enabled
		loader := startProjectLoader(context.Background(), path, nil, Options{FPS: 30, Settings: &s}, nil)
		select {
		case <-loader.done:
		case <-time.After(5 * time.Second):
			loader.cancel()
			t.Fatal("loading did not finish")
		}
		result := <-loader.result
		if result.err != nil {
			loader.cancel()
			t.Fatal(result.err)
		}
		lib := result.controller.assets
		pixels := lib.CostumeImage(lib.Costumes[0][0])
		partial, visible := 0, 0
		for i := 3; i < len(pixels.Pix); i += 4 {
			if pixels.Pix[i] > 0 {
				visible++
			}
			if pixels.Pix[i] > 0 && pixels.Pix[i] != 127 {
				partial++
			}
		}
		lib.Close()
		loader.cancel()
		if visible == 0 || (enabled && partial == 0) || (!enabled && partial != 0) {
			t.Fatalf("SVG antialias=%t: visible=%d partial coverage=%d", enabled, visible, partial)
		}
	}
}
