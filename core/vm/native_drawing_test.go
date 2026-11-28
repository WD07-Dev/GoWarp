package vm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"testing"

	"gowarp/core/project"
)

func addNativeDrawingBlocks(t testing.TB, blocks project.Blocks) {
	t.Helper()
	var drawing project.Blocks
	err := json.Unmarshal([]byte(`{
        "axisLoop":{"opcode":"control_repeat","inputs":{"TIMES":[1,[12,"x","x"]],"SUBSTACK":[2,"axisSetX"]}},
        "axisX":{"opcode":"motion_xposition"},
        "axisY":{"opcode":"motion_yposition"},
        "axisDirection":{"opcode":"motion_direction"},
        "axisNextX":{"opcode":"operator_add","inputs":{"NUM1":[2,"axisX"],"NUM2":[1,[4,"0.125"]]}},
        "axisNextY":{"opcode":"operator_subtract","inputs":{"NUM1":[2,"axisY"],"NUM2":[1,[4,"0.125"]]}},
        "axisSetX":{"opcode":"motion_setx","next":"axisChangeY","inputs":{"X":[2,"axisNextX"]}},
        "axisChangeY":{"opcode":"motion_changeyby","next":"axisSetY","inputs":{"DY":[1,[4,"-0.125"]]}},
        "axisSetY":{"opcode":"motion_sety","next":"axisChangeX","inputs":{"Y":[2,"axisNextY"]}},
        "axisChangeX":{"opcode":"motion_changexby","inputs":{"DX":[1,[4,"0.125"]]}},
        "axisReadDirection":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"axisDirection"]}},
        "axisError":{"opcode":"motion_changexby","inputs":{"DX":[1,[12,"missing","missing"]]}},
        "axisScript":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"axisLoop"},
        "directGhost":{"opcode":"looks_seteffectto","next":"directBrightness","fields":{"EFFECT":["GHOST",null]},"inputs":{"VALUE":[1,[12,"x","x"]]}},
        "directBrightness":{"opcode":"looks_seteffectto","next":"directColor","fields":{"EFFECT":["brightness",null]},"inputs":{"VALUE":[1,[12,"x","x"]]}},
        "directColor":{"opcode":"looks_seteffectto","next":"directUnknown","fields":{"EFFECT":["color",null]},"inputs":{"VALUE":[1,[12,"x","x"]]}},
        "directUnknown":{"opcode":"looks_seteffectto","fields":{"EFFECT":["unknown",null]},"inputs":{"VALUE":[1,[12,"missing","missing"]]}},
        "directDrawingLoop":{"opcode":"control_repeat","inputs":{"TIMES":[1,[12,"x","x"]],"SUBSTACK":[2,"directMove"]}},
        "directNumber":{"opcode":"operator_add","inputs":{"NUM1":[1,[12,"score","score"]],"NUM2":[1,[4,"0"]]}},
        "directMove":{"opcode":"motion_movesteps","next":"directRight","inputs":{"STEPS":[2,"directNumber"]}},
        "directRight":{"opcode":"motion_turnright","next":"directLeft","inputs":{"DEGREES":[2,"directNumber"]}},
        "directLeft":{"opcode":"motion_turnleft","next":"directSize","inputs":{"DEGREES":[2,"directNumber"]}},
        "directSize":{"opcode":"looks_setsizeto","next":"directEffect","inputs":{"SIZE":[2,"directNumber"]}},
        "directEffect":{"opcode":"looks_seteffectto","fields":{"EFFECT":["ghost",null]},"inputs":{"VALUE":[2,"directNumber"]}},
        "penSizeSet":{"opcode":"pen_setPenSizeTo","next":"penSizeChange","inputs":{"SIZE":[1,[12,"x","x"]]}},
        "penSizeChange":{"opcode":"pen_changePenSizeBy","inputs":{"SIZE":[1,[12,"y","y"]]}},
        "penSizeError":{"opcode":"pen_setPenSizeTo","inputs":{"SIZE":[1,[12,"missing","missing"]]}},
        "penSizeLoop":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"1000"]],"SUBSTACK":[2,"penSizeIncrement"]}},
        "penSizeIncrement":{"opcode":"data_changevariableby","next":"penSizeUpdate","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"0.125"]]}},
        "penSizeUpdate":{"opcode":"pen_setPenSizeTo","next":"penSizeGrow","inputs":{"SIZE":[1,[12,"score","score"]]}},
        "penSizeGrow":{"opcode":"pen_changePenSizeBy","inputs":{"SIZE":[1,[4,"0.25"]]}},
        "graphicsCallLoopBody":{"opcode":"control_repeat","inputs":{"TIMES":[1,[12,"x","x"]],"SUBSTACK":[2,"graphicsCallIncrement"]}},
        "graphicsCallIncrement":{"opcode":"data_changevariableby","next":"graphicsCall","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[1,[4,"1"]]}},
        "graphicsCall":{"opcode":"procedures_call","next":"graphicsCallRead","mutation":{"proccode":"observed","argumentids":"[]"}},
        "graphicsCallRead":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[12,"y","y"]]}},
        "penMenuNumber":{"opcode":"pen_menu_colorParam","fields":{"colorParam":[42,null]}},
        "penMenuInput":{"opcode":"pen_menu_colorParam","fields":{"colorParam":["color",null]},"inputs":{"unused":[1,[4,"1"]]}},
        "penMenuMissing":{"opcode":"pen_menu_colorParam"},
        "penMenuEmpty":{"opcode":"pen_menu_colorParam","fields":{"colorParam":[]}},
        "penParamError":{"opcode":"pen_setPenColorParamTo","inputs":{"COLOR_PARAM":[1,[10,"unknown"]],"VALUE":[1,[12,"missing","missing"]]}},
        "penParamSet":{"opcode":"pen_setPenColorParamTo","next":"penParamChange","inputs":{"COLOR_PARAM":[1,[12,"x","x"]],"VALUE":[2,"arg"]}},
        "penParamChange":{"opcode":"pen_changePenColorParamBy","inputs":{"COLOR_PARAM":[1,[12,"x","x"]],"VALUE":[1,[12,"y","y"]]}},
        "penParamLoop":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"1000"]],"SUBSTACK":[2,"penParamIncrement"]}},
        "penParamIncrement":{"opcode":"data_changevariableby","next":"penParamUpdate","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"0.125"]]}},
        "penParamUpdate":{"opcode":"pen_setPenColorParamTo","inputs":{"COLOR_PARAM":[1,"penParamMenu"],"VALUE":[1,[12,"score","score"]]}},
        "penParamMenu":{"opcode":"pen_menu_colorParam","fields":{"colorParam":["color",null]}},
        "drawingSet":{"opcode":"data_changevariableby","next":"drawingSize","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"2"]]}},
        "drawingSize":{"opcode":"pen_setPenSizeTo","next":"drawingGrow","inputs":{"SIZE":[1,[12,"score","score"]]}},
        "drawingGrow":{"opcode":"pen_changePenSizeBy","next":"drawingColor","inputs":{"SIZE":[1,[4,"0.5"]]}},
        "drawingColor":{"opcode":"pen_setPenColorToColor","next":"drawingUp","inputs":{"COLOR":[1,[9,"#ff0000"]]}},
        "drawingUp":{"opcode":"pen_penUp","next":"drawingStart"},
        "drawingStart":{"opcode":"motion_gotoxy","next":"drawingDown","inputs":{"X":[1,[4,"0"]],"Y":[1,[4,"0"]]}},
        "drawingDown":{"opcode":"pen_penDown","next":"drawingMove"},
        "drawingMove":{"opcode":"motion_gotoxy","next":"drawingFinish","inputs":{"X":[2,"regionLength"],"Y":[1,[12,"score","score"]]}},
        "drawingFinish":{"opcode":"pen_penUp"},
        "drawingLog":{"opcode":"procedures_call","next":"drawingBreakpoint","mutation":{"proccode":"log %s","argumentids":"[\"message\"]"},"inputs":{"message":[1,[10,"native log"]]}},
        "drawingBreakpoint":{"opcode":"procedures_call","mutation":{"proccode":"break\u200bpoint","argumentids":"[]"}},
        "stampSize":{"opcode":"looks_setsizeto","next":"stampClear","inputs":{"SIZE":[1,[4,"125"]]}},
        "stampClear":{"opcode":"looks_cleargraphiceffects","next":"stampEffect"},
        "stampEffect":{"opcode":"looks_seteffectto","next":"stampCostume","fields":{"EFFECT":["ghost",null]},"inputs":{"VALUE":[1,[4,"25"]]}},
        "stampCostume":{"opcode":"looks_switchcostumeto","next":"stampDirection","inputs":{"COSTUME":[1,"stampMenu"]}},
        "stampMenu":{"opcode":"looks_costume","fields":{"COSTUME":["second",null]}},
        "stampDirection":{"opcode":"motion_pointindirection","next":"stampStart","inputs":{"DIRECTION":[1,[4,"90"]]}},
        "stampStart":{"opcode":"motion_gotoxy","next":"stampForward","inputs":{"X":[2,"regionLength"],"Y":[1,[4,"2"]]}},
        "stampForward":{"opcode":"motion_movesteps","next":"stampLeft","inputs":{"STEPS":[1,[4,"4"]]}},
        "stampLeft":{"opcode":"motion_turnleft","next":"stampUp","inputs":{"DEGREES":[1,[4,"90"]]}},
        "stampUp":{"opcode":"motion_movesteps","next":"stampRight","inputs":{"STEPS":[1,[4,"2"]]}},
        "stampRight":{"opcode":"motion_turnright","next":"stampMissing","inputs":{"DEGREES":[1,[4,"90"]]}},
        "stampMissing":{"opcode":"procedures_call","next":"stampDraw","mutation":{"proccode":"missing definition","argumentids":"[\"missing\"]"},"inputs":{"missing":[1,[12,"missing variable","missing variable"]]}},
        "stampDraw":{"opcode":"pen_stamp"},
        "stampLoopBody":{"opcode":"control_repeat","inputs":{"TIMES":[1,[12,"x","x"]],"SUBSTACK":[2,"stampCall"]}},
        "stampCall":{"opcode":"procedures_call","mutation":{"proccode":"stamp","argumentids":"[]"}},
        "drawingError":{"opcode":"motion_gotoxy","inputs":{"X":[2,"regionLength"],"Y":[1,[12,"missing","missing"]]}},
        "drawingLoopBody":{"opcode":"control_repeat","inputs":{"TIMES":[1,[12,"x","x"]],"SUBSTACK":[2,"drawingSet"]}}
    }`), &drawing)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"penMenuNumber", "penMenuInput", "penMenuMissing", "penMenuEmpty"} {
		drawing[name+"Store"] = &project.Block{Opcode: "data_setvariableto", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), name}}}
	}
	for _, tc := range []struct{ id, opcode, input string }{
		{"moveInvalid", "motion_movesteps", "STEPS"},
		{"leftInvalid", "motion_turnleft", "DEGREES"},
		{"rightInvalid", "motion_turnright", "DEGREES"},
		{"directionInvalid", "motion_pointindirection", "DIRECTION"},
		{"sizeInvalid", "looks_setsizeto", "SIZE"},
		{"costumeInvalid", "looks_switchcostumeto", "COSTUME"},
		{"colorInvalid", "pen_setPenColorToColor", "COLOR"},
		{"effectInvalid", "looks_seteffectto", "VALUE"},
	} {
		drawing[tc.id] = &project.Block{
			Opcode: tc.opcode,
			Inputs: map[string][]any{tc.input: {float64(1), []any{float64(12), "missing", "missing"}}},
			Fields: map[string][]any{"EFFECT": {"ghost", nil}},
		}
	}
	for id, block := range drawing {
		blocks[id] = block
	}
}

