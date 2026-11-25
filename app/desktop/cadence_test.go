package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/app/config"
	"gowarp/app/editorbridge"
	"gowarp/core/extension"
	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
	runtimeoptions "gowarp/test/extensions/runtimeoptions"
)

func TestPlayerLogsWaitBetweenHostUpdates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var messages []string
	g := &game{controller: cadenceController(t, 60, `{}`), ctx: ctx, options: Options{
		Diagnostic: func(message string) { messages = append(messages, message) },
	}}
	// Cancel before entering the native loop; Update must still reset host-gap accounting.
	if err := g.Update(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(messages) != 0 || g.lastHostUpdateEnd.IsZero() {
		t.Fatalf("startup was treated as a stall or update end was lost: %v", messages)
	}
	g.lastHostUpdateEnd = time.Now().Add(-100 * time.Millisecond)
	g.hostDrawTime, g.hostDrawCalls = 12*time.Millisecond, 2
	if err := g.Update(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(messages) != 1 || !strings.Contains(messages[0], "phase=host_gap") || strings.Contains(messages[0], "scheduler=") {
		t.Fatalf("host wait was missing or attributed to VM work: %v", messages)
	}
	for _, field := range []string{"draw_calls=2", "host_draw_cpu=12ms", "host_other_time="} {
		if !strings.Contains(messages[0], field) {
			t.Errorf("host wait does not distinguish draw work: missing %s in %s", field, messages[0])
		}
	}
	if g.hostDrawTime != 0 || g.hostDrawCalls != 0 {
		t.Fatal("draw work from an earlier host gap was not cleared")
	}
	// Immediate catch-up updates must not keep reporting the old gap.
	if err := g.Update(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("host wait was reported again: %v", messages)
	}
}

func TestPlayerLogsShortSimulationStall(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	c.options.BeforeTick = func(*vm.Runtime) error {
		time.Sleep(40 * time.Millisecond)
		return nil
	}
	var messages []string
	g := &game{controller: c, ctx: context.Background(), options: Options{
		Diagnostic: func(message string) { messages = append(messages, message) },
	}}
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || !strings.Contains(messages[0], "phase=vm") {
		t.Fatalf("short VM stall was not identified: %v", messages)
	}
}

func TestPlayerSlowPhasesAreBoundedIndependently(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	var messages []string
	g := &game{controller: c, options: Options{
		Diagnostic: func(message string) { messages = append(messages, message) },
	}}
	// Two 20 ms ticks are not the same as one 40 ms tick.
	g.reportSlowPhase("vm", 40*time.Millisecond, 2)
	if len(messages) != 0 {
		t.Fatal("catch-up ticks were reported as one long tick")
	}
	for _, phase := range []string{"vm", "audio", "draw_cpu", "host_gap"} {
		g.reportSlowPhase(phase, 40*time.Millisecond, 1)
		g.reportSlowPhase(phase, time.Second, 1)
	}
	if len(messages) != 4 {
		t.Fatalf("phases suppressed each other or flooded the log: %v", messages)
	}
	for i, phase := range []string{"vm", "audio", "draw_cpu", "host_gap"} {
		if !strings.Contains(messages[i], "phase="+phase) {
			t.Fatalf("missing phase %q: %v", phase, messages)
		}
	}
}

func TestPlayerSlowLogIdentifiesScriptWork(t *testing.T) {
	c := cadenceController(t, 60, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"draw"},
		"draw":{"opcode":"pen_clear"}
	}`)
	var messages []string
	g := &game{controller: c, ctx: context.Background(), options: Options{
		Diagnostic: func(message string) { messages = append(messages, message) },
	}}
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	g.reportSlowPhase("vm", 40*time.Millisecond, 1)
	if len(messages) != 1 {
		t.Fatalf("expected one bounded log message, got %v", messages)
	}
	for _, field := range []string{"scheduler=", "passes=1", "turns=1", "native=false", "texture_uploads=0", "pen_submit=0s", "prepare_time=0s", "pixel_time=0s", "texture_time=0s", "write_time=0s", `hat="flag"`, `block="draw"`, "focused=", "minimized=", "actual_tps=", "actual_fps="} {
		if !strings.Contains(messages[0], field) {
			t.Fatalf("missing %s in %s", field, messages[0])
		}
	}
}

func TestPlayerOverloadDoesNotRepeatTicksBeforeDrawing(t *testing.T) {
	c := cadenceController(t, 60, `{
		"key":{"opcode":"event_whenkeypressed","topLevel":true,"next":"inc","fields":{"KEY_OPTION":["space",null]}},
		"inc":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
	}`)
	s := config.Default(nil)
	s.FPS = 60
	c.options.Settings = &s
	g := &game{controller: c, ctx: context.Background()}
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	/*
		Ebitengine can request five updates before Draw after a slow frame.
		Account for a 33 ms VM tick without sleeping or running the game.
	*/
	g.updateTime = 33 * time.Millisecond
	for i := 0; i < 4; i++ {
		in := Input{}
		if i == 0 {
			in.Pressed = []string{"space"}
		}
		if i == 1 {
			in.Released = []string{"space"}
		}
		if err := g.advanceController(in); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Frames() != 1 {
		t.Fatalf("overloaded player ran %d ticks before drawing, want 1", c.vm.Frames())
	}
	g.updateTime = 0
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 3 || c.credit != 3 || c.vm.Snapshot()[0].Variables["score"].Value != float64(1) {
		t.Fatalf("deferred updates or input lost: frames=%d score=%v", c.vm.Frames(), c.vm.Snapshot()[0].Variables["score"])
	}
}

func TestPlayerOverloadDoesNotPermanentlySlowSimulation(t *testing.T) {
	c := cadenceController(t, 60, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
		"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"fall"]}},
		"fall":{"opcode":"data_changevariableby","next":"draw","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"-1"]]}},
		"draw":{"opcode":"pen_clear"}
	}`)
	s := config.Default(nil)
	c.options.Settings = &s
	g := &game{controller: c, ctx: context.Background()}
	// Overload must retain skipped simulation ticks so gravity does not fall behind audio.
	for i := 0; i < 60; i++ {
		g.updateTime = 0
		if i%10 < 3 {
			g.updateTime = time.Second
		}
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
		g.recordUpdate()
	}
	if c.vm.Frames() != 60 || c.vm.Snapshot()[0].Variables["score"].Value != float64(-60) || g.updates != 60 {
		t.Fatalf("simulation lost time: frames=%d movement=%v recorded=%d", c.vm.Frames(), c.vm.Snapshot()[0].Variables["score"].Value, g.updates)
	}
}

