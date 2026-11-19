package vm

import (
	"context"
	"gowarp/core/project"
	"math"
	"strings"
	"testing"
)

func TestLogicExtensionReporters(t *testing.T) {
	tests := []struct {
		op     string
		args   map[string]any
		fields map[string][]any
		want   any
	}{
		{"strings_split", map[string]any{"STRING": "AxxBXXc", "SPLIT": "xx", "ITEM": "2"}, nil, "B"},
		{"strings_split", map[string]any{"STRING": "a,b", "SPLIT": ",", "ITEM": "1.5"}, nil, ""},
		{"strings_replace", map[string]any{"STRING": "a.A.", "SUBSTRING": "a.", "REPLACE": "[$&]"}, nil, "[a.][A.]"},
		{"strings_toCase", map[string]any{"STRING": "AbC가", "TEXTCASE": "lowercase"}, nil, "abc가"},
		{"strings_unicodeof", map[string]any{"STRING": "A😀가"}, nil, "65 55357 44032"},
		{"truefantommath_less_or_equal_block", map[string]any{"A": "Apple", "B": "apple"}, nil, true},
		{"truefantommath_more_or_equal_block", map[string]any{"A": "20", "B": "3"}, nil, true},
		{"percentcalc_calculatePercent", map[string]any{"n": "200", "n2": "25"}, nil, float64(12.5)},
		{"percentcalc_calculatePercent", map[string]any{"n": "0", "n2": "25"}, nil, float64(0)},
		{"percentExtension_convertToPercent", map[string]any{"VALUE": "0.125", "DECIMALS": "2"}, nil, "12.50%"},
		{"hexconverter_decToHex", map[string]any{"NUM": "255.5"}, map[string][]any{"CASE": {"대문자"}}, "FF.8"},
		{"darkencolorext_darkenColor", map[string]any{"COLOR": "#f80", "AMOUNT": "50"}, nil, "7f4400"},
		{"sipctime_formatTime", map[string]any{"VALUE": "3883.2", "ROUND": "rounded"}, nil, "01:04:43"},
		{"sipctime_formatTime", map[string]any{"VALUE": "59.8", "ROUND": "rounded"}, nil, "00:00:60"},
		{"sipctime_formatTime", map[string]any{"VALUE": "3883.2", "ROUND": "precise"}, nil, "01:04:43.200"},
		{"nkmoremotion_directionto2", map[string]any{"X": "10", "Y": "0"}, nil, float64(90)},
		{"jeremygamerTweening_tweenValue", map[string]any{"MODE": "quad", "DIRECTION": "in", "START": "10", "END": "30", "AMOUNT": "50"}, nil, float64(15)},
		{"jeremygamerTweening_tweenValue", map[string]any{"MODE": "linear", "DIRECTION": "in", "START": "10", "END": "30", "AMOUNT": "150"}, nil, float64(40)},
	}
	for _, tc := range tests {
		t.Run(tc.op, func(t *testing.T) {
			r := motionRuntime(t, `{}`)
			r.targets[1].state.X = 0
			r.targets[1].state.Y = 0
			got, err := evalBlock(t, r, 1, &project.Block{Opcode: tc.op, Inputs: literalInputs(tc.args), Fields: tc.fields})
			if err != nil || got != tc.want {
				t.Fatalf("got %#v want %#v: %v", got, tc.want, err)
			}
		})
	}
}

