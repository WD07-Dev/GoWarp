package vm

import (
	"math"
	"testing"
)

func TestExtensionTargetPlayRangeKeepsTargetVoice(t *testing.T) {
	r := soundRuntime(t, `{}`)
	r.ConfigureAudio([][]float64{nil, {1}})
	target := r.targets[1]
	target.state.Volume, target.state.Pitch, target.state.Pan = 37, 120, -25
	c := &extensionContext{r: r, th: &thread{target: target}}
	value, err := c.Call("audio.targetPlay", "beep", .25, false, .75)
	if err != nil {
		t.Fatal(err)
	}
	v := value.(*soundVoice)
	current, err := c.Call("audio.targetVoice", "beep")
	if err != nil || current != v || v.player != target {
		t.Fatal("bounded voice lost target identity", err)
	}
	commands := r.DrainAudio()
	if len(commands) != 1 || commands[0].Start != .25 || commands[0].End != .75 || commands[0].Volume != 37 || commands[0].Pitch != 120 || commands[0].Pan != -25 {
		t.Fatalf("bounded playback = %+v", commands)
	}
	target.state.Volume = 62
	r.refreshVoices(target)
	commands = r.DrainAudio()
	if len(commands) != 1 || commands[0].Volume != 62 || commands[0].End != .75 {
		t.Fatalf("target update = %+v", commands)
	}
	if _, err := c.Call("audio.stopTarget"); err != nil {
		t.Fatal(err)
	}
	if v.active || v.outputPending {
		t.Fatal("target stop left bounded playback active")
	}
	if got := r.DrainAudio(); len(got) != 1 || got[0].Kind != "stop" {
		t.Fatalf("stop = %+v", got)
	}
	if _, err := c.Call("audio.targetPlay", "beep", .25, false); err != nil {
		t.Fatal(err)
	}
	commands = r.DrainAudio()
	if len(commands) != 2 || commands[1].Kind != "play" || commands[1].End != 1 {
		t.Fatalf("ordinary playback changed = %+v", commands)
	}
}

func TestExtensionTargetPlayRangeBounds(t *testing.T) {
	for _, test := range []struct {
		name                           string
		start, end, wantStart, wantEnd float64
		active                         bool
	}{
		{"segment", .25, .75, .25, .75, true},
		{"negative start", -1, .5, 0, .5, true},
		{"end beyond duration", .25, 4, .25, 1, true},
		{"infinite end", .25, math.Inf(1), .25, 1, true},
		{"zero end", 0, 0, 0, 0, false},
		{"negative end", 0, -1, 0, 0, false},
		{"reversed", .75, .25, .75, .75, false},
		{"start beyond duration", 2, 4, 1, 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := soundRuntime(t, `{}`)
			r.ConfigureAudio([][]float64{nil, {1}})
			c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
			value, err := c.Call("audio.targetPlay", "beep", test.start, false, test.end)
			if err != nil {
				t.Fatal(err)
			}
			v := value.(*soundVoice)
			if v.position != test.wantStart || v.end != test.wantEnd || v.active != test.active || v.outputPending != test.active {
				t.Fatalf("voice = %+v", v)
			}
			commands := r.DrainAudio()
			if !test.active && len(commands) != 0 {
				t.Fatalf("empty segment emitted playback: %+v", commands)
			}
		})
	}
}

func TestExtensionTargetPlayRangeHeadlessCompletion(t *testing.T) {
	r := soundRuntime(t, `{}`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	value, err := c.Call("audio.targetPlay", "beep", .02, false, .06)
	if err != nil {
		t.Fatal(err)
	}
	r.elapsed = .03
	pending, err := c.Call("audio.voice.get", value, "pending")
	if err != nil || pending != true {
		t.Fatal("segment ended early", pending, err)
	}
	r.elapsed = .04
	pending, err = c.Call("audio.voice.get", value, "pending")
	if err != nil || pending != false {
		t.Fatal("segment exceeded end", pending, err)
	}
}
