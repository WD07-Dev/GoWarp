package media

import "testing"

func TestStreamIndependentRateZeroAndAbovePitchLimit(t *testing.T) {
	stream := NewStream(&Sound{Rate: 100, Samples: make([]float32, 200)}, 100)
	stream.SetRate(0)
	if _, err := stream.Read(make([]byte, 8*10)); err != nil {
		t.Fatal(err)
	}
	if stream.position != 0 {
		t.Fatalf("zero playback rate advanced source to %g", stream.position)
	}
	stream.SetRate(16)
	if _, err := stream.Read(make([]byte, 8*2)); err != nil {
		t.Fatal(err)
	}
	if stream.position != 32 {
		t.Fatalf("extension rate clamped to Scratch pitch range: position %g", stream.position)
	}
	stream.Set(100, 1000, 0)
	if stream.step != 8 {
		t.Fatalf("built-in pitch clamp changed: %g", stream.step)
	}
}
