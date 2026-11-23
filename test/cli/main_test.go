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

	"gowarp/app/compiler"
	"gowarp/app/desktop"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestCLIArgumentOrderAndOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "한글 프로젝트.sb3")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("project.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Write([]byte(`{"targets":[{"isStage":true,"name":"Stage","variables":{"score":["score",0]},"blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"42"]]}}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{path, "--headless", "2"}, {"--headless", "2", path, "--turbo"}} {
		var out, errs bytes.Buffer
		if code := run(context.Background(), args, &out, &errs); code != 0 {
			t.Fatalf("exit %d: %s", code, errs.String())
		}
		var result struct {
			Done    bool `json:"done"`
			Targets []struct {
				Variables map[string]struct {
					Value any `json:"value"`
				} `json:"variables"`
			} `json:"targets"`
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if !result.Done || result.Targets[0].Variables["score"].Value != "42" {
			t.Fatalf("bad output: %s", out.String())
		}
	}
}

func TestCLIInvalidOptions(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"a.sb3"},
		{"a.sb3", "--headless", "-1"},
		{"a.sb3", "--headless", "1", "--fps", "NaN"},
		{"a.sb3", "--headless", "1", "--fps", "0"},
		{"a.sb3", "--headless", "1", "--fps", "Inf"},
		{"a.sb3", "--headless", "1", "--unknown"},
		{"a.sb3", "b.sb3", "--headless", "1"},
		{"--headless"},
	} {
		var out, errs bytes.Buffer
		if code := run(context.Background(), args, &out, &errs); code == 0 {
			t.Fatalf("expected failure: %v", args)
		}
		if errs.Len() == 0 {
			t.Fatal("missing error message")
		}
	}
}

func TestCLIRemovedCompilationFlags(t *testing.T) {
	for _, option := range []string{"--compile", "--emit", "--emit-dir=source", "--runtime-source=source"} {
		t.Run(option, func(t *testing.T) {
			var out, errs bytes.Buffer
			if code := run(context.Background(), []string{"project.sb3", option}, &out, &errs); code != 2 || !strings.Contains(errs.String(), "flag provided but not defined") {
				t.Fatalf("removed option was not rejected: exit %d: %s", code, errs.String())
			}
		})
	}
}

func TestCLIHelp(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{"--help"}, &out, &errs); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "headless") {
		t.Fatal("missing usage")
	}
}

