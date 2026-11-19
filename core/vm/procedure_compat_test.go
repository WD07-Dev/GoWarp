package vm

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"gowarp/core/project"
)

func TestExtensionMenuShadows(t *testing.T) {
	for _, op := range []string{"runtimeoptions_menu_thing", "runtimeoptions_menu_enabled", "pen_menu_colorParam"} {
		r := fixture(t, `{}`)
		got, err := evalBlock(t, r, 0, &project.Block{Opcode: op, Fields: map[string][]any{"menu": {"001", nil}}})
		if err != nil || got != "001" {
			t.Fatalf("%s: %v, %v", op, got, err)
		}
	}
}

func TestUnboundArgumentAliases(t *testing.T) {
	for _, tc := range []struct {
		opcode, name string
		want         any
	}{
		{"argument_reporter_boolean", "IS COMPILED?", true},
		{"argument_reporter_boolean", "is turbowarp?", true},
		{"argument_reporter_string_number", "Last Key Pressed", "space"},
		{"argument_reporter_boolean", "last key pressed", "space"},
		{"argument_reporter_string_number", "is compiled?", float64(0)},
	} {
		r := fixture(t, `{}`)
		r.lastKey = "space"
		got, err := evalBlock(t, r, 0, &project.Block{Opcode: tc.opcode, Fields: map[string][]any{"VALUE": {tc.name, nil}}})
		if err != nil || got != tc.want {
			t.Fatalf("%s/%s: %v, %v", tc.opcode, tc.name, got, err)
		}
	}
}

func TestDebugProcedureStatementsAreRecognized(t *testing.T) {
	for _, code := range []string{"log %s", "warn %s", "error %s", "breakpoint", "tw:debugger;", "log\u200b %s"} {
		blocks := `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"call"},"call":{"opcode":"procedures_call","next":"after","mutation":{"proccode":"CODE","argumentids":"[\"message\"]"},"inputs":{"message":[1,[10,"hello"]]}},"after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}}`
		r := fixture(t, strings.Replace(blocks, "CODE", code, 1))
		if err := r.Run(context.Background(), 1, 30, false); err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 1 {
			t.Fatalf("%s did not continue caller", code)
		}
	}
}

func TestDebugConsoleOutput(t *testing.T) {
	blocks := `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"log"},"log":{"opcode":"procedures_call","next":"warn","mutation":{"proccode":"log %s","argumentids":"[\"a\"]"},"inputs":{"a":[1,[10,"first"]]}},"warn":{"opcode":"procedures_call","next":"error","mutation":{"proccode":"warn %s","argumentids":"[\"a\"]"},"inputs":{"a":[1,[10,"second"]]}},"error":{"opcode":"procedures_call","mutation":{"proccode":"error %s","argumentids":"[\"a\"]"},"inputs":{"a":[1,[10,"third"]]}}}`
	r := fixture(t, blocks)
	var output bytes.Buffer
	r.SetDebugWriter(&output)
	run(t, r, 1, false)
	if output.String() != "Stage: first\nwarn: Stage: second\nerror: Stage: third\n" {
		t.Fatalf("console output: %q", output.String())
	}
}

func TestWindowedBreakpointPausesAndResumes(t *testing.T) {
	r := fixture(t, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"break"},"break":{"opcode":"procedures_call","next":"after","mutation":{"proccode":"tw:debugger;"}},"after":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}}`)
	var output bytes.Buffer
	r.SetDebugWriter(&output)
	r.SetWindowed(true)
	run(t, r, 1, false)
	if !r.Paused() || r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 0 {
		t.Fatal("breakpoint did not suspend caller")
	}
	if output.String() != "breakpoint in Stage, paused (F8 resumes)\n" {
		t.Fatalf("breakpoint output: %q", output.String())
	}
	before := r.Frames()
	run(t, r, 5, false)
	if r.Frames() != before {
		t.Fatal("paused runtime advanced")
	}
	r.SetPaused(false)
	run(t, r, 1, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 1 {
		t.Fatal("breakpoint failed to resume at next statement")
	}
}

func TestBoundArgumentWinsOverCompatibilityAlias(t *testing.T) {
	r := fixture(t, strings.Replace(procedureProject, `"argumentnames":"[\"n\"]"`, `"argumentnames":"[\"is compiled?\"]"`, 1))
	r.targets[0].source.Blocks["arg"].Opcode = "argument_reporter_boolean"
	r.targets[0].source.Blocks["arg"].Fields["VALUE"][0] = "is compiled?"
	run(t, r, 10, false)
	if number(r.Snapshot()[0].Variables["score"].Value) != 109 {
		t.Fatal("compatibility alias shadowed real argument")
	}
}
