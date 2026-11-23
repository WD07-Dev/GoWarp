package vm

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"gowarp/core/project"
)

func looksRuntime(t *testing.T, blocks string) *Runtime {
	return eventProject(t, `[{"isStage":true,"name":"Stage","costumes":[{"name":"day"},{"name":"night"}],"variables":{"score":["score",0]},"blocks":{}},{"name":"Sprite","visible":true,"currentCostume":1,"costumes":[{"name":"2"},{"name":"walking"},{"name":"jump"}],"blocks":`+blocks+`}]`)
}

func TestCostumeSelection(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  int
	}{
		{"2", 0},
		{float64(2), 1},
		{"previous costume", 0},
		{"next costume", 2},
		{"missing", 1},
		{"-1", 1},
		{"Infinity", 0},
		{" ", 1},
	} {
		r := looksRuntime(t, `{}`)
		b := &project.Block{Opcode: "looks_switchcostumeto", Inputs: literalInputs(map[string]any{"COSTUME": tc.value})}
		r.targets[1].source.Blocks["flag"] = &project.Block{
			Opcode:   "event_whenflagclicked",
			Next:     "switch",
			TopLevel: true,
		}
		r.targets[1].source.Blocks["switch"] = b
		r.startHat(r.targets[1], "flag", false)
		run(t, r, 1, false)
		if r.Snapshot()[1].CurrentCostume != tc.want {
			t.Fatalf("%v: got %d want %d", tc.value, r.Snapshot()[1].CurrentCostume, tc.want)
		}
		got, err := evalBlock(t, r, 1, &project.Block{Opcode: "looks_costumenumbername", Fields: map[string][]any{"NUMBER_NAME": {"number"}}})
		if err != nil || got != float64(tc.want+1) {
			t.Fatalf("report: %v %v", got, err)
		}
	}
}

func TestCostumeNameSelection(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, tc := range []struct {
			name     string
			value    any
			backdrop bool
			want     int
		}{
			{"duplicate", "same", false, 0},
			{"case", "Same", false, 2},
			{"numeric name", "2", false, 3},
			{"numeric index", float64(2), false, 1},
			{"numeric fallback", "6", false, 5},
			{"named next", "next costume", false, 4},
			{"named random", "random backdrop", true, 5},
			{"previous", "previous costume", false, 6},
			{"next backdrop", "next backdrop", true, 8},
			{"previous backdrop", "previous backdrop", true, 6},
			{"unknown", "missing", false, 7},
			{"case miss", "SAME", false, 7},
		} {
			t.Run(strconv.FormatBool(owned)+"/"+tc.name, func(t *testing.T) {
				source := &project.Target{Costumes: make([]project.Costume, 32)}
				for i := range source.Costumes {
					source.Costumes[i].Name = "costume" + strconv.Itoa(i)
				}
				for i, name := range []string{"same", "same", "Same", "2", "next costume", "random backdrop"} {
					source.Costumes[i].Name = name
				}
				sprite := &target{source: source}
				if owned {
					sprite.owner = &Runtime{}
				}
				// Repeat to exercise both initial indexing and cached selection.
				for j := 0; j < 2; j++ {
					sprite.state.CurrentCostume = 7
					switchCostume(sprite, tc.value, tc.backdrop)
					if sprite.state.CurrentCostume != tc.want {
						t.Fatalf("got costume %d, want %d", sprite.state.CurrentCostume, tc.want)
					}
				}
			})
		}
	}
}

func BenchmarkSwitchCostumeName(b *testing.B) {
	for _, count := range []int{3, 704, 5210} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			source := &project.Target{Costumes: make([]project.Costume, count)}
			for i := range source.Costumes {
				source.Costumes[i].Name = "costume" + strconv.Itoa(i)
			}
			sprite := &target{source: source, owner: &Runtime{}}
			var name any = source.Costumes[count-1].Name
			switchCostume(sprite, name, false)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				switchCostume(sprite, name, false)
			}
		})
	}
}