func TestPlayerRepeatDelayRecoversAfterLongPresentation(t *testing.T) {
	c := cadenceController(t, 60, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"waitTicks"},
		"waitTicks":{"opcode":"control_repeat","next":"done","inputs":{"TIMES":[1,[4,"60"]]}},
		"done":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"100"]]}},
		"drawing":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
		"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"draw"]}},
		"draw":{"opcode":"pen_clear"}
	}`)
	s := config.Default(nil)
	c.options.Settings = &s
	g := &game{controller: c, ctx: context.Background()}
	for update := 0; update < 61; update++ {
		g.updateBudgetUntil = time.Now().Add(time.Second)
		g.updateTime = 0
		if update < 10 {
			g.updateTime = time.Second
		}
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
		if c.tickCount > 2 {
			t.Fatalf("recovery blocked presentation with %d ticks", c.tickCount)
		}
	}
	if c.vm.Frames() != 61 || c.credit != 0 || c.vm.Snapshot()[0].Variables["score"].Value != float64(100) {
		t.Fatalf("repeat delay remained behind after recovery: frames=%d pending=%g score=%v", c.vm.Frames(), c.credit, c.vm.Snapshot()[0].Variables["score"].Value)
	}
}

func TestPlayerDeferredTicksKeepFractionalCredit(t *testing.T) {
	for _, fps := range []float64{30, 29.97} {
		c := cadenceController(t, fps, `{}`)
		s := config.Default(nil)
		c.options.Settings = &s
		c.credit = .75
		g := &game{controller: c, ctx: context.Background(), updateTime: time.Second}
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
		g.updateTime = 0
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
		want := .75 + 2*fps/60 - 1
		if c.vm.Frames() != 1 || math.Abs(c.credit-want) > 1e-12 {
			t.Fatalf("fps=%g: frames=%d credit=%g, want 1/%g", fps, c.vm.Frames(), c.credit, want)
		}
	}
}

func TestPlayerOverloadBoundsWorkPerCallback(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	s := config.Default(nil)
	c.options.Settings = &s
	calls := 0
	c.options.BeforeTick = func(*vm.Runtime) error {
		calls++
		time.Sleep(20 * time.Millisecond)
		return nil
	}
	g := &game{controller: c, ctx: context.Background()}
	for i := 0; i < 2; i++ {
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 || c.vm.Frames() != 2 {
		t.Fatalf("slow callbacks must run one due tick each: calls=%d frames=%d", calls, c.vm.Frames())
	}
	c.options.BeforeTick = nil
	g.updateTime = 0
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 3 {
		t.Fatalf("slow update lost simulation time: frames=%d, want 3", c.vm.Frames())
	}
}

func TestPlayerResumesWithoutDrawingAfterSlowTick(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	s := config.Default(nil)
	c.options.Settings = &s
	c.options.BeforeTick = func(*vm.Runtime) error {
		time.Sleep(20 * time.Millisecond)
		return nil
	}
	g := &game{controller: c, ctx: context.Background()}
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	c.options.BeforeTick = nil
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 2 {
		t.Fatalf("ready update was delayed after a completed slow tick: %d frames", c.vm.Frames())
	}
	// Background windows can receive host updates without another Draw.
	time.Sleep(20 * time.Millisecond)
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 3 || c.credit != 0 {
		t.Fatalf("no-draw recovery lost simulation time: frames=%d credit=%g", c.vm.Frames(), c.credit)
	}
}

func TestPlayerMaintainsCadenceWithoutDrawing(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	s := config.Default(nil)
	c.options.Settings = &s
	c.options.BeforeTick = func(*vm.Runtime) error {
		time.Sleep(8 * time.Millisecond)
		return nil
	}
	g := &game{controller: c, ctx: context.Background()}
	for i := 0; i < 4; i++ {
		if i > 0 {
			time.Sleep(20 * time.Millisecond)
		}
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Frames() != 4 {
		t.Fatalf("background updates stalled without Draw: %d frames, want 4", c.vm.Frames())
	}
}

func TestPlayerCatchupRetainsDebtWithinBatchAndFrameLimits(t *testing.T) {
	for _, limit := range []int{0, 1} {
		c := cadenceController(t, 60, `{}`)
		s := config.Default(nil)
		c.options.Settings = &s
		c.options.Frames = limit
		c.credit = 20
		g := &game{controller: c, ctx: context.Background()}
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
		g.recordUpdate()
		want := 2
		if limit > 0 {
			want = limit
		}
		if c.vm.Frames() != want || g.updates != want || c.credit != float64(21-want) || g.limitReached != (limit > 0) {
			t.Fatalf("limit=%d: frames=%d recorded=%d credit=%g reached=%v", limit, c.vm.Frames(), g.updates, c.credit, g.limitReached)
		}
	}
}

func TestPlayerCatchupStopsAtTimeBudget(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	s := config.Default(nil)
	c.options.Settings = &s
	c.credit = 20
	c.options.BeforeTick = func(*vm.Runtime) error {
		time.Sleep(20 * time.Millisecond)
		return nil
	}
	g := &game{controller: c, ctx: context.Background()}
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 1 || c.credit != 20 {
		t.Fatalf("catch-up exceeded time budget or lost debt: frames=%d credit=%g", c.vm.Frames(), c.credit)
	}
}

func TestPlayerCatchupRetainsTicksDuringLongOverload(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	s := config.Default(nil)
	c.options.Settings = &s
	c.options.BeforeTick = func(*vm.Runtime) error {
		time.Sleep(20 * time.Millisecond)
		return nil
	}
	g := &game{controller: c, ctx: context.Background()}
	for i := 0; i < 20; i++ {
		g.updateTime = 0
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 2; j++ {
			if err := g.advanceController(Input{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if float64(c.vm.Frames())+c.credit != 60 {
		t.Fatalf("overload discarded game time: frames=%d pending=%g", c.vm.Frames(), c.credit)
	}
	c.options.BeforeTick = nil
	g.updateTime = 0
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.tickCount > 2 {
		t.Fatalf("recovery executed %d ticks in one host update", c.tickCount)
	}
}

func TestPlayerCatchupCostAppliesAcrossHostUpdates(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	s := config.Default(nil)
	c.options.Settings = &s
	c.options.BeforeTick = func(*vm.Runtime) error {
		time.Sleep(8 * time.Millisecond)
		return nil
	}
	g := &game{controller: c, ctx: context.Background()}
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	g.updateTime = time.Second/60 - time.Microsecond
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 1 {
		t.Fatalf("host catch-up ignored measured tick cost: %d ticks", c.vm.Frames())
	}
	c.options.BeforeTick = nil
	g.updateTime = 0
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 3 {
		t.Fatalf("new presentation interval did not resume: %d ticks", c.vm.Frames())
	}
}

func TestPlayerKeepsTwoUpdatesWhenPresentationSlows(t *testing.T) {
	c := cadenceController(t, 60, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
		"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"rotate"]}},
		"rotate":{"opcode":"data_changevariableby","next":"draw","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"6"]]}},
		"draw":{"opcode":"pen_clear"}
	}`)
	s := config.Default(nil)
	c.options.Settings = &s
	g := &game{controller: c, ctx: context.Background()}
	// A missed presentation leaves two 60 Hz host updates before the next Draw.
	g.lastHostFrame = time.Now().Add(-time.Second / 30)
	g.sampleHostFrame()
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	// Account for a 9 ms first tick without relying on a timed sleep.
	g.updateTime, g.tickCost = 9*time.Millisecond, 9*time.Millisecond
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 2 || c.vm.Snapshot()[0].Variables["score"].Value != float64(12) {
		t.Fatalf("presentation throttled rotation: frames=%d angle=%v", c.vm.Frames(), c.vm.Snapshot()[0].Variables["score"].Value)
	}
}

