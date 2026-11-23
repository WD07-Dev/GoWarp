package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gowarp/app/runtimehost"
	"gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
	camera "gowarp/test/extensions/DTcameracontrols"
	runtimeoptions "gowarp/test/extensions/runtimeoptions"
	stretch "gowarp/test/extensions/stretch"
)

func remainingFeaturesPath(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(cwd, "..", "..", "test", "fixtures", "projects", "remaining-features.sb3"))
}

func TestRemainingFeaturesExampleCLI(t *testing.T) {
	path := remainingFeaturesPath(t)
	var baseline []vm.TargetState
	for _, turbo := range []bool{false, true} {
		args := []string{path, "--headless", "30"}
		if turbo {
			args = append(args, "--turbo")
		}
		var out, errs bytes.Buffer
		if code := run(context.Background(), args, &out, &errs, remainingFeaturesExtensions()); code != 0 {
			t.Fatalf("turbo=%v exit %d: %s", turbo, code, errs.String())
		}
		var result struct {
			Done    bool
			Targets []vm.TargetState
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if !result.Done || len(result.Targets) != 3 {
			t.Fatalf("incomplete example: %s", out.String())
		}
		want := map[string]any{
			// The 20px skin's touching hull uses pixel centers: 9.5 * 1.5 = 14.25 half-width.
			"penTouch":        true,
			"edge":            true,
			"fencedX":         float64(310),
			"bounceX":         float64(285.75),
			"bounceDirection": float64(-90),
			"json":            `[3,true,null,"a,b"]`,
			"delayed":         float64(42),
			"cloneStretch":    float64(150),
			"fps":             float64(60),
			"width":           float64(600),
			"height":          float64(400),
			"cameraX":         float64(10),
			"cameraZoom":      float64(110),
			"ready":           "1",
		}
		for id, value := range want {
			if got := result.Targets[0].Variables[id].Value; !reflect.DeepEqual(got, value) {
				t.Errorf("turbo=%v %s=%#v want %#v", turbo, id, got, value)
			}
		}
		if got := result.Targets[0].Lists["items"].Items; !reflect.DeepEqual(got, []any{float64(3), true, nil, "a,b"}) {
			t.Fatalf("converted list: %#v", got)
		}
		original, clone := result.Targets[1], result.Targets[2]
		if original.X != -60 || original.Y != 40 || original.StretchX != 150 || original.StretchY != 50 || clone.CloneID == 0 || clone.X != 80 || clone.Y != 50 || clone.StretchX != 150 || clone.StretchY != 50 {
			t.Fatalf("sprite/clone state: %+v %+v", original, clone)
		}
		if baseline == nil {
			baseline = result.Targets
		} else if !reflect.DeepEqual(baseline, result.Targets) {
			t.Fatal("normal and turbo final states differ")
		}
	}
}

func TestRemainingFeaturesMonitorAndReporterIntegration(t *testing.T) {
	path := remainingFeaturesPath(t)
	p, err := project.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	r := vm.New(p)
	if err := r.SetExtensions(remainingFeaturesExtensions()); err != nil {
		t.Fatal(err)
	}
	if err := runtimehost.Headless(r, path, p); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background(), 1, 30, false); err != nil {
		t.Fatal(err)
	}
	if r.Done() || r.Snapshot()[0].Variables["delayed"].Value != float64(0) {
		t.Fatal("reporter wait completed in its starting frame")
	}
	if err := r.Run(context.Background(), 30, 30, false); err != nil {
		t.Fatal(err)
	}
	monitors := r.Monitors()
	if len(monitors) != 5 {
		t.Fatalf("monitor count=%d", len(monitors))
	}
	wantVisible := []bool{true, true, true, false, true}
	for i, m := range monitors {
		if m.Visible != wantVisible[i] {
			t.Errorf("monitor %s visibility=%v", m.ID, m.Visible)
		}
	}
	if monitors[0].Value != "true" || monitors[1].Value != "42" || !reflect.DeepEqual(monitors[2].Items, []string{"3", "true", "null", "a,b"}) || monitors[4].Label != "Painter: marker" || monitors[4].Value != "stretch clone" {
		t.Fatalf("monitor readouts: %+v", monitors)
	}
}

func TestRemainingFeaturesArchiveManifestAndHashes(t *testing.T) {
	path := remainingFeaturesPath(t)
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	manifest, err := os.ReadFile(strings.TrimSuffix(path, ".sb3") + ".project.json")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range r.File {
		if seen[entry.Name] {
			t.Fatalf("duplicate archive entry %q", entry.Name)
		}
		seen[entry.Name] = true
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name == "project.json" {
			if !bytes.Equal(data, manifest) {
				t.Fatal("archive manifest differs from editable source")
			}
			continue
		}
		digest := md5.Sum(data)
		if strings.TrimSuffix(entry.Name, filepath.Ext(entry.Name)) != hex.EncodeToString(digest[:]) {
			t.Fatalf("asset hash mismatch: %s", entry.Name)
		}
	}
	if !seen["project.json"] || len(seen) != 2 {
		t.Fatalf("unexpected archive entries: %v", seen)
	}
}

func remainingFeaturesExtensions() extension.Bundle {
	return extension.Bundle{runtimeoptions.New, stretch.New, camera.New}
}