func TestCostumeNameCacheSharedWithClonesAndBounded(t *testing.T) {
	source := &project.Target{Costumes: make([]project.Costume, 32)}
	for i := range source.Costumes {
		source.Costumes[i].Name = "costume" + strconv.Itoa(i)
	}
	r := New(&project.Project{Targets: []*project.Target{source}})
	sprite := r.targets[0]
	switchCostume(sprite, "costume31", false)
	r.createClone(sprite)
	if len(r.targets) != 2 {
		t.Fatal("clone not created")
	}
	clone := r.targets[1]
	switchCostume(clone, "costume20", false)
	for i := 0; i < 100; i++ {
		switchCostume(clone, "unknown"+strconv.Itoa(i), false)
	}
	if sprite.state.CurrentCostume != 31 || clone.state.CurrentCostume != 20 {
		t.Fatalf("costume selection crossed clone state: original=%d clone=%d", sprite.state.CurrentCostume, clone.state.CurrentCostume)
	}
	if len(r.costumeNames) != 1 || len(r.costumeNames[source]) != 32 {
		t.Fatal("clones or unknown names added cache entries")
	}
}

func TestRandomBackdropExcludesCurrentCostume(t *testing.T) {
	source := &project.Target{Costumes: make([]project.Costume, 32)}
	for i := range source.Costumes {
		source.Costumes[i].Name = "backdrop" + strconv.Itoa(i)
	}
	sprite := &target{source: source, owner: &Runtime{}}
	for i := 0; i < 100; i++ {
		previous := sprite.state.CurrentCostume
		switchCostume(sprite, "random backdrop", true)
		if sprite.state.CurrentCostume == previous || sprite.state.CurrentCostume < 0 || sprite.state.CurrentCostume >= 32 {
			t.Fatalf("invalid random backdrop %d after %d", sprite.state.CurrentCostume, previous)
		}
	}
}

