package vm

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gowarp/core/extension"
)

/*
Procedure tests inspect one script turn to verify yield/warp restoration.
A full VM tick can now include multiple such turns.
*/
func stepOnce(t *testing.T, r *Runtime) {
	t.Helper()
	if err := r.step(context.Background(), r.threads[0]); err != nil {
		t.Fatal(err)
	}
}

func TestStepDiagnosticsPreserveExecutionAndReset(t *testing.T) {
	r := fixture(t, repeatProject)
	plain := fixture(t, repeatProject)
	r.StartStepDiagnostics()
	run(t, r, 1, false)
	run(t, plain, 1, false)
	stats := r.FinishStepDiagnostics()
	if fmt.Sprint(r.Snapshot()) != fmt.Sprint(plain.Snapshot()) || r.Frames() != plain.Frames() {
		t.Fatal("diagnostics changed script execution")
	}
	if stats.Passes < 2 || stats.Turns < 2 || stats.Steps < 1 || stats.Elapsed <= 0 {
		t.Fatalf("missing scheduler work: %+v", stats)
	}
	for i, script := range stats.Longest {
		if script.Hat == "" || script.Block == "" || script.Elapsed <= 0 {
			t.Fatalf("missing completed script location: %+v", script)
		}
		if i > 0 && script.Elapsed > stats.Longest[i-1].Elapsed {
			t.Fatal("script timings are not sorted")
		}
	}
	r.StartStepDiagnostics()
	if got := r.FinishStepDiagnostics(); got.Turns != 0 || got.Elapsed != 0 || got.Longest[0].Hat != "" {
		t.Fatalf("previous update leaked into diagnostics: %+v", got)
	}
}

func TestStepDiagnosticsDoNotAllocatePerTurn(t *testing.T) {
	r := fixture(t, `{
	"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
	"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"draw"]}},
	"draw":{"opcode":"motion_changexby","inputs":{"DX":[1,[4,"1"]]}}
	}`)
	allocs := testing.AllocsPerRun(100, func() {
		r.StartStepDiagnostics()
		if err := r.Tick(context.Background(), 60, false); err != nil {
			t.Fatal(err)
		}
		r.FinishStepDiagnostics()
	})
	if allocs != 0 {
		t.Fatalf("diagnostics allocated %g objects per tick", allocs)
	}
}

func TestSchedulerFinishesCalculationWithoutAdvancingTime(t *testing.T) {
	r := fixture(t, repeatProject)
	run(t, r, 1, false)
	if got := number(r.stage.variables["score"].Value); got != 42 {
		t.Fatalf("calculation was deferred to another tick: %v", got)
	}
	if r.Frames() != 1 || r.elapsed != 1.0/30 {
		t.Fatalf("additional passes advanced time: frame=%d time=%v", r.Frames(), r.elapsed)
	}
}

func TestSchedulerLongLoopBodyDoesNotGainTicks(t *testing.T) {
	const count = stepsPerFrame + 20
	var blocks strings.Builder
	blocks.WriteString(`{
	"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
	"loop":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"3"]],"SUBSTACK":[2,"draw"]}},
	"draw":{"opcode":"motion_changexby","next":"add0","inputs":{"DX":[1,[4,"1"]]}}`)
	for i := 0; i < count; i++ {
		next := ""
		if i+1 < count {
			next = fmt.Sprintf("add%d", i+1)
		}
		fmt.Fprintf(&blocks, `,"add%d":{"opcode":"data_changevariableby","next":%q,"fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}`, i, next)
	}
	blocks.WriteByte('}')
	for _, compiled := range []bool{false, true} {
		t.Run(fmt.Sprintf("calculations=%v", compiled), func(t *testing.T) {
			r := eventProject(t, `[{"isStage":true,"variables":{"score":["score",0]}},{"name":"Sprite","visible":true,"blocks":`+blocks.String()+`}]`)
			if !compiled {
				disableCalculations(r)
			}
			for tick := 1; tick <= 3; tick++ {
				run(t, r, 1, false)
				if got := number(r.stage.variables["score"].Value); got != float64(tick*count) {
					t.Fatalf("tick %d: completed %g additions, want %d", tick, got, tick*count)
				}
				if got := r.targets[1].state.X; got != float64(tick) {
					t.Fatalf("tick %d: loop advanced %g times", tick, got)
				}
			}
		})
	}
}

