package desktop

import (
	"bytes"
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2/audio"

	"gowarp/app/editorbridge"
	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestEditorVolumeAppliesToCurrentAndFutureSounds(t *testing.T) {
	output := newAudioOutput(&media.Library{}, false)
	defer output.close()
	r := vm.New(&project.Project{})
	sound := &media.Sound{Rate: sampleRate, Samples: []float32{.5, .5}}
	if err := output.play(r, vm.AudioCommand{ID: 1, Volume: 50}, sound, true); err != nil {
		t.Fatal(err)
	}
	if got := output.players[1].player.Volume(); got != 1 {
		t.Fatalf("default master volume = %v", got)
	}
	peer := &editorbridge.Peer{Commands: make(chan editorbridge.Command, 1)}
	g := &game{options: Options{Editor: peer}, audio: output}
	for _, embedded := range []bool{false, true} {
		peer.Embedded = embedded
		for _, volume := range []float64{.4, 0, 1} {
			peer.Commands <- editorbridge.Command{Action: "volume", Volume: volume}
			g.editorInput(&Input{})
			if got := output.players[1].player.Volume(); got != volume {
				t.Fatalf("embedded=%v volume=%v, want %v", embedded, got, volume)
			}
			if !output.players[1].paused || output.mute {
				t.Fatal("volume changed playback state")
			}
		}
	}
	output.setVolume(.3)
	if err := output.play(r, vm.AudioCommand{ID: 2, Volume: 80}, sound, true); err != nil {
		t.Fatal(err)
	}
	if err := output.apply(r, []vm.AudioCommand{{Kind: "update", ID: 2, Volume: 20}}); err != nil {
		t.Fatal(err)
	}
	if got := output.players[2].player.Volume(); got != .3 {
		t.Fatalf("sound volume overwrote master volume: %v", got)
	}
	for _, invalid := range []float64{math.NaN(), math.Inf(1)} {
		output.setVolume(invalid)
	}
	if output.players[2].player.Volume() != .3 {
		t.Fatal("invalid volume was applied")
	}
}

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