func TestLogicTimersPauseSetAndRemove(t *testing.T) {
	r := listRuntime(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"start"},"start":{"opcode":"lmsTimers_startResetTimer","next":"wait","inputs":{"TIMER":[1,[10,"song"]]}},"wait":{"opcode":"control_wait","next":"pause","inputs":{"DURATION":[1,[4,"0.1"]]}},"pause":{"opcode":"lmsTimers_pauseTimer","inputs":{"TIMER":[1,[10,"song"]]}}}`, `[]`)
	if err := r.Run(context.Background(), 10, 30, false); err != nil {
		t.Fatal(err)
	}
	b := &project.Block{Opcode: "lmsTimers_valueOfTimer", Inputs: literalInputs(map[string]any{"TIMER": "song"})}
	got, err := evalBlock(t, r, 0, b)
	if err != nil || math.Abs(number(got)-.1) > .001 {
		t.Fatalf("timer %v: %v", got, err)
	}
	r.elapsed += 20
	again, _ := evalBlock(t, r, 0, b)
	if got != again {
		t.Fatal("paused timer advanced")
	}
}

func TestLogicTweenSuspendsAndResumes(t *testing.T) {
	r := listRuntime(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"tween"},"tween":{"opcode":"jeremygamerTweening_tweenVariable","next":"done","fields":{"VAR":["score",null]},"inputs":{"VALUE":[1,[4,"100"]],"SEC":[1,[4,"0.2"]],"MODE":[1,[10,"quad"]],"DIRECTION":[1,[10,"in"]]}},"done":{"opcode":"data_addtolist","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[10,"done"]]}}}`, `[]`)
	if err := r.Run(context.Background(), 1, 30, true); err != nil {
		t.Fatal(err)
	}
	if number(r.stage.variables["score"].Value) != 0 || len(r.stage.state.Lists["items"].Items) != 0 {
		t.Fatal("tween did not suspend")
	}
	if err := r.Run(context.Background(), 3, 30, true); err != nil {
		t.Fatal(err)
	}
	if math.Abs(number(r.stage.variables["score"].Value)-25) > 1e-8 {
		t.Fatalf("midpoint %v", r.stage.variables["score"].Value)
	}
	if err := r.Run(context.Background(), 5, 30, true); err != nil {
		t.Fatal(err)
	}
	if number(r.stage.variables["score"].Value) != 100 || len(r.stage.state.Lists["items"].Items) != 1 {
		t.Fatal("tween failed to finish")
	}
}

func TestLogicCloneDeletionKeepsOtherSprites(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"A"},{"name":"B"}]`)
	a, b := r.targets[1], r.targets[2]
	r.createClone(a)
	r.createClone(a)
	r.createClone(b)
	clone := r.targets[3]
	got, err := evalBlock(t, r, 3, &project.Block{Opcode: "lmsclonesplus_isClone"})
	if err != nil || got != true {
		t.Fatalf("clone check %v %v", got, err)
	}
	_, handled, err := nativeTestCall(t, r, &thread{target: a}, &project.Block{Opcode: "lmsclonesplus_deleteClonesInSprite"}, dataArguments(map[string]any{"INPUT": "A"}), false)
	if !handled || err != nil || r.cloneCount != 1 || len(r.targets) != 4 || !clone.dead || r.targets[3].source != b.source {
		t.Fatalf("delete clones %d %d %v", r.cloneCount, len(r.targets), err)
	}
}

func TestLogicTimerSetResumesAndRemovalIsEmpty(t *testing.T) {
	r := motionRuntime(t, `{}`)
	th := &thread{target: r.targets[1]}
	call := func(op string, values map[string]any) {
		t.Helper()
		_, handled, err := nativeTestCall(t, r, th, &project.Block{Opcode: op}, dataArguments(values), false)
		if !handled || err != nil {
			t.Fatalf("%s %v", op, err)
		}
	}
	read := func(name string) any {
		t.Helper()
		result, _, err := nativeTestCall(t, r, th, &project.Block{Opcode: "lmsTimers_valueOfTimer"}, dataArguments(map[string]any{"TIMER": name}), true)
		if err != nil {
			t.Fatal(err)
		}
		return result.Value
	}
	call("lmsTimers_setTimer", map[string]any{"TIMER": "a", "NUM": float64(10)})
	r.elapsed = 2
	call("lmsTimers_pauseTimer", map[string]any{"TIMER": "a"})
	r.elapsed = 5
	if read("a") != float64(12) {
		t.Fatal("pause did not preserve value")
	}
	call("lmsTimers_setTimer", map[string]any{"TIMER": "a", "NUM": float64(4)})
	r.elapsed = 7
	if read("a") != float64(6) {
		t.Fatal("set did not resume")
	}
	call("lmsTimers_removeTimer", map[string]any{"TIMER": "a"})
	if read("a") != "" {
		t.Fatal("missing timer not empty")
	}
	call("lmsTimers_startResetTimer", map[string]any{"TIMER": "b"})
	call("lmsTimers_removeTimers", nil)
	if read("b") != "" {
		t.Fatal("clear left a timer")
	}
}

func TestLogicCostumeNameAndWrappedNumber(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"Sprite","costumes":[{"name":"2","rotationCenterX":12},{"name":"other","rotationCenterX":34}]}]`)
	for _, tc := range []struct {
		input any
		want  any
	}{{"2", float64(12)}, {float64(2), float64(34)}, {float64(3), float64(12)}, {"missing", float64(0)}} {
		result, _, err := nativeTestCall(t, r, &thread{target: r.targets[1]}, &project.Block{Opcode: "lmsLooksPlus_costumeAttribute", Fields: map[string][]any{"ATTRIBUTE": {"rotationCenterX"}}}, dataArguments(map[string]any{"COSTUME": tc.input}), true)
		v := result.Value
		if err != nil || v != tc.want {
			t.Fatalf("%v: got %v want %v (%v)", tc.input, v, tc.want, err)
		}
	}
}