func TestSchedulerRedrawAndHiddenMotion(t *testing.T) {
	for _, visible := range []bool{false, true} {
		for _, turbo := range []bool{false, true} {
			t.Run(fmt.Sprintf("visible=%v/turbo=%v", visible, turbo), func(t *testing.T) {
				r := eventProject(t, fmt.Sprintf(`[{"name":"Sprite","visible":%v,"blocks":{
				"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
				"loop":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"6"]],"SUBSTACK":[2,"move"]}},
				"move":{"opcode":"motion_changexby","inputs":{"DX":[1,[4,"1"]]}}
				}}]`, visible))
				run(t, r, 1, turbo)
				want := 6.0
				if visible && !turbo {
					want = 1
				}
				if got := r.targets[0].state.X; got != want {
					t.Fatalf("x=%v, want %v", got, want)
				}
			})
		}
	}
}

func TestSchedulerKeepsRoundRobinOrderInTurbo(t *testing.T) {
	for _, turbo := range []bool{false, true} {
		r := eventProject(t, `[{"isStage":true,"lists":{"log":["log",[]]},"blocks":{
		"a":{"opcode":"event_whenflagclicked","topLevel":true,"next":"repeatA"},
		"b":{"opcode":"event_whenflagclicked","topLevel":true,"next":"repeatB"},
		"repeatA":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"3"]],"SUBSTACK":[2,"addA"]}},
		"repeatB":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"3"]],"SUBSTACK":[2,"addB"]}},
		"addA":{"opcode":"data_addtolist","fields":{"LIST":["log","log"]},"inputs":{"ITEM":[1,[10,"A"]]}},
		"addB":{"opcode":"data_addtolist","fields":{"LIST":["log","log"]},"inputs":{"ITEM":[1,[10,"B"]]}}
		}}]`)
		run(t, r, 1, turbo)
		if got := fmt.Sprint(r.stage.state.Lists["log"].Items); got != "[A B A B A B]" {
			t.Fatalf("turbo=%v: order=%s", turbo, got)
		}
	}
}

func TestSchedulerWaitEndsAdditionalPassesButNotCurrentPass(t *testing.T) {
	r := fixture(t, `{
	"a":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait"},
	"b":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
	"wait":{"opcode":"control_wait","inputs":{"DURATION":[1,[4,"0"]]}},
	"loop":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"6"]],"SUBSTACK":[2,"add"]}},
	"add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
	}`)
	run(t, r, 1, false)
	if got := number(r.stage.variables["score"].Value); got != 1 {
		t.Fatalf("wait must allow the current pass but prevent further passes: %v", got)
	}
	run(t, r, 1, false)
	if got := number(r.stage.variables["score"].Value); got != 6 {
		t.Fatalf("redraw request leaked into the next tick: %v", got)
	}
}

/*
Expected tick traces follow TurboWarp's compiled control.repeat/control.wait
and sequencer at scratch-vm revision 0cb82afe62506f3d9f2ff849c95593f0473b5d2c.
*/
func TestSchedulerWaitMatchesTurboWarp(t *testing.T) {
	for _, warp := range []bool{false, true} {
		for _, turbo := range []bool{false, true} {
			for _, duration := range []float64{-1, 0, .05} {
				t.Run(fmt.Sprintf("warp=%v/turbo=%v/duration=%g", warp, turbo, duration), func(t *testing.T) {
					r := fixture(t, fmt.Sprintf(`{
					"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"call"},
					"call":{"opcode":"procedures_call","mutation":{"proccode":"wait loop","argumentids":"[]"}},
					"def":{"opcode":"procedures_definition","topLevel":true,"next":"repeat","inputs":{"custom_block":[1,"proto"]}},
					"proto":{"opcode":"procedures_prototype","mutation":{"proccode":"wait loop","argumentids":"[]","argumentnames":"[]","argumentdefaults":"[]","warp":%v}},
					"repeat":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"6"]],"SUBSTACK":[2,"wait"]}},
					"wait":{"opcode":"control_wait","next":"add","inputs":{"DURATION":[1,[4,"%g"]]}},
					"add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
					}`, warp, duration))
					base := time.Unix(100, 0)
					now := base
					r.now = func() time.Time { return now }
					r.UseRealtimeClock()
					want := []float64{0, 1, 2, 3, 4, 5, 6, 6}
					if duration > 0 {
						want = []float64{0, 0, 0, 1, 1, 1, 2, 2}
					} else if warp || turbo {
						want = []float64{6, 6, 6, 6, 6, 6, 6, 6}
					}
					for tick, score := range want {
						now = base.Add(time.Duration((tick*1000+30)/60) * time.Millisecond)
						if err := r.Tick(context.Background(), 60, turbo); err != nil {
							t.Fatal(err)
						}
						if got := number(r.stage.variables["score"].Value); got != score {
							t.Fatalf("tick=%d score=%v, want %v", tick, got, score)
						}
					}
				})
			}
		}
	}
}

