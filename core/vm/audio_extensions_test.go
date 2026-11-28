package vm

import (
	"context"
	"gowarp/core/extension"
	"gowarp/core/project"
	music "gowarp/test/extensions/music"
	"math"
	"strings"
	"testing"
	"time"
)

func TestMusicTempoAndBeatWait(t *testing.T) {
	r := fixture(t, "{}")
	r.project.Targets[0].Tempo = 120
	def := music.New()
	if err := def.Prepare(r.project); err != nil {
		t.Fatal(err)
	}
	ctx := &extension.Context{Host: &extensionContext{r: r, th: &thread{target: r.stage}}, Local: map[string]any{}}
	call := func(op string, args map[string]any) extension.Result {
		t.Helper()
		ctx.Args = args
		for _, b := range def.Blocks {
			if b.Opcode == "music_"+op {
				out, err := b.Handler(ctx)
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
		}
		t.Fatalf("missing music block %s", op)
		return extension.Result{}
	}
	if got := call("getTempo", nil).Value; got != 120.0 {
		t.Fatalf("initial tempo: %v", got)
	}
	if out := call("restForBeats", map[string]any{"BEATS": 2}); !out.Again || !out.Yield {
		t.Fatal("rest did not yield")
	}
	call("setTempo", map[string]any{"TEMPO": 500})
	r.elapsed = .99
	if !call("restForBeats", map[string]any{"BEATS": 2}).Again {
		t.Fatal("tempo change shortened a running rest")
	}
	r.elapsed = 1
	if call("restForBeats", map[string]any{"BEATS": 2}).Again {
		t.Fatal("rest exceeded its deadline")
	}
	call("changeTempo", map[string]any{"TEMPO": -1000})
	if got := call("getTempo", nil).Value; got != 20.0 {
		t.Fatalf("clamped tempo: %v", got)
	}
	ctx.Local = map[string]any{}
	r.audioEnabled = true
	if !call("playDrumForBeats", map[string]any{"DRUM": 19, "BEATS": 0}).Again {
		t.Fatal("zero-beat drum did not yield once")
	}
	commands := r.DrainAudio()
	if len(commands) == 0 || !strings.HasPrefix(commands[0].URL, "data:audio/mpeg;base64,") {
		t.Fatalf("drum did not play embedded sample: %d commands", len(commands))
	}
	if call("playDrumForBeats", map[string]any{"DRUM": 19, "BEATS": 0}).Again {
		t.Fatal("zero-beat drum did not finish")
	}
}

func TestSoundExpandedLoop(t *testing.T) {
	r := soundRuntime(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
 "loop":{"opcode":"lmsSoundExpanded_startLooping","inputs":{"SOUND":[1,[10,"beep"]]}}
 }`)
	r.ConfigureAudio([][]float64{nil, {.1}})
	run(t, r, 1, false)
	commands := r.DrainAudio()
	if len(commands) != 1 || commands[0].Kind != "play" || !commands[0].Loop {
		t.Fatalf("loop did not play: %#v", commands)
	}
	r.AudioStarted(commands[0].ID)
	r.elapsed += .35
	voice := r.voices[r.targets[1].source][0]
	r.advanceVoice(voice)
	if !voice.active {
		t.Fatal("loop stopped at the end of its first pass")
	}
}

func TestTuneSharkImportedSoundClockPauseAndVolume(t *testing.T) {
	r := soundRuntime(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"import"},
 "import":{"opcode":"SPtuneShark3_importMenu","next":"play","inputs":{"SOUND":[1,[10,"beep"]],"NAME":[1,[10,"music"]]}},
 "play":{"opcode":"SPtuneShark3_startSoundAt","next":"loop","inputs":{"NAME":[1,[10,"music"]],"TIME":[1,[4,".02"]]}},
 "loop":{"opcode":"SPtuneShark3_toggleLoop","next":"vol","inputs":{"NAME":[1,[10,"music"]]},"fields":{"TYPE":["on",null]}},
 "vol":{"opcode":"SPtuneShark3_setVol","inputs":{"NAME":[1,[10,"music"]],"NUM":[1,[4,"25"]]}},
 "clock":{"opcode":"SPtuneShark3_soundProperty","inputs":{"NAME":[1,[10,"music"]],"PROP":[1,[10,"current time"]]}},
 "volume":{"opcode":"SPtuneShark3_soundProperty","inputs":{"NAME":[1,[10,"music"]],"PROP":[1,[10,"volume"]]}}
 }`)
	run(t, r, 1, false)
	target := r.targets[1]
	clock := func(id string) float64 {
		budget := 10000
		v, err := r.eval(context.Background(), &thread{target: target}, id, 0, &budget)
		if err != nil {
			t.Fatal(err)
		}
		return number(v)
	}
	before := clock("clock")
	r.elapsed += .03
	if got := clock("clock"); math.Abs(got-before-.03) > 1e-9 {
		t.Fatalf("music clock did not advance: %g -> %g", before, got)
	}
	control := func(op string) {
		t.Helper()
		_, handled, err := nativeTestCall(t, r, &thread{target: target}, &project.Block{Opcode: "SPtuneShark3_ctrlSounds", Fields: map[string][]any{"CONTROL": {op, nil}}}, nil, false)
		if !handled || err != nil {
			t.Fatal("channel control", handled, err)
		}
	}
	control("pause")
	paused := clock("clock")
	r.elapsed += .3
	if got := clock("clock"); got != paused {
		t.Fatalf("paused music clock advanced: %g -> %g", paused, got)
	}
	control("unpause")
	r.elapsed += .01
	if got := clock("clock"); math.Abs(got-paused-.01) > 1e-9 {
		t.Fatalf("resumed music clock jumped: %g -> %g", paused, got)
	}
	control("stop")
	stopped := clock("clock")
	r.elapsed += .2
	if got := clock("clock"); got != stopped {
		t.Fatalf("stopped music clock advanced: %g -> %g", stopped, got)
	}
	if got := clock("volume"); got != 25 {
		t.Fatalf("independent channel volume = %g", got)
	}
}

