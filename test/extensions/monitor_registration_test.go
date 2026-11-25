package extensions

import (
	"context"
	"encoding/json"
	"testing"

	"gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestAssetAndCloneMonitors(t *testing.T) {
	want := map[string]string{
		"lmsAssets_getAllSprites":   "[\"Sprite\"]",
		"lmsAssets_getAllCostumes":  "[\"backdrop\"]",
		"lmsAssets_getAllSounds":    "[\"chime\"]",
		"ShovelUtils_getAllSprites": "[\"Stage\",\"Sprite\"]",
		"lmsclonesplus_cloneCount":  "0",
	}
	p := &project.Project{}
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"name":"Stage","costumes":[{"name":"backdrop"}],"sounds":[{"name":"chime"}]},{"name":"Sprite"}]}`), p); err != nil {
		t.Fatal(err)
	}
	var bundle extension.Bundle
	for _, factory := range Bundle() {
		switch factory().ID {
		case "lmsAssets", "ShovelUtils", "lmsclonesplus":
			bundle = append(bundle, factory)
		}
	}
	for opcode := range want {
		p.Monitors = append(p.Monitors, project.Monitor{ID: opcode, Opcode: opcode, Visible: true})
	}
	r := vm.New(p)
	if err := r.SetExtensions(bundle); err != nil {
		t.Fatal(err)
	}
	if err := r.Tick(context.Background(), 60, false); err != nil {
		t.Fatal(err)
	}
	monitors := r.VisibleMonitors()
	if len(monitors) != len(want) {
		t.Fatalf("visible monitors = %d; want %d", len(monitors), len(want))
	}
	for _, m := range monitors {
		if m.Value != want[m.ID] || m.Label == m.ID {
			t.Errorf("%s: label=%q value=%q; want %q", m.ID, m.Label, m.Value, want[m.ID])
		}
	}
}
