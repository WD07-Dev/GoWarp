package desktop

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
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
		c.vm.SetGraphics(graphics)
		graphics.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{-20, 0, 20, 0}, Size: 8, Color: color.NRGBA{R: 255, A: 255}})
		for i := 0; i < 60; i++ {
			if err := g.advanceController(Input{}); err != nil {
				t.Fatal(err)
			}
			g.draw(screen, false)
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

func TestFocusReturnPresentsBeforeCatchUp(t *testing.T) {
	runGPUFrame(t, func() {
		c := cadenceController(t, 60, `{
		 "key":{"opcode":"event_whenkeypressed","topLevel":true,"next":"add","fields":{"KEY_OPTION":["space",null]}},
		 "add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
		}`)
		settings := config.Default(nil)
		settings.Player.Controls = false
		c.options.Settings = &settings
		g := &game{ctx: context.Background(), controller: c}
		defer g.close()
		screen := ebiten.NewImage(480, 360)
		defer screen.Deallocate()
		g.updateFocus(false)
		g.updateFocus(true)
		// On focus return, present before overdue updates and retain the newly pressed key.
		for i := 0; i < 3; i++ {
			g.updateFocus(true)
			g.updateBudgetUntil = time.Now().Add(-time.Second)
			in := Input{}
			if i == 0 {
				in.Pressed = []string{"space"}
			}
			if err := g.advanceController(in); err != nil {
				t.Fatal(err)
			}
		}
		if c.vm.Frames() != 0 || c.credit != 3 {
			t.Fatalf("catch-up ran before the first restored frame: frames=%d credit=%g", c.vm.Frames(), c.credit)
		}
		g.draw(screen, true)
		if g.stageCanvas == nil || g.drawErr != nil {
			t.Fatalf("focus return did not render: %v", g.drawErr)
		}
		if runtime.GOOS == "windows" {
			g.draw(screen, true) // The first present can be a visibility probe.
		}
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
		if c.vm.Frames() == 0 || c.vm.Snapshot()[0].Variables["score"].Value != float64(1) {
			t.Fatalf("simulation or queued key did not resume: frames=%d score=%v", c.vm.Frames(), c.vm.Snapshot()[0].Variables["score"].Value)
		}
	})
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

func TestFocusReturnWaitsForVisibilityProbeAndPresentation(t *testing.T) {
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
		if c.vm.Frames() != 0 {
			t.Fatal("simulation resumed after the visibility probe, before actual presentation")
		}
		g.draw(screen, true)
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
		if c.vm.Frames() == 0 || c.vm.Snapshot()[0].Variables["score"].Value != float64(1) {
			t.Fatal("simulation or queued input did not resume after presentation")
		}
	})
}