func TestSoundExpandedOffsetAndProjectVolume(t *testing.T) {
	r := soundRuntime(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"volume"},
 "volume":{"opcode":"lmsSoundExpanded_setProjectVolume","next":"play","inputs":{"VALUE":[1,[4,"50"]]}},
 "play":{"opcode":"lmsSoundExpanded_playSoundAt","inputs":{"SOUND":[1,[10,"beep"]],"START":[1,[4,".04"]]}}
 }`)
	r.ConfigureAudio([][]float64{nil, {.1}})
	run(t, r, 1, false)
	commands := r.DrainAudio()
	if len(commands) != 1 || commands[0].Start != .04 || commands[0].Volume != 50 {
		t.Fatalf("offset/master volume command = %#v", commands)
	}
	r.AudioStarted(commands[0].ID)
	voice := r.voices[r.targets[1].source][0]
	r.elapsed += .061
	r.advanceVoice(voice)
	if voice.active {
		t.Fatal("offset sound played for full asset duration")
	}
}

func TestProjectVolumeUpdatesMusicEffectsAndURLs(t *testing.T) {
	r := soundRuntime(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"master"},
 "master":{"opcode":"lmsSoundExpanded_setProjectVolume","next":"import","inputs":{"VALUE":[1,[4,"50"]]}},
 "import":{"opcode":"SPtuneShark3_importMenu","next":"volume","inputs":{"SOUND":[1,[10,"beep"]],"NAME":[1,[10,"music"]]}},
 "volume":{"opcode":"SPtuneShark3_setVol","next":"play","inputs":{"NAME":[1,[10,"music"]],"NUM":[1,[4,"40"]]}},
 "play":{"opcode":"SPtuneShark3_startSound","inputs":{"NAME":[1,[10,"music"]]}}
 }`)
	r.ConfigureAudio([][]float64{nil, {1}})
	run(t, r, 1, false)
	commands := r.DrainAudio()
	if len(commands) != 1 {
		t.Fatalf("music did not start: %+v", commands)
	}
	if commands[0].Kind != "play" || commands[0].Volume != 20 {
		t.Errorf("music should start at channel 40%% × project 50%%: %+v", commands)
	}
	musicID := commands[0].ID
	target := r.targets[1]
	effect := r.playSound(target, "beep")
	c := &extensionContext{r: r, th: &thread{target: target}}
	urlVoice, err := c.Call("audio.url", "data:audio/wav;base64,test")
	if err != nil {
		t.Fatal(err)
	}
	r.DrainAudio()
	for _, volume := range []float64{25, 0, 100} {
		if _, err := c.Call("audio.projectVolume", volume); err != nil {
			t.Fatal(err)
		}
		commands = r.DrainAudio()
		want := map[int]float64{musicID: volume * .4, effect.id: volume, urlVoice.(*soundVoice).id: volume}
		if len(commands) != len(want) {
			t.Errorf("project volume %g updated %d voices, want %d: %+v", volume, len(commands), len(want), commands)
		}
		for _, cmd := range commands {
			gain, found := want[cmd.ID]
			if !found || cmd.Kind != "update" || cmd.Volume != gain {
				t.Errorf("project volume %g: unexpected command %+v", volume, cmd)
			}
			delete(want, cmd.ID)
		}
		if len(want) != 0 {
			t.Errorf("voices missed project volume update: %v", want)
		}
	}
	// A paused music stream must receive the latest gain before resuming.
	setVoice := func(voice *soundVoice, key string, value any) {
		t.Helper()
		if _, err := c.Call("audio.voice.set", voice, key, value); err != nil {
			t.Fatalf("setting voice %s: %v", key, err)
		}
	}
	var music *soundVoice
	for _, channel := range r.audioExtensions.channels {
		music = channel.voice
	}
	setVoice(music, "paused", true)
	if _, err := c.Call("audio.projectVolume", 50.0); err != nil {
		t.Fatal(err)
	}
	r.DrainAudio()
	setVoice(music, "volume", 30.0)
	commands = r.DrainAudio()
	if len(commands) != 1 || commands[0].Volume != 15 || music.player.state.Volume != 30 {
		t.Fatalf("channel gain compounded with project gain: %+v", commands)
	}
	setVoice(music, "paused", false)
	commands = r.DrainAudio()
	if len(commands) != 1 || commands[0].Kind != "resume" || commands[0].Volume != 15 {
		t.Fatalf("resumed music lost project gain: %+v", commands)
	}
}