type nativeDrawingRecorder struct {
	surfaceRecorder
	scores []any
}

func (p *nativeDrawingRecorder) SetView(View) {
}

type nativeDataFreeGraphics struct {
	nativeDrawingRecorder
	independent bool
}

type nativeSwitchingGraphics struct {
	nativeDrawingRecorder
	r *Runtime
}

func (p *nativeSwitchingGraphics) CostumeSize(target DisplayTarget) (float64, float64) {
	p.r.SetGraphics(&nativeDataFreeGraphics{independent: true})
	target.State.Variables["y"] = Variable{Value: float64(19)}
	return 20, 30
}

func (p *nativeDataFreeGraphics) ScriptDataIndependent() bool {
	return p.independent
}

func (p *nativeDataFreeGraphics) Pen(command PenCommand) {
	command.Target.State.Variables = nil
	command.Target.State.Lists = nil
	p.surfaceRecorder.Pen(command)
}

func testNativeGraphicsCaches(t *testing.T) {
	for _, procedure := range []string{"drawingLoop", "stampLoop", "graphicsCallLoop"} {
		var wantState []TargetState
		var wantCommands []PenCommand
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			r.options.Fencing = true
			graphics := &nativeDataFreeGraphics{independent: true}
			r.SetGraphics(graphics)
			*r.targets[1].variables["x"] = Variable{Value: float64(1000)}
			r.startHat(r.targets[1], procedure+"Hat", false)
			if err := r.Run(context.Background(), 1, 60, false); err != nil {
				t.Fatal(err)
			}
			if !compiled {
				wantState, wantCommands = r.Snapshot(), graphics.commands
			} else if !reflect.DeepEqual(wantState, r.Snapshot()) || !reflect.DeepEqual(wantCommands, graphics.commands) {
				t.Fatalf("%s: retaining calculation caches changed drawing", procedure)
			}
		}
	}
	r := New(nativeCodegenFixture(t))
	r.options.Fencing = false
	graphics := &nativeDataFreeGraphics{independent: true}
	r.SetGraphics(graphics)
	allocations := testing.AllocsPerRun(5, func() {
		*r.targets[1].variables["x"] = Variable{Value: float64(1000)}
		*r.targets[1].variables["score"] = Variable{Value: float64(0)}
		graphics.commands = graphics.commands[:0]
		r.startHat(r.targets[1], "drawingLoopHat", false)
		if err := r.Run(context.Background(), 1, 60, false); err != nil {
			t.Fatal(err)
		}
	})
	t.Logf("1000 drawing iterations: %g allocations", allocations)
	if allocations > 2550 {
		t.Fatalf("drawing discarded calculation caches: %g allocations", allocations)
	}
	observer := &nativeCacheGraphics{}
	r.SetGraphics(observer)
	r.options.Fencing = true
	r.startHat(r.targets[1], "observedHat", false)
	if err := r.Run(context.Background(), 1, 60, false); err != nil {
		t.Fatal(err)
	}
	if observer.observed != float64(6) || r.targets[1].variables["score"].Value != float64(19) {
		t.Fatal("changing graphics lost script data synchronization")
	}
	r.SetGraphics(&nativeSwitchingGraphics{r: r})
	r.startHat(r.targets[1], "observedHat", false)
	if err := r.Run(context.Background(), 1, 60, false); err != nil {
		t.Fatal(err)
	}
	if r.targets[1].variables["score"].Value != float64(19) {
		t.Fatal("switching graphics inside a callback kept stale variables")
	}
}

