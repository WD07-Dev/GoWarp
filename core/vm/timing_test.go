package vm

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"testing"
	"time"

	"gowarp/core/project"
)

func TestTickIntervalMatchesBrowserScheduling(t *testing.T) {
	for _, tc := range []struct {
		fps  float64
		want time.Duration
	}{
		{0, time.Second / 60},
		{.5, 2 * time.Second},
		{25, 40 * time.Millisecond},
		{30, 33 * time.Millisecond},
		{60, 16 * time.Millisecond},
		{120, 8 * time.Millisecond},
		{240, 4 * time.Millisecond},
		{1000, 4 * time.Millisecond},
	} {
		r := &Runtime{fps: tc.fps}
		if got := r.TickInterval(); got != tc.want {
			t.Errorf("FPS %g: interval=%v, want %v", tc.fps, got, tc.want)
		}
	}
}

func TestRealtimeWaitTracksElapsedTimeUnderLoad(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait"},
		"wait":{"opcode":"control_wait","next":"done","inputs":{"DURATION":[1,[4,"0.2"]]}},
		"done":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"timer"]}},
		"timer":{"opcode":"sensing_timer"}
	}`)
	now := time.Unix(100, 0)
	r.now = func() time.Time { return now }
	r.UseRealtimeClock()
	if err := r.Tick(context.Background(), 60, false); err != nil {
		t.Fatal(err)
	}
	/*
		Only two VM ticks fit in 250 ms under load. The wait must nevertheless
		expire along with real audio, rather than requiring twelve VM ticks.
	*/
	now = now.Add(250 * time.Millisecond)
	if err := r.Tick(context.Background(), 60, false); err != nil {
		t.Fatal(err)
	}
	if got := number(r.stage.variables["score"].Value); got != .25 {
		t.Fatalf("delayed tick reported timer=%g, want 0.25 and a completed wait", got)
	}
	if r.Frames() != 2 || !r.Done() {
		t.Fatal("real time changed frame count or failed to finish the wait")
	}
}

func TestRealtimeWaitUsesMillisecondBoundaries(t *testing.T) {
	for _, duration := range []string{"0.1", "0.1005"} {
		t.Run(duration, func(t *testing.T) {
			r := fixture(t, `{
				"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait"},
				"wait":{"opcode":"control_wait","next":"done","inputs":{"DURATION":[1,[4,"`+duration+`"]]}},
				"done":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
			}`)
			base := time.Unix(100, 900000)
			now := base
			r.now = func() time.Time { return now }
			r.UseRealtimeClock()
			for _, elapsed := range []time.Duration{0, 99099 * time.Microsecond, 99100 * time.Microsecond, 100100 * time.Microsecond} {
				now = base.Add(elapsed)
				if err := r.Tick(context.Background(), 60, false); err != nil {
					t.Fatal(err)
				}
				// Date.now() sees 0, 99, 100 and 101 elapsed milliseconds.
				wantDone := elapsed >= 99100*time.Microsecond
				if duration == "0.1005" {
					wantDone = elapsed >= 100100*time.Microsecond
				}
				if got := number(r.stage.variables["score"].Value) == 1; got != wantDone {
					t.Fatalf("after %v: completed=%v, want %v", elapsed, got, wantDone)
				}
			}
		})
	}
}

func TestRealtimeConsecutiveWaitsDoNotAccumulateSubmillisecondJitter(t *testing.T) {
	blocks := map[string]any{
		"flag": map[string]any{"opcode": "event_whenflagclicked", "topLevel": true, "next": "wait0"},
		"done": map[string]any{
			"opcode": "data_setvariableto", "fields": map[string]any{"VARIABLE": []string{"score", "score"}},
			"inputs": map[string]any{"VALUE": []any{1, []any{4, "1"}}},
		},
	}
	for i := 0; i < 100; i++ {
		next := "wait" + strconv.Itoa(i+1)
		if i == 99 {
			next = "done"
		}
		blocks["wait"+strconv.Itoa(i)] = map[string]any{
			"opcode": "control_wait", "next": next,
			"inputs": map[string]any{"DURATION": []any{1, []any{4, "0.1"}}},
		}
	}
	data, err := json.Marshal(blocks)
	if err != nil {
		t.Fatal(err)
	}
	r := fixture(t, string(data))
	base := time.Unix(100, 900000)
	now := base
	r.now = func() time.Time { return now }
	r.UseRealtimeClock()
	for frame := 0; frame <= 600; frame++ {
		/*
			Each six-frame interval is just short of 100 ms, while its
			millisecond timestamps still differ by exactly 100 ms.
		*/
		now = base.Add(time.Duration(frame)*time.Second/60 - time.Duration(frame)*100*time.Nanosecond)
		if err := r.Tick(context.Background(), 60, false); err != nil {
			t.Fatal(err)
		}
		if got := number(r.stage.variables["score"].Value) == 1; got != (frame == 600) {
			t.Fatalf("frame %d: completed=%v, want completion at frame 600", frame, got)
		}
	}
}

func TestRealtimeExtensionTimersAndMusicStayAligned(t *testing.T) {
	r := soundRuntime(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"timer"},
		"timer":{"opcode":"lmsTimers_startResetTimer","next":"import","inputs":{"TIMER":[1,[10,"song"]]}},
		"import":{"opcode":"SPtuneShark3_importMenu","next":"play","inputs":{"SOUND":[1,[10,"beep"]],"NAME":[1,[10,"music"]]}},
		"play":{"opcode":"SPtuneShark3_startSound","inputs":{"NAME":[1,[10,"music"]]}}
	}`)
	r.targets[1].source.Sounds[0].SampleCount = 1000
	now := time.Unix(100, 0)
	r.now = func() time.Time { return now }
	r.UseRealtimeClock()
	if err := r.Tick(context.Background(), 60, false); err != nil {
		t.Fatal(err)
	}
	for _, delay := range []time.Duration{250 * time.Millisecond, 750 * time.Millisecond} {
		now = now.Add(delay)
		if err := r.Tick(context.Background(), 60, false); err != nil {
			t.Fatal(err)
		}
		want := now.Sub(time.Unix(100, 0)).Seconds()
		for _, block := range []*project.Block{
			{Opcode: "lmsTimers_valueOfTimer", Inputs: literalInputs(map[string]any{"TIMER": "song"})},
			{Opcode: "SPtuneShark3_soundProperty", Inputs: literalInputs(map[string]any{"NAME": "music", "PROP": "current time"})},
		} {
			got, err := evalBlock(t, r, 1, block)
			if err != nil || math.Abs(number(got)-want) > 1e-9 {
				t.Fatalf("%s after delayed tick = %v, want %g: %v", block.Opcode, got, want, err)
			}
		}
	}
}

