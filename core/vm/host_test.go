package vm

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestSceneIntoPreservesCloneSnapshots(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"Sprite","visible":true,"variables":{"v":["v",1]}}]`)
	r.createClone(r.targets[1])
	clone := r.targets[len(r.targets)-1]
	previous := r.Scene()
	current := r.SceneInto(nil)
	if !reflect.DeepEqual(previous, current) {
		t.Fatal("reusable snapshot changed layer order or clone identity")
	}
	r.MoveSprite(1, clone.state.CloneID, 12, 34)
	storage := &current[0]
	current = r.SceneInto(current)
	if &current[0] != storage || current[1].State.X != 12 || previous[1].State.X != 0 {
		t.Fatal("scene storage was not reused independently of the previous snapshot")
	}
	r.deleteClone(clone)
	tail := current[len(current)-1:]
	current = r.SceneInto(current)
	if len(current) != 2 || current[1].State.CloneID != 0 {
		t.Fatal("deleted clone remained in the scene")
	}
	if !reflect.DeepEqual(tail[0], DisplayTarget{}) {
		t.Fatal("unused scene storage retained target references")
	}
	oversized := make([]DisplayTarget, 1000)
	current = r.SceneInto(oversized)
	if cap(current) >= cap(oversized) {
		t.Fatal("scene retained peak clone storage after shrinking")
	}
}

func TestHostTickAdvancesIdleAndSceneCopies(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"Sprite","visible":true,"variables":{"v":["v",1]}}]`)
	if err := r.Tick(context.Background(), 30, false); err != nil {
		t.Fatal(err)
	}
	if r.Frames() != 1 {
		t.Fatal("idle clock did not advance")
	}
	r.createClone(r.targets[1])
	scene := r.Scene()
	if len(scene) != 3 || scene[1].Source != 1 || scene[1].State.CloneID == 0 || scene[2].State.CloneID != 0 {
		t.Fatalf("scene identity/layer order %+v", scene)
	}
	if scene[1].State.Variables != nil || scene[1].State.Lists != nil {
		t.Fatal("scene leaks mutable VM data")
	}
	r.MoveSprite(1, scene[1].State.CloneID, 12, 34)
	if r.Scene()[1].State.X != 12 || r.Scene()[2].State.X != 0 {
		t.Fatal("drag changed wrong target")
	}
}

