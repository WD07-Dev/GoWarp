package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"gowarp/core/project"
)

func addNativeSensingBlocks(t testing.TB, blocks project.Blocks) {
	t.Helper()
	var extra project.Blocks
	err := json.Unmarshal([]byte(`{
        "sensingReadBody":{"opcode":"data_setvariableto","next":"sensingReadTime","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"sensingKey"]}},
        "sensingKey":{"opcode":"sensing_keypressed","inputs":{"KEY_OPTION":[1,[12,"x","x"]]}},
        "sensingReadTime":{"opcode":"data_setvariableto","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[2,"sensingTime"]}},
        "sensingTime":{"opcode":"sensing_timer"},
        "sensingMenuBody":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"sensingMenuKey"]}},
        "sensingMenuKey":{"opcode":"sensing_keypressed","inputs":{"KEY_OPTION":[2,"sensingMenuValue"]}},
        "sensingMenuValue":{"opcode":"sensing_keyoptions","fields":{"KEY_OPTION":["right arrow",null]}},
        "sensingEmptyMenuBody":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"sensingEmptyMenuValue"]}},
        "sensingEmptyMenuValue":{"opcode":"sensing_keyoptions"},
        "sensingLoopBody":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"1000"]],"SUBSTACK":[2,"sensingAccumulate"]}},
        "sensingAccumulate":{"opcode":"data_changevariableby","next":"sensingCheck","fields":{"VARIABLE":["y","y"]},"inputs":{"VALUE":[2,"sensingDelta"]}},
        "sensingDelta":{"opcode":"operator_multiply","inputs":{"NUM1":[2,"sensingTime"],"NUM2":[1,[4,"0.125"]]}},
        "sensingCheck":{"opcode":"control_if","inputs":{"CONDITION":[2,"sensingBoth"],"SUBSTACK":[2,"sensingCount"]}},
        "sensingBoth":{"opcode":"operator_and","inputs":{"OPERAND1":[2,"sensingKey"],"OPERAND2":[2,"sensingPositive"]}},
        "sensingPositive":{"opcode":"operator_gt","inputs":{"OPERAND1":[1,[12,"y","y"]],"OPERAND2":[1,[4,"0"]]}},
        "sensingCount":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
    }`), &extra)
	if err != nil {
		t.Fatal(err)
	}
	for id, block := range extra {
		blocks[id] = block
	}
}

func testNativeSensing(t *testing.T) {
	fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
	slow.native = false
	if err := prepareProcedures(fast.targets[1]); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sensingRead", "sensingMenu", "sensingEmptyMenu"} {
		if fast.targets[1].procedures[name].native == nil {
			t.Fatalf("%s should compile without an interpreter boundary", name)
		}
	}
	if fast.targets[1].procedures["sensingLoop"].regions["sensingAccumulate"] == nil {
		t.Fatal("sensing interrupted compilation of the complete loop body")
	}
	for i, tc := range []struct {
		key  any
		keys map[string]bool
		want bool
	}{
		{"right", map[string]bool{"right arrow": true}, true},
		{"right", map[string]bool{"right arrow": false}, false},
		{"SHIFT", map[string]bool{"shiftleft": true, "up arrow": true}, true},
		{"shift", map[string]bool{"shiftright": true}, true},
		{"any", map[string]bool{"a": true}, true},
		{"any", map[string]bool{"a": false}, false},
		{" ", map[string]bool{"space": true}, true},
		{float64(1), map[string]bool{"1": true}, true},
		{"unknown", map[string]bool{}, false},
	} {
		for _, procedure := range []string{"sensingRead", "sensingMenu", "sensingEmptyMenu", "sensingLoop"} {
			for _, r := range []*Runtime{fast, slow} {
				target := r.targets[1]
				r.keys = tc.keys
				r.timerSeconds = r.elapsed - float64(i%3)
				*target.variables["x"] = Variable{Value: tc.key}
				*target.variables["y"] = Variable{Value: float64(4)}
				*target.variables["score"] = Variable{Value: float64(0)}
				wantTime := r.elapsed - r.timerSeconds
				th := r.startHat(target, procedure+"Hat", false)
				if err := r.Run(context.Background(), 1, 60, false); err != nil {
					t.Fatal(err)
				}
				if !th.done {
					t.Fatalf("%s native=%v did not finish", procedure, r.native)
				}
				if procedure == "sensingRead" {
					if target.variables["score"].Value != tc.want || target.variables["y"].Value != wantTime {
						t.Fatalf("native=%v key=%v: stale key or timer state", r.native, tc.key)
					}
				}
			}
			if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) {
				t.Fatalf("%s key=%v: native sensing differs from interpreter", procedure, tc.key)
			}
		}
	}

	for _, compiled := range []bool{false, true} {
		r := New(nativeCodegenFixture(t))
		r.native = compiled
		target := r.targets[1]
		c := &extensionContext{r: r, th: &thread{target: target}}
		if _, err := c.Call("variable.delete", "x"); err != nil {
			t.Fatal(err)
		}
		r.startHat(target, "sensingReadHat", false)
		err := r.Run(context.Background(), 1, 60, false)
		if err == nil || !strings.Contains(err.Error(), `unknown variable "x"`) {
			t.Fatalf("native=%v: missing key variable error = %v", compiled, err)
		}
		if target.variables["score"].Value != float64(0) {
			t.Fatalf("native=%v: failed key lookup changed the output", compiled)
		}
	}

}

func BenchmarkNativeSensingLoop(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, compiled := range []bool{false, true} {
		b.Run(fmt.Sprintf("compiled=%v", compiled), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			r.native = compiled
			r.keys = map[string]bool{"shiftleft": true}
			target := r.targets[1]
			*target.variables["x"] = Variable{Value: "shift"}
			b.ReportAllocs()
			for b.Loop() {
				r.timerSeconds = r.elapsed - 2
				*target.variables["y"] = Variable{Value: float64(4)}
				*target.variables["score"] = Variable{Value: float64(0)}
				th := r.startHat(target, "sensingLoopHat", false)
				if err := r.Run(context.Background(), 1, 60, false); err != nil {
					b.Fatal(err)
				}
				if !th.done || target.variables["score"].Value != float64(1000) {
					b.Fatal("loop did not complete all iterations")
				}
			}
		})
	}
}
