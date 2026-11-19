package vm

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"weak"
)

func TestPlayingSoundReleasesDeletedCloneData(t *testing.T) {
	for _, urlSound := range []bool{false, true} {
		t.Run(fmt.Sprint(urlSound), func(t *testing.T) {
			r := soundRuntime(t, `{}`)
			r.ConfigureAudio([][]float64{nil, {1}})
			var voice *soundVoice
			var collected weak.Pointer[any]
			func() {
				r.createClone(r.targets[1])
				clone := r.targets[len(r.targets)-1]
				clone.state.Volume, clone.state.Pitch, clone.state.Pan = 39, 12, -25
				items := make([]any, 1<<18)
				clone.state.Lists["temporary"] = List{Items: items}
				collected = weak.Make(&items[0])
				if urlSound {
					c := &extensionContext{r: r, th: &thread{target: clone}}
					value, err := c.Call("audio.url", "data:audio/wav;base64,test")
					if err != nil {
						t.Fatal(err)
					}
					voice = value.(*soundVoice)
				} else {
					voice = r.playSound(clone, "beep")
				}
				r.DrainAudio()
				r.deleteClone(clone)
				r.compactThreads()
			}()
			runtime.GC()
			if collected.Value() != nil {
				t.Error("playing sound retains deleted clone list storage")
			}
			if !voice.active || !voice.outputPending || len(r.DrainAudio()) != 0 {
				t.Fatal("deleting the clone stopped its playing sound")
			}
			r.emitAudio("update", voice)
			commands := r.DrainAudio()
			if len(commands) != 1 || commands[0].Source != 1 || commands[0].Volume != 39 || commands[0].Pitch != 12 || commands[0].Pan != -25 {
				t.Fatalf("releasing clone data changed playback parameters: %+v", commands)
			}
			r.AudioFinished(voice.id)
			if voice.outputPending {
				t.Fatal("deleted clone's sound ignored output completion")
			}
			runtime.KeepAlive(voice)
			runtime.KeepAlive(r)
		})
	}
}

func TestRetiredSoundHandleReleasesDeletedCloneData(t *testing.T) {
	for _, mode := range []string{"replaced", "finished-url", "muted-url"} {
		t.Run(mode, func(t *testing.T) {
			r := soundRuntime(t, `{}`)
			if mode != "muted-url" {
				r.ConfigureAudio([][]float64{nil, {1}})
			}
			var voice *soundVoice
			var data weak.Pointer[any]
			func() {
				r.createClone(r.targets[1])
				clone := r.targets[len(r.targets)-1]
				items := make([]any, 1<<18)
				clone.state.Lists["temporary"] = List{Items: items}
				data = weak.Make(&items[0])
				if mode == "replaced" {
					voice = r.playSound(clone, "beep")
					r.playSound(clone, "beep")
				} else {
					c := &extensionContext{r: r, th: &thread{target: clone}}
					value, err := c.Call("audio.url", "data:audio/wav;base64,test")
					if err != nil {
						t.Fatal(err)
					}
					voice = value.(*soundVoice)
					if mode == "finished-url" {
						r.AudioFinished(voice.id)
					}
				}
				r.DrainAudio()
				r.deleteClone(clone)
				r.compactThreads()
			}()
			runtime.GC()
			if data.Value() != nil {
				t.Fatal("retired voice handle retains deleted clone list storage")
			}
			runtime.KeepAlive(voice)
			runtime.KeepAlive(r)
		})
	}
}