func TestSchedulerHiddenPenStillRequestsRedraw(t *testing.T) {
	r := fixture(t, `{
	"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
	"loop":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"6"]],"SUBSTACK":[2,"stamp"]}},
	"stamp":{"opcode":"pen_stamp"}
	}`)
	pen := &framePen{}
	r.graphics = pen
	run(t, r, 2, false)
	if pen.stamps != 2 {
		t.Fatalf("hidden pen script ran extra passes: %d stamps", pen.stamps)
	}
}

func TestSchedulerRealtimeBudgetAndSingleTickEvent(t *testing.T) {
	r := fixture(t, `{
	"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
	"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"count"]}},
	"count":{"opcode":"control_incr_counter"}
	}`)
	ticks := 0
	if err := r.SetExtensions(extension.Bundle{func() extension.Extension {
		return extension.Extension{ID: "example", Events: map[string]extension.Handler{
			"tick": func(*extension.Context) (extension.Result, error) {
				ticks++
				return extension.Result{}, nil
			},
		}}
	}}); err != nil {
		t.Fatal(err)
	}
	r.UseRealtimeClock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Tick(ctx, 60, false); err != nil {
		t.Fatalf("scheduler did not return within its work budget: %v", err)
	}
	if r.counter <= 1 || ticks != 1 || r.Frames() != 1 {
		t.Fatalf("passes=%v tick events=%d frames=%d", r.counter, ticks, r.Frames())
	}
	if r.elapsed != 0 {
		t.Fatalf("extra passes advanced the clock: %v", r.elapsed)
	}
}

func TestSchedulerReusesTurnBudgets(t *testing.T) {
	r := fixture(t, `{
	"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},
	"loop":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"count"]}},
	"count":{"opcode":"control_incr_counter"}
	}`)
	th := r.threads[0]
	ctx := context.Background()
	allocations := testing.AllocsPerRun(100, func() {
		if err := r.step(ctx, th); err != nil {
			t.Fatal(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("each additional script turn allocated %v objects", allocations)
	}
}

func TestSchedulerResumesRestartedBroadcastInSameTick(t *testing.T) {
	r := fixture(t, `{
	"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"send"},
	"send":{"opcode":"event_broadcast","inputs":{"BROADCAST_INPUT":[1,[11,"go",null]]}},
	"hat":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"count","fields":{"BROADCAST_OPTION":["go",null]}},
	"count":{"opcode":"data_changevariableby","next":"if","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
	"if":{"opcode":"control_if","next":"loop","inputs":{"CONDITION":[2,"less"],"SUBSTACK":[2,"again"]}},
	"less":{"opcode":"operator_lt","inputs":{"OPERAND1":[1,[12,"score","score"]],"OPERAND2":[1,[4,"3"]]}},
	"again":{"opcode":"event_broadcast","inputs":{"BROADCAST_INPUT":[1,[11,"go",null]]}},
	"loop":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"1"]],"SUBSTACK":[2,"counter"]}},
	"counter":{"opcode":"control_incr_counter"}
	}`)
	run(t, r, 1, false)
	if got := number(r.stage.variables["score"].Value); got != 3 || r.counter != 3 || !r.Done() {
		t.Fatalf("restart was deferred: score=%v counter=%v done=%v", got, r.counter, r.Done())
	}
}
