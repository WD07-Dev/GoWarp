package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gowarp/core/project"
)

func TestRuntimePreparesReferencedShortSounds(t *testing.T) {
	dir := t.TempDir()
	data := wavFixture(1, 1, 1, 8, nil, []byte{128, 192, 64, 128}, nil)
	want, err := DecodeSound("wav", data)
	if err != nil {
		t.Fatal(err)
	}
	target := &project.Target{IsStage: true, Blocks: project.Blocks{
		"menu": {Opcode: "sound_sounds_menu", Fields: map[string][]any{"SOUND_MENU": {"used", nil}}},
		"play": {Opcode: "sound_play", Inputs: map[string][]any{"SOUND_MENU": {1, []any{10, "literal"}}}},
	}}
	for _, name := range []string{"used", "literal", "unused"} {
		if err := os.WriteFile(filepath.Join(dir, name+".wav"), data, 0600); err != nil {
			t.Fatal(err)
		}
		target.Sounds = append(target.Sounds, project.Sound{Name: name, MD5Ext: name + ".wav", DataFormat: "wav"})
	}
	lib, err := LoadRuntime(context.Background(), dir, &project.Project{Targets: []*project.Target{target}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	for _, name := range []string{"used", "literal", "unused"} {
		if err := os.Remove(filepath.Join(dir, name+".wav")); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		sound, err := lib.Sound(0, i)
		if err != nil || !slices.Equal(sound.Samples, want.Samples) {
			t.Fatalf("referenced sound was not prepared correctly: %v", err)
		}
	}
	if lib.cache.entries["sound:wav:unused.wav"] != nil {
		t.Fatal("unreferenced sound was decoded")
	}
}

func TestRuntimeSoundPreparationLimits(t *testing.T) {
	for _, tc := range []struct {
		name             string
		frames, prepared int
	}{{"count", 4, 32}, {"bytes", 80000, 13}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := &project.Target{IsStage: true, Blocks: project.Blocks{}}
			data := wavFixture(1, 1, 1, 8, nil, make([]byte, tc.frames), nil)
			for i := 0; i < 40; i++ {
				name := fmt.Sprintf("effect%d", i)
				if err := os.WriteFile(filepath.Join(dir, name+".wav"), data, 0600); err != nil {
					t.Fatal(err)
				}
				target.Sounds = append(target.Sounds, project.Sound{Name: name, MD5Ext: name + ".wav", DataFormat: "wav"})
				target.Blocks[name] = &project.Block{Opcode: "sound_sounds_menu", Fields: map[string][]any{"SOUND_MENU": {name, nil}}}
			}
			p := &project.Project{Targets: []*project.Target{target}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lib, err := LoadRuntime(ctx, dir, p, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer lib.Close()
			if len(lib.cache.entries) != tc.prepared || lib.cache.bytes > 8<<20 {
				t.Fatalf("preparation exceeded budget: count=%d bytes=%d", len(lib.cache.entries), lib.cache.bytes)
			}
			cancel()
			if err := lib.prepareReferencedSounds(p, nil); err != context.Canceled {
				t.Fatalf("cancellation ignored: %v", err)
			}
		})
	}
}

func TestRuntimePreparesLiteralCostumes(t *testing.T) {
	dir, p := referencedCostumeFixture(t)
	p.Targets[1].Blocks = project.Blocks{
		"switch": {Opcode: "looks_switchcostumeto", Inputs: map[string][]any{"COSTUME": {1, []any{10, p.Targets[1].Costumes[1].Name}}}},
	}
	lib, err := LoadRuntime(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if err := os.Remove(filepath.Join(dir, "used.png")); err != nil {
		t.Fatal(err)
	}
	if lib.CostumeImage(lib.Costumes[1][1]) == nil {
		t.Fatalf("literal costume was not prepared: %v", lib.Err())
	}
}