func BenchmarkNativeGraphicsCaches(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, independent := range []bool{false, true} {
		b.Run(fmt.Sprintf("independent=%v", independent), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			r.options.Fencing = false
			graphics := &nativeDataFreeGraphics{independent: independent}
			r.SetGraphics(graphics)
			b.ReportAllocs()
			for b.Loop() {
				*r.targets[1].variables["x"] = Variable{Value: float64(1000)}
				*r.targets[1].variables["score"] = Variable{Value: float64(0)}
				graphics.commands = graphics.commands[:0]
				th := r.startHat(r.targets[1], "drawingLoopHat", false)
				if err := r.Run(context.Background(), 1, 60, false); err != nil {
					b.Fatal(err)
				}
				if !th.done || len(graphics.commands) != 2000 || r.targets[1].variables["score"].Value != float64(2000) {
					b.Fatal("drawing iterations did not finish")
				}
			}
		})
	}
}

func (p *nativeDrawingRecorder) Bounds(t DisplayTarget, tight bool) (Bounds, bool) {
	return (&fractionalFenceGraphics{}).Bounds(t, tight)
}

func (p *nativeDrawingRecorder) CostumeSize(DisplayTarget) (float64, float64) {
	return 20, 30
}

func (p *nativeDrawingRecorder) Pen(command PenCommand) {
	p.surfaceRecorder.Pen(command)
	p.scores = append(p.scores, command.Target.State.Variables["score"].Value)
}

