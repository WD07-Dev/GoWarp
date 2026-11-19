package vm

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"gowarp/core/project"
)

func motionRuntime(t *testing.T, blocks string) *Runtime {
	t.Helper()
	return eventProject(t, `[{"isStage":true,"name":"Stage","variables":{"score":["score",0]},"blocks":{}},{"name":"Sprite","x":0,"y":0,"direction":90,"size":100,"visible":true,"blocks":`+blocks+`}]`)
}

type fractionalFenceGraphics struct {
	Graphics
}

func (g *fractionalFenceGraphics) Bounds(t DisplayTarget, tight bool) (Bounds, bool) {
	return Bounds{Left: t.State.X - .2, Right: t.State.X + 1.8, Bottom: t.State.Y - .2, Top: t.State.Y + 1.8}, true
}

func TestFractionalPositionFencing(t *testing.T) {
	for _, highQuality := range []bool{true, false} {
		r := motionRuntime(t, `{}`)
		r.ConfigureRendering(highQuality, !highQuality)
		r.graphics = &fractionalFenceGraphics{}
		target := r.targets[1]
		target.state.X, target.state.Y = .4, .4
		target.setXY(1000, 1000)
		if target.state.X != 239 || target.state.Y != 179 {
			t.Fatalf("highQuality=%v: fractional fencing used rounded origin: %g,%g", highQuality, target.state.X, target.state.Y)
		}
	}
}

const motionProject = `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"goto"},
 "goto":{"opcode":"motion_gotoxy","next":"move","inputs":{"X":[1,[4,"10"]],"Y":[1,[4,"20"]]}},
 "move":{"opcode":"motion_movesteps","next":"left","inputs":{"STEPS":[1,[4,"5"]]}},
 "left":{"opcode":"motion_turnleft","next":"up","inputs":{"DEGREES":[1,[4,"90"]]}},
 "up":{"opcode":"motion_movesteps","next":"right","inputs":{"STEPS":[1,[4,"10"]]}},
 "right":{"opcode":"motion_turnright","next":"dx","inputs":{"DEGREES":[1,[4,"450"]]}},
 "dx":{"opcode":"motion_changexby","next":"dy","inputs":{"DX":[1,[4,"2"]]}},
 "dy":{"opcode":"motion_changeyby","next":"style","inputs":{"DY":[1,[4,"-5"]]}},
 "style":{"opcode":"motion_setrotationstyle","fields":{"STYLE":["don't rotate",null]}}
}`

func TestMotionSequence(t *testing.T) {
	r := motionRuntime(t, motionProject)
	run(t, r, 1, false)
	s := r.Snapshot()[1]
	if s.X != 17 || s.Y != 25 || s.Direction != 90 || s.RotationStyle != "don't rotate" {
		t.Fatalf("bad motion: %+v", s)
	}
}

func TestDirectionWrapAndCoordinateReporters(t *testing.T) {
	for _, tc := range []struct {
		direction, want string
	}{{"-180", "180"}, {"540", "180"}, {"-179.5", "180.5"}, {"Infinity", "90"}} {
		blocks := `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"point"},"point":{"opcode":"motion_pointindirection","next":"set","inputs":{"DIRECTION":[1,[4,"` + tc.direction + `"]]}},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"dir"]}},"dir":{"opcode":"motion_direction"}}`
		r := motionRuntime(t, blocks)
		run(t, r, 1, false)
		if text(r.Snapshot()[0].Variables["score"].Value) != tc.want {
			t.Fatalf("direction %s", tc.direction)
		}
	}
	for _, axis := range []string{"x", "y"} {
		blocks := `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"pos"},"pos":{"opcode":"motion_set` + axis + `","next":"set","inputs":{"` + strings.ToUpper(axis) + `":[1,[4,"-0.0000000001"]]}},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"position"]}},"position":{"opcode":"motion_` + axis + `position"}}`
		r := motionRuntime(t, blocks)
		run(t, r, 1, false)
		value := r.Snapshot()[0].Variables["score"].Value.(float64)
		if value != 0 || !math.Signbit(value) {
			t.Fatalf("%s precision not preserved: %v", axis, value)
		}
	}
}

