package media

import (
	"bytes"
	"context"
	"testing"

	"gowarp/app/assets"
	"gowarp/core/project"
)

func TestSVGUsesProjectFont(t *testing.T) {
	costume := project.Costume{MD5Ext: "text.svg", DataFormat: "svg"}
	data := []byte(`<svg width="160" height="40"><text x="2" y="30" font-family="Project Korean" font-size="24">한글 테스트</text></svg>`)
	p := &project.Project{
		CustomFonts: []project.CustomFont{{Family: "Project Korean", MD5Ext: "font.otf"}},
		Targets:     []*project.Target{{IsStage: true, Costumes: []project.Costume{costume}}},
	}
	path := writeMediaProject(t, p, map[string][]byte{"text.svg": data, "font.otf": assets.PretendardRegular})
	p, err := project.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := LoadRuntime(context.Background(), path, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if lib.customFont("Project Korean") == nil {
		t.Fatal("project font was not loaded")
	}
	for _, scale := range []float64{1, 4} {
		img := lib.CostumeImageAtScale(lib.Costumes[0][0], scale)
		if img == nil {
			t.Fatalf("scale %g: %v", scale, lib.Err())
		}
		if img.Bounds().Dx() != int(160*scale) || img.Bounds().Dy() != int(40*scale) {
			t.Fatalf("scale %g fell back to a low resolution image", scale)
		}
		visible := false
		for i := 3; i < len(img.Pix); i += 4 {
			visible = visible || img.Pix[i] != 0
		}
		if !visible {
			t.Fatalf("scale %g: Korean text is blank", scale)
		}
		fallback, _, err := rasterSVGWithFonts(data, scale, nil)
		if err == nil && bytes.Equal(img.Pix, fallback.Pix) {
			t.Fatalf("scale %g: custom font rendered as the fallback font", scale)
		}
	}
	if err := lib.EnableDisplayPreparation(path, p); err != nil {
		t.Fatal(err)
	}
	lib.DisplayCostumeImageKey(lib.Costumes[0][0], 8, nil)
	/*
		Rasterize on the host too: shared font outlines must be safe to
		read concurrently, and the worker must use the project font.
	*/
	want, _, err := rasterSVGWithFonts(data, 8, lib.customFont)
	if err != nil {
		t.Fatal(err)
	}
	waitDisplayDensity(t, lib, lib.Costumes[0][0], 8)
	got := lib.CostumeImageAtScale(lib.Costumes[0][0], 8)
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("background preparation changed the project font")
	}
}
