package vm

import "testing"

func TestAudioReportersDoNotMutatePlayback(t *testing.T) {
	r := soundRuntime(t, `{}`)
	r.ConfigureAudio([][]float64{nil, {1}})
	target := r.targets[1]
	target.state.Pitch, target.state.Pan = 75, -20
	c := &extensionContext{r: r, th: &thread{target: target}}
	voice := r.playSound(target, "beep")
	if _, err := c.Call("audio.projectVolume", 37.125); err != nil {
		t.Fatal(err)
	}
	r.DrainAudio()
	for _, tc := range []struct {
		op   string
		args []any
		want any
	}{
		{"audio.projectVolume", nil, 37.125},
		{"audio.effect", []any{"pitch", "_myself_"}, 75.0},
		{"audio.effect", []any{"pan", target.state.Name}, -20.0},
		{"audio.effect", []any{"pitch", "_stage_"}, 0.0},
		{"audio.effect", []any{"pitch", "missing"}, 0.0},
	} {
		got, err := c.Call(tc.op, tc.args...)
		if err != nil || got != tc.want {
			t.Fatalf("%s: %v %v want %v", tc.op, got, err, tc.want)
		}
	}
	if !voice.active || r.extensionAudio().volume != 37.125 || len(r.DrainAudio()) != 0 {
		t.Fatal("reporter changed playback")
	}
}

func TestAudioStopTargetSelectionAndFamily(t *testing.T) {
	r := soundRuntime(t, `{}`)
	original := r.targets[1]
	clone := *original
	clone.state.CloneID = 1
	voice := r.playSoundAt(&clone, "beep", 0, true)
	c := &extensionContext{r: r, th: &thread{target: original}}
	for _, selection := range []string{"_stage_", original.state.Name, "missing"} {
		if _, err := c.Call("audio.stopTarget", selection); err != nil {
			t.Fatal(err)
		}
		if !voice.active {
			t.Fatalf("%s stopped a clone-owned sound", selection)
		}
	}
	if _, err := c.Call("audio.stopTarget", "_myfamily_"); err != nil {
		t.Fatal(err)
	}
	if voice.active {
		t.Fatal("family stop kept clone-owned sound")
	}
}

func TestURLUntilDoneCompletesWithOutputAndWithoutBackend(t *testing.T) {
	for _, output := range []bool{false, true} {
		r := soundRuntime(t, `{
   "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"play"},
   "play":{"opcode":"notSound_playUntilDone","inputs":{"path":[1,[10,"https://example.invalid/sound.wav"]]},"next":"after"},
   "after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
  }`)
		if output {
			r.ConfigureAudio([][]float64{nil, {1}})
		}
		run(t, r, 2, false)
		if output {
			if number(r.Snapshot()[0].Variables["score"].Value) != 0 {
				t.Fatal("URL wait ignored output")
			}
			commands := r.DrainAudio()
			if len(commands) != 1 || commands[0].Kind != "play" {
				t.Fatalf("URL wait replayed sound: %+v", commands)
			}
			r.AudioFinished(commands[0].ID)
			run(t, r, 1, false)
		}
		if number(r.Snapshot()[0].Variables["score"].Value) != 1 {
			t.Fatalf("URL wait did not finish (output=%v)", output)
		}
	}
}
