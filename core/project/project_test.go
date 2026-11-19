package project

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func archive(t *testing.T, names, contents []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "프로젝트.sb3")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for i, name := range names {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(contents[i])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadSB3(t *testing.T) {
	data := `{"targets":[{"isStage":true,"name":"Stage","variables":{"score":["점수",3]},"blocks":{"monitor":[12,"점수","score",0,0],"flag":{"opcode":"event_whenflagclicked","topLevel":true}}}]}`
	p, err := Load(archive(t, []string{"project.json"}, []string{data}))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Targets) != 1 || p.Targets[0].Variables["score"][0] != "점수" || len(p.Targets[0].Blocks) != 1 {
		t.Fatalf("unexpected project: %+v", p)
	}
}

func TestRejectInvalidProjects(t *testing.T) {
	for _, data := range []string{
		`{`,
		`{"targets":[]}`,
		`{"targets":[null]}`,
		`{"targets":[{"isStage":false}]}`,
		`{"targets":[{"isStage":true},{"isStage":true}]}`,
		`{"targets":[{"isStage":true,"variables":{"x":[]}}]}`,
	} {
		t.Run(data, func(t *testing.T) {
			if _, err := Load(archive(t, []string{"project.json"}, []string{data})); err == nil {
				t.Fatal("expected invalid project error")
			}
		})
	}
	for _, names := range [][]string{{"other.json"}, {"project.json", "project.json"}} {
		if _, err := Load(archive(t, names, []string{`{"targets":[{"isStage":true}]}`, `{}`})); err == nil {
			t.Fatal("expected missing/duplicate manifest error")
		}
	}
}

func TestLoadLists(t *testing.T) {
	data := `{"targets":[{"isStage":true,"lists":{"items":["items",[1,"02",true]]}}]}`
	p, err := Load(archive(t, []string{"project.json"}, []string{data}))
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Targets[0].Lists["items"][1].([]any); len(got) != 3 || got[1] != "02" {
		t.Fatalf("lost list values: %v", got)
	}
}