func TestLogicTweenEasingCurves(t *testing.T) {
	for _, tc := range []struct {
		mode, dir string
		x, want   float64
	}{{"sine", "in", .5, .2928932188134524}, {"expo", "out", .5, .96875}, {"circ", "in out", .25, .0669872981077807}, {"cubic", "in", .5, .125}, {"quart", "out", .5, .9375}, {"quint", "in out", .25, .015625}, {"back", "in out", .25, -.09968184375}, {"bounce", "out", .5, .765625}, {"elastic", "out", .5, 1.015625}} {
		r := motionRuntime(t, `{}`)
		result, handled, err := nativeTestCall(t, r, &thread{target: r.targets[1]}, &project.Block{Opcode: "jeremygamerTweening_tweenValue"}, dataArguments(map[string]any{"MODE": tc.mode, "DIRECTION": tc.dir, "START": 0.0, "END": 1.0, "AMOUNT": tc.x * 100}), true)
		got := number(result.Value)
		if !handled || err != nil || math.Abs(got-tc.want) > 1e-12 {
			t.Fatalf("%s %s: %v want %v", tc.mode, tc.dir, got, tc.want)
		}
	}
}

func TestLogicZeroDurationPropertyTweenHonorsRedraw(t *testing.T) {
	r := motionRuntime(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"tween"},"tween":{"opcode":"jeremygamerTweening_tweenProperty","inputs":{"PROPERTY":[1,[10,"y position"]],"VALUE":[1,[4,"50"]],"SEC":[1,[4,"0"]],"MODE":[1,[10,"linear"]],"DIRECTION":[1,[10,"in"]]}}}`)
	run(t, r, 1, false)
	if r.targets[1].state.Y != 0 {
		t.Fatal("zero duration tween did not yield")
	}
	run(t, r, 1, false)
	if r.targets[1].state.Y != 50 {
		t.Fatal("zero duration tween did not finish")
	}
}

func TestLogicPropertyTweenRequestsRedrawOnFirstTurn(t *testing.T) {
	r := motionRuntime(t, `{
		"a":{"opcode":"event_whenflagclicked","topLevel":true,"next":"tween"},
		"tween":{"opcode":"jeremygamerTweening_tweenProperty","inputs":{"PROPERTY":[1,[10,"y position"]],"VALUE":[1,[4,"100"]],"SEC":[1,[4,"1"]],"MODE":[1,[10,"linear"]],"DIRECTION":[1,[10,"in"]]}},
		"b":{"opcode":"event_whenflagclicked","topLevel":true,"next":"forever"},
		"forever":{"opcode":"control_forever","inputs":{"SUBSTACK":[2,"inc"]}},
		"inc":{"opcode":"control_incr_counter"}
	}`)
	for frame := 0; frame < 3; frame++ {
		if err := r.Run(context.Background(), 1, 60, false); err != nil {
			t.Fatal(err)
		}
		if r.counter != float64(frame+1) || !r.redrawRequested {
			t.Fatalf("frame %d: tween permitted extra passes: counter=%g redraw=%v", frame, r.counter, r.redrawRequested)
		}
		if math.Abs(r.targets[1].state.Y-float64(frame)*100/60) > 1e-9 {
			t.Fatal("first-turn redraw changed tween progress")
		}
	}
}

func TestLogicZeroVariableTweenFinishesWithoutExtraTick(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"tween"},
		"tween":{"opcode":"jeremygamerTweening_tweenVariable","next":"inc","fields":{"VAR":["score",null]},"inputs":{"VALUE":[1,[4,"100"]],"SEC":[1,[4,"0"]],"MODE":[1,[10,"linear"]],"DIRECTION":[1,[10,"in"]]}},
		"inc":{"opcode":"control_incr_counter"}
	}`)
	run(t, r, 1, false)
	if r.counter != 1 || r.stage.variables["score"].Value != float64(100) {
		t.Fatal("zero-duration variable tween inserted an extra tick")
	}
}

