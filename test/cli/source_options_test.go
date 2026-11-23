package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gowarp/app/compiler"
	"gowarp/app/desktop"
	"gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
	runtimeoptions "gowarp/test/extensions/runtimeoptions"
)

func sourceOptionsProject(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "project.sb3")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("project.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func sourceResult(t *testing.T, out []byte) []vm.TargetState {
	t.Helper()
	var result struct {
		Targets []vm.TargetState
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	return result.Targets
}

func TestSourceStageDumpDiagnostics(t *testing.T) {
	path := sourceOptionsProject(t, `{"targets":[{"isStage":true,"name":"Stage","variables":{"w":["width",0]},"lists":{"l":["items",["a",2]]},"blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["width","w"]},"inputs":{"VALUE":[2,"width"]}},"width":{"opcode":"runtimeoptions_getDimension","inputs":{"dimension":[1,[10,"width"]]}}}}]}`)
	dump := filepath.Join(t.TempDir(), "nested", "state.txt")
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{path, "--headless", "1", "--stage", "600x400", "--dump", dump, "--verbose", "--sprites"}, &out, &errs, extension.Bundle{runtimeoptions.New}); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if sourceResult(t, out.Bytes())[0].Variables["w"].Value != float64(600) {
		t.Fatal("stage override ignored")
	}
	data, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "Stage.items # 2\nStage.items[1] = a\nStage.items[2] = 2\nStage.width = 600\n" {
		t.Fatalf("dump: %q", data)
	}
	if !strings.Contains(errs.String(), "frames") || !strings.Contains(errs.String(), "Stage") {
		t.Fatal("missing diagnostics")
	}
}

func TestInlineScriptRunsAfterIdle(t *testing.T) {
	path := sourceOptionsProject(t, `{"targets":[{"isStage":true,"name":"Stage","variables":{"score":["score",0]},"blocks":{"key":{"opcode":"event_whenkeypressed","topLevel":true,"next":"add","fields":{"KEY_OPTION":["space",null]}},"add":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},"click":{"opcode":"event_whenstageclicked","topLevel":true,"next":"ten"},"ten":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"10"]]}}}}]}`)
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{path, "--headless", "12", "--input", "space@0.1-0.2;click:0,-50@0.25"}, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if sourceResult(t, out.Bytes())[0].Variables["score"].Value != float64(11) {
		t.Fatalf("scripted events: %s", out.String())
	}
}

func TestTraceCapturesAppendsBeforeClear(t *testing.T) {
	path := sourceOptionsProject(t, `{"targets":[{"isStage":true,"name":"Stage","lists":{"l":["trace",[]]},"comments":{"note":{"blockId":"add","text":"first\nitem"}},"blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"add"},"add":{"opcode":"data_addtolist","next":"clear","fields":{"LIST":["trace","l"]},"inputs":{"ITEM":[1,[4,"1"]]}},"clear":{"opcode":"data_deletealloflist","next":"second","fields":{"LIST":["trace","l"]}},"second":{"opcode":"data_addtolist","fields":{"LIST":["trace","l"]},"inputs":{"ITEM":[1,[4,"2"]]}}}}]}`)
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{path, "--headless", "1", "--trace-list", "trace"}, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if errs.String() != "Stage: trace += 1  // first item\nStage: trace += 2\n" {
		t.Fatalf("trace: %q", errs.String())
	}
	sourceResult(t, out.Bytes())
}

func TestUARTBridgePreservesBytesAndGuestIdentity(t *testing.T) {
	path := sourceOptionsProject(t, `{"targets":[{"isStage":true,"name":"Stage","lists":{"in":["Terminal.inputBuffer",[]],"out":["Terminal.outputBuffer",[]]}},{"name":"RISCV","blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"add"},"add":{"opcode":"data_addtolist","next":"clear","fields":{"LIST":["Terminal.outputBuffer","out"]},"inputs":{"ITEM":[1,[4,"65"]]}},"clear":{"opcode":"data_deletealloflist","fields":{"LIST":["Terminal.outputBuffer","out"]}}}},{"name":"Terminal","blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"add"},"add":{"opcode":"data_addtolist","fields":{"LIST":["Terminal.outputBuffer","out"]},"inputs":{"ITEM":[1,[4,"66"]]}}}}]}`)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "in"), []byte{0, 255, 65}, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{path, "--headless", "2", "--uart", dir}, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "out"))
	if err != nil || string(data) != "A" {
		t.Fatalf("UART output: %q %v", data, err)
	}
	got := sourceResult(t, out.Bytes())[0].Lists["in"].Items
	if len(got) != 3 || got[0] != float64(0) || got[1] != float64(255) || got[2] != float64(65) {
		t.Fatalf("UART input: %v", got)
	}
}

