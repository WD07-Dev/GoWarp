package vm

import (
	"math"
	"testing"
)

func TestAudioContextPauseFreezesExistingAndNewVoices(t *testing.T) {
	r := soundRuntime(t, `{}`)
	target := r.targets[1]
	host := &extensionContext{r: r, th: &thread{target: target}}
	voice := r.playSoundAt(target, "beep", .02, true)
	r.elapsed += .01
	if _, err := host.Call("audio.contextPaused", true); err != nil {
		t.Fatal(err)
	}
	position := voice.position
	r.elapsed += 10
	r.advanceVoice(voice)
	if voice.position != position {
		t.Fatal("paused context advanced its voice")
	}
	newVoice := r.playSoundAt(target, "beep", .04, true)
	r.elapsed += 5
	r.advanceVoice(newVoice)
	if newVoice.position != .04 {
		t.Fatal("new sound played while context was paused")
	}
	if _, err := host.Call("audio.contextPaused", false); err != nil {
		t.Fatal(err)
	}
	r.elapsed += .01
	r.advanceVoice(newVoice)
	if math.Abs(newVoice.position-.05) > 1e-9 {
		t.Fatalf("resume included paused time: %v", newVoice.position)
	}
	newVoice.paused = true
	r.pauseAudioContext(true)
	r.elapsed += 1
	r.pauseAudioContext(false)
	if !newVoice.paused {
		t.Fatal("context resume unpaused an independently paused voice")
	}
}

func TestPausedContextSendsPauseForNewHostPlayback(t *testing.T) {
	r := soundRuntime(t, `{}`)
	r.ConfigureAudio([][]float64{nil, {1}})
	r.pauseAudioContext(true)
	voice := r.playSoundAt(r.targets[1], "beep", .2, true)
	commands := r.DrainAudio()
	if len(commands) != 2 || commands[0].Kind != "play" || commands[1].Kind != "pause" || commands[1].ID != voice.id {
		t.Fatalf("new paused playback: %+v", commands)
	}
	r.pauseAudioContext(false)
	commands = r.DrainAudio()
	if len(commands) != 1 || commands[0].Kind != "resume" {
		t.Fatalf("resume: %+v", commands)
	}
}

func TestStopTargetSoundsPreservesCloneOwner(t *testing.T) {
	r := soundRuntime(t, `{}`)
	original := r.targets[1]
	clone := *original
	clone.state.CloneID = 1
	voice := r.playSoundAt(&clone, "beep", 0, true)
	host := &extensionContext{r: r, th: &thread{target: original}}
	if _, err := host.Call("audio.stopTarget"); err != nil {
		t.Fatal(err)
	}
	if !voice.active {
		t.Fatal("original target stopped its clone's sound")
	}
	host.th.target = &clone
	if _, err := host.Call("audio.stopTarget"); err != nil {
		t.Fatal(err)
	}
	if voice.active || voice.outputPending {
		t.Fatal("own sound still playing or retaining an until-done waiter")
	}
}

func TestLoopStartOffsetIsOnlyUsedOnFirstPass(t *testing.T) {
	r := soundRuntime(t, `{}`)
	for _, start := range []float64{.08, .1} {
		voice := r.playSoundAt(r.targets[1], "beep", start, true)
		if !voice.active {
			t.Fatal("loop starting at its end was stopped")
		}
		r.elapsed += .04
		r.advanceVoice(voice)
		want := math.Mod(start+.04, .1)
		if math.Abs(voice.position-want) > 1e-9 {
			t.Fatalf("loop restarted at intro offset: %g want %g", voice.position, want)
		}
	}
}
