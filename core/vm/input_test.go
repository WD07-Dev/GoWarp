package vm

import (
	"testing"
	"time"

	"gowarp/core/project"
)

func TestShiftArrowKeyState(t *testing.T) {
	for _, immediate := range []bool{false, true} {
		for _, arrow := range []string{"left arrow", "right arrow", "up arrow", "down arrow"} {
			r := motionRuntime(t, "{}")
			c := &extensionContext{r: r, th: &thread{target: r.stage}}
			set := func(key string, down bool) {
				if immediate {
					r.SetKeyState(key, down)
				} else {
					r.applyInput(InputEvent{Kind: "key", Key: key, Down: down})
				}
			}
			check := func(key string, want bool) {
				t.Helper()
				got, err := evalBlock(t, r, 1, &project.Block{Opcode: "sensing_keypressed",
					Inputs: literalInputs(map[string]any{"KEY_OPTION": key})})
				if err != nil || got != want {
					t.Errorf("immediate=%t arrow=%s sensing %s=%v, want %t (err=%v)", immediate, arrow, key, got, want, err)
				}
				got, err = c.Call("input.key", key)
				if err != nil || got != want {
					t.Errorf("extension key %s=%v, want %t (err=%v)", key, got, want, err)
				}
			}
			set("shiftleft", true)
			set(arrow, true)
			check("Shift", true)
			check(arrow, true)
			set("shiftright", true)
			set("shiftleft", false)
			check("shift", true)
			check("shiftleft", false)
			check("shiftright", true)
			check(arrow, true)
			set("shiftright", false)
			check("shift", false)
			check(arrow, true)
			check("any", true)
			set(arrow, false)
			check("any", false)
			set("shift", true)
			check("shift", true)
			set("shift", false)
			check("shift", false)
		}
	}
}

func TestInputStartsIdleKeyScripts(t *testing.T) {
	r := motionRuntime(t, `{"hat":{"opcode":"event_whenkeypressed","topLevel":true,"fields":{"KEY_OPTION":["space",null]},"next":"inc"},"inc":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}}`)
	if err := r.QueueEvents([]InputEvent{
		{
			Frame: 2,
			Kind:  "key",
			Key:   "space",
			Down:  true,
		},
		{
			Frame: 3,
			Kind:  "key",
			Key:   "space",
			Down:  false,
		},
	}); err != nil {
		t.Fatal(err)
	}
	run(t, r, 10, false)
	if r.Frames() != 4 || number(r.Snapshot()[0].Variables["score"].Value) != 1 {
		t.Fatalf("idle input lost: frame=%d", r.Frames())
	}
	got, err := evalBlock(t, r, 1, &project.Block{Opcode: "sensing_keypressed", Inputs: literalInputs(map[string]any{"KEY_OPTION": "space"})})
	if err != nil || got != false {
		t.Fatalf("key release: %v %v", got, err)
	}
}