func TestRealtimeExtensionTimerAdvancesWithinTick(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"start"},
		"start":{"opcode":"lmsTimers_startResetTimer","inputs":{"TIMER":[1,[10,"song"]]}}
	}`)
	now := time.Unix(100, 0)
	r.now = func() time.Time { return now }
	r.UseRealtimeClock()
	if err := r.Tick(context.Background(), 60, false); err != nil {
		t.Fatal(err)
	}
	now = now.Add(125 * time.Millisecond)
	got, err := evalBlock(t, r, 0, &project.Block{
		Opcode: "lmsTimers_valueOfTimer", Inputs: literalInputs(map[string]any{"TIMER": "song"}),
	})
	if err != nil || got != .125 {
		t.Fatalf("timer between ticks = %v, %v; want 0.125", got, err)
	}
	if r.elapsed != 0 || r.Frames() != 1 {
		t.Fatal("reading the extension clock advanced the VM clock")
	}
}

func TestExtensionMonotonicTime(t *testing.T) {
	for _, realtime := range []bool{false, true} {
		t.Run(strconv.FormatBool(realtime), func(t *testing.T) {
			r := fixture(t, `{}`)
			r.elapsed = 2
			now := time.Unix(100, 900000)
			r.now = func() time.Time { return now }
			if realtime {
				r.UseRealtimeClock()
			}
			c := &extensionContext{r: r, th: &thread{target: r.stage}}
			for i := 0; i < 2; i++ {
				want := 2.0
				if realtime && i == 1 {
					want += .0005
				}
				got, err := c.Call("runtime.monotonicTime")
				if err != nil || got != want {
					t.Fatalf("clock = %v, %v; want %g", got, err, want)
				}
				if r.elapsed != 2 {
					t.Fatal("reading the extension clock changed the step timestamp")
				}
				now = now.Add(500 * time.Microsecond)
			}
		})
	}
}

func TestRealtimeRestartResetsTimerAndZeroWaitStillYields(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait"},
		"wait":{"opcode":"control_wait","next":"done","inputs":{"DURATION":[1,[4,"0"]]}},
		"done":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"timer"]}},
		"timer":{"opcode":"sensing_timer"}
	}`)
	now := time.Unix(100, 0)
	r.now = func() time.Time { return now }
	r.UseRealtimeClock()
	for i := 0; i < 2; i++ {
		if i > 0 {
			r.Stop()
			now = now.Add(10 * time.Second)
			r.GreenFlag()
		}
		*r.stage.variables["score"] = Variable{Name: "score", Value: float64(-1)}
		for tick := 0; tick < 2; tick++ {
			if err := r.Tick(context.Background(), 60, false); err != nil {
				t.Fatal(err)
			}
			want := float64(-1)
			if tick == 1 {
				want = 0
			}
			if got := number(r.stage.variables["score"].Value); got != want {
				t.Fatalf("run %d tick %d: timer=%g, want %g", i, tick, got, want)
			}
		}
	}
}

