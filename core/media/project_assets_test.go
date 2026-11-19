package media

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gowarp/core/project"
)

func writeMediaProject(t *testing.T, p *project.Project, assets map[string][]byte) string {
	t.Helper()
	manifest, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	write := func(name string, contents []byte) {
		t.Helper()
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	write("project.json", manifest)
	for name, contents := range assets {
		write(name, contents)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "media.sb3")
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mixedMediaProject(t *testing.T) (string, *project.Project) {
	t.Helper()
	pixels := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			pixels.SetNRGBA(x, y, color.NRGBA{B: 255, A: 255})
		}
	}
	var bitmap bytes.Buffer
	if err := png.Encode(&bitmap, pixels); err != nil {
		t.Fatal(err)
	}
	uri := embeddedImage(t, "png", color.NRGBA{B: 255, A: 255})
	p := &project.Project{Targets: []*project.Target{
		{Name: "Stage", IsStage: true, Costumes: []project.Costume{{MD5Ext: "backdrop.svg", DataFormat: "svg"}},
			Sounds: []project.Sound{{MD5Ext: "tone.wav", DataFormat: "wav"}}},
		{Name: "Sprite", Costumes: []project.Costume{
			{MD5Ext: "bitmap.png", DataFormat: "png", BitmapResolution: 2},
			{MD5Ext: "mixed.svg", DataFormat: "svg"},
			{MD5Ext: "text.svg", DataFormat: "svg"},
			{},
		}, Sounds: []project.Sound{{MD5Ext: "tone.wav", DataFormat: "wav"}}},
		{Name: "Empty"},
	}}
	path := writeMediaProject(t, p, map[string][]byte{
		"backdrop.svg": []byte(`<svg width="16" height="12"><rect width="16" height="12" fill="red"/></svg>`),
		"bitmap.png":   bitmap.Bytes(),
		"mixed.svg":    []byte(`<svg width="16" height="12"><rect width="16" height="12" fill="red"/><g transform="translate(4 2) scale(2)"><image width="2" height="2" href="` + uri + `"/></g><path d="M6 2h2v4h-2z" fill="lime"/></svg>`),
		"text.svg":     []byte(`<svg width="80" height="32"><text x="2" y="24" font-size="20">Hi<tspan fill="blue">!</tspan></text></svg>`),
		"tone.wav":     wavFixture(1, 1, 2, 16, nil, []byte{0, 0, 0xff, 0x3f, 0, 0, 0, 0xc0}, nil),
	})
	loaded, err := project.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, loaded
}

func TestVisualProjectAssetsLoad(t *testing.T) {
	path, p := mixedMediaProject(t)
	// A nonexistent sound proves visual loading never even reads audio assets.
	p.Targets[0].Sounds[0].MD5Ext = "missing.wav"
	lib, err := LoadVisual(path, p)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if len(lib.Costumes) != len(p.Targets) || len(lib.Sounds) != len(p.Targets) {
		t.Fatal("missing target assets")
	}
	for i, target := range p.Targets {
		if len(lib.Costumes[i]) != len(target.Costumes) {
			t.Fatalf("%s: missing costumes", target.Name)
		}
		for j, costume := range lib.Costumes[i] {
			if costume.Image == nil {
				t.Fatalf("%s costume %d: missing pixels", target.Name, j)
			}
		}
		if len(lib.Sounds[i]) != 0 {
			t.Fatalf("%s: visual loading decoded sounds", target.Name)
		}
	}
	if p.Targets[0].Sounds[0].MD5Ext != "missing.wav" || len(p.Targets[1].Sounds) != 1 {
		t.Fatal("visual loading mutated the project's sounds")
	}
}
