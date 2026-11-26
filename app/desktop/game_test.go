package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/app/config"
	"gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
	windowcontrols "gowarp/test/extensions/cubesterWindowControls"
	f11detector "gowarp/test/extensions/f11detector"
)

func TestHostFrameReportsCombinedFullscreen(t *testing.T) {
	runGPUFrame(t, func() {
		for _, host := range []bool{false, true} {
			for _, project := range []bool{false, true} {
				c := cadenceController(t, 60, `{
				"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},
				"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"fullscreen"]}},
				"fullscreen":{"opcode":"f11detector_isBrowserFullscreen"}
			}`)
				if err := c.vm.SetExtensions(extension.Bundle{f11detector.New}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { c.vm.CloseExtensions() })
				g := &game{controller: c, hostFullscreen: host, projectFullscreen: project}
				ebiten.SetFullscreen(host || project)
				g.sampleHostFrame()
				c.vm.GreenFlag()
				if err := c.vm.Tick(context.Background(), 60, false); err != nil {
					t.Fatal(err)
				}
				if got := c.vm.Snapshot()[0].Variables["score"].Value; got != (host || project) {
					t.Fatalf("host=%t project=%t: fullscreen reporter = %v", host, project, got)
				}
				ebiten.SetFullscreen(false)
				g.sampleHostFrame()
				c.vm.GreenFlag()
				if err := c.vm.Tick(context.Background(), 60, false); err != nil {
					t.Fatal(err)
				}
				if got := c.vm.Snapshot()[0].Variables["score"].Value; got != (host || project) {
					t.Fatalf("window state was queried again within the same frame: %v", got)
				}
				if host {
					g.LayoutF(480, 360)
				} else {
					g.Layout(480, 360)
				}
				g.sampleHostFrame()
				c.vm.GreenFlag()
				if err := c.vm.Tick(context.Background(), 60, false); err != nil {
					t.Fatal(err)
				}
				if got := c.vm.Snapshot()[0].Variables["score"].Value; got != false {
					t.Fatalf("native fullscreen exit was missed: %v", got)
				}
			}
		}
	})
}

func TestPlayerEscapeKeepsWindowOpen(t *testing.T) {
	runGPUFrame(t, func() {
		settings := config.Default(nil)
		player := &game{options: Options{Settings: &settings}}
		if err := player.handleEscape(); err != nil {
			t.Fatalf("Escape closed the packaged player: %v", err)
		}
		ebiten.SetFullscreen(true)
		if err := player.handleEscape(); err != nil || ebiten.IsFullscreen() {
			t.Fatalf("Escape did not leave fullscreen without closing: %v", err)
		}
		player.failure = errors.New("loading failed")
		if err := player.handleEscape(); err != ebiten.Termination {
			t.Fatalf("Escape cannot dismiss an error window: %v", err)
		}
		cli := &game{}
		if err := cli.handleEscape(); err != ebiten.Termination {
			t.Fatalf("CLI Escape shortcut changed: %v", err)
		}
	})
}