func TestPlayerPresentationBudgetStaysBounded(t *testing.T) {
	for _, interval := range []time.Duration{time.Millisecond, time.Second} {
		c := cadenceController(t, 60, `{}`)
		s := config.Default(nil)
		c.options.Settings = &s
		g := &game{controller: c, ctx: context.Background()}
		g.lastHostFrame = time.Now().Add(-interval)
		g.sampleHostFrame()
		if g.updateBudget < time.Second/60 || g.updateBudget > time.Second/30 {
			t.Fatalf("interval %v gave unbounded update budget %v", interval, g.updateBudget)
		}
		// Even after a long pause, two 20 ms ticks must not run in one draw.
		g.updateTime, g.tickCost = 20*time.Millisecond, 20*time.Millisecond
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
		if c.vm.Frames() != 0 {
			t.Fatal("presentation interval allowed an expensive extra tick")
		}
	}
}

func TestPlayerSlowPresentationKeepsSimulationCadenceAfterBudgetExpiry(t *testing.T) {
	c := cadenceController(t, 60, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
		"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"fall"]}},
		"fall":{"opcode":"data_changevariableby","next":"draw","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"-1"]]}},
		"draw":{"opcode":"pen_clear"}
	}`)
	s := config.Default(nil)
	c.options.Settings = &s
	g := &game{controller: c, ctx: context.Background()}
	for frame := 0; frame < 30; frame++ {
		/*
			With a roughly 30 Hz presentation, the next pair of host updates
			arrives just AFTER the budget set by the previous Draw expires.
		*/
		g.updateBudget = time.Second / 30
		g.lastHostFrame = time.Now().Add(-35 * time.Millisecond)
		g.updateBudgetUntil = g.lastHostFrame.Add(g.updateBudget)
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
		// Model two 9 ms ticks inside a 33 ms presentation interval without sleeping.
		g.updateTime, g.tickCost = 9*time.Millisecond, 9*time.Millisecond
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Frames() != 60 || c.credit != 0 || c.vm.Snapshot()[0].Variables["score"].Value != float64(-60) {
		t.Fatalf("presentation expiry slowed gravity: frames=%d pending=%g score=%v", c.vm.Frames(), c.credit, c.vm.Snapshot()[0].Variables["score"].Value)
	}
}

func TestPlayerPresentationBudgetExpiresWithoutDrawing(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	s := config.Default(nil)
	c.options.Settings = &s
	g := &game{controller: c, ctx: context.Background()}
	g.lastHostFrame = time.Now().Add(-time.Second)
	g.sampleHostFrame()
	g.lastHostFrame = time.Now().Add(-time.Second)
	g.updateBudgetUntil = time.Now().Add(-time.Millisecond)
	g.updateTime = time.Second
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 1 {
		t.Fatal("background updates did not resume after the presentation budget expired")
	}
	g.updateTime, g.tickCost = 9*time.Millisecond, 9*time.Millisecond
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 1 {
		t.Fatal("background updates retained the larger presentation budget")
	}
}

func TestPlayerCatchupReservesTimeForExpensiveNextTick(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	s := config.Default(nil)
	c.options.Settings = &s
	c.credit = 4
	c.options.BeforeTick = func(*vm.Runtime) error {
		time.Sleep(12 * time.Millisecond)
		return nil
	}
	c.options.AfterTick = func(*vm.Runtime) error {
		// Positive time remains, but less than another tick plus presentation.
		c.tickDeadline = time.Now().Add(10 * time.Millisecond)
		return nil
	}
	g := &game{controller: c, ctx: context.Background()}
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.tickCount != 1 {
		t.Fatalf("started another expensive tick without spare time: %d ticks", c.tickCount)
	}
}

func TestPlayerCatchupKeepsNormalCadence(t *testing.T) {
	for _, tc := range []struct {
		fps    float64
		frames int
	}{{30, 1}, {60, 2}, {120, 2}} {
		c := cadenceController(t, tc.fps, `{}`)
		s := config.Default(nil)
		s.FPS = tc.fps
		c.options.Settings = &s
		g := &game{controller: c, ctx: context.Background()}
		for i := 0; i < 2; i++ {
			g.updateTime = time.Microsecond
			if err := g.advanceController(Input{}); err != nil {
				t.Fatal(err)
			}
		}
		if c.vm.Frames() != tc.frames {
			t.Fatalf("fps=%g frames=%d, want %d", tc.fps, c.vm.Frames(), tc.frames)
		}
	}
}

func TestPlayerOverloadKeepsStopAndRestartResponsive(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	s := config.Default(nil)
	c.options.Settings = &s
	g := &game{controller: c, ctx: context.Background(), updateTime: time.Second}
	if err := g.advanceController(Input{Stop: true}); err != nil {
		t.Fatal(err)
	}
	if !c.vm.Stopped() || c.deferTick {
		t.Fatal("overloaded player delayed stop or leaked tick deferral")
	}
	if err := g.advanceController(Input{Restart: true}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Stopped() || c.deferTick {
		t.Fatal("overloaded player delayed restart or leaked tick deferral")
	}
}

func TestPlayerCatchupPreservesReplayAndCLI(t *testing.T) {
	for _, replay := range []bool{false, true} {
		c := cadenceController(t, 60, `{}`)
		c.options.Replay = replay
		if replay {
			s := config.Default(nil)
			c.options.Settings = &s
		}
		g := &game{controller: c, ctx: context.Background(), updateTime: time.Second}
		for i := 0; i < 5; i++ {
			if err := g.advanceController(Input{}); err != nil {
				t.Fatal(err)
			}
		}
		if c.vm.Frames() != 5 {
			t.Fatalf("replay=%v: deterministic execution skipped ticks: %d", replay, c.vm.Frames())
		}
	}
}

func cadenceController(t *testing.T, fps float64, blocks string) *controller {
	t.Helper()
	var p project.Project
	raw := `{"targets":[{"isStage":true,"name":"Stage","variables":{"score":["score",0]},"blocks":` + blocks + `}]}`
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	return newController(&p, &media.Library{Sounds: [][]*media.Sound{nil}}, Options{FPS: fps})
}

func TestNativeInitialFPSBeforeFirstTick(t *testing.T) {
	c := cadenceController(t, 1.5, `{}`)
	if c.vm.FPS() != 1.5 || c.vm.Frames() != 0 {
		t.Fatalf("initial FPS=%g frames=%d", c.vm.FPS(), c.vm.Frames())
	}
}

func TestNativeFractionalCadence(t *testing.T) {
	for _, tc := range []struct {
		fps             float64
		updates, frames int
		seconds         float64
	}{{.1, 20, 2, 20}, {1.5, 20, 15, 10}, {29.97, 3000, 2997, 100}, {30, 30, 30, 1}} {
		c := cadenceController(t, tc.fps, `{}`)
		for i := 0; i < tc.updates; i++ {
			if err := c.advance(context.Background(), Input{}); err != nil {
				t.Fatal(err)
			}
		}
		if c.vm.Frames() != tc.frames || math.Abs(c.vm.Time()-tc.seconds) > 1e-9 {
			t.Fatalf("fps=%g: frames=%d seconds=%g", tc.fps, c.vm.Frames(), c.vm.Time())
		}
	}
}

func TestNativeSkippedTicksKeepKeyAndClickEdges(t *testing.T) {
	c := cadenceController(t, .1, `{"key":{"opcode":"event_whenkeypressed","topLevel":true,"next":"one","fields":{"KEY_OPTION":["space",null]}},"one":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},"click":{"opcode":"event_whenstageclicked","topLevel":true,"next":"ten"},"ten":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"10"]]}}}`)
	for i := 0; i < 10; i++ {
		in := Input{MouseX: float64(240 + i), MouseY: ToolbarHeight + 180}
		if i == 0 {
			in.Pressed = []string{"space"}
			in.MousePressed = true
			in.MouseDown = true
		}
		if i == 1 {
			in.Released = []string{"space"}
		}
		if err := c.advance(context.Background(), in); err != nil {
			t.Fatal(err)
		}
		if i < 9 && c.vm.Frames() != 0 {
			t.Fatal("VM advanced on skipped host update")
		}
	}
	if c.vm.Frames() != 1 || c.vm.Snapshot()[0].Variables["score"].Value != float64(11) {
		t.Fatalf("lost input edges: frames=%d state=%+v", c.vm.Frames(), c.vm.Snapshot())
	}
}

func TestNativeSkippedTicksKeepAnswerEditing(t *testing.T) {
	c := cadenceController(t, .1, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"ask"},"ask":{"opcode":"sensing_askandwait","next":"set","inputs":{"QUESTION":[1,[10,"Name?"]]}},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"answer"]}},"answer":{"opcode":"sensing_answer"}}`)
	for i := 0; i < 10; i++ {
		if err := c.advance(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Question() == nil {
		t.Fatal("first execution tick did not ask")
	}
	inputs := []Input{{Text: "Hi한"}, {Backspace: true}, {Enter: true}, {Text: "ignored after submit"}}
	for _, in := range inputs {
		if err := c.advance(context.Background(), in); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 6; i++ {
		if err := c.advance(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Frames() != 2 || c.vm.Snapshot()[0].Variables["score"].Value != "Hi" {
		t.Fatalf("lost answer: frames=%d state=%+v", c.vm.Frames(), c.vm.Snapshot())
	}
}

func TestNativeSkippedTicksKeepStopAndRestartResponsive(t *testing.T) {
	c := cadenceController(t, .1, `{}`)
	if err := c.advance(context.Background(), Input{Stop: true}); err != nil {
		t.Fatal(err)
	}
	if !c.vm.Stopped() || c.vm.Frames() != 0 {
		t.Fatal("stop waited for an execution tick")
	}
	if err := c.advance(context.Background(), Input{Restart: true}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Stopped() {
		t.Fatal("restart waited for an execution tick")
	}
}

func TestNativeFrameLimitCountsExecutionTicks(t *testing.T) {
	c := cadenceController(t, .1, `{}`)
	c.options.Frames = 3
	g := &game{controller: c}
	for i := 0; i < 30; i++ {
		if err := c.advance(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
		g.recordUpdate()
		if i < 29 && g.limitReached {
			t.Fatalf("frame limit reached on skipped host update %d", i+1)
		}
	}
	if !g.limitReached || g.updates != 3 || c.vm.Frames() != 3 {
		t.Fatalf("limit=%v updates=%d frames=%d", g.limitReached, g.updates, c.vm.Frames())
	}
}

func TestNativeMouseSamplesCoalesceUntilExecutionTick(t *testing.T) {
	c := cadenceController(t, .0001, `{}`)
	for i := 0; i < 1000; i++ {
		if err := c.advance(context.Background(), Input{MouseX: float64(i % 480), MouseY: ToolbarHeight + 180}); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Frames() != 0 || len(c.pending) != 0 {
		t.Fatalf("skipped mouse updates accumulated: frames=%d events=%d", c.vm.Frames(), len(c.pending))
	}
	if c.pendingMouse.X != -201 {
		t.Fatalf("latest mouse sample lost: %+v", c.pendingMouse)
	}
}

func TestNativeReplayCadenceAndTickHooks(t *testing.T) {
	c := cadenceController(t, 1.5, `{}`)
	c.options.Replay = true
	before, after := 0, 0
	c.options.BeforeTick = func(*vm.Runtime) error {
		before++
		return nil
	}
	c.options.AfterTick = func(*vm.Runtime) error {
		after++
		return nil
	}
	for i := 0; i < 8; i++ {
		if err := c.advance(context.Background(), Input{Pressed: []string{"space"}}); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Frames() != 6 || before != 6 || after != 6 {
		t.Fatalf("replay cadence frames=%d before=%d after=%d", c.vm.Frames(), before, after)
	}
	if len(c.pending) != 0 {
		t.Fatal("replay retained physical input")
	}
}

func TestNativeCadenceTracksRuntimeFPSChanges(t *testing.T) {
	c := cadenceController(t, 30, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"fps"},"fps":{"opcode":"runtimeoptions_setFramerate","inputs":{"fps":[1,[4,"1.5"]]}}}`)
	if err := c.vm.SetExtensions(extension.Bundle{runtimeoptions.New}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.vm.CloseExtensions() })
	if err := c.advance(context.Background(), Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.FPS() != 1.5 || c.hostTPS() != 2 {
		t.Fatal("host cadence did not follow runtime FPS")
	}
	for i := 0; i < 4; i++ {
		if err := c.advance(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Frames() != 4 {
		t.Fatalf("FPS change tick count=%d", c.vm.Frames())
	}
}

func TestPlayerSlowDrawLogIncludesDeferredUploads(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	var message string
	g := &game{controller: c, options: Options{Diagnostic: func(value string) { message = value }}}
	g.gpu.uploadCount = 2
	g.gpu.uploadBytes = 1024
	g.gpu.uploadTime = 20 * time.Millisecond
	g.reportSlowPhase("draw_cpu", 40*time.Millisecond, 1)
	for _, field := range []string{"texture_uploads=2", "upload_bytes=1024", "upload_time=20ms"} {
		if !strings.Contains(message, field) {
			t.Fatalf("missing %s: %s", field, message)
		}
	}
	if strings.Contains(message, "script1=") {
		t.Fatal("draw reported stale VM script diagnostics")
	}
}

func TestRealtimeRepeatDelayMatchesBrowserInterval(t *testing.T) {
	/*
		TurboWarp schedules fixed FPS with setInterval. Its millisecond delay
		is an integer: 60 FPS uses 16 ms, so 600 loop yields take 9.6 seconds.
	*/
	c := cadenceController(t, 60, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"repeat"},
		"repeat":{"opcode":"control_repeat","next":"done","inputs":{"TIMES":[1,[4,"600"]]}},
		"done":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
		"drawing":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
		"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"draw"]}},
		"draw":{"opcode":"pen_clear"}
	}`)
	base := time.Unix(100, 0)
	now := base
	c.cadenceNow = func() time.Time { return now }
	c.tickLimit = 2
	/*
		Drive host callbacks independently of VM ticks, including callbacks
		which must do no work and those which need an additional tick.
	*/
	for update := 0; update <= 1152; update++ {
		now = base.Add(time.Duration(update) * time.Second / 120)
		if err := c.advance(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Frames() != 601 || c.vm.Snapshot()[0].Variables["score"].Value != float64(1) {
		t.Fatalf("600-tick delay missed 9.6-second deadline: frames=%d score=%v", c.vm.Frames(), c.vm.Snapshot()[0].Variables["score"].Value)
	}
	if c.vm.FPS() != 60 {
		t.Fatalf("browser scheduling changed the project's FPS: %g", c.vm.FPS())
	}
}

func TestRealtimeHostCadenceDoesNotRequireCatchupAtNormalSpeed(t *testing.T) {
	for _, fps := range []float64{30, 60, 120, 240} {
		c := cadenceController(t, fps, "{}")
		settings := config.Default(nil)
		settings.FPS = fps
		c.options.Settings = &settings
		base := time.Unix(100, 0)
		now := base
		c.cadenceNow = func() time.Time { return now }
		// A busy frame can afford one VM tick per host callback.
		c.tickLimit = 1
		hostTPS := c.hostTPS()
		for update := 0; update <= 10*hostTPS; update++ {
			now = base.Add(time.Duration(update) * time.Second / time.Duration(hostTPS))
			if err := c.advance(context.Background(), Input{}); err != nil {
				t.Fatal(err)
			}
		}
		want := 1 + int(math.Round(float64(10*time.Second)/float64(c.vm.TickInterval())))
		if c.vm.Frames() != want || c.credit >= .5 {
			t.Fatalf("fps=%g hostTPS=%d frames=%d want=%d pending=%g", fps, hostTPS, c.vm.Frames(), want, c.credit)
		}
	}
}

func TestRealtimeCadenceCountsElapsedTimeOnce(t *testing.T) {
	for _, fps := range []float64{30, 60, 120} {
		c := cadenceController(t, fps, `{}`)
		now := time.Unix(100, 0)
		c.cadenceNow = func() time.Time { return now }
		c.tickLimit = 2
		if err := c.advance(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
		for _, want := range []int{3, 5} {
			now = now.Add(250 * time.Millisecond)
			c.deferTick = true
			if err := c.advance(context.Background(), Input{}); err != nil {
				t.Fatal(err)
			}
			c.deferTick = false
			for range 32 {
				if err := c.advance(context.Background(), Input{}); err != nil {
					t.Fatal(err)
				}
				if c.tickCount > 2 {
					t.Fatal("catch-up exceeded per-callback limit")
				}
			}
			if c.vm.Frames() != want {
				t.Fatalf("fps=%g: frames=%d, want %d; callbacks must not replay expired intervals", fps, c.vm.Frames(), want)
			}
		}
	}
}

func TestRealtimeCadenceExcludesPauseAndRestart(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	now := time.Unix(100, 0)
	c.cadenceNow = func() time.Time { return now }
	c.tickLimit = 2
	if err := c.advance(context.Background(), Input{}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	c.deferTick = true
	if err := c.advance(context.Background(), Input{}); err != nil {
		t.Fatal(err)
	}
	c.deferTick = false
	c.vm.SetPaused(true)
	if err := c.advance(context.Background(), Input{}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	c.vm.SetPaused(false)
	if err := c.advance(context.Background(), Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 2 || c.credit != 0 {
		t.Fatalf("pause became tick debt: frames=%d credit=%g", c.vm.Frames(), c.credit)
	}
	now = now.Add(time.Hour)
	if err := c.advance(context.Background(), Input{Restart: true}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 3 || c.credit != 0 {
		t.Fatalf("restart inherited old tick debt: frames=%d credit=%g", c.vm.Frames(), c.credit)
	}
}

func TestRealtimeCadenceFollowsFPSChange(t *testing.T) {
	c := cadenceController(t, 60, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"fps"},"fps":{"opcode":"runtimeoptions_setFramerate","inputs":{"fps":[1,[4,"30"]]}}}`)
	if err := c.vm.SetExtensions(extension.Bundle{runtimeoptions.New}); err != nil {
		t.Fatal(err)
	}
	defer c.vm.CloseExtensions()
	now := time.Unix(100, 0)
	c.cadenceNow = func() time.Time { return now }
	c.tickLimit = 2
	if err := c.advance(context.Background(), Input{}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		now = now.Add(33 * time.Millisecond)
		if err := c.advance(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Frames() != 4 {
		t.Fatalf("elapsed interval used old FPS: frames=%d, want 4", c.vm.Frames())
	}
}

func TestReplayCadenceIgnoresElapsedTime(t *testing.T) {
	c := cadenceController(t, 60, `{}`)
	c.options.Replay = true
	now := time.Unix(100, 0)
	c.cadenceNow = func() time.Time { return now }
	c.tickLimit = 2
	for range 3 {
		now = now.Add(time.Hour)
		if err := c.advance(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Frames() != 3 {
		t.Fatalf("wall time changed replay: frames=%d", c.vm.Frames())
	}
}

func TestRealtimePlayerUsesOneHostUpdatePerFrame(t *testing.T) {
	c := cadenceController(t, 60, "{}")
	settings := config.Default(nil)
	c.options.Settings = &settings
	c.cadenceNow = time.Now
	g := &game{controller: c}
	if got := g.updateTPS(); got != ebiten.SyncWithFPS {
		t.Fatalf("host still schedules catch-up updates: TPS=%d", got)
	}
	c.options.Replay = true
	if got := g.updateTPS(); got != c.hostTPS() {
		t.Fatalf("replay lost its fixed update cadence: TPS=%d", got)
	}
	c.options.Replay = false
	c.cadenceNow = nil
	if got := g.updateTPS(); got != c.hostTPS() {
		t.Fatalf("deterministic capture lost its fixed cadence: TPS=%d", got)
	}
}

func TestRealtimeOverloadDoesNotLeaveRecoveryBurst(t *testing.T) {
	c := cadenceController(t, 60, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
		"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"move"]}},
		"move":{"opcode":"data_changevariableby","next":"draw","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
		"draw":{"opcode":"pen_clear"}
	}`)
	now := time.Unix(100, 0)
	c.cadenceNow = func() time.Time { return now }
	c.tickLimit = 1
	for range 40 {
		now = now.Add(48 * time.Millisecond)
		if err := c.advance(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if c.credit >= .5 {
		t.Fatalf("slow playback accumulated %g overdue ticks", c.credit)
	}
	c.tickLimit = 2
	for range 10 {
		now = now.Add(16 * time.Millisecond)
		if err := c.advance(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
		if c.tickCount != 1 {
			t.Fatalf("recovered frame replayed %d ticks at once", c.tickCount)
		}
	}
	if got := c.vm.Snapshot()[0].Variables["score"].Value; got != float64(c.vm.Frames()) {
		t.Fatalf("overload skipped script instructions: score=%v frames=%d", got, c.vm.Frames())
	}
}

func TestRealtimeOverloadKeepsFractionalCadence(t *testing.T) {
	c := cadenceController(t, 60, "{}")
	now := time.Unix(100, 0)
	c.cadenceNow = func() time.Time { return now }
	c.tickLimit = 2
	if err := c.advance(context.Background(), Input{}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(97*16*time.Millisecond + 4*time.Millisecond)
	if err := c.advance(context.Background(), Input{}); err != nil {
		t.Fatal(err)
	}
	if c.tickCount != 2 || math.Abs(c.credit-.25) > 1e-9 {
		t.Fatalf("recovery lost its fractional phase: ticks=%d credit=%g", c.tickCount, c.credit)
	}
	for range 10 {
		if err := c.advance(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
		if c.tickCount != 0 {
			t.Fatal("repeated callback replayed old time")
		}
	}
	now = now.Add(4 * time.Millisecond)
	if err := c.advance(context.Background(), Input{}); err != nil {
		t.Fatal(err)
	}
	if c.tickCount != 1 || math.Abs(c.credit+.5) > 1e-9 {
		t.Fatalf("next tick lost its cadence: ticks=%d credit=%g", c.tickCount, c.credit)
	}
}

func TestRealtimePlayerCatchupIsBoundedBetweenDraws(t *testing.T) {
	c := cadenceController(t, 60, "{}")
	settings := config.Default(nil)
	c.options.Settings = &settings
	now := time.Unix(100, 0)
	c.cadenceNow = func() time.Time { return now }
	c.credit = 20
	g := &game{controller: c, ctx: context.Background(), windowFullscreenRead: true}
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		// Expired wall-clock budget must not grant more foreground work before Draw.
		now = now.Add(16 * time.Millisecond)
		g.updateBudgetUntil = time.Now().Add(-time.Second)
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Frames() != 2 || c.credit != 5 {
		t.Fatalf("catch-up burst before Draw: frames=%d pending=%g", c.vm.Frames(), c.credit)
	}
	g.sampleHostFrame()
	if err := g.advanceController(Input{}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Frames() != 4 || c.credit != 0 {
		t.Fatalf("presentation retained an overdue burst: frames=%d pending=%g", c.vm.Frames(), c.credit)
	}
}

func TestRealtimePlayerKeepsConfiguredCadenceAtDisplayRates(t *testing.T) {
	for _, fps := range []float64{30, 60, 120, 240} {
		for _, display := range []int{30, 60, 144} {
			t.Run(fmt.Sprintf("%g/%d", fps, display), func(t *testing.T) {
				c := cadenceController(t, fps, "{}")
				settings := config.Default(nil)
				settings.FPS = fps
				c.options.Settings = &settings
				base := time.Unix(100, 0)
				now := base
				c.cadenceNow = func() time.Time { return now }
				g := &game{controller: c, ctx: context.Background(), windowFullscreenRead: true}
				for frame := 0; frame <= 2*display; frame++ {
					now = base.Add(time.Duration(frame) * time.Second / time.Duration(display))
					g.lastHostFrame = time.Now().Add(-time.Second / time.Duration(display))
					g.sampleHostFrame()
					if err := g.advanceController(Input{}); err != nil {
						t.Fatal(err)
					}
				}
				want := 1 + int(math.Round(float64(2*time.Second)/float64(c.vm.TickInterval())))
				if c.vm.Frames() != want || c.credit >= .5 {
					t.Fatalf("display rate throttled logic: frames=%d want=%d pending=%g", c.vm.Frames(), want, c.credit)
				}
			})
		}
	}
}

func TestRealtimePlayerBackgroundBudgetRenewsWithoutDraw(t *testing.T) {
	c := cadenceController(t, 60, "{}")
	settings := config.Default(nil)
	c.options.Settings = &settings
	now := time.Unix(100, 0)
	c.cadenceNow = func() time.Time { return now }
	c.credit = 20
	g := &game{controller: c, ctx: context.Background(), unfocused: true}
	for range 3 {
		now = now.Add(16 * time.Millisecond)
		g.updateBudgetUntil = time.Now().Add(-time.Second)
		if err := g.advanceController(Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if c.vm.Frames() != 4 || c.credit != 0 {
		t.Fatalf("background stopped without Draw: frames=%d pending=%g", c.vm.Frames(), c.credit)
	}
}

func TestEmbeddedCadenceContinuesWithoutNativeDraw(t *testing.T) {
	for _, callbacks := range []int{30, 60} {
		t.Run(fmt.Sprint(callbacks), func(t *testing.T) {
			c := cadenceController(t, 60, "{}")
			settings := config.Default(nil)
			c.options.Settings = &settings
			base := time.Unix(100, 0)
			now := base
			c.cadenceNow = func() time.Time { return now }
			g := &game{
				controller: c, ctx: context.Background(),
				options: Options{Editor: &editorbridge.Peer{Embedded: true}},
			}
			for update := 0; update <= 10*callbacks; update++ {
				now = base.Add(time.Duration(update) * time.Second / time.Duration(callbacks))
				// The native window may stop drawing while the browser remains active.
				g.updateBudgetUntil = time.Now().Add(-time.Millisecond)
				if err := g.advanceController(Input{}); err != nil {
					t.Fatal(err)
				}
			}
			want := 1 + int(math.Round(float64(10*time.Second)/float64(c.vm.TickInterval())))
			if c.vm.Frames() != want {
				t.Fatalf("embedded logic waited for native Draw: frames=%d want=%d", c.vm.Frames(), want)
			}
			if c.vm.TickInterval() != 16*time.Millisecond || c.vm.FPS() != 60 {
				t.Fatal("embedded preview changed TurboWarp's timer interval")
			}
		})
	}
}