func TestLogicSplitMovementReturnAndPersistence(t *testing.T) {
	r := motionRuntime(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"split"},"split":{"opcode":"edgetalesplit_addVerticalSplit","inputs":{"X":[1,[4,"12"]],"Y":[1,[4,"34"]],"WIDTH":[1,[4,"5"]],"OFFSET":[1,[4,"20"]],"DURATION":[1,[4,"2"]],"EASING":[1,[10,"되돌리기"]],"DIRECTION":[1,[10,"vertical"]],"RETURN":[1,[10,"no"]],"WAIT":[1,[4,"0"]],"RETURN_EASING":[1,[10,"sin"]],"ANGLE":[1,[4,"0"]]}}}`)
	run(t, r, 1, false)
	// Read through the public renderer API after command execution.
	effects := r.SplitEffects()
	if len(effects) != 1 || effects[0].Project(480, 360, 1).TopX != 252 || effects[0].Project(480, 360, 1).MoveX != 0 {
		t.Fatalf("split state %+v", effects)
	}
	r.elapsed = 1
	if err := r.extensionEvent("tick", r.stage); err != nil {
		t.Fatal(err)
	}
	effects = r.SplitEffects()
	if effects[0].Project(480, 360, 1).MoveY != 22 {
		t.Fatalf("eased split %+v", effects[0])
	}
	r.elapsed = 10
	if err := r.extensionEvent("tick", r.stage); err != nil {
		t.Fatal(err)
	}
	if effects = r.SplitEffects(); len(effects) != 1 || effects[0].Project(480, 360, 1).MoveY != 20 {
		t.Fatal("nonreturning split must persist")
	}
}

func TestLogicPercentUsesJavaScriptTieRounding(t *testing.T) {
	r := motionRuntime(t, `{}`)
	for _, tc := range []struct {
		value string
		want  string
	}{{"0.025", "3%"}, {"-0.025", "-3%"}} {
		got, err := evalBlock(t, r, 1, &project.Block{Opcode: "percentExtension_convertToPercent", Inputs: literalInputs(map[string]any{"VALUE": tc.value, "DECIMALS": "0"})})
		if err != nil || got != tc.want {
			t.Fatalf("got %v want %v: %v", got, tc.want, err)
		}
	}
}

func TestLogicCloneDeletionResolvesLiteralSpriteNames(t *testing.T) {
	r := motionRuntime(t, `{}`)
	r.createClone(r.targets[1])
	_, _, err := nativeTestCall(t, r, &thread{target: r.targets[1]}, &project.Block{Opcode: "lmsclonesplus_deleteClonesInSprite"}, dataArguments(map[string]any{"INPUT": "_myself_"}), false)
	if err != nil || r.cloneCount != 1 {
		t.Fatal("Clones Plus treats its input as a sprite name, not the core clone menu")
	}
}

func TestLogicTweenInsideReporterProcedure(t *testing.T) {
	blocks := strings.Replace(reporterProcedureProject, `"next":"return","inputs":{"custom_block"`, `"next":"tween","inputs":{"custom_block"`, 1)
	blocks = strings.Replace(blocks, `"return":`, `"tween":{"opcode":"jeremygamerTweening_tweenVariable","next":"return","fields":{"VAR":["score",null]},"inputs":{"VALUE":[1,[4,"10"]],"SEC":[1,[4,"0.1"]],"MODE":[1,[10,"linear"]],"DIRECTION":[1,[10,"in"]]}},"return":`, 1)
	r := fixture(t, blocks)
	run(t, r, 1, true)
	if number(r.stage.variables["score"].Value) != 0 {
		t.Fatal("reporter tween completed immediately")
	}
	run(t, r, 5, true)
	if !r.Done() || number(r.stage.variables["score"].Value) != 50 {
		t.Fatal("reporter tween lost continuation")
	}
}

func TestLogicCloneDeletionUsesFirstNamedOriginal(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"Same"},{"name":"Same"}]`)
	r.createClone(r.targets[1])
	r.createClone(r.targets[2])
	firstClone, secondClone := r.targets[3], r.targets[4]
	_, handled, err := nativeTestCall(t, r, &thread{target: r.targets[1]}, &project.Block{Opcode: "lmsclonesplus_deleteClonesInSprite"}, dataArguments(map[string]any{"INPUT": "Same"}), false)
	if !handled || err != nil || !firstClone.dead || secondClone.dead || r.cloneCount != 1 {
		t.Fatalf("named original selection changed: first=%v second=%v count=%d err=%v", firstClone.dead, secondClone.dead, r.cloneCount, err)
	}
}

