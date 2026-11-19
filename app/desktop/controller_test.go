package desktop

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"testing"

	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestControllerClickAndAnswer(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"variables":{"v":["v",0]}},{"name":"Sprite","visible":true,"draggable":true,"costumes":[{"name":"c"}],"blocks":{"hat":{"opcode":"event_whenthisspriteclicked","topLevel":true,"next":"ask"},"ask":{"opcode":"sensing_askandwait","next":"set","inputs":{"QUESTION":[1,[10,"name?"]]}},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["v","v"]},"inputs":{"VALUE":[2,"answer"]}},"answer":{"opcode":"sensing_answer"}}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	im := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	lib := &media.Library{
		Costumes: [][]*media.Costume{nil, {{
			Image:   im,
			Width:   20,
			Height:  20,
			CenterX: 10,
			CenterY: 10,
		}}},
		Sounds: [][]*media.Sound{nil, nil},
	}
	c := newController(&p, lib, Options{FPS: 30, Mute: true})
	if err := c.advance(context.Background(), Input{
		MouseX:       240,
		MouseY:       ToolbarHeight + 180,
		MouseDown:    true,
		MousePressed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Question() == nil {
		t.Fatal("sprite click did not start question")
	}
	if err := c.advance(context.Background(), Input{
		MouseX:    250,
		MouseY:    ToolbarHeight + 170,
		MouseDown: true,
		Text:      "Go한글",
	}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Scene()[1].State.X != 10 || c.vm.Scene()[1].State.Y != 10 {
		t.Fatal("drag did not move sprite")
	}
	if err := c.advance(context.Background(), Input{Enter: true}); err != nil {
		t.Fatal(err)
	}
	if c.vm.Question() != nil || c.vm.Snapshot()[0].Variables["v"].Value != "Go한글" {
		t.Fatal("answer not delivered")
	}
}

func TestStartingDragRaisesSprite(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true},{"name":"Dragged","visible":true,"draggable":true,"layerOrder":1},{"name":"Other","visible":true,"x":100,"layerOrder":2}]}`), &p); err != nil {
		t.Fatal(err)
	}
	im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	costume := &media.Costume{
		Image:   im,
		Width:   2,
		Height:  2,
		CenterX: 1,
		CenterY: 1,
	}
	lib := &media.Library{Costumes: [][]*media.Costume{nil, {costume}, {costume}}}
	c := newController(&p, lib, Options{FPS: 30})
	if err := c.advance(context.Background(), Input{
		MouseX:       240,
		MouseY:       ToolbarHeight + 180,
		MouseDown:    true,
		MousePressed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if scene := c.vm.Scene(); scene[len(scene)-1].State.Name != "Dragged" {
		t.Fatal("dragged sprite was not raised to front")
	}
	if err := c.advance(context.Background(), Input{}); err != nil {
		t.Fatal(err)
	}
	if err := c.advance(context.Background(), Input{
		MouseX:       340,
		MouseY:       ToolbarHeight + 180,
		MouseDown:    true,
		MousePressed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if scene := c.vm.Scene(); scene[len(scene)-1].State.Name != "Dragged" {
		t.Fatal("clicking a non-draggable sprite changed layer order")
	}
}

func TestControllerReplayIgnoresPhysicalInput(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"variables":{"v":["v",0]},"blocks":{"hat":{"opcode":"event_whenkeypressed","topLevel":true,"next":"set","fields":{"KEY_OPTION":["space",null]}},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["v","v"]},"inputs":{"VALUE":[2,"mouse"]}},"mouse":{"opcode":"sensing_mousex"}}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	c := newController(&p, &media.Library{Sounds: [][]*media.Sound{nil}}, Options{FPS: 30, Replay: true})
	if err := c.vm.QueueEvents([]vm.InputEvent{{
		Kind: "mouse",
		X:    30,
		Y:    40,
	}, {
		Frame: 1,
		Kind:  "key",
		Key:   "space",
		Down:  true,
	}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := c.advance(context.Background(), Input{MouseX: 240, Pressed: []string{"space"}}); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.vm.Snapshot()[0].Variables["v"].Value; got != float64(30) {
		t.Fatalf("replay mouse overwritten: %v", got)
	}
}

func TestControllerStopDoesNotQueueClicksForRestart(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"variables":{"v":["v",0]},"blocks":{"hat":{"opcode":"event_whenstageclicked","topLevel":true,"next":"inc"},"inc":{"opcode":"data_changevariableby","fields":{"VARIABLE":["v","v"]},"inputs":{"VALUE":[1,[4,"1"]]}}}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	c := newController(&p, &media.Library{Sounds: [][]*media.Sound{nil}}, Options{FPS: 30})
	for _, in := range []Input{{Stop: true}, {
		MouseX:       240,
		MouseY:       200,
		MousePressed: true,
	}, {Restart: true}} {
		if err := c.advance(context.Background(), in); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.vm.Snapshot()[0].Variables["v"].Value; got != float64(0) {
		t.Fatalf("stopped click replayed: %v", got)
	}
}

func TestControllerKeyReleaseWhileStopped(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"variables":{"v":["v",false]},"blocks":{"hat":{"opcode":"event_whenflagclicked","topLevel":true,"next":"set"},"set":{"opcode":"data_setvariableto","fields":{"VARIABLE":["v","v"]},"inputs":{"VALUE":[2,"key"]}},"key":{"opcode":"sensing_keypressed","inputs":{"KEY_OPTION":[1,[10,"space"]]}}}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	c := newController(&p, &media.Library{Sounds: [][]*media.Sound{nil}}, Options{FPS: 30})
	for _, in := range []Input{{Pressed: []string{"space"}}, {Stop: true}, {Released: []string{"space"}}, {Restart: true}} {
		if err := c.advance(context.Background(), in); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.vm.Snapshot()[0].Variables["v"].Value; got != false {
		t.Fatalf("released key stuck after restart: %v", got)
	}
}
