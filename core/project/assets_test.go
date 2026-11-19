package project

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestReadAssetsDoesNotExtract(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sb3")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, value := range map[string]string{"shape.svg": "<svg/>", "ignored.txt": "ignore"} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	p := &Project{Targets: []*Target{{Costumes: []Costume{{MD5Ext: "shape.svg"}}}}}
	assets, err := ReadAssets(path, p)
	if err != nil {
		t.Fatal(err)
	}
	if string(assets["shape.svg"]) != "<svg/>" || len(assets) != 1 {
		t.Fatalf("assets %v", assets)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("assets extracted to disk")
	}
	p.Targets[0].Costumes[0].MD5Ext = "missing.svg"
	if _, err := ReadAssets(path, p); err == nil {
		t.Fatal("missing asset accepted")
	}
	p.Targets[0].Costumes[0].MD5Ext = "../shape.svg"
	if _, err := ReadAssets(path, p); err == nil {
		t.Fatal("unsafe asset name accepted")
	}
}

func TestReadUnpackedAssets(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "project.json")
	if err := os.WriteFile(manifest, []byte(`{"targets":[{"isStage":true}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shape.svg"), []byte("<svg/>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("ignore"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &Project{Targets: []*Target{{Costumes: []Costume{{MD5Ext: "shape.svg"}}}}}
	for _, input := range []string{dir, manifest} {
		assets, err := ReadAssets(input, p)
		if err != nil {
			t.Fatalf("assets %s: %v", input, err)
		}
		if len(assets) != 1 || string(assets["shape.svg"]) != "<svg/>" {
			t.Fatalf("wrong assets: %v", assets)
		}
	}
}

func TestReadUnpackedAssetLimitsAndConfinement(t *testing.T) {
	for _, kind := range []string{"missing", "oversized", "directory", "traversal", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			name := "shape.svg"
			path := filepath.Join(dir, name)
			switch kind {
			case "oversized":
				f, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				if err = f.Truncate(64<<20 + 1); err != nil {
					t.Fatal(err)
				}
				f.Close()
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "traversal":
				name = "../shape.svg"
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside.svg")
				if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			p := &Project{Targets: []*Target{{Costumes: []Costume{{MD5Ext: name}}}}}
			if _, err := ReadAssets(dir, p); err == nil {
				t.Fatalf("accepted %s asset", kind)
			}
		})
	}
}

func TestReadUnpackedAllowsContainedSymlinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.svg"), []byte("<svg/>"), 0600); err != nil {
		t.Fatal(err)
	}
	// The relative symlink remains within the explicitly selected project root.
	if err := os.Symlink("real.svg", filepath.Join(dir, "shape.svg")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	p := &Project{Targets: []*Target{{Costumes: []Costume{{MD5Ext: "shape.svg"}}}}}
	assets, err := ReadAssets(dir, p)
	if err != nil || string(assets["shape.svg"]) != "<svg/>" {
		t.Fatalf("contained symlink not readable: %v", err)
	}
}