func TestNativeCompilesAxisMotion(t *testing.T) {
	p := nativeCodegenFixture(t)
	target := &target{source: p.Targets[1], sourceIndex: 1}
	if err := prepareProcedures(target); err != nil {
		t.Fatal(err)
	}
	g := &nativeGenerator{known: make(map[*procedure]*nativeFunction), active: make(map[*procedure]bool)}
	for _, name := range []string{"axisReporters", "axisError"} {
		if g.compile(target, name) == nil {
			t.Errorf("%s still requires interpreted blocks", name)
		}
	}
	g.compileRegions(target, "axisMotion")
	for _, function := range g.functions {
		if function.code == "axisMotion" && function.region == "axisSetX" {
			return
		}
	}
	t.Fatal("axis loop body still requires interpreted blocks")
}

func testNativeAxisMotion(t *testing.T) {
	for _, hat := range []string{"axisMotionHat", "axisScript", "axisReportersHat", "axisErrorHat"} {
		for _, stage := range []bool{false, true} {
			for _, fencing := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stage=%v/fencing=%v", hat, stage, fencing), func(t *testing.T) {
					fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
					slow.native = false
					recorders := [2]*nativeDrawingRecorder{{}, {}}
					for i, r := range []*Runtime{fast, slow} {
						target := r.targets[1]
						target.state.IsStage = stage
						target.state.X, target.state.Y = 300-1e-10, -200+1e-10
						target.state.Direction, target.state.Size = -37.5, 10.6
						target.state.Pen.Down = true
						r.options.Fencing = fencing
						r.SetGraphics(recorders[i])
						r.startHat(target, hat, false)
					}
					for tick := range 8 {
						fastErr := fast.Run(context.Background(), 1, 60, false)
						slowErr := slow.Run(context.Background(), 1, 60, false)
						if fmt.Sprint(fastErr) != fmt.Sprint(slowErr) ||
							!reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) ||
							!reflect.DeepEqual(recorders[0], recorders[1]) ||
							fast.Done() != slow.Done() {
							t.Fatalf("tick %d: compiled motion differs: errors %v / %v", tick, fastErr, slowErr)
						}
						if hat == "axisErrorHat" {
							if fastErr == nil {
								t.Fatal("invalid input did not fail")
							}
							break
						}
					}
					if hat == "axisReportersHat" && fast.targets[1].variables["score"].Value != float64(-37.5) {
						t.Fatal("direction changed")
					}
				})
			}
		}
	}
}

