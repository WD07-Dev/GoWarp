package vm

import (
	"math"
	"strings"
	"testing"
)

const glideProject = `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"glide"},
 "glide":{"opcode":"motion_glidesecstoxy","next":"report","inputs":{"SECS":[1,[4,"0.1"]],"X":[1,[4,"30"]],"Y":[1,[4,"-15"]]}},
 "report":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"x"]}},
 "x":{"opcode":"motion_xposition"}}
`

func TestGlideInterpolationAndContinuation(t *testing.T) {
	for _, turbo := range []bool{false, true} {
		r := motionRuntime(t, glideProject)
		run(t, r, 1, turbo)
		if r.Snapshot()[1].X != 0 || r.Done() {
			t.Fatal("glide jumped or completed immediately")
		}
		run(t, r, 1, turbo)
		s := r.Snapshot()[1]
		if math.Abs(s.X-10) > 1e-9 || math.Abs(s.Y+5) > 1e-9 {
			t.Fatalf("bad intermediate position: %+v", s)
		}
		run(t, r, 2, turbo)
		s = r.Snapshot()[1]
		if !r.Done() || s.X != 30 || s.Y != -15 || number(r.Snapshot()[0].Variables["score"].Value) != 30 {
			t.Fatal("glide did not finish/resume")
		}
	}
}

func TestGlideNonpositiveAndMissingDestination(t *testing.T) {
	for _, duration := range []string{"0", "-1"} {
		r := motionRuntime(t, strings.Replace(glideProject, `"0.1"`, `"`+duration+`"`, 1))
		run(t, r, 1, false)
		if !r.Done() || r.Snapshot()[1].X != 30 {
			t.Fatal("nonpositive glide yielded")
		}
	}
	r := motionRuntime(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"glide"},"glide":{"opcode":"motion_glideto","inputs":{"SECS":[1,[4,"10"]],"TO":[1,[10,"missing"]]}}}`)
	run(t, r, 1, false)
	if !r.Done() || r.Snapshot()[1].X != 0 {
		t.Fatal("missing destination stalled")
	}
}

func TestGlideCapturesDestinationOnce(t *testing.T) {
	// The mover must capture the destination before its green-flag script moves it.
	r := eventProject(t, `[
 {"isStage":true},
 {"name":"Mover","layerOrder":2,"blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"glide"},"glide":{"opcode":"motion_glideto","inputs":{"SECS":[1,[4,"0.1"]],"TO":[1,"menu"]}},"menu":{"opcode":"motion_glideto_menu","fields":{"TO":["Destination",null]}}}},
 {"name":"Destination","layerOrder":1,"x":30,"y":15,"blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"move"},"move":{"opcode":"motion_setx","inputs":{"X":[1,[4,"100"]]}}}}
 ]`)
	run(t, r, 4, true)
	s := r.Snapshot()
	if !r.Done() || s[1].X != 30 || s[1].Y != 15 || s[2].X != 100 {
		t.Fatalf("glide chased moving destination: %+v", s)
	}
}

func TestCloneDuringGlideCopiesPositionOnly(t *testing.T) {
	// Save the glide hat first so it advances before the clone is created.
	blocks := strings.Replace(glideProject, `"glide":`, `"zFlag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait"},"wait":{"opcode":"control_wait","next":"clone","inputs":{"DURATION":[1,[4,"0"]]}},"clone":{"opcode":"control_create_clone_of","inputs":{"CLONE_OPTION":[1,[10,"_myself_"]]}},"glide":`, 1)
	r := motionRuntime(t, blocks)
	run(t, r, 4, false)
	s := r.Snapshot()
	if len(s) != 3 || s[1].X != 30 || math.Abs(s[2].X-10) > 1e-9 {
		t.Fatal("clone inherited glide continuation")
	}
}

func TestWarpProcedureGlideResumesArguments(t *testing.T) {
	blocks := strings.Replace(procedureProject, `"warp":"false"`, `"warp":"true"`, 1)
	blocks = strings.Replace(blocks, `"next":"repeat"`, `"next":"glide"`, 1)
	blocks = strings.Replace(blocks, `"repeat":`, `"glide":{"opcode":"motion_glidesecstoxy","next":"repeat","inputs":{"SECS":[1,[4,"0.1"]],"X":[2,"arg"],"Y":[1,[4,"0"]]}},"repeat":`, 1)
	r := motionRuntime(t, blocks)
	run(t, r, 3, true)
	if number(r.Snapshot()[0].Variables["score"].Value) != 0 {
		t.Fatal("warp skipped glide")
	}
	run(t, r, 1, true)
	if !r.Done() || r.Snapshot()[1].X != 3 || number(r.Snapshot()[0].Variables["score"].Value) != 109 {
		t.Fatal("glide lost procedure continuation")
	}
}

func TestBroadcastRestartsGlide(t *testing.T) {
	r := eventProject(t, `[
 {"isStage":true,"blocks":{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"send"},
 "send":{"opcode":"event_broadcast","next":"wait","inputs":{"BROADCAST_INPUT":[1,[11,"go","go"]]}},
 "wait":{"opcode":"control_wait","next":"again","inputs":{"DURATION":[1,[4,"0"]]}},
 "again":{"opcode":"event_broadcast","inputs":{"BROADCAST_INPUT":[1,[11,"go","go"]]}}
 }},
 {"name":"Sprite","blocks":{
 "hat":{"opcode":"event_whenbroadcastreceived","topLevel":true,"next":"glide","fields":{"BROADCAST_OPTION":["go","go"]}},
 "glide":{"opcode":"motion_glidesecstoxy","inputs":{"SECS":[1,[4,"0.1"]],"X":[1,[4,"30"]],"Y":[1,[4,"0"]]}}
 }}]`)
	run(t, r, 4, false)
	if r.Done() || math.Abs(r.Snapshot()[1].X-20) > 1e-9 {
		t.Fatal("glide was not restarted")
	}
	run(t, r, 1, false)
	if !r.Done() || r.Snapshot()[1].X != 30 {
		t.Fatal("restarted glide did not finish")
	}
}