func TestBackdropWaitAndEvents(t *testing.T) {
	r := looksRuntime(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"switch"},
 "switch":{"opcode":"looks_switchbackdroptoandwait","next":"done","inputs":{"BACKDROP":[1,[10,"night"]]}},
 "done":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"10"]]}},
 "night":{"opcode":"event_whenbackdropswitchesto","topLevel":true,"next":"wait","fields":{"BACKDROP":["night",null]}},
 "wait":{"opcode":"control_wait","next":"inc","inputs":{"DURATION":[1,[4,"0.1"]]}},
 "inc":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}}
 }`)
	run(t, r, 1, false)
	if r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 0 || r.Snapshot()[0].CurrentCostume != 1 {
		t.Fatal("did not wait for backdrop receiver")
	}
	run(t, r, 10, false)
	if !r.Done() || number(r.Snapshot()[0].Variables["score"].Value) != 11 {
		t.Fatal("backdrop receiver not completed")
	}
}

func TestTimedBubbleOwnership(t *testing.T) {
	r := looksRuntime(t, `{
 "a":{"opcode":"event_whenflagclicked","topLevel":true,"next":"timed"},
 "timed":{"opcode":"looks_sayforsecs","inputs":{"MESSAGE":[1,[10,"old"]],"SECS":[1,[4,"0.1"]]}},
 "b":{"opcode":"event_whenflagclicked","topLevel":true,"next":"delay"},
 "delay":{"opcode":"control_wait","next":"new","inputs":{"DURATION":[1,[4,"0"]]}},
 "new":{"opcode":"looks_think","inputs":{"MESSAGE":[1,[10,"new"]]}}
 }`)
	run(t, r, 1, false)
	if r.Snapshot()[1].Bubble != "old" {
		t.Fatal("missing timed bubble")
	}
	run(t, r, 10, false)
	if s := r.Snapshot()[1]; s.Bubble != "new" || !s.Think {
		t.Fatalf("old timer cleared replacement: %+v", s)
	}
}

func TestTimedBubbleClearsAndCloneHasNoSpeech(t *testing.T) {
	r := looksRuntime(t, `{
 "a":{"opcode":"event_whenflagclicked","topLevel":true,"next":"timed"},
 "timed":{"opcode":"looks_sayforsecs","inputs":{"MESSAGE":[1,[10,"hello"]],"SECS":[1,[4,"0.1"]]}},
 "b":{"opcode":"event_whenflagclicked","topLevel":true,"next":"clone"},
 "clone":{"opcode":"control_create_clone_of","inputs":{"CLONE_OPTION":[1,[10,"_myself_"]]}}
 }`)
	run(t, r, 1, false)
	if r.Snapshot()[2].Bubble != "" {
		t.Fatal("clone copied speech")
	}
	run(t, r, 10, false)
	if r.Snapshot()[1].Bubble != "" {
		t.Fatal("timed speech not cleared")
	}
}

func TestGraphicEffectsCloneAndSnapshotIsolation(t *testing.T) {
	r := looksRuntime(t, `{
 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"effect"},
 "effect":{"opcode":"looks_seteffectto","next":"clone","fields":{"EFFECT":["ghost",null]},"inputs":{"VALUE":[1,[4,"150"]]}},
 "clone":{"opcode":"control_create_clone_of","next":"clear","inputs":{"CLONE_OPTION":[1,[10,"_myself_"]]}},
 "clear":{"opcode":"looks_cleargraphiceffects"}
 }`)
	run(t, r, 1, false)
	s := r.Snapshot()
	if s[1].Effects.Ghost != 0 || s[2].Effects.Ghost != 100 {
		t.Fatal("effects aliased or unclamped")
	}
	s[2].Effects.Ghost = 12
	if r.Snapshot()[2].Effects.Ghost != 100 {
		t.Fatal("snapshot aliases effects")
	}
}

func TestBubbleLimitAndNonfiniteEffects(t *testing.T) {
	r := looksRuntime(t, `{}`)
	r.targets[1].source.Blocks["say"] = &project.Block{
		Opcode: "looks_say",
		Next:   "effect",
		Inputs: literalInputs(map[string]any{"MESSAGE": strings.Repeat("a", 400)}),
	}
	r.targets[1].source.Blocks["effect"] = &project.Block{
		Opcode: "looks_seteffectto",
		Inputs: literalInputs(map[string]any{"VALUE": "Infinity"}),
		Fields: map[string][]any{"EFFECT": {"color"}},
	}
	r.threads = append(r.threads, &thread{target: r.targets[1], pc: "say"})
	run(t, r, 1, false)
	if len(r.Snapshot()[1].Bubble) != 330 {
		t.Fatal("bubble not limited")
	}
	if _, err := json.Marshal(r.Snapshot()); err != nil {
		t.Fatal(err)
	}
}

func TestLayerMoves(t *testing.T) {
	r := eventProject(t, `[{"isStage":true,"name":"Stage"},{"name":"A","layerOrder":2,"blocks":{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"back"},"back":{"opcode":"looks_gotofrontback","fields":{"FRONT_BACK":["back",null]}}}},{"name":"B","layerOrder":1}]`)
	run(t, r, 1, false)
	if r.targets[1].state.Name != "A" || r.targets[1].state.LayerOrder != 1 || r.targets[2].state.LayerOrder != 2 {
		t.Fatal("layer order not updated")
	}
}

// Only the optional preparation callback is exercised; geometry stays unused.
type costumePreparationGraphics struct {
	Graphics
	selected [][2]int
}

func (g *costumePreparationGraphics) PrepareCostume(source, costume int) {
	g.selected = append(g.selected, [2]int{source, costume})
}

func TestCostumeSelectionPreparesBeforeDrawing(t *testing.T) {
	r := looksRuntime(t, `{}`)
	graphics := &costumePreparationGraphics{}
	r.graphics = graphics
	sprite := r.targets[1]
	sprite.state.Visible = false
	sprite.state.CloneID = 7
	switchCostume(sprite, "jump", false)
	switchCostume(r.stage, "night", true)
	if len(graphics.selected) != 2 {
		t.Fatalf("selected costumes not prepared at block execution: %d", len(graphics.selected))
	}
	got := graphics.selected[0]
	if got != [2]int{1, 2} {
		t.Fatalf("hidden clone preparation used wrong source: %+v", got)
	}
	if got := graphics.selected[1]; got != [2]int{0, 1} {
		t.Fatalf("backdrop preparation used wrong source: %+v", got)
	}
}

func TestRepeatedCostumeSelectionDoesNotRepeatPreparation(t *testing.T) {
	r := looksRuntime(t, `{}`)
	graphics := &costumePreparationGraphics{}
	r.graphics = graphics
	sprite := r.targets[1]
	for range 100 {
		switchCostume(sprite, "jump", false)
	}
	if len(graphics.selected) != 1 {
		t.Fatalf("unchanged costume repeated preparation %d times", len(graphics.selected))
	}
	sprite.state.Text = &TextSkin{Visible: true}
	switchCostume(sprite, "jump", false)
	if len(graphics.selected) != 2 || sprite.state.Text.Visible {
		t.Fatal("return from text to the same costume did not prepare its pixels")
	}
	switchCostume(sprite, "walking", false)
	if len(graphics.selected) != 3 || graphics.selected[2] != [2]int{1, 1} {
		t.Fatal("new costume selection lost its preparation request")
	}
}