func BenchmarkNativeAxisMotion(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, compiled := range []bool{false, true} {
		b.Run(fmt.Sprintf("compiled=%v", compiled), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			r.native, r.options.Fencing = compiled, false
			target := r.targets[1]
			target.variables["x"].Value = float64(1000)
			b.ReportAllocs()
			for b.Loop() {
				target.state.X, target.state.Y = 0, 0
				th := r.startHat(target, "axisMotionHat", false)
				if err := r.Run(context.Background(), 1, 60, false); err != nil {
					b.Fatal(err)
				}
				if !th.done || target.state.X != 250 || target.state.Y != -250 {
					b.Fatal("axis loop did not finish at the expected position")
				}
			}
		})
	}
}

func testNativeDrawing(t *testing.T) {
	for _, proc := range []string{"drawing", "drawingLoop", "drawingError", "stamp", "stampLoop"} {
		for _, fencing := range []bool{false, true} {
			for _, graphics := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/fencing=%v/graphics=%v", proc, fencing, graphics), func(t *testing.T) {
					var want *nativeDrawingRecorder
					var wantState []TargetState
					var wantError string
					var wantRedraw bool
					for _, compiled := range []bool{false, true} {
						r := New(nativeCodegenFixture(t))
						r.native = compiled
						r.options.Fencing = fencing
						if fencing {
							*r.targets[1].variables["x"] = Variable{Value: float64(1000)}
						}
						recorder := &nativeDrawingRecorder{}
						if graphics {
							r.SetGraphics(recorder)
						}
						r.startHat(r.targets[1], proc+"Hat", false)
						err := r.Run(context.Background(), 1, 60, false)
						if (err != nil) != (!graphics || proc == "drawingError") {
							t.Fatalf("unexpected error: %v", err)
						}
						p := r.targets[1].procedures[proc]
						if compiled && p.native == nil && p.regions["drawingSet"] == nil && p.regions["stampCall"] == nil {
							t.Fatal("drawing calculations fell back to interpretation")
						}
						if !compiled {
							want, wantState, wantError = recorder, r.Snapshot(), fmt.Sprint(err)
							wantRedraw = r.redrawRequested
							if graphics && (proc == "stamp" || proc == "stampLoop") {
								if len(want.commands) == 0 {
									t.Fatal("missing stamp")
								}
								state := want.commands[0].Target.State
								if state.CurrentCostume != 1 || state.Effects.Ghost != 25 || state.Size != 125 || state.Direction != 90 {
									t.Fatalf("unexpected stamp: %+v", state)
								}
								if !fencing && (state.X != 9 || state.Y != 4) {
									t.Fatalf("unexpected stamp position: %g,%g", state.X, state.Y)
								}
							} else if graphics && proc != "drawingError" {
								if len(want.commands) < 2 || want.commands[1].Size != 2.5 || want.scores[1] != float64(2) {
									t.Fatal("pen missed the preceding variable update")
								}
								if !fencing && (want.commands[1].Line[2] != 5 || want.commands[1].Line[3] != 2) {
									t.Fatal("unexpected line endpoints")
								}
							}
						} else if !reflect.DeepEqual(want, recorder) || !reflect.DeepEqual(wantState, r.Snapshot()) || wantError != fmt.Sprint(err) || wantRedraw != r.redrawRequested {
							t.Fatalf("compiled drawing changed commands, sprite state or error: %v", err)
						}
					}
				})
			}
		}
	}
}

