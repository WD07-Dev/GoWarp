package extension

import (
	e "gowarp/core/extension"
	"testing"
)

type tuneHost struct{ active, paused map[any]bool }

func (h *tuneHost) Call(op string, args ...any) (any, error) {
	switch op {
	case "audio.asset":
		return args[0], nil
	case "audio.voice.get":
		if args[1] == "active" {
			return h.active[args[0]], nil
		}
		if args[1] == "paused" {
			return h.paused[args[0]], nil
		}
	}
	return nil, nil
}
func TestSoundListsKeepJavascriptOrderAndPlayingState(t *testing.T) {
	x := New()
	blocks := map[string]e.Block{}
	for _, b := range x.Blocks {
		blocks[b.Opcode] = b
	}
	host := &tuneHost{active: map[any]bool{}, paused: map[any]bool{}}
	c := &e.Context{Host: host, State: x.State}
	invoke := func(op string, args map[string]any) any {
		t.Helper()
		c.Args = args
		b := blocks["SPtuneShark3_"+op]
		if b.Handler == nil {
			t.Fatalf("missing %s", op)
		}
		r, err := b.Handler(c)
		if err != nil {
			t.Fatal(err)
		}
		return r.Value
	}
	if got := invoke("allSounds", nil); got != "[]" {
		t.Fatal(got)
	}
	for _, name := range []string{"z", "10", "a", "2", "01"} {
		invoke("importMenu", map[string]any{"NAME": name, "SOUND": "beep"})
	}
	if got := invoke("allSounds", nil); got != `["2","10","z","a","01"]` {
		t.Fatal(got)
	}
	invoke("importMenu", map[string]any{"NAME": "z", "SOUND": "beep"})
	if got := invoke("allSounds", nil); got != `["2","10","a","01","z"]` {
		t.Fatal(got)
	}
	for _, name := range []string{"2", "a", "z"} {
		channels(c)[name].Voice = name
		host.active[name] = true
	}
	host.paused["a"] = true
	if got := invoke("allPlaySounds", nil); got != `["2","z"]` {
		t.Fatal(got)
	}
	host.active["2"] = false
	if got := invoke("allPlaySounds", nil); got != `["z"]` {
		t.Fatal(got)
	}
	invoke("deleteSound", map[string]any{"NAME": "z"})
	if got := invoke("allSounds", nil); got != `["2","10","a","01"]` {
		t.Fatal(got)
	}
	invoke("deleteAllSounds", nil)
	if got := invoke("allSounds", nil); got != "[]" {
		t.Fatal(got)
	}
	for _, op := range []string{"allSounds", "allPlaySounds"} {
		b := blocks["SPtuneShark3_"+op]
		if b.Kind != "reporter" || b.Text == "" || len(b.Inputs) != 0 {
			t.Fatalf("invalid monitor metadata: %+v", b)
		}
	}
}
