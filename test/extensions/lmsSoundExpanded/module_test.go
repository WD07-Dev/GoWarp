package extension

import (
	e "gowarp/core/extension"
	"math"
	"reflect"
	"testing"
)

type soundHost struct {
	op   string
	args []any
}

func (h *soundHost) Call(op string, args ...any) (any, error) {
	h.op, h.args = op, args
	return nil, nil
}

func TestAdditionalSoundCommands(t *testing.T) {
	for _, tc := range []struct {
		opcode, operation string
		args              map[string]any
		want              []any
	}{
		{"pauseSounds", "audio.contextPaused", nil, []any{true}},
		{"resumeSounds", "audio.contextPaused", nil, []any{false}},
		{"stopOwnSounds", "audio.stopTarget", nil, nil},
		{"startSoundFromAndLoop", "audio.targetPlay", map[string]any{"SOUND": "music", "START": 1.25}, []any{"music", 1.25, true}},
		{"startSoundFromAndLoop", "audio.targetPlay", map[string]any{"SOUND": "music", "START": math.Inf(1)}, []any{"music", float64(0), true}},
	} {
		t.Run(tc.opcode, func(t *testing.T) {
			host := &soundHost{}
			found := false
			for _, b := range New().Blocks {
				if b.Opcode == "lmsSoundExpanded_"+tc.opcode {
					found = true
					if _, err := b.Handler(&e.Context{Host: host, Args: tc.args}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if !found || host.op != tc.operation || !reflect.DeepEqual(host.args, tc.want) {
				t.Fatalf("command=%s %v", host.op, host.args)
			}
		})
	}
}

type playbackHost struct {
	starts  int
	pending bool
	volume  float64
}

func (h *playbackHost) Call(op string, args ...any) (any, error) {
	switch op {
	case "audio.targetPlay":
		h.starts++
		return "voice", nil
	case "audio.targetVoice":
		return "voice", nil
	case "audio.voice.get":
		return h.pending, nil
	case "audio.projectVolume":
		if len(args) > 0 {
			h.volume = args[0].(float64)
		}
		return h.volume, nil
	case "audio.effect":
		return 12.0, nil
	}
	return nil, nil
}
func TestSoundReportersAndWait(t *testing.T) {
	blocks := map[string]e.Block{}
	for _, b := range New().Blocks {
		blocks[b.Opcode] = b
	}
	host := &playbackHost{pending: true, volume: 33.33333}
	c := &e.Context{Host: host, Args: map[string]any{"SOUND": "music", "START": 0.0}, Local: map[string]any{}}
	for _, tc := range []struct {
		op   string
		want any
	}{{"getProjectVolume", 33.33}, {"isSoundPlaying", true}, {"isLooping", true}, {"getSoundEffect", 12.0}} {
		b := blocks["lmsSoundExpanded_"+tc.op]
		if b.Handler == nil {
			t.Fatalf("missing %s", tc.op)
		}
		got, err := b.Handler(c)
		if err != nil || got.Value != tc.want {
			t.Fatalf("%s: %v %v", tc.op, got, err)
		}
		if b.Text == "" {
			t.Fatalf("missing label %s", tc.op)
		}
		if (tc.op == "isSoundPlaying" || tc.op == "isLooping") && b.Kind != "boolean" {
			t.Fatalf("wrong kind %s", b.Kind)
		}
	}
	b := blocks["lmsSoundExpanded_playSoundAtAndWait"]
	for range 2 {
		got, err := b.Handler(c)
		if err != nil || !got.Again || !got.Yield {
			t.Fatalf("wait=%+v %v", got, err)
		}
	}
	host.pending = false
	got, err := b.Handler(c)
	if err != nil || got.Again || host.starts != 1 {
		t.Fatalf("completion=%+v starts=%d err=%v", got, host.starts, err)
	}
	c.Args["VALUE"] = 100.0
	_, err = blocks["lmsSoundExpanded_changeProjectVolume"].Handler(c)
	if err != nil || host.volume != 100 {
		t.Fatalf("volume=%v err=%v", host.volume, err)
	}
}