func BenchmarkNativeDrawing(b *testing.B) {
	benchmarkNativeDrawing(b, "drawingLoop")
}

func BenchmarkNativeStamp(b *testing.B) {
	benchmarkNativeDrawing(b, "stampLoop")
}

func benchmarkNativeDrawing(b *testing.B, procedure string) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, compiled := range []bool{false, true} {
		b.Run(fmt.Sprintf("compiled=%v", compiled), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			r.native = compiled
			r.options.Fencing = false
			recorder := &surfaceRecorder{}
			r.graphics = recorder
			target := r.targets[1]
			b.ReportAllocs()
			for b.Loop() {
				*target.variables["x"] = Variable{Value: float64(1000)}
				*target.variables["score"] = Variable{Value: float64(0)}
				recorder.commands = recorder.commands[:0]
				th := r.startHat(target, procedure+"Hat", false)
				if err := r.Run(context.Background(), 1, 60, false); err != nil {
					b.Fatal(err)
				}
				if !th.done {
					b.Fatal("drawing did not finish")
				}
			}
		})
	}
}

func testNativeDrawingDebug(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		r := New(nativeCodegenFixture(t))
		r.native = compiled
		var output bytes.Buffer
		r.SetDebugWriter(&output)
		r.SetWindowed(true)
		r.startHat(r.targets[1], "drawingDebugHat", false)
		if err := r.Run(context.Background(), 1, 60, false); err != nil {
			t.Fatal(err)
		}
		if !r.Paused() || output.String() != "Sprite: native log\nbreakpoint in Sprite, paused (F8 resumes)\n" {
			t.Fatalf("native=%v: debug calls were skipped: %q", compiled, output.String())
		}
	}
}

func testNativeStampAllocations(t *testing.T) {
	r := New(nativeCodegenFixture(t))
	r.options.Fencing = false
	recorder := &surfaceRecorder{commands: make([]PenCommand, 0, 1000)}
	r.graphics = recorder
	target := r.targets[1]
	*target.variables["x"] = Variable{Value: float64(1000)}
	allocations := testing.AllocsPerRun(5, func() {
		recorder.commands = recorder.commands[:0]
		th := r.startHat(target, "stampLoopHat", false)
		if err := r.Run(context.Background(), 1, 60, false); err != nil {
			t.Fatal(err)
		}
		if !th.done || len(recorder.commands) != 1000 {
			t.Fatal("stamp loop did not complete")
		}
	})
	if allocations > 20 {
		t.Fatalf("1000 stamps allocated temporary execution states: %g", allocations)
	}
}

func testNativePenParameters(t *testing.T) {
	for _, tc := range []struct {
		name         string
		value, delta any
		want         float64
	}{
		{"color", "125", float64(-150), 75},
		{"saturation", "-10", float64(25), 25},
		{"brightness", "120", float64(-35), 65},
		{"transparency", "20", float64(150), 100},
		{"unknown", "50", float64(5), 0},
	} {
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			target := r.targets[1]
			target.state.Pen = PenState{}
			*target.variables["x"] = Variable{Value: tc.name}
			*target.variables["y"] = Variable{Value: tc.delta}
			*target.variables["score"] = Variable{Value: tc.value}
			r.startHat(target, "penParamsHat", false)
			if err := r.Run(context.Background(), 1, 60, false); err != nil {
				t.Fatal(err)
			}
			if compiled && target.procedures["penParams"].native == nil {
				t.Fatal("pen color parameters forced the procedure back to interpretation")
			}
			p := target.state.Pen
			got := map[string]float64{"color": p.Hue, "saturation": p.Saturation, "brightness": p.Brightness, "transparency": p.Transparency}[tc.name]
			if got != tc.want {
				t.Fatalf("%s native=%v: got %g, want %g", tc.name, compiled, got, tc.want)
			}
		}
	}
}

