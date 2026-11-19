package vm

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gowarp/core/extension"
	files "gowarp/test/extensions/files"
)

func TestFilesReporterYieldsAndResumesNestedInput(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},
		"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"join"]}},
		"join":{"opcode":"operator_join","inputs":{"STRING1":[1,[10,"selected:"]],"STRING2":[2,"pick"]}},
		"pick":{"opcode":"files_showPickerAs","inputs":{"as":[1,"encoding"]}},
		"encoding":{"opcode":"files_menu_encoding","fields":{"encoding":["url",null]},"shadow":true},
		"otherFlag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"count"},
		"count":{"opcode":"control_incr_counter"}
	}`)
	r = New(r.project)
	var calls atomic.Int32
	release := make(chan struct{})
	r.SetFilePicker(func(ctx context.Context, encoding string) (string, error) {
		calls.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if encoding != "url" {
			return "wrong encoding", nil
		}
		return "data:image/png;base64,AQID", nil
	})
	if err := r.SetExtensions(extension.Bundle{files.New}); err != nil {
		t.Fatal(err)
	}
	defer r.CloseExtensions()
	run(t, r, 3, true)
	if r.counter != 1 {
		t.Fatal("pending picker blocked another script")
	}
	if got := r.stage.variables["score"].Value; got != float64(0) {
		t.Fatalf("pending picker completed: %v", got)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for !r.Done() && time.Now().Before(deadline) {
		run(t, r, 1, true)
		time.Sleep(time.Millisecond)
	}
	if got := r.stage.variables["score"].Value; got != "selected:data:image/png;base64,AQID" {
		t.Fatalf("nested reporter = %v", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("picker opened %d times", calls.Load())
	}
}

func TestFilesReporterStopAndFlagCancelPendingPicker(t *testing.T) {
	for _, restart := range []bool{false, true} {
		r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},
		"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"pick"]}},
		"pick":{"opcode":"files_showPicker"}
	}`)
		r = New(r.project)
		started, cancelled := make(chan struct{}), make(chan struct{})
		r.SetFilePicker(func(ctx context.Context, encoding string) (string, error) {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return "", ctx.Err()
		})
		if err := r.SetExtensions(extension.Bundle{files.New}); err != nil {
			t.Fatal(err)
		}
		defer r.CloseExtensions()
		run(t, r, 1, false)
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("picker did not start")
		}
		if restart {
			r.GreenFlag()
		} else {
			r.Stop()
		}
		select {
		case <-cancelled:
		case <-time.After(time.Second):
			t.Fatal("stop left the picker running")
		}
	}
}

func TestFilesHostRejectsUnavailablePicker(t *testing.T) {
	r := fixture(t, `{}`)
	c := &extensionContext{r: r}
	if _, err := c.Call("files.pick", "url"); err == nil || !strings.Contains(err.Error(), "file picker is unavailable") {
		t.Fatalf("picker without desktop host: %v", err)
	}
}

func TestFilesReporterStopAllBlockCancelsPendingRequest(t *testing.T) {
	r := fixture(t, `{
		"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},
		"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"pick"]}},
		"pick":{"opcode":"files_showPicker"},
		"stopFlag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait"},
		"wait":{"opcode":"control_wait","inputs":{"DURATION":[1,[4,"0.1"]]},"next":"stop"},
		"stop":{"opcode":"control_stop","fields":{"STOP_OPTION":["all",null]}}
	}`)
	r = New(r.project)
	started, cancelled := make(chan struct{}), make(chan struct{})
	r.SetFilePicker(func(ctx context.Context, encoding string) (string, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return "", ctx.Err()
	})
	if err := r.SetExtensions(extension.Bundle{files.New}); err != nil {
		t.Fatal(err)
	}
	defer r.CloseExtensions()
	run(t, r, 1, false)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("picker did not start")
	}
	run(t, r, 5, false)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("stop all block left request running")
	}
}

func TestFilesHostAsyncResultAndCancellation(t *testing.T) {
	for _, closeRuntime := range []bool{false, true} {
		r := fixture(t, `{}`)
		started := make(chan string, 1)
		finished := make(chan struct{})
		r.SetFilePicker(func(ctx context.Context, encoding string) (string, error) {
			started <- encoding
			<-ctx.Done()
			close(finished)
			return "discard stale result", ctx.Err()
		})
		c := &extensionContext{r: r}
		value, err := c.Call("files.pick", "url")
		if err != nil {
			t.Fatal(err)
		}
		result := value.(<-chan string)
		select {
		case encoding := <-started:
			if encoding != "url" {
				t.Fatalf("encoding = %q", encoding)
			}
		case <-time.After(time.Second):
			t.Fatal("picker did not start asynchronously")
		}
		select {
		case <-result:
			t.Fatal("pending picker returned early")
		default:
		}
		if closeRuntime {
			if err := r.CloseExtensions(); err != nil {
				t.Fatal(err)
			}
		} else if _, err := c.Call("files.cancel"); err != nil {
			t.Fatal(err)
		}
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("picker was not cancelled")
		}
		select {
		case got := <-result:
			if got != "" {
				t.Fatalf("cancelled picker = %q", got)
			}
		case <-time.After(time.Second):
			t.Fatal("cancelled picker did not resolve")
		}
	}
}

func TestFilesHostPickerResult(t *testing.T) {
	for _, fail := range []bool{false, true} {
		r := fixture(t, `{}`)
		defer r.CloseExtensions()
		r.SetFilePicker(func(context.Context, string) (string, error) {
			if fail {
				return "", errors.New("read failed")
			}
			return "data:image/png;base64,AQID", nil
		})
		value, err := (&extensionContext{r: r}).Call("files.pick", "url")
		if err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-value.(<-chan string):
			want := "data:image/png;base64,AQID"
			if fail {
				want = ""
			}
			if got != want {
				t.Fatalf("result = %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("picker did not resolve")
		}
	}
}