type costumeMetadataGraphics struct {
	Graphics
	width, height float64
}

func (g *costumeMetadataGraphics) CostumeSize(DisplayTarget) (float64, float64) {
	return g.width, g.height
}

func TestLogicCostumeDimensionsUseScratchMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, format          string
		resolution            float64
		width, height         float64
		wantWidth, wantHeight float64
	}{
		{"bitmap glyph", "png", 2, 4, 5, 8, 10},
		{"normalized bitmap", "png", 1, 8, 10, 16, 20},
		{"vector", "svg", 1, 4.25, 5.25, 5, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"Sprite","costumes":[{"name":"glyph"}]}]`)
			meta := &r.targets[1].source.Costumes[0]
			meta.DataFormat, meta.BitmapResolution = tc.format, tc.resolution
			g := &costumeMetadataGraphics{width: tc.width, height: tc.height}
			r.graphics = g
			for _, attribute := range []struct {
				name string
				want float64
			}{{"width", tc.wantWidth}, {"height", tc.wantHeight}} {
				result, _, err := nativeTestCall(t, r, &thread{target: r.targets[1]}, &project.Block{Opcode: "lmsLooksPlus_costumeAttribute", Fields: map[string][]any{"ATTRIBUTE": {attribute.name}}}, dataArguments(map[string]any{"COSTUME": "glyph"}), true)
				if err != nil || result.Value != attribute.want {
					t.Fatalf("%s = %v/%v, want %v", attribute.name, result.Value, err, attribute.want)
				}
			}
		})
	}
}