func TestTuneSharkPlayAndStopWaitsForRange(t *testing.T) {
	r := soundRuntime(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"import"},
 "import":{"opcode":"SPtuneShark3_importMenu","next":"play","inputs":{"SOUND":[1,[10,"beep"]],"NAME":[1,[10,"music"]]}},
 "play":{"opcode":"SPtuneShark3_playAndStop","next":"after","inputs":{"NAME":[1,[10,"music"]],"TIME":[1,[4,"0"]],"MAX":[1,[4,".06"]]}},
 "after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
 }`)
	run(t, r, 1, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 0 {
		t.Fatal("play-and-stop did not wait")
	}
	run(t, r, 4, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 1 {
		t.Fatal("play-and-stop did not release at its end")
	}
}

func TestTuneSharkLifecycleRespectsControlAndDisposes(t *testing.T) {
	for _, enabled := range []string{"on", "off"} {
		t.Run(enabled, func(t *testing.T) {
			r := soundRuntime(t, `{
   "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"control"},
   "control":{"opcode":"SPtuneShark3_enableControl","fields":{"ON_OFF":["`+enabled+`",null]},"next":"import"},
   "import":{"opcode":"SPtuneShark3_importMenu","inputs":{"SOUND":[1,[10,"beep"]],"NAME":[1,[10,"music"]]},"next":"play"},
   "play":{"opcode":"SPtuneShark3_startSound","inputs":{"NAME":[1,[10,"music"]]}}
  }`)
			run(t, r, 1, false)
			var voice *soundVoice
			for _, channel := range r.audioExtensions.channels {
				voice = channel.voice
			}
			if voice == nil || !voice.active {
				t.Fatal("independent voice did not start")
			}
			r.Stop()
			if voice.active != (enabled == "off") {
				t.Fatal("stop ignored extension control preference")
			}
			if err := r.CloseExtensions(); err != nil {
				t.Fatal(err)
			}
			if voice.active || len(r.audioExtensions.channels) != 0 {
				t.Fatal("dispose retained independent voice")
			}
		})
	}
}

func TestTuneSharkWaitsForAudioOutputCompletion(t *testing.T) {
	r := soundRuntime(t, `{
  "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"import"},
  "import":{"opcode":"SPtuneShark3_importMenu","inputs":{"SOUND":[1,[10,"beep"]],"NAME":[1,[10,"music"]]},"next":"play"},
  "play":{"opcode":"SPtuneShark3_playAndStop","inputs":{"NAME":[1,[10,"music"]],"TIME":[1,[4,"0"]],"MAX":[1,[4,".2"]]},"next":"after"},
  "after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
 }`)
	r.ConfigureAudio([][]float64{nil, {.1}})
	run(t, r, 4, false)
	commands := r.DrainAudio()
	if len(commands) != 1 || commands[0].Kind != "play" {
		t.Fatalf("wait replayed audio side effect: %#v", commands)
	}
	if number(r.Snapshot()[0].Variables["score"].Value) != 0 {
		t.Fatal("wait ignored pending native output")
	}
	r.AudioFinished(commands[0].ID)
	run(t, r, 1, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 1 {
		t.Fatal("native output completion did not release wait")
	}
}

func TestTuneSharkCutoffAllowsTimeBasedTransition(t *testing.T) {
	blocks := `{
 "music":{"opcode":"event_whenflagclicked","topLevel":true,"next":"import"},
 "import":{"opcode":"SPtuneShark3_importMenu","next":"play","inputs":{"SOUND":[1,[10,"beep"]],"NAME":[1,[10,"music"]]}},
 "play":{"opcode":"SPtuneShark3_playAndStop","inputs":{"NAME":[1,[10,"music"]],"TIME":[1,[4,"0"]],"MAX":[1,[4,".06"]]}},
 "transition":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait"},
 "wait":{"opcode":"control_wait_until","next":"after","inputs":{"CONDITION":[2,"past"]}},
 "past":{"opcode":"operator_gt","inputs":{"OPERAND1":[2,"clock"],"OPERAND2":[1,[4,".06"]]}},
 "clock":{"opcode":"SPtuneShark3_soundProperty","inputs":{"NAME":[1,[10,"music"]],"PROP":[1,[10,"current time"]]}},
 "after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
 }`
	for _, tc := range []struct {
		cutoff     string
		elapsed    time.Duration
		startDelay time.Duration
	}{
		{".06", 67 * time.Millisecond, 0}, {"17", 17005 * time.Millisecond, 0},
		{".06", 60400 * time.Microsecond, 0}, {"17", 17000400 * time.Microsecond, 0},
		{".04", 12 * time.Millisecond, 38 * time.Millisecond},
	} {
		t.Run(tc.cutoff, func(t *testing.T) {
			r := soundRuntime(t, strings.ReplaceAll(blocks, ".06", tc.cutoff))
			r.ConfigureAudio([][]float64{nil, {110}})
			now := time.Unix(100, 0)
			r.now = func() time.Time { return now }
			r.UseRealtimeClock()
			if err := r.Tick(context.Background(), 60, false); err != nil {
				t.Fatal(err)
			}
			now = now.Add(74 * time.Second)
			if err := r.Tick(context.Background(), 60, false); err != nil {
				t.Fatal(err)
			}
			if number(r.Snapshot()[0].Variables["score"].Value) != 0 {
				t.Fatal("music time condition passed before playback started")
			}
			now = now.Add(tc.startDelay)
			for _, command := range r.audioCommands {
				if command.Kind == "play" {
					r.AudioStarted(command.ID)
				}
			}
			now = now.Add(tc.elapsed)
			if err := r.Tick(context.Background(), 60, false); err != nil {
				t.Fatal(err)
			}
			if number(r.Snapshot()[0].Variables["score"].Value) != 1 {
				t.Fatal("play-and-stop clamped the clock to its cutoff, leaving the transition waiting forever")
			}
			commands := r.DrainAudio()
			if len(commands) != 2 || commands[0].Kind != "play" || commands[0].End != 110 || commands[1].Kind != "stop" {
				t.Fatalf("cutoff should stop playback on the script turn: %+v", commands)
			}
			for _, ch := range r.audioExtensions.channels {
				if ch.voice.active {
					t.Fatal("cutoff left the music playing")
				}
			}
		})
	}
}