func TestMouseMotionAndSensing(t *testing.T) {
	r := motionRuntime(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"goto"},"goto":{"opcode":"motion_goto","inputs":{"TO":[1,[10,"_mouse_"]]}}}`)
	if err := r.QueueEvents([]InputEvent{{
		Kind: "mouse",
		X:    30,
		Y:    40,
		Down: true,
	}}); err != nil {
		t.Fatal(err)
	}
	run(t, r, 1, false)
	if s := r.Snapshot()[1]; s.X != 30 || s.Y != 40 {
		t.Fatal("mouse goto failed")
	}
	for _, tc := range []struct {
		op   string
		want any
	}{{"sensing_mousex", float64(30)}, {"sensing_mousey", float64(40)}, {"sensing_mousedown", true}} {
		got, err := evalBlock(t, r, 1, &project.Block{Opcode: tc.op})
		if err != nil || got != tc.want {
			t.Fatalf("%s: %v %v", tc.op, got, err)
		}
	}
}

func TestAskWaitSerializesQuestions(t *testing.T) {
	r := motionRuntime(t, `{
 "a":{"opcode":"event_whenflagclicked","topLevel":true,"next":"askA"},
 "askA":{"opcode":"sensing_askandwait","next":"saveA","inputs":{"QUESTION":[1,[10,"first"]]}},
 "saveA":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"answer"]}},
 "b":{"opcode":"event_whenflagclicked","topLevel":true,"next":"askB"},
 "askB":{"opcode":"sensing_askandwait","next":"addB","inputs":{"QUESTION":[1,[10,"second"]]}},
 "addB":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"answer"]}},
 "answer":{"opcode":"sensing_answer"}}`)
	if err := r.QueueEvents([]InputEvent{{
		Frame:  1,
		Kind:   "answer",
		Answer: "10",
	}, {
		Frame:  3,
		Kind:   "answer",
		Answer: "20",
	}}); err != nil {
		t.Fatal(err)
	}
	run(t, r, 1, false)
	if q := r.Question(); q == nil || q.Text != "first" {
		t.Fatalf("wrong active question: %+v", q)
	}
	run(t, r, 1, false)
	if q := r.Question(); q == nil || q.Text != "second" {
		t.Fatalf("second question lost: %+v", q)
	}
	run(t, r, 5, false)
	if r.Question() != nil || !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 30 {
		t.Fatal("answer resume failed")
	}
}

func TestGreaterThanHatsDetectRisingEdges(t *testing.T) {
	r := motionRuntime(t, `{"hat":{"opcode":"event_whengreaterthan","topLevel":true,"fields":{"WHENGREATERTHANMENU":["LOUDNESS",null]},"inputs":{"VALUE":[1,[4,"10"]]},"next":"inc"},"inc":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}}`)
	if err := r.QueueEvents([]InputEvent{
		{
			Frame: 0,
			Kind:  "loudness",
			Value: 20,
		},
		{
			Frame: 2,
			Kind:  "loudness",
			Value: 0,
		},
		{
			Frame: 3,
			Kind:  "loudness",
			Value: 20,
		},
	}); err != nil {
		t.Fatal(err)
	}
	run(t, r, 6, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 2 {
		t.Fatal("edge hat did not rearm")
	}
}

func TestGreaterThanHatsKeepCloneThresholdsIndependent(t *testing.T) {
	r := eventProject(t, `[
		{"isStage":true,"name":"Stage"},
		{"name":"Sprite","variables":{"hits":["hits",0],"limit":["limit",10]},"blocks":{
			"hat":{"opcode":"event_whengreaterthan","topLevel":true,"next":"inc",
				"fields":{"WHENGREATERTHANMENU":["LOUDNESS",null]},
				"inputs":{"VALUE":[3,[12,"limit","limit"],[4,"0"]]}},
			"inc":{"opcode":"data_changevariableby","fields":{"VARIABLE":["hits","hits"]},"inputs":{"VALUE":[1,[4,"1"]]}}
		}}
	]`)
	original := r.targets[1]
	r.createClone(original)
	clone := r.targets[2]
	clone.variables["limit"] = &Variable{Name: "limit", Value: float64(30)}
	r.createClone(clone)
	nested := r.targets[3]
	nested.variables["limit"] = &Variable{Name: "limit", Value: float64(5)}
	check := func(want ...float64) {
		t.Helper()
		for i, target := range []*target{original, clone, nested} {
			if got := number(target.variables["hits"].Value); got != want[i] {
				t.Fatalf("target %d: hits=%v, want %v", i, got, want[i])
			}
		}
	}
	r.loudness = 20
	run(t, r, 2, false)
	check(1, 0, 1)
	clone.variables["limit"] = &Variable{Name: "limit", Value: float64(15)}
	run(t, r, 2, false)
	check(1, 1, 1)
	r.loudness = 0
	run(t, r, 1, false)
	r.loudness = 20
	run(t, r, 1, false)
	check(2, 2, 2)
	r.deleteClone(clone)
	r.loudness = 0
	run(t, r, 1, false)
	r.loudness = 20
	run(t, r, 1, false)
	if number(original.variables["hits"].Value) != 3 || number(nested.variables["hits"].Value) != 3 {
		t.Fatal("deleting one clone stopped the remaining edge hats")
	}
}

func TestPropertyDistanceAndClockReporters(t *testing.T) {
	r := eventProject(t, `[{"name":"Stage","isStage":true,"variables":{"v":["score",12]},"costumes":[{"name":"sky"}]},{"name":"Sprite","x":30,"y":40,"size":125.6,"volume":80}]`)
	r.now = func() time.Time {
		return time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
	}
	tests := []struct {
		op     string
		inputs map[string]any
		fields map[string][]any
		want   any
	}{
		{"sensing_distanceto", map[string]any{"DISTANCETOMENU": "_mouse_"}, nil, float64(50)},
		{"sensing_distanceto", map[string]any{"DISTANCETOMENU": "missing"}, nil, float64(10000)},
		{
			"sensing_of",
			map[string]any{"OBJECT": "_stage_"},
			map[string][]any{"PROPERTY": {"score"}},
			float64(12),
		},
		{"sensing_of", map[string]any{"OBJECT": "Sprite"}, map[string][]any{"PROPERTY": {"size"}}, 125.6},
		{
			"sensing_of",
			map[string]any{"OBJECT": "_stage_"},
			map[string][]any{"PROPERTY": {"backdrop name"}},
			"sky",
		},
		{"sensing_current", nil, map[string][]any{"CURRENTMENU": {"DAYOFWEEK"}}, float64(1)},
		{"sensing_current", nil, map[string][]any{"CURRENTMENU": {"YEAR"}}, float64(2000)},
	}
	for _, tc := range tests {
		got, err := evalBlock(t, r, 1, &project.Block{
			Opcode: tc.op,
			Inputs: literalInputs(tc.inputs),
			Fields: tc.fields,
		})
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %v %v want %v", tc.op, got, err, tc.want)
		}
	}
}

func TestInvalidEventsDoNotPartiallyApply(t *testing.T) {
	r := motionRuntime(t, `{}`)
	if err := r.QueueEvents([]InputEvent{{
		Kind: "key",
		Key:  "space",
		Down: true,
	}, {Frame: -1, Kind: "mouse"}}); err == nil {
		t.Fatal("bad frame accepted")
	}
	if !r.Done() {
		t.Fatal("invalid event batch partially queued")
	}
}

func TestPropertyVariableLookupKeepsIDOrderWithoutAllocating(t *testing.T) {
	r := eventProject(t, `[{"name":"Stage","isStage":true,"variables":{"z":["score",30],"a":["score",10],"m":["other",20]}}]`)
	block := &project.Block{Opcode: "sensing_of", Fields: map[string][]any{"PROPERTY": {"score"}}}
	input := func(string) (any, error) {
		return "_stage_", nil
	}
	var got any
	var err error
	check := func(want any) {
		t.Helper()
		allocations := testing.AllocsPerRun(100, func() {
			got, err = r.sensingReporter(r.stage, block, input)
			if err != nil || got != want {
				t.Fatalf("property = %v, %v; want %v", got, err, want)
			}
		})
		if allocations != 0 {
			t.Errorf("property lookup allocated %g times", allocations)
		}
	}
	check(float64(10))
	r.stage.variables[""] = &Variable{Name: "score", Value: nil}
	check(nil)
	delete(r.stage.variables, "")
	delete(r.stage.variables, "a")
	check(float64(30))
	delete(r.stage.variables, "z")
	check(float64(0))
}
