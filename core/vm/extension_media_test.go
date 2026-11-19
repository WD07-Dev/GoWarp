package vm

import (
	"runtime"
	"testing"
	"weak"

	"gowarp/core/project"
)

func TestExtensionSoundHandleReleasesDeletedClone(t *testing.T) {
	r := soundRuntime(t, `{}`)
	r.ConfigureAudio([][]float64{nil, {1}})
	var asset any
	var collected weak.Pointer[target]
	// End the clone's local scope before checking whether GC can reclaim it.
	func() {
		r.createClone(r.targets[1])
		clone := r.targets[len(r.targets)-1]
		clone.state.Lists["temporary"] = List{Items: make([]any, 1<<18)}
		collected = weak.Make(clone)
		c := &extensionContext{r: r, th: &thread{target: clone}}
		var err error
		asset, err = c.Call("audio.asset", "beep", true)
		if err != nil {
			t.Fatal(err)
		}
		r.deleteClone(clone)
		r.compactThreads()
	}()
	runtime.GC()
	if collected.Value() != nil {
		t.Error("sound handle retains a deleted clone and its temporary lists")
	}
	// The asset remains playable after its importing clone disappears.
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	voice, err := c.Call("audio.play", asset, .25, .75, 40.0, false)
	if err != nil {
		t.Fatal(err)
	}
	commands := r.DrainAudio()
	if len(commands) != 1 {
		t.Fatalf("playback commands = %d, want 1: %+v", len(commands), commands)
	}
	command := commands[0]
	if command.Source != 1 || command.Start != .25 || command.End != .75 || command.Volume != 40 {
		t.Fatalf("playback = %+v, want source=1, start=0.25, end=0.75, volume=40", command)
	}
	if _, err := c.Call("audio.voice.set", voice, "volume", 25.0); err != nil {
		t.Fatal(err)
	}
	commands = r.DrainAudio()
	if len(commands) != 1 {
		t.Fatalf("volume commands = %d, want 1: %+v", len(commands), commands)
	}
	if got := commands[0].Volume; got != 25 {
		t.Fatalf("voice volume = %v, want 25", got)
	}
	runtime.KeepAlive(asset)
	runtime.KeepAlive(r)
}

func TestExtensionMediaGraphicsPrimitives(t *testing.T) {
	r := motionRuntime(t, `{}`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[len(r.targets)-1]}}
	call := func(op string, args ...any) any {
		t.Helper()
		v, err := c.Call(op, args...)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		return v
	}
	call("camera.set", "x", 31.0)
	call("canvas.set", "contrast", 42.0)
	if got := call("camera.get", "x"); got != 31.0 {
		t.Fatalf("camera x = %v, want 31", got)
	}
	if got := call("canvas.get", "contrast"); got != 42.0 {
		t.Fatalf("canvas contrast = %v, want 42", got)
	}
	call("graphics.text.set", map[string]any{"text": "hello", "visible": true, "font": "Galmuri14"})
	first := c.th.target.state.Text
	call("graphics.text.set", map[string]any{"text": "second"})
	if first.Text != "hello" {
		t.Fatalf("previous text snapshot changed to %q", first.Text)
	}
	if got := c.th.target.state.Text.Text; got != "second" {
		t.Fatalf("current text = %q, want %q", got, "second")
	}
	call("graphics.shader.define", "test", "void main(){}", map[string]any{"amount": 2.0})
	call("graphics.shader.select", "test")
	call("graphics.shader.uniform", "test", "amount", 3.0)
	if got := r.View().Shader.Uniforms["amount"]; got != float32(3) {
		t.Fatalf("shader amount = %v (%T), want float32(3)", got, got)
	}
	if _, err := c.Call("missing.operation"); err == nil {
		t.Fatal("unknown operation handled")
	}
}

func TestCanvasPropertyUpdatesPreserveDefaults(t *testing.T) {
	r := fixture(t, "{}")
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	r.redrawRequested = false

	value, err := c.Call("canvas.get", "scale")
	if err != nil || value != float64(100) {
		t.Fatalf("default scale = %v, %v", value, err)
	}
	if r.options.View.Canvas.Initialized || r.redrawRequested {
		t.Fatal("reading defaults changed the canvas")
	}

	for _, update := range []struct {
		name  string
		value any
	}{
		{"renderWidth", float64(960)},
		{"renderHeight", float64(720)},
		{"background", "#123456"},
		{"nearest", true},
	} {
		if _, err := c.Call("canvas.set", update.name, update.value); err != nil {
			t.Fatal(err)
		}
		value, err := c.Call("canvas.get", update.name)
		if err != nil || value != update.value {
			t.Fatalf("%s = %v, %v; want %v", update.name, value, err, update.value)
		}
	}
	canvas := r.options.View.Canvas
	if canvas.ScaleX != 100 || canvas.ScaleY != 100 || canvas.Contrast != 100 || !r.redrawRequested {
		t.Fatalf("property update lost defaults or redraw: %+v", canvas)
	}

	r.redrawRequested = false
	if _, err := c.Call("canvas.set", "unknown", 5); err == nil {
		t.Fatal("unknown canvas property accepted")
	}
	if r.options.View.Canvas != canvas || r.redrawRequested {
		t.Fatal("invalid property changed the canvas")
	}
}

