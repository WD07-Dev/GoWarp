package desktop

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font/basicfont"

	"gowarp/app/config"
	"gowarp/core/vm"
)

func TestUnfocusedPlayerKeepsRunningWithoutComposingScreen(t *testing.T) {
	runGPUFrame(t, func() {
		c := cadenceController(t, 60, `{}`)
		settings := config.Default(nil)
		settings.Player.Controls = false
		c.options.Settings = &settings
		g := &game{ctx: context.Background(), controller: c}
		defer g.close()
		screen := ebiten.NewImage(480, 360)
		defer screen.Deallocate()
		frozen := color.RGBA{R: 23, G: 41, B: 57, A: 255}
		screen.Fill(frozen)
		graphics, err := g.gpu.enablePen(c.renderer)
		if err != nil {
			t.Fatal(err)
		}
		pen := deferredPen{graphics}
		c.vm.SetGraphics(pen)
		for i := 0; i < 60; i++ {
			pen.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{-20, 0, 20, 0}, Size: 8, Color: color.NRGBA{R: 255, A: 255}})
			if err := g.advanceController(Input{}); err != nil {
				t.Fatal(err)
			}
			g.draw(screen, false)
			if len(graphics.commands) != 0 || len(graphics.draws) != 0 || len(graphics.lineVertices) != 0 || len(graphics.stampVertices) != 0 {
				t.Fatalf("frame %d left pen work waiting for focus", i)
			}
		}
		if graphics.pixels != nil {
			t.Fatal("background drawing read the pen image back to the CPU")
		}
		if c.vm.Frames() != 60 {
			t.Fatalf("background simulation completed %d frames, want 60", c.vm.Frames())
		}
		if g.stageCanvas != nil {
			t.Fatal("unfocused player still composed the stage")
		}
		if got := color.RGBAModel.Convert(screen.At(240, 180)); got != frozen {
			t.Fatalf("background replaced the last screen: %v", got)
		}
		g.draw(screen, true)
		if g.drawErr != nil {
			t.Fatal(g.drawErr)
		}
		if g.backgroundFrame != nil {
			t.Fatal("restored window retained its background frame")
		}
		if got := color.RGBAModel.Convert(screen.At(240, 180)); got != (color.RGBA{R: 255, A: 255}) {
			t.Fatalf("pen drawing missing after focus returned: %v", got)
		}
	})
}

func TestUnfocusedPlayerStillCapturesFinalFrame(t *testing.T) {
	runGPUFrame(t, func() {
		c := cadenceController(t, 60, `{}`)
		settings := config.Default(nil)
		settings.Player.Controls = false
		c.options.Settings = &settings
		c.options.Screenshot = filepath.Join(t.TempDir(), "frame.png")
		g := &game{controller: c, limitReached: true}
		defer g.close()
		screen := ebiten.NewImage(480, 360)
		defer screen.Deallocate()
		g.draw(screen, false)
		if !g.captured || g.drawErr != nil {
			t.Fatalf("background capture did not finish: captured=%v error=%v", g.captured, g.drawErr)
		}
		if _, err := os.Stat(c.options.Screenshot); err != nil {
			t.Fatal(err)
		}
	})
}

func TestUnfocusedPlayerResubmitsLastFrame(t *testing.T) {
	runGPUFrame(t, func() {
		for _, alpha := range []uint8{255, 128} {
			c := cadenceController(t, 60, `{}`)
			settings := config.Default(nil)
			c.options.Settings = &settings
			g := &game{controller: c}
			defer g.close()
			screen := ebiten.NewImage(32, 24)
			defer screen.Deallocate()
			frozen := color.RGBA{R: 23, G: 41, B: 57, A: alpha}
			screen.Fill(frozen)
			g.draw(screen, false)
			cached := g.backgroundFrame
			for range 8 {
				// A new back buffer must receive the cached frame, without running scene composition.
				screen.Clear()
				g.draw(screen, false)
				if g.backgroundFrame != cached {
					t.Fatal("background presentation allocated another frame")
				}
				if got := color.RGBAModel.Convert(screen.At(10, 10)); got != frozen {
					t.Fatalf("background frame not resubmitted: got %v, want %v", got, frozen)
				}
			}
			if g.stageCanvas != nil {
				t.Fatal("background presentation recomposed the scene")
			}
			g.windowMinimized = true
			screen.Clear()
			g.draw(screen, false)
			if got := color.RGBAModel.Convert(screen.At(10, 10)); got != (color.RGBA{}) {
				t.Fatal("minimized window submitted a frame")
			}
			g.windowMinimized = false
			g.draw(screen, false)
			if got := color.RGBAModel.Convert(screen.At(10, 10)); got != frozen {
				t.Fatal("minimizing lost the cached frame")
			}
			resized := ebiten.NewImage(64, 48)
			defer resized.Deallocate()
			g.draw(resized, false)
			if got := color.RGBAModel.Convert(resized.At(60, 40)); got != frozen || g.backgroundFrame != cached {
				t.Fatal("background resize lost or reallocated the cached frame")
			}
		}
	})
}