func TestListProcedureExample(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Clean(filepath.Join(cwd, "..", "..", "test", "fixtures", "projects", "list-sum.sb3"))
	for _, flags := range [][]string{{"--headless", "30"}, {"--headless", "1", "--turbo"}} {
		var out, errs bytes.Buffer
		if code := run(context.Background(), append([]string{path}, flags...), &out, &errs); code != 0 {
			t.Fatalf("exit %d: %s", code, errs.String())
		}
		var result struct {
			Done    bool `json:"done"`
			Targets []struct {
				Variables map[string]struct {
					Value float64 `json:"value"`
				} `json:"variables"`
				Lists map[string]struct {
					Items []float64 `json:"items"`
				} `json:"lists"`
			} `json:"targets"`
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if !result.Done || result.Targets[0].Variables["sum"].Value != 55 {
			t.Fatalf("unexpected sum: %s", out.String())
		}
		items := result.Targets[0].Lists["numbers"].Items
		if len(items) != 10 || items[0] != 1 || items[9] != 10 {
			t.Fatalf("unexpected list: %v", items)
		}
	}
}

func TestBroadcastClonesExample(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Clean(filepath.Join(cwd, "..", "..", "test", "fixtures", "projects", "broadcast-clones.sb3"))
	for _, flags := range [][]string{{"--headless", "30"}, {"--headless", "30", "--turbo"}} {
		var out, errs bytes.Buffer
		if code := run(context.Background(), append([]string{path}, flags...), &out, &errs); code != 0 {
			t.Fatalf("exit %d: %s", code, errs.String())
		}
		var result struct {
			Done    bool `json:"done"`
			Targets []struct {
				Variables map[string]struct {
					Value any `json:"value"`
				} `json:"variables"`
			} `json:"targets"`
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if !result.Done || len(result.Targets) != 2 {
			t.Fatalf("clones not removed: %s", out.String())
		}
		values := result.Targets[0].Variables
		if values["sum"].Value != float64(6) || values["collected"].Value != float64(3) || values["finished"].Value != "1" {
			t.Fatalf("wrong result: %s", out.String())
		}
	}
}

func TestMotionStateExample(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Clean(filepath.Join(cwd, "..", "..", "test", "fixtures", "projects", "motion-state.sb3"))
	for _, flags := range [][]string{{"--headless", "30"}, {"--headless", "30", "--turbo"}} {
		var out, errs bytes.Buffer
		if code := run(context.Background(), append([]string{path}, flags...), &out, &errs); code != 0 {
			t.Fatalf("exit %d: %s", code, errs.String())
		}
		var result struct {
			Done    bool `json:"done"`
			Targets []struct {
				CloneID       int     `json:"cloneId"`
				X             float64 `json:"x"`
				Y             float64 `json:"y"`
				Direction     float64 `json:"direction"`
				Size          float64 `json:"size"`
				Visible       bool    `json:"visible"`
				RotationStyle string  `json:"rotationStyle"`
				Variables     map[string]struct {
					Value float64 `json:"value"`
				} `json:"variables"`
			} `json:"targets"`
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if !result.Done || len(result.Targets) != 4 {
			t.Fatalf("wrong targets: %s", out.String())
		}
		mover, clone := result.Targets[1], result.Targets[3]
		if mover.X != 30 || mover.Y != 40 || mover.Direction != 90 || mover.Size != 125.6 || !mover.Visible || mover.RotationStyle != "left-right" {
			t.Fatalf("wrong mover: %+v", mover)
		}
		if clone.CloneID == 0 || clone.Visible || clone.Direction != 0 || clone.Size != mover.Size || clone.X != 30 || clone.Y != 40 {
			t.Fatalf("wrong clone: %+v", clone)
		}
		vars := result.Targets[0].Variables
		if vars["x"].Value != 30 || vars["y"].Value != 40 || vars["sizeValue"].Value != 126 {
			t.Fatalf("wrong reporters: %+v", vars)
		}
	}
}

func TestInputEventsExample(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(cwd, "..", ".."))
	inputPath := filepath.Join(t.TempDir(), "inputs.json")
	inputs := `[
 {"frame":0,"kind":"mouse","x":30,"y":40},
 {"frame":1,"kind":"key","key":"space","down":true},
 {"frame":2,"kind":"key","key":"space","down":false},
 {"frame":15,"kind":"answer","answer":"GoWarp"}
]`
	if err := os.WriteFile(inputPath, []byte(inputs), 0600); err != nil {
		t.Fatal(err)
	}
	for _, turbo := range []bool{false, true} {
		args := []string{
			filepath.Join(root, "test", "fixtures", "projects", "interactive-state.sb3"),
			"--headless",
			"30",
			"--inputs",
			inputPath,
		}
		if turbo {
			args = append(args, "--turbo")
		}
		var out, errs bytes.Buffer
		if code := run(context.Background(), args, &out, &errs); code != 0 {
			t.Fatalf("exit %d: %s", code, errs.String())
		}
		var result struct {
			Done     bool `json:"done"`
			Question any  `json:"question"`
			Targets  []struct {
				Name           string  `json:"name"`
				X              float64 `json:"x"`
				Y              float64 `json:"y"`
				CurrentCostume int     `json:"currentCostume"`
				Variables      map[string]struct {
					Value any `json:"value"`
				} `json:"variables"`
			} `json:"targets"`
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if !result.Done || result.Question != nil || result.Targets[0].Variables["sum"].Value != float64(6) || result.Targets[0].Variables["square"].Value != float64(36) || result.Targets[0].Variables["answer"].Value != "GoWarp" || result.Targets[0].Variables["keys"].Value != float64(1) || result.Targets[0].CurrentCostume != 1 || result.Targets[1].X != 30 || result.Targets[1].Y != 40 {
			t.Fatalf("unexpected output: %s", out.String())
		}
	}
}

func TestInvalidInputEventFile(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Clean(filepath.Join(cwd, "..", "..", "test", "fixtures", "projects", "counter.sb3"))
	for _, content := range []string{
		`[{"frame":-1,"kind":"key","key":"space"}]`,
		`[{"frame":0,"kind":"typo"}]`,
		`[{"frame":0,"kind":"mouse","typo":1}]`,
		`[] []`,
	} {
		input := filepath.Join(t.TempDir(), "inputs.json")
		if err := os.WriteFile(input, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		var out, errs bytes.Buffer
		if code := run(context.Background(), []string{path, "--headless", "1", "--inputs", input}, &out, &errs); code != 1 {
			t.Fatalf("invalid input exit %d: %s", code, errs.String())
		}
	}
}

func TestWindowRoutingAndOptions(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Clean(filepath.Join(cwd, "..", "..", "test", "fixtures", "projects", "interactive-state.sb3"))
	previous := nativeRunner
	defer func() {
		nativeRunner = previous
	}()
	calls := 0
	nativeRunner = func(ctx context.Context, path string, p *project.Project, options desktop.Options, events []vm.InputEvent) (*vm.Runtime, error) {
		calls++
		if path != projectPath || options.Frames != 5 || !options.Mute || options.FPS != 30 {
			t.Fatalf("native options %+v", options)
		}
		return vm.New(p), nil
	}
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{projectPath, "--window", "--frames", "5", "--mute"}, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if calls != 1 {
		t.Fatal("window mode was not dispatched")
	}
	for _, args := range [][]string{
		{projectPath, "--window", "--headless", "1"},
		{projectPath, "--window", "--frames", "-1"},
		{projectPath, "--headless", "1", "--screenshot", "test.png"},
	} {
		out.Reset()
		errs.Reset()
		if code := run(context.Background(), args, &out, &errs); code != 2 {
			t.Fatalf("bad options exit %d: %s", code, errs.String())
		}
	}
}

func TestPackRejectsSourceOverwrite(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Clean(filepath.Join(cwd, "..", "..", "test", "fixtures", "projects", "counter.sb3"))
	original, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{projectPath, "--pack", "-o", projectPath}, &out, &errs); code != 1 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	after, err := os.ReadFile(projectPath)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("source changed")
	}
}

func TestPackKeepsProjectAndRuntimeOptions(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Clean(filepath.Join(cwd, "..", "..", "test", "fixtures", "projects", "counter.sb3"))
	output := filepath.Join(t.TempDir(), "packed")
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{projectPath, "--pack", "-o", output, "--headless", "2", "--turbo"}, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	embedded, err := compiler.ReadEmbedded(output)
	if err != nil || embedded == nil {
		t.Fatalf("missing payload: %v", err)
	}
	original, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, embedded.Project) || embedded.Name != "counter.sb3" {
		t.Fatal("embedded project differs")
	}
	if strings.Join(embedded.Args, " ") != "--headless 2 --fps 30 --turbo" {
		t.Fatalf("options: %v", embedded.Args)
	}
}

func TestPackCancellationKeepsOutput(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Clean(filepath.Join(cwd, "..", "..", "test", "fixtures", "projects", "counter.sb3"))
	output := filepath.Join(t.TempDir(), "game")
	if err = os.WriteFile(output, []byte("existing output"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errs bytes.Buffer
	if code := run(ctx, []string{projectPath, "--pack", "-o", output}, &out, &errs); code != 1 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	after, err := os.ReadFile(output)
	if err != nil || string(after) != "existing output" {
		t.Fatal("existing output changed")
	}

}

func TestEmbeddedHeadlessOverridesDefaultWindow(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Clean(filepath.Join(cwd, "..", "..", "test", "fixtures", "projects", "counter.sb3"))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	embedded := &compiler.Embedded{
		Project: data,
		Name:    "counter.sb3",
		Args:    []string{"--window", "--frames", "5", "--mute"},
	}
	if code := runExecutable(context.Background(), embedded, []string{"--headless", "1"}, &out, &errs); code != 0 {
		t.Fatalf("headless override exit %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), `"frames": 1`) {
		t.Fatalf("headless override not applied: %s", out.String())
	}
}

func TestPackDefaultWindowOptions(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Clean(filepath.Join(cwd, "..", "..", "test", "fixtures", "projects", "counter.sb3"))
	output := filepath.Join(t.TempDir(), "game")
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{path, "--pack", "-o", output, "--mute", "--frames", "2"}, &out, &errs); code != 0 {
		t.Fatalf("default window options exit %d: %s", code, errs.String())
	}
	embedded, err := compiler.ReadEmbedded(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(embedded.Args, " ") != "--window --fps 30 --mute --frames 2" {
		t.Fatalf("wrong defaults: %v", embedded.Args)
	}
	if code := run(context.Background(), []string{path, "--pack", "-o", output, "--fps", "1.5"}, &out, &errs); code != 2 {
		t.Fatalf("accepted fractional native fps: exit %d", code)
	}
}

func TestPackUnpackedProject(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "project.json")
	if err := os.WriteFile(manifest, []byte(`{"targets":[{"isStage":true,"name":"Stage"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{dir, manifest} {
		var out, errs bytes.Buffer
		output := filepath.Join(t.TempDir(), "packed")
		if code := run(context.Background(), []string{input, "--pack", "-o", output, "--headless", "0"}, &out, &errs); code != 0 {
			t.Fatalf("pack %s exit %d: %s", input, code, errs.String())
		}
		embedded, err := compiler.ReadEmbedded(output)
		if err != nil || embedded == nil {
			t.Fatalf("read payload: %v", err)
		}
		payload := filepath.Join(t.TempDir(), embedded.Name)
		if err = os.WriteFile(payload, embedded.Project, 0600); err != nil {
			t.Fatal(err)
		}
		p, err := project.Load(payload)
		if err != nil || p.Targets[0].Name != "Stage" {
			t.Fatalf("invalid embedded archive for %s: %v", input, err)
		}
		wantName := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input)) + ".sb3"
		if embedded.Name != wantName {
			t.Fatalf("save identity changed: name %q want %q", embedded.Name, wantName)
		}
	}
}
