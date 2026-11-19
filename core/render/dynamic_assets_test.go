package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestDynamicCostumeSnapshotRoundTrip(t *testing.T) {
	lib := &media.Library{Costumes: [][]*media.Costume{{}}}
	r := New(lib)
	im := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, im); err != nil {
		t.Fatal(err)
	}
	meta, err := r.AddCostume(0, project.Costume{Name: "captured", DataFormat: "png", BitmapResolution: 1}, encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if meta.RotationCenterX != 2 || meta.RotationCenterY != 1 {
		t.Fatalf("import center: %+v", meta)
	}
	scene := []vm.DisplayTarget{{Source: 0, State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}}
	data, err := r.Snapshot(scene)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Bounds() != image.Rect(0, 0, 480, 360) || color.NRGBAModel.Convert(snapshot.At(240, 180)) != (color.NRGBA{R: 255, A: 255}) {
		t.Fatal("snapshot lost rendered costume pixels")
	}
	if _, err = r.AddCostume(0, project.Costume{Name: "snapshot", DataFormat: "png", BitmapResolution: 1}, data); err != nil {
		t.Fatal(err)
	}
	if len(lib.Costumes[0]) != 2 || lib.Costumes[0][1].Width != 480 {
		t.Fatal("snapshot not imported as real costume")
	}
	if err = r.DeleteCostume(0, 0); err != nil {
		t.Fatal(err)
	}
	if len(lib.Costumes[0]) != 1 || lib.Costumes[0][0].Width != 480 {
		t.Fatal("delete did not update renderer costume indexes")
	}
}