func testNativePenSize(t *testing.T) {
	for _, limits := range []bool{false, true} {
		for _, tc := range []struct {
			value, delta  any
			want, limited float64
		}{
			{"2.5", 0.25, 2.75, 2.75},
			{-5.0, 0.5, 0.5, 1.5},
			{5000.0, -10.0, 4990, 1190},
			{"invalid", 0.5, 0.5, 1.5},
			{math.NaN(), 2.0, 2, 3},
			{5.0, math.NaN(), 5, 5},
			{math.Inf(1), math.Inf(-1), math.NaN(), 1},
			{math.Copysign(0, -1), 0.0, 0, 1},
		} {
			for _, compiled := range []bool{false, true} {
				r := New(nativeCodegenFixture(t))
				r.native, r.options.MiscLimits = compiled, limits
				target := r.targets[1]
				target.variables["x"].Value, target.variables["y"].Value = tc.value, tc.delta
				th := r.startHat(target, "penSizesHat", false)
				if err := r.Run(context.Background(), 1, 60, false); err != nil {
					t.Fatal(err)
				}
				want := tc.want
				if limits {
					want = tc.limited
				}
				got := target.state.Pen.Size
				if !th.done || (math.Float64bits(got) != math.Float64bits(want) && !(math.IsNaN(got) && math.IsNaN(want))) {
					t.Fatalf("native=%v limits=%v size=%v delta=%v: got %g, want %g", compiled, limits, tc.value, tc.delta, got, want)
				}
				if compiled && target.procedures["penSizes"].native == nil {
					t.Fatal("pen sizes were not compiled")
				}
				before := target.state.Pen.Size
				r.startHat(target, "penSizeErrorHat", false)
				if err := r.Run(context.Background(), 1, 60, false); err == nil || math.Float64bits(target.state.Pen.Size) != math.Float64bits(before) {
					t.Fatalf("invalid size input changed the pen: %v", err)
				}
			}
		}
	}
}

func runNativePenSize(t testing.TB, r *Runtime) {
	t.Helper()
	target := r.targets[1]
	target.variables["score"].Value = float64(0)
	th := r.startHat(target, "penSizeBatchHat", false)
	if err := r.Run(context.Background(), 1, 60, false); err != nil {
		t.Fatal(err)
	}
	if !th.done || target.state.Pen.Size != 125.25 || target.variables["score"].Value != float64(125) {
		t.Fatal("pen size loop did not finish with the expected size and variable")
	}
}

func BenchmarkNativePenSize(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	r := New(nativeCodegenFixture(b))
	b.ReportAllocs()
	for b.Loop() {
		runNativePenSize(b, r)
	}
}

func testNativePenSizeAllocations(t *testing.T) {
	r := New(nativeCodegenFixture(t))
	allocations := testing.AllocsPerRun(5, func() { runNativePenSize(t, r) })
	if allocations > 20 {
		t.Fatalf("1000 pen size updates allocated %g times", allocations)
	}
}

func runNativePenParameters(t testing.TB, r *Runtime) {
	t.Helper()
	target := r.targets[1]
	*target.variables["score"] = Variable{Value: float64(0)}
	th := r.startHat(target, "penParamBatchHat", false)
	if err := r.Run(context.Background(), 1, 60, false); err != nil {
		t.Fatal(err)
	}
	if !th.done || target.state.Pen.Hue != 25 || target.variables["score"].Value != float64(125) {
		t.Fatal("pen parameter loop did not finish with the expected color and variable")
	}
}

func BenchmarkNativePenParameters(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, compiled := range []bool{false, true} {
		b.Run(fmt.Sprintf("compiled=%v", compiled), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			r.native = compiled
			b.ReportAllocs()
			for b.Loop() {
				runNativePenParameters(b, r)
			}
		})
	}
}

func testNativePenParameterLoop(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		r := New(nativeCodegenFixture(t))
		r.native = compiled
		allocations := testing.AllocsPerRun(5, func() { runNativePenParameters(t, r) })
		if compiled && allocations > 20 {
			t.Fatalf("pen parameter loop allocated %g times", allocations)
		}
	}
}

