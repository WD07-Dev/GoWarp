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
