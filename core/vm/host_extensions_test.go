package vm

import (
	"context"
	"testing"
)

func TestCurrentExtensionKeyFallsBackAfterRelease(t *testing.T) {
	r := fixture(t, `{"key":{"opcode":"enderKeysPlusV2_currentKeyPressed"}}`)
	th := &thread{target: r.stage}
	read := func(want string) {
		t.Helper()
		budget := 100
		got, err := r.eval(context.Background(), th, "key", 0, &budget)
		if err != nil || got != want {
			t.Fatalf("current key = %v, %v; want %q", got, err, want)
		}
	}
	read("None")
	r.applyInput(InputEvent{Kind: "key", Key: "a", Down: true})
	r.applyInput(InputEvent{Kind: "key", Key: "b", Down: true})
	read("b")
	r.applyInput(InputEvent{Kind: "key", Key: "b"})
	read("a")
	r.applyInput(InputEvent{Kind: "key", Key: "a"})
	read("None")
}

func TestWindowControlsDistinguishBrowserFullscreen(t *testing.T) {
	r := fixture(t, `{"api":{"opcode":"cubesterWindowControls_isFullscreen"},"browser":{"opcode":"f11detector_isBrowserFullscreen"}}`)
	th := &thread{target: r.stage}
	read := func(id string, want bool) {
		t.Helper()
		budget := 100
		got, err := r.eval(context.Background(), th, id, 0, &budget)
		if err != nil || got != want {
			t.Fatalf("%s = %v, %v; want %v", id, got, err, want)
		}
	}
	r.SetHostFrame(true, 16)
	read("browser", true)
	read("api", false)
	c := &extensionContext{r: r}
	for _, enabled := range []bool{true, false} {
		if _, err := c.Call("host.command", "fullscreen", enabled); err != nil {
			t.Fatal(err)
		}
		read("api", enabled)
		read("browser", true)
	}
	if _, err := c.Call("host.command", "fullscreen", true); err != nil {
		t.Fatal(err)
	}
	r.SetHostFrame(false, 16)
	read("api", false)
	read("browser", false)
}

func TestStopClickedRunsCleanupWithoutRestartingFlag(t *testing.T) {
	r := fixture(t, `{
	 "stop":{"opcode":"lmsMoreEvents_whenStopClicked","topLevel":true,"next":"add"},
	 "add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
	}`)
	r.Stop()
	run(t, r, 1, false)
	if number(r.stage.variables["score"].Value) != 1 {
		t.Fatal("stop-click cleanup did not execute")
	}
	r.GreenFlag()
	run(t, r, 1, false)
	if number(r.stage.variables["score"].Value) != 1 {
		t.Fatal("green flag incorrectly ran stop-click cleanup")
	}
}

func TestExtensionKeyPreservesCaseWithoutChangingScratchKeys(t *testing.T) {
	for _, immediate := range []bool{false, true} {
		r := fixture(t, `{"key":{"opcode":"enderKeysPlusV2_currentKeyPressed"},
   "hat":{"opcode":"event_whenkeypressed","topLevel":true,"fields":{"KEY_OPTION":["a",null]},"next":"add"},
   "add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}}`)
		read := func(want string) {
			t.Helper()
			budget := 100
			got, err := r.eval(context.Background(), &thread{target: r.stage}, "key", 0, &budget)
			if err != nil || got != want {
				t.Fatalf("immediate=%v key=%v err=%v want=%q", immediate, got, err, want)
			}
		}
		if immediate {
			r.SetKeyState("A", true)
			read("A")
		}
		r.applyInput(InputEvent{Kind: "key", Key: "A", Down: true})
		read("A")
		if !r.keys["a"] || r.lastKey != "a" {
			t.Fatal("Scratch key normalization changed")
		}
		run(t, r, 1, false)
		if number(r.stage.variables["score"].Value) != 1 {
			t.Fatal("uppercase input did not start lowercase key hat exactly once")
		}
		r.applyInput(InputEvent{Kind: "key", Key: "b", Down: true})
		read("b")
		r.applyInput(InputEvent{Kind: "key", Key: "b"})
		read("A")
		// Shift may be released before the letter, so release casing can differ.
		if immediate {
			r.SetKeyState("a", false)
			read("None")
		}
		r.applyInput(InputEvent{Kind: "key", Key: "a"})
		read("None")
		if r.keys["a"] {
			t.Fatal("uppercase key remained pressed")
		}
	}
}