func TestAudioBridgeCommandsAndCompletion(t *testing.T) {
	r := soundRuntime(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"play"},"play":{"opcode":"sound_playuntildone","next":"after","inputs":{"SOUND_MENU":[1,[10,"beep"]]}},"after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}}`)
	r.ConfigureAudio([][]float64{nil, {.2}})
	run(t, r, 1, false)
	commands := r.DrainAudio()
	if len(commands) != 1 || commands[0].Kind != "play" || commands[0].Source != 1 || commands[0].Sound != 0 {
		t.Fatalf("commands %+v", commands)
	}
	if len(r.DrainAudio()) != 0 {
		t.Fatal("commands replayed")
	}
	run(t, r, 9, false)
	if r.Done() {
		t.Fatal("virtual clock ended wait before audio output drained")
	}
	r.AudioFinished(commands[0].ID)
	run(t, r, 1, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 1 {
		t.Fatal("audio completion not resumed")
	}
}

func TestAudioBridgeSameFrameRestartAndStop(t *testing.T) {
	r := soundRuntime(t, `{}`)
	r.ConfigureAudio([][]float64{nil, {.1}})
	r.fps = 30
	first := r.playSound(r.targets[1], "beep")
	second := r.playSound(r.targets[1], "beep")
	if first.outputPending || !second.outputPending {
		t.Fatal("restarted waiter stuck")
	}
	r.Stop()
	commands := r.DrainAudio()
	if len(commands) != 4 || commands[0].Kind != "play" || commands[1].Kind != "stop" || commands[2].Kind != "play" || commands[3].Kind != "stop" {
		t.Fatalf("lost audio transitions: %+v", commands)
	}
}

func TestAudioClockWaitsForPlaybackStart(t *testing.T) {
	for _, kind := range []string{"sound", "asset", "url"} {
		t.Run(kind, func(t *testing.T) {
			r := soundRuntime(t, `{}`)
			r.ConfigureAudio([][]float64{nil, {120}})
			c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
			var voice *soundVoice
			switch kind {
			case "sound":
				voice = r.playSound(r.targets[1], "beep")
			case "asset":
				asset, err := c.Call("audio.asset", "beep", true)
				if err != nil {
					t.Fatal(err)
				}
				value, err := c.Call("audio.play", asset, 0.0, 120.0, 100.0, false)
				if err != nil {
					t.Fatal(err)
				}
				voice = value.(*soundVoice)
			case "url":
				value, err := c.Call("audio.url", "https://example.invalid/music.wav")
				if err != nil {
					t.Fatal(err)
				}
				voice = value.(*soundVoice)
			}
			r.elapsed = 74
			r.advanceVoice(voice)
			if voice.position != 0 || !voice.active {
				t.Fatalf("unstarted audio advanced: position=%v active=%v", voice.position, voice.active)
			}
			r.AudioStarted(voice.id)
			r.elapsed = 75
			r.advanceVoice(voice)
			if voice.position != 1 {
				t.Fatalf("playback included preparation time: %v", voice.position)
			}
			r.AudioStarted(voice.id)
			r.elapsed = 76
			r.advanceVoice(voice)
			if voice.position != 2 {
				t.Fatalf("repeated start reset the clock: %v", voice.position)
			}
			if _, err := c.Call("audio.voice.set", voice, "paused", true); err != nil {
				t.Fatal(err)
			}
			r.elapsed = 90
			r.advanceVoice(voice)
			if voice.position != 2 {
				t.Fatalf("paused voice advanced: %v", voice.position)
			}
			if _, err := c.Call("audio.voice.set", voice, "paused", false); err != nil {
				t.Fatal(err)
			}
			r.AudioStarted(voice.id)
			r.elapsed = 91
			r.advanceVoice(voice)
			if voice.position != 3 {
				t.Fatalf("resume included paused time: %v", voice.position)
			}
		})
	}
}

func TestAudioStartUsesHostTimeBetweenTicks(t *testing.T) {
	for _, delay := range []time.Duration{2 * time.Second, 99100 * time.Microsecond} {
		t.Run(delay.String(), func(t *testing.T) {
			r := soundRuntime(t, `{}`)
			r.ConfigureAudio([][]float64{nil, {120}})
			now := time.Unix(100, 900000)
			r.now = func() time.Time { return now }
			r.UseRealtimeClock()
			r.syncTime()
			voice := r.playSoundAt(r.targets[1], "beep", 10, false)
			now = now.Add(delay)
			r.AudioStarted(voice.id)
			r.advanceVoice(voice)
			now = now.Add(time.Second)
			r.syncTime()
			r.advanceVoice(voice)
			if voice.position != 11 {
				t.Fatalf("start used stale or mismatched VM time: %v", voice.position)
			}
		})
	}
}

func TestAudioClockWithoutOutputStillAdvances(t *testing.T) {
	r := soundRuntime(t, `{}`)
	voice := r.playSound(r.targets[1], "beep")
	r.elapsed = .05
	r.advanceVoice(voice)
	if voice.position != .05 {
		t.Fatalf("headless playback waits for host: %v", voice.position)
	}
}

func TestAudioResumeWaitsForHostPlayback(t *testing.T) {
	r := soundRuntime(t, `{}`)
	r.ConfigureAudio([][]float64{nil, {120}})
	voice := r.playSound(r.targets[1], "beep")
	r.AudioStarted(voice.id)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	r.elapsed = .5
	if _, err := c.Call("audio.voice.set", voice, "paused", true); err != nil {
		t.Fatal(err)
	}
	r.elapsed = 1.5
	if _, err := c.Call("audio.voice.set", voice, "paused", false); err != nil {
		t.Fatal(err)
	}
	r.elapsed = 2
	r.advanceVoice(voice)
	if voice.position != .5 {
		t.Fatalf("resume clock ran before host playback: %g", voice.position)
	}
	r.AudioStarted(voice.id)
	r.elapsed = 2.25
	r.advanceVoice(voice)
	if voice.position != .75 {
		t.Fatalf("resume included command delivery delay: %g", voice.position)
	}
}

func TestGreenFlagPreservesDataAndClearsTransientState(t *testing.T) {
	r := fixture(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"inc"},"inc":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}}`)
	run(t, r, 1, false)
	r.targets[0].state.Effects.Ghost = 50
	r.targets[0].state.Bubble = "old"
	r.GreenFlag()
	run(t, r, 1, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 2 || r.Snapshot()[0].Effects.Ghost != 0 || r.Snapshot()[0].Bubble != "" {
		t.Fatal("green flag reset data or retained effects")
	}
}

func TestUsesColorSensing(t *testing.T) {
	for _, tc := range []struct {
		opcode string
		want   bool
	}{
		{"pen_stamp", false}, {"sensing_touchingobject", false},
		{"sensing_touchingcolor", true}, {"sensing_coloristouchingcolor", true},
	} {
		t.Run(tc.opcode, func(t *testing.T) {
			r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"Sprite","blocks":{"test":{"opcode":"`+tc.opcode+`"}}}]`)
			if got := r.UsesColorSensing(); got != tc.want {
				t.Fatalf("UsesColorSensing() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAudioClockKeepsSubmillisecondStepTime(t *testing.T) {
	r := soundRuntime(t, `{}`)
	r.ConfigureAudio([][]float64{nil, {120}})
	now := time.Unix(100, 900000)
	r.now = func() time.Time { return now }
	r.UseRealtimeClock()
	r.syncTime()
	voice := r.playSound(r.targets[1], "beep")
	r.AudioStarted(voice.id)
	now = now.Add(100400 * time.Microsecond)
	r.syncTime()
	r.advanceVoice(voice)
	if math.Abs(voice.position-.1004) > 1e-12 {
		t.Fatalf("audio lost submillisecond time: %.9f", voice.position)
	}
	if r.elapsed != .101 {
		t.Fatalf("Scratch step clock changed: %g", r.elapsed)
	}
	now = now.Add(3 * time.Millisecond)
	r.advanceVoice(voice)
	if math.Abs(voice.position-.1004) > 1e-12 {
		t.Fatalf("audio clock changed within the same step: %.9f", voice.position)
	}
	r.syncTime()
	r.advanceVoice(voice)
	if math.Abs(voice.position-.1034) > 1e-12 {
		t.Fatalf("next step lost audio time: %.9f", voice.position)
	}
}

func TestAudioStepClockKeepsOutputClockSeparate(t *testing.T) {
	r := soundRuntime(t, "{}")
	r.ConfigureAudio([][]float64{nil, {120}})
	base := time.Unix(100, 0)
	now := base
	r.now = func() time.Time { return now }
	r.UseRealtimeClock()
	r.syncTime()
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	call := func(op string, args ...any) any {
		t.Helper()
		value, err := c.Call(op, args...)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	asset := call("audio.asset", "beep", true)
	voice := call("audio.play", asset, 0.0, 0.0, 100.0, false).(*soundVoice)
	call("audio.voice.set", voice, "clock", "step")
	check := func(logical, output float64) {
		t.Helper()
		got := number(call("audio.voice.get", voice, "position"))
		if math.Abs(got-logical) > 1e-12 || math.Abs(voice.position-output) > 1e-12 {
			t.Fatalf("position: step=%g output=%g, want %g/%g", got, voice.position, logical, output)
		}
	}
	now = base.Add(38 * time.Millisecond)
	r.AudioStarted(voice.id)
	now = base.Add(50 * time.Millisecond)
	r.syncTime()
	check(.05, .012)
	call("audio.voice.set", voice, "rate", 2.0)
	now = base.Add(75 * time.Millisecond)
	r.syncTime()
	check(.1, .062)
	call("audio.voice.set", voice, "paused", true)
	now = base.Add(time.Second)
	r.syncTime()
	check(.1, .062)
	call("audio.voice.set", voice, "paused", false)
	now = base.Add(1038 * time.Millisecond)
	r.AudioStarted(voice.id)
	now = base.Add(1050 * time.Millisecond)
	r.syncTime()
	check(.2, .086)
	call("audio.voice.stop", voice)
	now = base.Add(2 * time.Second)
	r.syncTime()
	check(.2, .086)
}

func TestAudioStepClockWaitsForPreparedPlayback(t *testing.T) {
	r := soundRuntime(t, "{}")
	r.ConfigureAudio([][]float64{nil, {120}})
	base := time.Unix(100, 0)
	now := base
	r.now = func() time.Time { return now }
	r.UseRealtimeClock()
	r.syncTime()
	voice := r.playSound(r.targets[1], "beep")
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	if _, err := c.Call("audio.voice.set", voice, "clock", "step"); err != nil {
		t.Fatal(err)
	}
	now = base.Add(74 * time.Second)
	r.syncTime()
	r.advanceVoice(voice)
	value, err := c.Call("audio.voice.get", voice, "position")
	if err != nil || number(value) != 0 {
		t.Fatal("preparation advanced the step clock", value, err)
	}
	now = now.Add(38 * time.Millisecond)
	r.AudioStarted(voice.id)
	now = base.Add(74050 * time.Millisecond)
	r.syncTime()
	value, err = c.Call("audio.voice.get", voice, "position")
	if err != nil || math.Abs(number(value)-.05) > 1e-12 {
		t.Fatal("preparation was included in the step clock", value, err)
	}
}