func TestProjectFullscreenPreservesF11(t *testing.T) {
	runGPUFrame(t, func() {
		var p project.Project
		if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"command"},"command":{"opcode":"cubesterWindowControls_enterFullscreen"}}}]}`), &p); err != nil {
			t.Fatal(err)
		}
		r := vm.New(&p)
		if err := r.SetExtensions(extension.Bundle{windowcontrols.New}); err != nil {
			t.Fatal(err)
		}
		defer r.CloseExtensions()
		g := &game{controller: &controller{vm: r}}
		if g.sampleFullscreen() {
			t.Fatal("player started in fullscreen")
		}
		command := func(enter bool) {
			t.Helper()
			op := "cubesterWindowControls_exitFullscreen"
			if enter {
				op = "cubesterWindowControls_enterFullscreen"
			}
			if _, err := g.windowCall("set", []any{"main", `{"title":"changing title"}`}); err != nil {
				t.Fatal(err)
			}
			p.Targets[0].Blocks["command"].Opcode = op
			r.GreenFlag()
			if err := r.Tick(context.Background(), 30, false); err != nil {
				t.Fatal(err)
			}
			if err := g.applyHostCommands(); err != nil {
				t.Fatal(err)
			}
			if err := g.applyWindowChanges(); err != nil {
				t.Fatal(err)
			}
		}
		for _, browser := range []bool{false, true} {
			if browser {
				g.toggleFullscreen()
			}
			command(true)
			if !g.sampleFullscreen() {
				t.Fatal("project could not enter fullscreen")
			}
			command(false)
			if g.sampleFullscreen() != browser {
				t.Fatalf("project exit changed F11 fullscreen: browser=%v", browser)
			}
		}
		g.toggleFullscreen()
		if g.sampleFullscreen() {
			t.Fatal("second F11 did not leave fullscreen")
		}
		g.toggleFullscreen()
		command(true)
		if err := g.handleEscape(); err != nil || g.sampleFullscreen() {
			t.Fatalf("Escape did not exit both fullscreen modes: %v", err)
		}
		command(false)
		if g.sampleFullscreen() {
			t.Fatal("Escape retained F11 fullscreen")
		}
	})
}

func TestCloseOnStopOnlyClosesForStopAllBlock(t *testing.T) {
	for _, tc := range []struct {
		name, blocks                          string
		enabled, hostStop, restart, wantClose bool
	}{
		{name: "enabled", blocks: `"stop":{"opcode":"control_stop","fields":{"STOP_OPTION":["all",null]}}`, enabled: true, wantClose: true},
		{name: "disabled", blocks: `"stop":{"opcode":"control_stop","fields":{"STOP_OPTION":["all",null]}}`},
		{name: "this script", blocks: `"stop":{"opcode":"control_stop","fields":{"STOP_OPTION":["this script",null]}}`, enabled: true},
		{name: "scripts finished", blocks: `"stop":{"opcode":"looks_show"}`, enabled: true},
		{name: "player stop button", blocks: `"stop":{"opcode":"looks_show"}`, enabled: true, hostStop: true},
		{name: "green flag restart", blocks: `"stop":{"opcode":"looks_show"}`, enabled: true, restart: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p project.Project
			if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"stop"},`+tc.blocks+`}}]}`), &p); err != nil {
				t.Fatal(err)
			}
			s := config.Default(nil)
			s.Player.CloseOnStop = tc.enabled
			r := vm.New(&p)
			g := &game{controller: &controller{vm: r}, options: Options{Settings: &s}}
			if err := r.Tick(context.Background(), 30, false); err != nil {
				t.Fatal(err)
			}
			if tc.hostStop {
				r.Stop()
			}
			if tc.restart {
				r.GreenFlag()
			}
			err := g.applyHostCommands()
			if (err == ebiten.Termination) != tc.wantClose || (err != nil && err != ebiten.Termination) {
				t.Fatalf("stop result = %v, want close %v", err, tc.wantClose)
			}
			if err := g.applyHostCommands(); err != nil {
				t.Fatalf("stop notification was not consumed: %v", err)
			}
		})
	}
}

func TestKeyNamePreservesTypedLetterCase(t *testing.T) {
	for _, tc := range []struct {
		key        ebiten.Key
		text, want string
	}{
		{ebiten.KeyA, "A", "A"}, // Shift or Caps Lock supplied by the OS.
		{ebiten.KeyA, "a", "a"}, // Includes Shift with Caps Lock enabled.
		{ebiten.KeyZ, "Z", "Z"},
		{ebiten.KeyA, "bA", "A"},
		{ebiten.KeyA, "", "a"}, // Physical release or a control shortcut.
		{ebiten.KeyA, "한", "a"},
		{ebiten.KeyArrowLeft, "A", "left arrow"},
		{ebiten.KeyArrowRight, "", "right arrow"},
		{ebiten.KeyArrowUp, "", "up arrow"},
		{ebiten.KeyArrowDown, "", "down arrow"},
		{ebiten.KeyShiftLeft, "", "shiftleft"},
		{ebiten.KeyShiftRight, "", "shiftright"},
		{ebiten.KeySpace, " ", "space"},
		{ebiten.KeyEnter, "", "enter"},
	} {
		if got := keyName(tc.key, tc.text); got != tc.want {
			t.Errorf("key=%v text=%q: got %q want %q", tc.key, tc.text, got, tc.want)
		}
	}
}