func TestRuntimeFPSChangePreservesPendingDurations(t *testing.T) {
	for _, tc := range []struct {
		name, action string
	}{
		{"wait", `"opcode":"control_wait","inputs":{"DURATION":[1,[4,"0.2"]]}`},
		{"speech", `"opcode":"looks_sayforsecs","inputs":{"MESSAGE":[1,[10,"hello"]],"SECS":[1,[4,"0.2"]]}`},
		{
			"glide",
			`"opcode":"motion_glidesecstoxy","inputs":{"SECS":[1,[4,"0.2"]],"X":[1,[4,"20"]],"Y":[1,[4,"0"]]}`,
		},
		{"sound", `"opcode":"sound_playuntildone","inputs":{"SOUND_MENU":[1,[10,"tone"]]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := motionRuntime(t, `{
			"a":{"opcode":"event_whenflagclicked","topLevel":true,"next":"action"},
			"action":{`+tc.action+`,"next":"done"},
			"done":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
			"z":{"opcode":"event_whenflagclicked","topLevel":true,"next":"pause"},
			"pause":{"opcode":"control_wait","next":"fps","inputs":{"DURATION":[1,[4,"0"]]}},
			"fps":{"opcode":"runtimeoptions_setFramerate","inputs":{"fps":[1,[4,"60"]]}}
			}`)
			r.targets[1].source.Sounds = []project.Sound{{
				Name:        "tone",
				Rate:        10,
				SampleCount: 2,
			}}
			if err := r.Run(context.Background(), 7, 30, false); err != nil {
				t.Fatal(err)
			}
			if got := r.stage.variables["score"].Value; got != float64(0) {
				t.Fatalf("pending %s completed early after FPS changed", tc.name)
			}
			if tc.name == "glide" && math.Abs(r.targets[1].state.X-35.0/3) > 1e-9 {
				t.Fatalf("glide position %v", r.targets[1].state.X)
			}
			if tc.name == "speech" && r.targets[1].state.Bubble != "hello" {
				t.Fatal("timed speech ended early")
			}
			if err := r.Run(context.Background(), 5, 30, false); err != nil {
				t.Fatal(err)
			}
			if got := r.stage.variables["score"].Value; got != "1" {
				t.Fatalf("pending %s did not finish: %v", tc.name, got)
			}
			if tc.name == "speech" && r.targets[1].state.Bubble != "" {
				t.Fatal("timed speech did not clear")
			}
			if r.FPS() != 60 {
				t.Fatalf("runtime FPS reset to %v", r.FPS())
			}
			if err := r.Run(context.Background(), 1, 60, false); err == nil {
				t.Fatal("external FPS argument changed without rejection")
			}
		})
	}
}

func TestRuntimeNonpositiveFPSUsesSixtyWithoutChangingReporter(t *testing.T) {
	for _, rate := range []float64{0, -10} {
		r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"fps"},
		"fps":{"opcode":"runtimeoptions_setFramerate","next":"report","inputs":{"fps":[1,[4,"`+strconv.FormatFloat(rate, 'f', -1, 64)+`"]]}},
		"report":{"opcode":"data_setvariableto","next":"wait","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"raw"]}},
		"raw":{"opcode":"runtimeoptions_getFramerate"},
		"wait":{"opcode":"control_wait","next":"done","inputs":{"DURATION":[1,[4,"0.1"]]}},
		"done":{"opcode":"data_setvariableto","fields":{"VARIABLE":["done","done"]},"inputs":{"VALUE":[1,[4,"1"]]}}
		}`)
		r.stage.variables["done"] = &Variable{Name: "done", Value: float64(0)}
		if err := r.Run(context.Background(), 6, 30, false); err != nil {
			t.Fatal(err)
		}
		if got := r.stage.variables["score"].Value; got != rate {
			t.Fatalf("reported FPS=%v want %v", got, rate)
		}
		if r.FPS() != 60 || r.stage.variables["done"].Value != float64(0) {
			t.Fatal("nonpositive FPS wait used wrong clock")
		}
		if err := r.Run(context.Background(), 1, 30, false); err != nil {
			t.Fatal(err)
		}
		if r.stage.variables["done"].Value != "1" {
			t.Fatal("60 FPS fallback wait did not finish")
		}
	}
}