func testNativePenParameterError(t *testing.T) {
	var want string
	for _, compiled := range []bool{false, true} {
		r := New(nativeCodegenFixture(t))
		r.native = compiled
		target := r.targets[1]
		before := target.state.Pen
		r.startHat(target, "penParamErrorHat", false)
		err := r.Run(context.Background(), 1, 60, false)
		if err == nil {
			t.Fatal("unknown parameter skipped its invalid value input")
		}
		if !compiled {
			want = err.Error()
		}
		if err.Error() != want || target.state.Pen != before {
			t.Fatalf("native=%v changed error or pen state: %v", compiled, err)
		}
	}
}

func testNativePenMenus(t *testing.T) {
	for _, name := range []string{"penMenuNumber", "penMenuInput", "penMenuMissing", "penMenuEmpty"} {
		var wantError string
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			target := r.targets[1]
			r.startHat(target, name+"Hat", false)
			err := r.Run(context.Background(), 1, 60, false)
			if name == "penMenuNumber" {
				if err != nil || target.variables["score"].Value != "42" {
					t.Fatalf("%s native=%v: value=%v error=%v", name, compiled, target.variables["score"].Value, err)
				}
				continue
			}
			if err == nil {
				t.Fatalf("%s native=%v: malformed menu was accepted", name, compiled)
			}
			if !compiled {
				wantError = err.Error()
			}
			if err.Error() != wantError {
				t.Fatalf("%s: native error %v differs from %s", name, err, wantError)
			}
		}
	}
}

func runNativeDrawingDispatch(t testing.TB, r *Runtime) {
	t.Helper()
	target := r.targets[1]
	target.state.X, target.state.Y, target.state.Direction = 0, 0, 90
	target.variables["score"].Value = float64(0.125)
	target.variables["x"].Value = float64(1000)
	th := r.startHat(target, "directDrawingHat", false)
	if err := r.Run(context.Background(), 1, 60, false); err != nil {
		t.Fatal(err)
	}
	if !th.done || target.state.X != 125 || target.state.Y != 0 ||
		target.state.Direction != 90 || target.state.Size != 0.125 || target.state.Effects.Ghost != 0.125 {
		t.Fatalf("drawing loop did not finish with expected motion and effects: %+v", target.state)
	}
}

func testNativeDrawingDispatch(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		r := New(nativeCodegenFixture(t))
		r.native = compiled
		r.options.Fencing = false
		allocations := testing.AllocsPerRun(5, func() { runNativeDrawingDispatch(t, r) })
		if compiled && allocations > 20 {
			t.Fatalf("1000 drawing iterations allocated %g times", allocations)
		}
		p := r.targets[1].procedures["directDrawing"]
		if compiled && p.native == nil && p.regions["directMove"] == nil {
			t.Fatal("drawing loop was not compiled")
		}
	}
}

func BenchmarkNativeDrawingDispatch(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, compiled := range []bool{false, true} {
		b.Run(fmt.Sprintf("compiled=%v", compiled), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			r.native = compiled
			r.options.Fencing = false
			b.ReportAllocs()
			for b.Loop() {
				runNativeDrawingDispatch(b, r)
			}
		})
	}
}

func testNativeDrawingInputs(t *testing.T) {
	for _, value := range []any{-150.0, 150.0, 0.125, "25", "invalid", math.NaN(), math.Inf(1), math.Inf(-1)} {
		var want GraphicEffects
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			target := r.targets[1]
			target.variables["x"].Value = value
			r.startHat(target, "directEffectsHat", false)
			if err := r.Run(context.Background(), 1, 60, false); err != nil {
				t.Fatal(err)
			}
			if !compiled {
				want = target.state.Effects
			} else if target.procedures["directEffects"].native == nil || target.state.Effects != want {
				t.Fatalf("compiled effects differ for %v: got %+v, want %+v", value, target.state.Effects, want)
			}
		}
	}
	for _, name := range []string{"move", "left", "right", "direction", "size", "costume", "color", "effect"} {
		procedure := name + "Invalid"
		var wantError string
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			target := r.targets[1]
			before := target.state
			r.startHat(target, procedure+"Hat", false)
			err := r.Run(context.Background(), 1, 60, false)
			if err == nil || !reflect.DeepEqual(before, target.state) {
				t.Fatalf("%s native=%v: invalid input changed state or succeeded: %v", name, compiled, err)
			}
			if !compiled {
				wantError = err.Error()
			} else if target.procedures[procedure].native == nil || err.Error() != wantError {
				t.Fatalf("%s: compiled error = %v, want %s", name, err, wantError)
			}
		}
	}
}
