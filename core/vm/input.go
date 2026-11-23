package vm

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
)

/*
InputEvent is applied before scripts at its zero-based virtual frame.
Host callers must serialize QueueEvents and Run; Runtime is not concurrent.
*/
type InputEvent struct {
	Frame   int     `json:"frame"`
	Kind    string  `json:"kind"`
	Key     string  `json:"key,omitempty"`
	Down    bool    `json:"down,omitempty"`
	X       float64 `json:"x,omitempty"`
	Y       float64 `json:"y,omitempty"`
	Target  string  `json:"target,omitempty"`
	CloneID int     `json:"cloneId,omitempty"`
	Answer  string  `json:"answer,omitempty"`
	Value   float64 `json:"value,omitempty"`
}

type Question struct {
	Text    string `json:"text"`
	Target  string `json:"target"`
	CloneID int    `json:"cloneId,omitempty"`
}

func (r *Runtime) QueueEvents(events []InputEvent) error {
	for _, e := range events {
		if e.Frame < r.frame {
			return fmt.Errorf("input frame %d precedes current frame %d", e.Frame, r.frame)
		}
		switch e.Kind {
		case "key":
			if e.Key == "" {
				return fmt.Errorf("key event has no key")
			}
		case "mouse", "answer", "click", "click-at", "loudness", "username":
		default:
			return fmt.Errorf("unknown input kind %q", e.Kind)
		}
		if !finite(e.X) || !finite(e.Y) || !finite(e.Value) {
			return fmt.Errorf("input values must be finite")
		}
	}
	r.events = append(r.events, events...)
	sort.SliceStable(r.events, func(i, j int) bool {
		return r.events[i].Frame < r.events[j].Frame
	})
	return nil
}

func (r *Runtime) cleanQuestion() {
	if th := r.askThread; th != nil && (th.done || th.replaced || th.target.dead) {
		if th.target.bubbleOwner == th {
			th.target.state.Bubble = ""
			th.target.bubbleOwner = nil
		}
		r.askThread = nil
		r.askPrompt = ""
	}
}

func (r *Runtime) Question() *Question {
	r.cleanQuestion()
	if r.askThread == nil {
		return nil
	}
	return &Question{
		Text:    r.askPrompt,
		Target:  r.askThread.target.state.Name,
		CloneID: r.askThread.target.state.CloneID,
	}
}

func normalizeKey(key string) string {
	key = strings.ToLower(key)
	switch key {
	case " ":
		return "space"
	case "left", "right", "up", "down":
		return key + " arrow"
	}
	return key
}

func (r *Runtime) keyPressed(key string) bool {
	switch key {
	case "shift":
		return r.keys[key] || r.keys["shiftleft"] || r.keys["shiftright"]
	case "any":
		for _, down := range r.keys {
			if down {
				return true
			}
		}
		return false
	default:
		return r.keys[key]
	}
}

func (r *Runtime) applyInput(e InputEvent) {
	switch e.Kind {
	case "mouse":
		r.mouseX, r.mouseY, r.mouseDown = e.X, e.Y, e.Down
	case "key":
		key := normalizeKey(e.Key)
		r.trackHostKey(e.Key, e.Down)
		r.keys[key] = e.Down
		if !e.Down {
			return
		}
		r.lastKey = key
		if r.askThread != nil {
			return
		}
		for i := len(r.executable) - 1; i >= 0; i-- {
			r.startHatsFor(r.executable[i], "event_whenkeypressed", key, false)
			if key != "any" {
				r.startHatsFor(r.executable[i], "event_whenkeypressed", "any", false)
			}
		}
	case "click-at":
		r.clickAt(e.X, e.Y)
	case "click":
		for _, t := range r.targets {
			if t.state.CloneID != e.CloneID {
				continue
			}
			if (e.Target == "_stage_" && t.state.IsStage) || (e.Target == t.state.Name) {
				op := "event_whenthisspriteclicked"
				if t.state.IsStage {
					op = "event_whenstageclicked"
				}
				r.startHatsFor(t, op, "", true)
				break
			}
		}
	case "answer":
		if r.askThread != nil {
			r.answer = e.Answer
			r.askThread = nil
			r.askPrompt = ""
		}
	case "loudness":
		r.loudness = e.Value
	case "username":
		r.username = e.Answer
	}
}

func (r *Runtime) pollEdgeHats(ctx context.Context) error {
	for i := len(r.executable) - 1; i >= 0; i-- {
		t := r.executable[i]
		/*
			Most hats run only on explicit events. Poll only rising-edge hats;
			large clone groups otherwise rescan every script on every frame.
		*/
		for _, id := range t.edgeHats {
			b := t.source.Blocks[id]
			if t.edgeInputs == nil {
				t.edgeInputs = map[string]*thread{}
			}
			evaluation := t.edgeInputs[id]
			if evaluation == nil {
				evaluation = &thread{target: t, inputRequest: &inputRequest{block: b, name: "VALUE"}}
				t.edgeInputs[id] = evaluation
			}
			if err := r.step(ctx, evaluation); err != nil {
				return err
			}
			if !evaluation.done {
				continue
			}
			threshold := evaluation.inputRequest.value
			delete(t.edgeInputs, id)
			value := r.loudness
			if strings.EqualFold(field(b, "WHENGREATERTHANMENU", 0), "timer") {
				value = r.elapsed - r.timerSeconds
			}
			above := value > number(threshold)
			if t.edges == nil {
				t.edges = map[string]bool{}
			}
			if above && !t.edges[id] {
				r.startHat(t, id, false)
			}
			t.edges[id] = above
		}
	}
	return nil
}

// mousePosition follows TurboWarp's integer / three-decimal reporter precision.
func (r *Runtime) mousePosition() (float64, float64) {
	scale := 1000.0
	if r.options.MiscLimits {
		scale = 1
	}
	return math.Floor(r.mouseX*scale+.5) / scale, math.Floor(r.mouseY*scale+.5) / scale
}
