package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gowarp/core/vm"
)

func TestGameplayStartsAfterKeyRelease(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Clean(filepath.Join(cwd, "..", "..", "test", "fixtures", "projects", "gameplay-startup.sb3"))
	for _, tc := range []struct {
		name    string
		inputs  []string
		started bool
	}{
		{name: "no input"},
		{name: "held key", inputs: []string{"--input", "space@0-1"}},
		{name: "released key", inputs: []string{"--input", "space@0-0.033"}},
		{name: "fresh press", inputs: []string{"--input", "space@0.1-0.133"}, started: true},
		// Scripted input adds a 50ms hold, so leave a full release frame.
		{name: "release then press", inputs: []string{"--input", "space@0-0.033;space@0.2-0.3"}, started: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var out, errs bytes.Buffer
			args := append([]string{path, "--headless", "30"}, tc.inputs...)
			if code := run(ctx, args, &out, &errs); code != 0 {
				t.Fatalf("exit %d: %s", code, errs.String())
			}
			var result struct {
				Frames  int              `json:"frames"`
				Targets []vm.TargetState `json:"targets"`
			}
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Frames != 30 {
				t.Fatalf("expected 30 execution frames, got %d", result.Frames)
			}
			wantClones, wantCostume := 0, 0
			if tc.started {
				wantClones, wantCostume = 3, 1
			}
			if len(result.Targets) != 3+wantClones {
				t.Fatalf("target count=%d, want %d", len(result.Targets), 3+wantClones)
			}
			if got := result.Targets[0].Variables["started"].Value; got != float64(wantClones) {
				t.Fatalf("clone startup count=%v, want %d", got, wantClones)
			}
			title := result.Targets[1]
			if title.Name != "Title" || title.CloneID != 0 || title.CurrentCostume != wantCostume {
				t.Fatalf("unexpected title scene: %+v", title)
			}
			for _, clone := range result.Targets[3:] {
				if clone.Name != "Worker" || clone.CloneID == 0 || !clone.Visible {
					t.Fatalf("clone did not start: %+v", clone)
				}
			}
		})
	}
}
