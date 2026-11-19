package vm

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"gowarp/core/project"
)

func TestProjectUnlimitedClonesKeepsOtherRuntimeSettings(t *testing.T) {
	var p project.Project
	data := `{"targets":[{"isStage":true,"comments":{"config":{"text":"{\"framerate\":60,\"runtimeOptions\":{\"maxClones\":Infinity,\"miscLimits\":false}} // _twconfig_"}}}]}`
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatal(err)
	}
	r := New(&p)
	if r.FPS() != 60 || r.Options().MiscLimits || r.Options().CloneLimit <= 300 {
		t.Fatalf("extended JSON discarded runtime settings: %+v at %g FPS", r.Options(), r.FPS())
	}
}

func TestProjectHighQualityPen(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"comments":{"config":{"text":"{\"hq\":true} // _twconfig_"}}}]`)
	if !r.Options().HighQualityPen {
		t.Fatal("project high quality pen setting was ignored")
	}
}

func TestGraphicsExtensionState(t *testing.T) {
	r := motionRuntime(t, `{}`)
	installTestExtensions(t, r, "stretch_setStretch", "DTcameracontrols_setBoth", "runtimeoptions_setCloneLimit")
	th := &thread{target: r.targets[1]}
	call := func(op string, args map[string]any) {
		t.Helper()
		input := func(key string) (any, error) { return args[key], nil }
		var handled bool
		var err error
		if strings.HasPrefix(op, "pen_") {
			handled, err = r.graphicsStatement(th, &project.Block{Opcode: op}, input)
		} else {
			_, handled, err = nativeTestCall(t, r, th, &project.Block{Opcode: op}, input, false)
		}
		if !handled || err != nil {
			t.Fatalf("%s: handled=%v error=%v", op, handled, err)
		}
	}
	call("stretch_setStretch", map[string]any{"X": float64(-50), "Y": float64(200)})
	call("stretch_changeStretchX", map[string]any{"DX": float64(25)})
	if th.target.state.StretchX != -25 || th.target.state.StretchY != 200 {
		t.Fatal(th.target.state)
	}
	call("DTcameracontrols_setBoth", map[string]any{"x": float64(30), "y": float64(40)})
	call("DTcameracontrols_rotateCCW", map[string]any{"val": float64(90)})
	if r.View().X != 30 || r.View().Y != 40 || r.View().Direction != 0 {
		t.Fatal(r.View())
	}
	call("runtimeoptions_setCloneLimit", map[string]any{"limit": float64(1)})
	r.createClone(th.target)
	r.createClone(th.target)
	if r.cloneCount != 1 {
		t.Fatalf("clone limit ignored: %d", r.cloneCount)
	}
	call("pen_setPenSizeTo", map[string]any{"SIZE": float64(2000)})
	call("pen_setPenColorToColor", map[string]any{"COLOR": "#f00"})
	if th.target.state.Pen.Size != 1200 || th.target.state.Pen.Hue != 0 || th.target.state.Pen.Saturation != 100 {
		t.Fatal(th.target.state.Pen)
	}
}

func TestGraphicsNonfiniteStateSnapshot(t *testing.T) {
	r := motionRuntime(t, `{}`)
	th := &thread{target: r.targets[1]}
	call := func(op string, args map[string]any) {
		t.Helper()
		input := func(key string) (any, error) { return args[key], nil }
		var handled bool
		var err error
		if strings.HasPrefix(op, "pen_") {
			handled, err = r.graphicsStatement(th, &project.Block{Opcode: op}, input)
		} else {
			_, handled, err = nativeTestCall(t, r, th, &project.Block{Opcode: op}, input, false)
		}
		if !handled || err != nil {
			t.Fatalf("%s: handled=%v error=%v", op, handled, err)
		}
	}
	call("stretch_setStretch", map[string]any{"X": "not a number", "Y": "Infinity"})
	call("pen_setPenColorParamTo", map[string]any{"COLOR_PARAM": "color", "VALUE": math.Inf(1)})
	data, err := json.Marshal(r.Snapshot())
	if err != nil {
		t.Fatalf("nonfinite graphics state prevented JSON output: %v", err)
	}
	var snapshots []map[string]any
	if err = json.Unmarshal(data, &snapshots); err != nil {
		t.Fatal(err)
	}
	sprite := snapshots[1]
	if sprite["stretchX"] != "NaN" || sprite["stretchY"] != "Infinity" {
		t.Fatalf("incorrect stretch snapshot: %s", data)
	}
	pen, ok := sprite["pen"].(map[string]any)
	if !ok || pen["hue"] != "NaN" {
		t.Fatalf("incorrect pen snapshot: %s", data)
	}
	if !math.IsNaN(th.target.state.StretchX) || !math.IsInf(th.target.state.StretchY, 1) || !math.IsNaN(th.target.state.Pen.Hue) {
		t.Fatal("snapshot changed numeric runtime state")
	}
}
