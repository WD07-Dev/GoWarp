package desktop

import (
	"bytes"
	"testing"

	"github.com/hajimehoshi/ebiten/v2/audio"

	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestMutedAudioValidatesAssetsWithoutOpeningDevice(t *testing.T) {
	output := newAudioOutput(&media.Library{Sounds: [][]*media.Sound{{{Rate: 8000, Samples: []float32{.5, .5}}}}}, true)
	defer output.close()
	r := vm.New(&project.Project{})
	if err := output.apply(r, []vm.AudioCommand{{
		Kind:   "play",
		ID:     1,
		Source: 0,
		Sound:  0,
		Volume: 100,
	}}); err != nil {
		t.Fatal(err)
	}
	if output.context != nil {
		t.Fatal("muted output opened audio device")
	}
	if err := output.apply(r, []vm.AudioCommand{{
		Kind:   "play",
		ID:     2,
		Source: 99,
	}}); err == nil {
		t.Fatal("bad source did not fail")
	}
}

func TestPendingAudioPauseResumeAndStop(t *testing.T) {
	context := audio.CurrentContext()
	if context == nil {
		context = audio.NewContext(sampleRate)
	}
	output := newAudioOutput(&media.Library{}, false)
	defer output.close()
	r := vm.New(&project.Project{})
	player, err := context.NewPlayerF32(bytes.NewReader(make([]byte, 128)))
	if err != nil {
		t.Fatal(err)
	}
	output.players[1] = playing{player: player, stream: media.NewStream(&media.Sound{Rate: sampleRate}, sampleRate), pending: true}
	if err := output.apply(r, []vm.AudioCommand{{Kind: "pause", ID: 1}}); err != nil {
		t.Fatal(err)
	}
	output.poll(r)
	if p, ok := output.players[1]; !ok || !p.paused || !p.pending || player.IsPlaying() {
		t.Fatal("paused pending sound started or finished before playback")
	}
	if err := output.apply(r, []vm.AudioCommand{{Kind: "resume", ID: 1}}); err != nil {
		t.Fatal(err)
	}
	output.poll(r)
	if p, ok := output.players[1]; !ok || p.paused || p.pending || !player.IsPlaying() {
		t.Fatal("resumed ready sound did not start")
	}
	stopped, err := context.NewPlayerF32(bytes.NewReader(make([]byte, 128)))
	if err != nil {
		t.Fatal(err)
	}
	output.players[2] = playing{player: stopped, stream: media.NewStream(&media.Sound{Rate: sampleRate}, sampleRate), pending: true}
	if err := output.apply(r, []vm.AudioCommand{{Kind: "stop", ID: 2}}); err != nil {
		t.Fatal(err)
	}
	output.poll(r)
	if _, ok := output.players[2]; ok || stopped.IsPlaying() {
		t.Fatal("stopped pending sound remained active")
	}
}
