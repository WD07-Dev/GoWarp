package vm

import (
	"encoding/base64"
	"fmt"
	"testing"

	"gowarp/core/project"
)

type assetGraphics struct {
	Graphics
	count int
}

func (g *assetGraphics) AddCostume(_ int, meta project.Costume, _ []byte) (project.Costume, error) {
	g.count++
	return meta, nil
}
func (g *assetGraphics) DeleteCostume(_ int, _ int) error {
	g.count--
	return nil
}
func (g *assetGraphics) Snapshot(_ []DisplayTarget) ([]byte, error) {
	return []byte("snapshot pixels"), nil
}

func TestDynamicCostumeMetadataAndCloneSelection(t *testing.T) {
	r := motionRuntime(t, `{}`)
	target := r.targets[len(r.targets)-1]
	const costumeCount = 17
	target.source.Costumes = make([]project.Costume, costumeCount)
	for i := range target.source.Costumes {
		target.source.Costumes[i].Name = fmt.Sprintf("costume%d", i)
	}
	target.source.Costumes[0].Name = "first"
	target.source.Costumes[1].Name = "second"
	g := &assetGraphics{count: costumeCount}
	r.graphics = g
	c := &extensionContext{r: r, th: &thread{target: target}}
	r.createClone(target)
	clone := r.targets[len(r.targets)-1]
	setCostume(clone, 1)
	costumeIndex(target, "second")
	if r.costumeNames[target.source] == nil {
		t.Fatal("test did not populate the costume name cache")
	}
	if _, err := c.assetsCall("assets.deleteCostume", []any{"first"}); err != nil {
		t.Fatal(err)
	}
	if clone.state.CurrentCostume != 0 || clone.state.CostumeName != "second" || g.count != costumeCount-1 {
		t.Fatal("delete did not preserve clone costume")
	}
	if index, ok := costumeIndex(target, "second"); !ok || index != 0 {
		t.Fatal("deleting a costume left a stale name index")
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("fixture"))
	if _, err := c.assetsCall("assets.addCostume", []any{uri, "capture"}); err != nil {
		t.Fatal(err)
	}
	if target.state.CurrentCostume != costumeCount-1 || target.state.CostumeName != "capture" || g.count != costumeCount {
		t.Fatal("import did not select new costume")
	}
	if index, ok := costumeIndex(target, "capture"); !ok || index != costumeCount-1 {
		t.Fatal("costume name cache stale")
	}
	snapshot, err := c.assetsCall("assets.snapshot", nil)
	if err != nil || snapshot != "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("snapshot pixels")) {
		t.Fatalf("snapshot reporter: %v/%v", snapshot, err)
	}
}

func TestCostumeURLValidation(t *testing.T) {
	data, format, err := costumeURL("data:image/svg+xml,%3Csvg%2F%3E")
	if err != nil || format != "svg" || string(data) != "<svg/>" {
		t.Fatalf("SVG data URL: %q/%s/%v", data, format, err)
	}
	for _, source := range []string{"file:///tmp/costume.png", "data:text/plain,not-a-costume", "data:image/png;base64,broken"} {
		if _, _, err := costumeURL(source); err == nil {
			t.Fatalf("accepted invalid costume URL %q", source)
		}
	}
}