func TestFocusedCLIReplacesTransparentFrame(t *testing.T) {
	runGPUFrame(t, func() {
		c := cadenceController(t, 60, `{}`)
		view := c.renderer.DisplayView()
		view.Color = "2164195328" // Half-transparent red in numeric ARGB form.
		c.renderer.SetView(view)
		g := &game{controller: c, face: basicfont.Face7x13}
		defer g.close()
		screen := ebiten.NewImage(480, 360+ToolbarHeight+AnswerHeight)
		defer screen.Deallocate()
		screen.Fill(color.RGBA{B: 255, A: 255})
		for i := 0; i < 2; i++ {
			g.draw(screen, true)
			if got := color.RGBAModel.Convert(screen.At(240, ToolbarHeight+180)); got != (color.RGBA{R: 128, A: 128}) {
				t.Fatalf("frame %d blended over the previous screen: %v", i, got)
			}
		}
	})
}

func TestFocusReturnKeepsRealtimeTicksAndInput(t *testing.T) {
	runGPUFrame(t, func() {
		c := cadenceController(t, 60, `{
		 "key":{"opcode":"event_whenkeypressed","topLevel":true,"next":"add","fields":{"KEY_OPTION":["space",null]}},
		 "add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
		}`)
		settings := config.Default(nil)
		settings.Player.Controls = false
		c.options.Settings = &settings
		now := time.Unix(100, 0)
		c.cadenceNow = func() time.Time { return now }
		g := &game{ctx: context.Background(), controller: c}
		defer g.close()
		screen := ebiten.NewImage(480, 360)
		defer screen.Deallocate()
		for i := range 60 {
			focused := i%6 != 0
			g.updateFocus(focused)
			in := Input{}
			if i%6 == 1 {
				in.Pressed = []string{"space"}
			}
			if err := g.advanceController(in); err != nil {
				t.Fatal(err)
			}
			if c.vm.Frames() != i+1 {
				t.Fatalf("frame %d: focus transition lost a tick: frames=%d credit=%g", i, c.vm.Frames(), c.credit)
			}
			if got := c.vm.Snapshot()[0].Variables["score"].Value; got != float64((i+5)/6) {
				t.Fatalf("frame %d: restored input delayed: score=%v", i, got)
			}
			g.draw(screen, focused)
			if g.drawErr != nil {
				t.Fatal(g.drawErr)
			}
			now = now.Add(c.vm.TickInterval())
		}
	})
}

func TestFocusReturnRenewsSpentBackgroundBudget(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	settings := config.Default(nil)
	c.options.Settings = &settings
	c.cadenceNow = func() time.Time { return time.Unix(100, 0) }
	g := &game{
		ctx: context.Background(), controller: c, unfocused: true,
		updateTime: time.Second, frameTicks: 2,
		updateBudget: time.Second / 30, updateBudgetUntil: time.Now().Add(time.Second),
	}
	g.updateFocus(true)
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 1 {
		t.Fatal("restored window reused an exhausted background budget")
	}
}

func TestFocusPresentationDoesNotPauseBackgroundOrReplay(t *testing.T) {
	for _, tc := range []struct {
		name   string
		focus  []bool
		replay bool
	}{
		{"startup", []bool{true}, false},
		{"background", []bool{false}, false},
		{"focus-lost-again", []bool{false, true, false}, false},
		{"replay", []bool{false, true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cadenceController(t, 60, `{}`)
			settings := config.Default(nil)
			c.options.Settings = &settings
			c.options.Replay = tc.replay
			g := &game{ctx: context.Background(), controller: c}
			for _, focused := range tc.focus {
				g.updateFocus(focused)
			}
			if err := g.advanceController(Input{}); err != nil {
				t.Fatal(err)
			}
			if c.vm.Frames() != 1 {
				t.Fatal("simulation was held waiting for an unnecessary presentation")
			}
		})
	}
}

func TestFocusVisibilityProbeDoesNotSuspendSimulation(t *testing.T) {
	runGPUFrame(t, func() {
		c := cadenceController(t, 60, `{
		 "key":{"opcode":"event_whenkeypressed","topLevel":true,"next":"add","fields":{"KEY_OPTION":["space",null]}},
		 "add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
		}`)
		settings := config.Default(nil)
		settings.Player.Controls = false
		c.options.Settings = &settings
		g := &game{ctx: context.Background(), controller: c, focusDraws: 2}
		defer g.close()
		screen := ebiten.NewImage(480, 360)
		defer screen.Deallocate()
		/*
			An occluded DirectX swap chain uses the first Present only to test
			visibility. Exercise that two-frame contract on every test platform.
		*/
		g.draw(screen, true)
		if err := g.advanceController(Input{Pressed: []string{"space"}}); err != nil {
			t.Fatal(err)
		}
		if c.vm.Frames() != 1 || c.vm.Snapshot()[0].Variables["score"].Value != float64(1) {
			t.Fatal("visibility probe delayed simulation or input")
		}
		g.draw(screen, true)
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
		if c.vm.Frames() != 2 || c.vm.Snapshot()[0].Variables["score"].Value != float64(1) {
			t.Fatal("presentation delayed simulation or repeated input")
		}
	})
}