func TestProjectZeroFPSAndCameraFencingPrecedence(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"comments":{"config":{"text":"_twconfig_ {\"framerate\":0,\"runtimeOptions\":{\"fencing\":true}}"}}}],"extensions":["DTcameracontrols"]}`), &p); err != nil {
		t.Fatal(err)
	}
	r := New(&p)
	installTestExtensions(t, r, "DTcameracontrols_getX", "runtimeoptions_getFramerate")
	got, _, err := nativeTestCall(t, r, &thread{target: r.stage}, &project.Block{Opcode: "runtimeoptions_getFramerate"}, dataArguments(nil), true)
	if err != nil || got.Value != float64(0) || r.FPS() != 60 {
		t.Fatalf("configured raw/effective FPS = %v/%v (%v)", got, r.FPS(), err)
	}
	if r.Options().Fencing {
		t.Fatal("project config overrode camera extension fencing")
	}
	if err := r.Tick(context.Background(), 30, false); err != nil {
		t.Fatal(err)
	}
	if math.Abs(r.elapsed-1.0/60) > 1e-12 {
		t.Fatalf("configured clock elapsed %v", r.elapsed)
	}
	plain := New(&project.Project{Targets: []*project.Target{{IsStage: true}}})
	got, _, err = nativeTestCall(t, plain, &thread{target: plain.stage}, &project.Block{Opcode: "runtimeoptions_getFramerate"}, dataArguments(nil), true)
	if err != nil || got.Value != float64(30) || plain.FPS() != 30 {
		t.Fatal("unconfigured project lost default 30 FPS")
	}
}

func TestRuntimeTurboOverrideSurvivesTicks(t *testing.T) {
	r := fixture(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"turbo"},"turbo":{"opcode":"runtimeoptions_setEnabled","inputs":{"thing":[1,[10,"turbo mode"]],"enabled":[1,[10,"true"]]}}}`)
	for i := 0; i < 2; i++ {
		if err := r.Tick(context.Background(), 30, false); err != nil {
			t.Fatal(err)
		}
	}
	if !r.turbo {
		t.Fatal("Tick reset runtime turbo setting")
	}
}

func TestRuntimeFPSChangeKeepsTimerMonotonic(t *testing.T) {
	r := fixture(t, `{
	"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait"},
	"wait":{"opcode":"control_wait","next":"before","inputs":{"DURATION":[1,[4,"0"]]}},
	"before":{"opcode":"data_setvariableto","next":"fps","fields":{"VARIABLE":["before","before"]},"inputs":{"VALUE":[2,"timer"]}},
	"fps":{"opcode":"runtimeoptions_setFramerate","next":"after","inputs":{"fps":[1,[4,"60"]]}},
	"after":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"timer"]}},
	"timer":{"opcode":"sensing_timer"}}`)
	r.stage.variables["before"] = &Variable{Name: "before", Value: float64(0)}
	if err := r.Run(context.Background(), 2, 30, false); err != nil {
		t.Fatal(err)
	}
	before, after := number(r.stage.variables["before"].Value), number(r.stage.variables["score"].Value)
	if math.Abs(before-1.0/30) > 1e-12 || after != before {
		t.Fatalf("FPS changed timer from %v to %v", before, after)
	}
	r.GreenFlag()
	if r.elapsed-r.timerSeconds != 0 {
		t.Fatal("green flag did not reset virtual timer")
	}
}

func TestCameraExtensionRestoresItsInterpolationCompatibility(t *testing.T) {
	r := motionRuntime(t, `{}`)
	if err := r.Configure(60, false, true, 480, 360, "player", "#ffffff"); err != nil {
		t.Fatal(err)
	}
	r.ConfigureRendering(true, true)
	r.ConfigureLimits(false, false)
	installTestExtensions(t, r, "DTcameracontrols_getX")
	if r.Options().Interpolation {
		t.Fatal("camera port omitted the original extension's interpolation disable")
	}
	if !r.Options().HighQualityPen || r.Options().Fencing || r.Options().MiscLimits {
		t.Fatal("camera setup changed unrelated player options")
	}
}