func TestSizeVisibilityAndCloneState(t *testing.T) {
	r := motionRuntime(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"size"},
 "size":{"opcode":"looks_setsizeto","next":"change","inputs":{"SIZE":[1,[4,"125.2"]]}},
 "change":{"opcode":"looks_changesizeby","next":"hide","inputs":{"CHANGE":[1,[4,"0.4"]]}},
 "hide":{"opcode":"looks_hide","next":"clone"},
 "clone":{"opcode":"control_create_clone_of","next":"show","inputs":{"CLONE_OPTION":[1,[10,"_myself_"]]}},
 "show":{"opcode":"looks_show","next":"report"},
 "report":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"sizeReporter"]}},
 "sizeReporter":{"opcode":"looks_size"}}`)
	run(t, r, 1, false)
	s := r.Snapshot()
	if len(s) != 3 || !s[1].Visible || s[2].Visible || math.Abs(s[2].Size-125.6) > 1e-9 || number(s[0].Variables["score"].Value) != 126 {
		t.Fatalf("bad appearance: %+v", s)
	}
}

func TestStagePositionAndSizeAreUnchanged(t *testing.T) {
	r := fixture(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"move"},"move":{"opcode":"motion_gotoxy","next":"size","inputs":{"X":[1,[4,"10"]],"Y":[1,[4,"20"]]}},"size":{"opcode":"looks_setsizeto","inputs":{"SIZE":[1,[4,"200"]]}}}`)
	run(t, r, 1, false)
	s := r.Snapshot()[0]
	if s.X != 0 || s.Y != 0 || s.Size != 100 {
		t.Fatal("stage moved or resized")
	}
}

func TestNamedDestinationsAndMissingSprite(t *testing.T) {
	data := `[{"isStage":true,"name":"Destination"},{"name":"Destination","x":30,"y":40},{"name":"Mover","blocks":{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"point"},
 "point":{"opcode":"motion_pointtowards","next":"goto","inputs":{"TOWARDS":[1,"pointMenu"]}},
 "pointMenu":{"opcode":"motion_pointtowards_menu","fields":{"TOWARDS":["Destination",null]}},
 "goto":{"opcode":"motion_goto","next":"missing","inputs":{"TO":[1,"gotoMenu"]}},
 "gotoMenu":{"opcode":"motion_goto_menu","fields":{"TO":["Destination",null]}},
 "missing":{"opcode":"motion_goto","inputs":{"TO":[1,[10,"absent"]]}}
 }}]`
	r := eventProject(t, data)
	run(t, r, 1, false)
	s := r.Snapshot()[2]
	if s.X != 30 || s.Y != 40 || math.Abs(s.Direction-36.86989764584402) > 1e-12 {
		t.Fatalf("bad destination: %+v", s)
	}
}

func TestRandomDestinationAndDefaultMouse(t *testing.T) {
	blocks := `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"goto"},"goto":{"opcode":"motion_goto","inputs":{"TO":[1,[10,"_random_"]]}}}`
	r := motionRuntime(t, blocks)
	run(t, r, 1, false)
	s := r.Snapshot()[1]
	if s.X < -240 || s.X > 240 || s.Y < -180 || s.Y > 180 || math.Trunc(s.X) != s.X || math.Trunc(s.Y) != s.Y {
		t.Fatal("random destination out of bounds")
	}
	r = motionRuntime(t, strings.Replace(blocks, "_random_", "_mouse_", 1))
	run(t, r, 1, false)
	if s := r.Snapshot()[1]; s.X != 0 || s.Y != 0 {
		t.Fatal("default mouse position")
	}
}