func TestSoundUpdatesKeepPhysicsCadence(t *testing.T) {
	/*
		last-breath-ep runs a warp movement procedure followed by volume
		adjustment in a normal forever loop, with miscLimits disabled.
	*/
	for _, opcode := range []string{"sound_setvolumeto", "sound_changevolumeby", "sound_seteffectto", "sound_changeeffectby"} {
		for _, limits := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/limits=%v", opcode, limits), func(t *testing.T) {
				r := fixture(t, `{
					"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
					"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"call"]}},
					"call":{"opcode":"procedures_call","next":"audio","mutation":{"proccode":"gravity","argumentids":"[]"}},
					"audio":{"opcode":"`+opcode+`","fields":{"EFFECT":["pitch",null]},"inputs":{"VOLUME":[1,[4,"50"]],"VALUE":[1,[4,"1"]]}},
					"def":{"opcode":"procedures_definition","topLevel":true,"next":"accelerate","inputs":{"custom_block":[1,"proto"]}},
					"proto":{"opcode":"procedures_prototype","mutation":{"proccode":"gravity","argumentids":"[]","argumentnames":"[]","argumentdefaults":"[]","warp":"true"}},
					"accelerate":{"opcode":"data_changevariableby","next":"move","fields":{"VARIABLE":["velocity","velocity"]},"inputs":{"VALUE":[1,[4,"1"]]}},
					"move":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[12,"velocity","velocity"]]}}
				}`)
				r.options.MiscLimits = limits
				r.stage.variables["velocity"] = &Variable{Name: "velocity", Value: float64(0)}
				if err := r.Run(context.Background(), 60, 60, false); err != nil {
					t.Fatal(err)
				}
				steps := float64(60)
				if got := number(r.stage.variables["velocity"].Value); got != steps {
					t.Fatalf("gravity advanced %g times in %d VM frames, want %g", got, r.Frames(), steps)
				}
				if got, want := number(r.stage.variables["score"].Value), steps*(steps+1)/2; got != want {
					t.Fatalf("displacement=%g, want %g", got, want)
				}
			})
		}
	}
}

func soundRuntime(t *testing.T, blocks string) *Runtime {
	return eventProject(t, `[{"isStage":true,"name":"Stage","variables":{"score":["score",0]}},{"name":"Sprite","sounds":[{"name":"beep","rate":100,"sampleCount":10}],"blocks":`+blocks+`}]`)
}

func TestSoundWaitAndPitch(t *testing.T) {
	r := soundRuntime(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"pitch"},
 "pitch":{"opcode":"sound_seteffectto","next":"play","fields":{"EFFECT":["pitch",null]},"inputs":{"VALUE":[1,[4,"120"]]}},
 "play":{"opcode":"sound_playuntildone","next":"after","inputs":{"SOUND_MENU":[1,[10,"beep"]]}},
 "after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
 }`)
	run(t, r, 2, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 0 {
		t.Fatal("sound wait skipped")
	}
	run(t, r, 3, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 1 || r.Snapshot()[1].Pitch != 120 {
		t.Fatal("sound did not finish")
	}
}

func TestSoundRestartResolvesPreviousWait(t *testing.T) {
	r := soundRuntime(t, `{
 "a":{"opcode":"event_whenflagclicked","topLevel":true,"next":"play"},
 "play":{"opcode":"sound_playuntildone","next":"after","inputs":{"SOUND_MENU":[1,[10,"beep"]]}},
 "after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
 "b":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait"},
 "wait":{"opcode":"control_wait","next":"again","inputs":{"DURATION":[1,[4,"0"]]}},
 "again":{"opcode":"sound_play","inputs":{"SOUND_MENU":[1,[10,"1tail"]]}}
 }`)
	run(t, r, 3, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 1 {
		t.Fatal("restarted voice kept old waiter")
	}
}

func TestVolumeClampAndClearSoundEffects(t *testing.T) {
	r := soundRuntime(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"volume"},
 "volume":{"opcode":"sound_changevolumeby","next":"pan","inputs":{"VOLUME":[1,[4,"-200"]]}},
 "pan":{"opcode":"sound_seteffectto","next":"clone","fields":{"EFFECT":["pan",null]},"inputs":{"VALUE":[1,[4,"500"]]}},
 "clone":{"opcode":"control_create_clone_of","next":"clear","inputs":{"CLONE_OPTION":[1,[10,"_myself_"]]}},
 "clear":{"opcode":"sound_cleareffects"}
 }`)
	run(t, r, 5, false)
	s := r.Snapshot()
	if s[1].Volume != 0 || s[1].Pan != 0 || s[2].Pan != 100 {
		t.Fatal("sound state not clamped or cloned")
	}
}

func TestMiscLimitsPitchRange(t *testing.T) {
	for _, limits := range []bool{true, false} {
		r := soundRuntime(t, `{
          "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"pitch"},
          "pitch":{"opcode":"sound_seteffectto","fields":{"EFFECT":["pitch",null]},"inputs":{"VALUE":[1,[4,"1500"]]}}
        }`)
		r.options.MiscLimits = limits
		run(t, r, 1, false)
		want := 1000.0
		if limits {
			want = 360
		}
		if got := r.Snapshot()[1].Pitch; got != want {
			t.Fatalf("limits=%v: pitch=%g, want %g", limits, got, want)
		}
	}
}
