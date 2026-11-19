package vm

import (
	"gowarp/core/project"
	"math"
	"testing"
)

func TestTuneSharkLegacyPitchOverlapPauseDelete(t *testing.T) {
	r := soundRuntime(t, `{}`)
	r.ConfigureAudio([][]float64{nil, {2}})
	th := &thread{target: r.targets[1]}
	call := func(op string, args map[string]any, fields map[string][]any) any {
		t.Helper()
		reporter := op == "soundCheck"
		result, handled, err := nativeTestCall(t, r, th, &project.Block{Opcode: "SPtuneShark3_" + op, Fields: fields}, dataArguments(args), reporter)
		if !handled || err != nil {
			t.Fatalf("%s: handled %v, %v", op, handled, err)
		}
		return result
	}
	name := map[string]any{"NAME": "effect"}
	call("importMenu", map[string]any{"NAME": "effect", "SOUND": "beep"}, nil)
	call("setThingNew", map[string]any{"NAME": "effect", "TYPE": "pitch", "VALUE": float64(100)}, nil)
	call("toggleOverlap", name, map[string][]any{"TYPE": {"on"}})
	call("startSound", name, nil)
	call("startSound", name, nil)
	commands := r.DrainAudio()
	if len(commands) != 2 || commands[0].ID == commands[1].ID || commands[0].Pitch != 120 || commands[1].Pitch != 120 {
		t.Fatalf("overlap/pitch: %+v", commands)
	}
	for _, channel := range r.extensionAudio().channels {
		if math.Abs(channel.voice.rate-2) > 1e-9 {
			t.Fatal("voice clock did not apply Tune Shark pitch")
		}
	}
	call("pauseSound", name, map[string][]any{"UN_PAUSE": {"pause"}})
	for _, channel := range r.extensionAudio().channels {
		if !channel.voice.paused {
			t.Fatal("pause missed overlapping voice")
		}
	}
	call("resetEffect", map[string]any{"NAME": "effect", "EFFECT": "all effects"}, nil)
	// Reset changes the original channel while existing overlaps keep their own effect state.
	call("deleteSound", name, nil)
	if len(r.extensionAudio().channels) != 0 {
		t.Fatal("delete retained independent voices")
	}
}

func TestExtensionVoiceZeroRateMatchesOutputClock(t *testing.T) {
	r := soundRuntime(t, `{}`)
	r.ConfigureAudio([][]float64{nil, {2}})
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	asset, err := c.Call("audio.asset", "beep", true)
	if err != nil {
		t.Fatal(err)
	}
	value, err := c.Call("audio.play", asset, 0.0, 0.0, 100.0, false, 0.0)
	if err != nil {
		t.Fatal(err)
	}
	commands := r.DrainAudio()
	if len(commands) != 1 || commands[0].Rate == nil || *commands[0].Rate != 0 {
		t.Fatalf("zero rate missing from output command: %+v", commands)
	}
	voice := value.(*soundVoice)
	r.AudioStarted(voice.id)
	r.elapsed = 1
	r.advanceVoice(voice)
	if voice.position != 0 || !voice.active {
		t.Fatal("zero rate must freeze the active voice clock")
	}
	if _, err := c.Call("audio.voice.set", value, "rate", 16.0); err != nil {
		t.Fatal(err)
	}
	updated := r.DrainAudio()
	if len(updated) != 1 || updated[0].Rate == nil || *updated[0].Rate != 16 || *commands[0].Rate != 0 {
		t.Fatal("rate updates changed an earlier command or were clamped")
	}
}