func TestNonfiniteMotionJSON(t *testing.T) {
	r := motionRuntime(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"pos"},"pos":{"opcode":"motion_setx","next":"size","inputs":{"X":[1,[4,"Infinity"]]}},"size":{"opcode":"looks_setsizeto","inputs":{"SIZE":[1,[4,"Infinity"]]}}}`)
	run(t, r, 1, false)
	data, err := json.Marshal(r.Snapshot())
	if err != nil {
		t.Fatalf("state not serializable: %v", err)
	}
	if !strings.Contains(string(data), `"x":"Infinity"`) || !strings.Contains(string(data), `"size":"Infinity"`) {
		t.Fatalf("nonfinite values lost: %s", data)
	}
}

func TestMiscLimitsPenSizeAndMousePrecision(t *testing.T) {
	for _, tc := range []struct {
		limits              bool
		size                string
		wantSize, wantMouse float64
	}{
		{true, "0", 1, -1},
		{false, "0", 0, -1.235},
		{true, "1500", 1200, -1},
		{false, "1500", 1500, -1.235},
	} {
		r := motionRuntime(t, `{
          "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"size"},
          "size":{"opcode":"pen_setPenSizeTo","next":"mouse","inputs":{"SIZE":[1,[4,"`+tc.size+`"]]}},
          "mouse":{"opcode":"motion_goto","inputs":{"TO":[1,[10,"_mouse_"]]}}
        }`)
		r.options.MiscLimits = tc.limits
		r.applyInput(InputEvent{Kind: "mouse", X: -1.2346, Y: -1.2346})
		run(t, r, 1, false)
		s := r.Snapshot()[1]
		if s.Pen.Size != tc.wantSize || s.X != tc.wantMouse || s.Y != tc.wantMouse {
			t.Errorf("limits=%v, size=%s: pen=%g, mouse=%g,%g", tc.limits, tc.size, s.Pen.Size, s.X, s.Y)
		}
	}
}

func TestPreparedCoordinatesMatchInterpreter(t *testing.T) {
	fast, ft := coordinateMotionRuntime()
	slow, st := coordinateMotionRuntime()
	slow.coordinateMoves = map[*project.Block]*coordinateMove{st.target.source.Blocks["move"]: nil}
	for _, value := range []any{12.5, -3.75, "6.25", "", "hello", true, math.NaN(), math.Inf(1)} {
		for _, pair := range []struct {
			r  *Runtime
			th *thread
		}{{fast, ft}, {slow, st}} {
			pair.th.params["x"] = calculationValue{value: value}
			pair.th.pc, pair.th.done = "move", false
			if err := pair.r.step(context.Background(), pair.th); err != nil {
				t.Fatal(err)
			}
		}
		if ft.target.state.X != st.target.state.X || ft.target.state.Y != st.target.state.Y {
			t.Fatalf("value %v: prepared %g,%g, interpreter %g,%g", value,
				ft.target.state.X, ft.target.state.Y, st.target.state.X, st.target.state.Y)
		}
	}
}

func TestCoordinateReporterWaitKeepsEarlierInput(t *testing.T) {
	r := motionRuntime(t, `{
	 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"move"},
	 "move":{"opcode":"motion_gotoxy","inputs":{"X":[3,[12,"score","score"],[4,"0"]],"Y":[2,"call"]}},
	 "call":{"opcode":"procedures_call","mutation":{"proccode":"delayed"}},
	 "def":{"opcode":"procedures_definition","topLevel":true,"next":"wait","inputs":{"custom_block":[1,"proto"]}},
	 "proto":{"opcode":"procedures_prototype","mutation":{"proccode":"delayed","argumentids":"[]","argumentnames":"[]","argumentdefaults":"[]","warp":"false"}},
	 "wait":{"opcode":"control_wait","next":"return","inputs":{"DURATION":[1,[4,"0"]]}},
	 "return":{"opcode":"procedures_return","inputs":{"VALUE":[1,[4,"7.5"]]}}
	}`)
	*r.stage.variables["score"] = Variable{Value: 12.5}
	run(t, r, 1, false)
	if r.targets[1].state.X != 0 || r.targets[1].state.Y != 0 {
		t.Fatal("coordinate move ran before its reporter returned")
	}
	*r.stage.variables["score"] = Variable{Value: 99.0}
	run(t, r, 1, false)
	if r.targets[1].state.X != 12.5 || r.targets[1].state.Y != 7.5 {
		t.Fatalf("reporter resume changed earlier coordinate: %g,%g", r.targets[1].state.X, r.targets[1].state.Y)
	}
}