func TestStageShaderSourcesDoesNotSelectOrChangePrograms(t *testing.T) {
	r := fixture(t, "{}")
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	for name, source := range map[string]string{"z": "second", "a": "first", "copy": "first"} {
		if _, err := c.Call("graphics.shader.define", name, source, map[string]any{}); err != nil {
			t.Fatal(err)
		}
	}
	sources := r.StageShaderSources()
	if len(sources) != 2 || sources[0].Code != "first" || sources[1].Code != "second" {
		t.Fatalf("unique sources = %v", sources)
	}
	sources[0].Code = "changed"
	if got := r.StageShaderSources()[0].Code; got != "first" {
		t.Fatalf("editing returned sources changed stored shader to %q", got)
	}
	if r.View().Shader != nil {
		t.Fatal("listing shader sources selected a shader")
	}
}

func TestExtensionMediaIndependentAudio(t *testing.T) {
	r := fixture(t, `{}`)
	r.stage.source.Sounds = append(r.stage.source.Sounds, project.Sound{Name: "beep", Rate: 100, SampleCount: 100})
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	call := func(op string, args ...any) any {
		t.Helper()
		v, err := c.Call(op, args...)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		return v
	}
	asset := call("audio.asset", "beep", true)
	voice := call("audio.play", asset, 0.25, 0.75, 40.0, false)
	r.elapsed = 0.2
	if got := call("audio.voice.get", voice, "position"); got != 0.45 {
		t.Fatalf("playing voice position = %v, want 0.45", got)
	}
	call("audio.voice.set", voice, "paused", true)
	r.elapsed = 0.4
	if got := call("audio.voice.get", voice, "position"); got != 0.45 {
		t.Fatalf("paused voice position = %v, want 0.45", got)
	}
	call("audio.voice.set", voice, "paused", false)
	r.elapsed = 0.8
	if got := call("audio.voice.get", voice, "active"); got != false {
		t.Fatalf("voice active after end = %v, want false", got)
	}
}

func TestExtensionMediaRuntimeOptions(t *testing.T) {
	r := fixture(t, `{}`)
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	if _, err := c.Call("options.set", "framerate", 60.0); err != nil {
		t.Fatal(err)
	}
	if !r.fpsOverride || r.fps != 60 {
		t.Fatal("framerate not applied")
	}
	if _, err := c.Call("camera.dimensions", 5000.0, 360.0); err == nil {
		t.Fatal("invalid dimensions accepted")
	}
	if _, err := c.Call("options.set", "framerate", 1001.0); err == nil {
		t.Fatal("invalid FPS accepted")
	}
	if r.fps != 60 {
		t.Fatalf("invalid FPS changed framerate to %v", r.fps)
	}
	if got := r.View().Width; got != 480 {
		t.Fatalf("invalid dimensions changed stage width to %v", got)
	}
}

func TestExtensionMediaNativeValues(t *testing.T) {
	r := fixture(t, "{}")
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	for _, args := range [][]any{{"turbo", 1}, {"username", 42}} {
		if _, err := c.Call("options.set", args...); err != nil {
			t.Fatal(err)
		}
	}
	turbo, err := c.Call("options.get", "turbo")
	if err != nil {
		t.Fatal(err)
	}
	if turbo != true {
		t.Fatalf("turbo = %v, want true for numeric input 1", turbo)
	}
	if r.username != "42" {
		t.Fatalf("username = %q, want %q for numeric input 42", r.username, "42")
	}
}

func TestExtensionMediaRejectsInvalidPropertiesAndClampsQuality(t *testing.T) {
	r := fixture(t, `{}`)
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	for _, tc := range []struct {
		op   string
		args []any
	}{
		{"canvas.set", []any{"missing", 1}},
		{"stretch.set", []any{"z", 1}},
		{"graphics.text.set", []any{map[string]any{"missing": 1}}},
		{"camera.set", []any{"width", 5000}},
	} {
		if _, err := c.Call(tc.op, tc.args...); err == nil {
			t.Errorf("%s failed to reject invalid property: %v", tc.op, err)
		}
	}
	if _, err := c.Call("graphics.quality", 0, 500); err != nil {
		t.Fatal(err)
	}
	if r.stage.state.RenderQuality != 100 {
		t.Fatalf("unbounded quality: %g", r.stage.state.RenderQuality)
	}
}