func TestPacedRunCancellation(t *testing.T) {
	path := sourceOptionsProject(t, `{"targets":[{"isStage":true,"name":"Stage","blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"loop"},"loop":{"opcode":"control_forever"}}}]}`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	var out, errs bytes.Buffer
	code := run(ctx, []string{path, "--headless", "3", "--fps", "1", "--paced"}, &out, &errs)
	if code != 1 || !strings.Contains(errs.String(), "deadline") || time.Since(start) > time.Second {
		t.Fatalf("cancellation exit=%d elapsed=%v errors=%s", code, time.Since(start), errs.String())
	}
}

func TestInlineClickReleasePreservesCoordinates(t *testing.T) {
	path := sourceOptionsProject(t, `{"targets":[{"isStage":true,"name":"Stage","variables":{"x":["x",0]},"blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"wait"},"wait":{"opcode":"control_wait","next":"set","inputs":{"DURATION":[1,[4,"0.2"]]}},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["x","x"]},"inputs":{"VALUE":[2,"mouse"]}},"mouse":{"opcode":"sensing_mousex"}}}]}`)
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{path, "--headless", "8", "--input", "click:-10,20@0"}, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if sourceResult(t, out.Bytes())[0].Variables["x"].Value != float64(-10) {
		t.Fatal("mouse release moved the cursor")
	}
}

func TestSourceOptionValidation(t *testing.T) {
	for _, options := range [][]string{
		{"--headless", "1", "--input", "space"}, {"--headless", "1", "--input", "space@NaN"}, {"--headless", "1", "--input", "space@2-1"}, {"--headless", "1", "--input", "click:1@0"}, {"--headless", "1", "--input", "click:NaN,0@0"}, {"--headless", "1", "--stage", "600"}, {"--headless", "1", "--stage", "600xNaN"}, {"--headless", "1", "--stage", "5000x400"}, {"--window", "--scale", "0"}, {"--window", "--scale", "Inf"}, {"--headless", "1", "--scale", "2"}, {"--window", "--paced"}, {"--window", "--at", "-2"},
	} {
		var out, errs bytes.Buffer
		if code := run(context.Background(), append([]string{"missing.sb3"}, options...), &out, &errs); code != 2 {
			t.Fatalf("options %v: exit %d errors=%s", options, code, errs.String())
		}
	}
}

func TestNativeSourceOptionsAndPackDefaults(t *testing.T) {
	path := sourceOptionsProject(t, `{"targets":[{"isStage":true,"name":"Stage","blocks":{}}]}`)
	old := nativeRunner
	defer func() {
		nativeRunner = old
	}()
	nativeRunner = func(ctx context.Context, path string, p *project.Project, options desktop.Options, events []vm.InputEvent) (*vm.Runtime, error) {
		if options.Scale != 1.5 || options.Frames != 3 || options.Screenshot == "" || options.Configure == nil || options.BeforeTick == nil || options.AfterTick == nil {
			t.Fatalf("native options %+v", options)
		}
		r := vm.New(p)
		if err := options.Configure(r); err != nil {
			return nil, err
		}
		if r.View().Width != 640 || r.View().Height != 480 {
			t.Fatal("native stage override lost")
		}
		return r, nil
	}
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{
		path,
		"--window",
		"--stage",
		"640x480",
		"--scale",
		"1.5",
		"--screenshot",
		filepath.Join(t.TempDir(), "screen.png"),
		"--at",
		"3",
	}, &out, &errs); code != 0 {
		t.Fatalf("native exit %d: %s", code, errs.String())
	}
	output := filepath.Join(t.TempDir(), "packed")
	out.Reset()
	errs.Reset()
	if code := run(context.Background(), []string{
		path,
		"--pack",
		"-o",
		output,
		"--headless",
		"3",
		"--input",
		"space@1",
		"--stage",
		"600x400",
		"--dump",
		"state.txt",
		"--paced",
		"--trace-list",
		"log",
	}, &out, &errs); code != 0 {
		t.Fatalf("pack exit %d: %s", code, errs.String())
	}
	embedded, err := compiler.ReadEmbedded(output)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(embedded.Args, " ")
	for _, want := range []string{"--input space@1", "--stage 600x400", "--dump state.txt", "--paced", "--trace-list log"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %s in %s", want, args)
		}
	}
}

func TestDumpCannotOverwriteProject(t *testing.T) {
	path := sourceOptionsProject(t, `{"targets":[{"isStage":true,"name":"Stage","blocks":{}}]}`)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{path, "--headless", "0", "--dump", path}, &out, &errs); code != 1 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("dump overwrote project")
	}
}