func TestRejectMalformedLists(t *testing.T) {
	for _, value := range []string{`[]`, `[1,[]]`, `["items",[{}]]`} {
		data := `{"targets":[{"isStage":true,"lists":{"items":` + value + `}}]}`
		if _, err := Load(archive(t, []string{"project.json"}, []string{data})); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}

func TestTargetAppearanceDefaultsAndExplicitValues(t *testing.T) {
	data := `{"targets":[{"isStage":true},{"name":"default"},{"name":"explicit","direction":0,"size":0,"visible":false,"rotationStyle":"left-right"},{"name":"visible","size":125,"visible":true,"rotationStyle":"invalid"}]}`
	p, err := Load(archive(t, []string{"project.json"}, []string{data}))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Targets[0].Visible || p.Targets[1].Direction != 90 || p.Targets[1].Size != 100 || p.Targets[1].RotationStyle != "all around" {
		t.Fatal("incorrect defaults")
	}
	if p.Targets[2].Direction != 0 || p.Targets[2].Size != 0 || p.Targets[2].Visible || p.Targets[2].RotationStyle != "left-right" {
		t.Fatal("explicit zero/false lost")
	}
	if !p.Targets[3].Visible || p.Targets[3].Size != 125 || p.Targets[3].RotationStyle != "all around" {
		t.Fatal("appearance data lost")
	}
}

func TestLoadUnpackedProject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project.json")
	data := `{"targets":[{"isStage":true,"name":"Stage","variables":{"score":["score",42]}}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{dir, path} {
		p, err := Load(input)
		if err != nil {
			t.Fatalf("load %s: %v", input, err)
		}
		if p.Targets[0].Variables["score"][1] != float64(42) {
			t.Fatal("unpacked manifest lost state")
		}
	}
}

func TestLoadUnpackedRejectsInvalidManifests(t *testing.T) {
	for _, kind := range []string{"missing", "malformed", "oversized", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "project.json")
			switch kind {
			case "malformed":
				if err := os.WriteFile(path, []byte(`{`), 0600); err != nil {
					t.Fatal(err)
				}
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
			}
			if _, err := Load(dir); err == nil {
				t.Fatalf("accepted %s manifest", kind)
			}
		})
	}
}

func TestLoadNullAndMissingInitialValues(t *testing.T) {
	data := `{"targets":[{"isStage":true,"variables":{"null":["null",null],"missing":["missing"]},"lists":{"values":["values",[null,1]],"missing":["missing"],"null":["null",null]}}]}`
	p, err := Load(archive(t, []string{"project.json"}, []string{data}))
	if err != nil {
		t.Fatal(err)
	}
	stage := p.Targets[0]
	if stage.Variables["null"][1] != nil {
		t.Fatal("null initial variable changed")
	}
	if _, ok := stage.Variables["missing"][1].(Undefined); !ok {
		t.Fatal("missing variable did not retain undefined")
	}
	if items := stage.Lists["values"][1].([]any); len(items) != 2 || items[0] != nil || items[1] != float64(1) {
		t.Fatal("null list item changed")
	}
	for _, id := range []string{"missing", "null"} {
		if len(stage.Lists[id][1].([]any)) != 0 {
			t.Fatal("missing list must start empty")
		}
	}
}

func TestArchiveBytesPreservesManifestAndReferencedAssets(t *testing.T) {
	dir := t.TempDir()
	manifest := []byte("{\n  \"targets\": [{\"isStage\":true,\"costumes\":[{\"md5ext\":\"shape.svg\"}]}]\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "project.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shape.svg"), []byte("<svg/>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored"), []byte("not embedded"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := ArchiveBytes(dir)
	if err != nil {
		t.Fatal(err)
	}
	packed := filepath.Join(t.TempDir(), "project.sb3")
	if err = os.WriteFile(packed, data, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(packed)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := ReadAssets(packed, p)
	if err != nil || len(assets) != 1 || string(assets["shape.svg"]) != "<svg/>" {
		t.Fatalf("wrong archived assets: %v %v", assets, err)
	}
	actual, err := readManifest(packed)
	if err != nil || string(actual) != string(manifest) {
		t.Fatal("manifest formatting was changed")
	}
	same, err := ArchiveBytes(packed)
	if err != nil || string(same) != string(data) {
		t.Fatal("original SB3 bytes changed")
	}
	z, err := zip.OpenReader(packed)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if len(z.File) != 2 {
		t.Fatal("unreferenced files embedded")
	}
}

func TestLoadNonarrayInitialListsDefaultEmpty(t *testing.T) {
	for _, value := range []string{`5`, `true`, `"text"`, `{}`} {
		data := `{"targets":[{"isStage":true,"lists":{"items":["items",` + value + `]}}]}`
		p, err := Load(archive(t, []string{"project.json"}, []string{data}))
		if err != nil {
			t.Fatalf("list value %s: %v", value, err)
		}
		if len(p.Targets[0].Lists["items"][1].([]any)) != 0 {
			t.Fatalf("nonarray list %s did not default empty", value)
		}
	}
}

func TestSavedScriptOrder(t *testing.T) {
	var target Target
	err := json.Unmarshal([]byte(`{"blocks":{
  "z":{"opcode":"event_whenflagclicked","topLevel":true},
  "10":{"opcode":"event_whenflagclicked","topLevel":true},
  "a":{"opcode":"event_whenflagclicked","topLevel":true},
  "2":{"opcode":"event_whenflagclicked","topLevel":true},
  "01":{"opcode":"event_whenflagclicked","topLevel":true},
  "4294967295":{"opcode":"event_whenflagclicked","topLevel":true},
  "0":{"opcode":"event_whenflagclicked","topLevel":true},
  "reporter":[12,"score","score",0,0],
  "body":{"opcode":"looks_show"}
 }}`), &target)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"0", "2", "10", "z", "a", "01", "4294967295"}
	if !slices.Equal(target.ScriptOrder, want) {
		t.Fatalf("script order = %v, want %v", target.ScriptOrder, want)
	}
}

func TestBlockDecodeValidation(t *testing.T) {
	for _, data := range []string{`[]`, `42`, `{"bad":null}`, `{"bad":{}}`, `{"bad":{"opcode":1}}`} {
		var target Target
		if err := json.Unmarshal([]byte(`{"blocks":`+data+`}`), &target); err == nil {
			t.Errorf("accepted invalid blocks: %s", data)
		}
	}
	for _, data := range []string{`{}`, `{"blocks":null}`, `{"blocks":{}}`} {
		var target Target
		if err := json.Unmarshal([]byte(data), &target); err != nil {
			t.Errorf("empty blocks: %v", err)
		}
	}
}
