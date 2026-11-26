package media

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"
	"testing/iotest"
)

func TestMP3ClickMatchesReference(t *testing.T) {
	want := readMediaFixture(t, "click.f32")
	sound, err := DecodeSound("mp3", readMediaFixture(t, "click.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	peak := float64(0)
	for _, sample := range sound.Samples {
		peak = max(peak, math.Abs(float64(sample)))
	}
	if math.Abs(peak-0.24949625) > 0.00002 {
		t.Errorf("MP3 transient distorted: peak %g, want 0.24949625", peak)
	}
	if sound.Rate != 48000 || len(sound.Samples)*4 != len(want) {
		t.Fatalf("MP3 rate/length = %d/%d, want 48000/%d", sound.Rate, len(sound.Samples)*4, len(want))
	}
	for i, sample := range sound.Samples {
		expected := math.Float32frombits(binary.LittleEndian.Uint32(want[i*4:]))
		if math.IsNaN(float64(sample)) || math.Abs(float64(sample-expected)) > 0.00002 {
			t.Fatalf("MP3 sample %d = %g, want %g", i, sample, expected)
		}
	}
}

func TestMP3ReadAndSeek(t *testing.T) {
	data := readMediaFixture(t, "click.mp3")
	s, err := openMP3Stream(data)
	if err != nil {
		t.Fatal(err)
	}
	want, err := io.ReadAll(s)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(want)) != s.Length() {
		t.Fatalf("decoded %d bytes, metadata says %d", len(want), s.Length())
	}
	if _, err := s.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err := iotest.TestReader(s, want); err != nil {
		t.Fatal(err)
	}
	for _, seek := range []struct {
		offset int64
		whence int
	}{
		{17, io.SeekStart},
		{-11, io.SeekEnd},
		{-8, io.SeekCurrent},
		{0, io.SeekStart},
		{0, io.SeekEnd},
		{13673 * 8, io.SeekStart},
	} {
		pos, err := s.Seek(seek.offset, seek.whence)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, min(1031, int(s.Length()-pos)))
		if _, err := io.ReadFull(s, buf); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(buf, want[pos:pos+int64(len(buf))]) {
			t.Fatalf("seek to byte %d changed decoded samples", pos)
		}
	}
	for _, offset := range []int64{-1, s.Length() + 1, math.MaxInt64} {
		pos, _ := s.Seek(0, io.SeekCurrent)
		if _, err := s.Seek(offset, io.SeekStart); err == nil {
			t.Fatalf("invalid offset %d accepted", offset)
		}
		if after, err := s.Seek(0, io.SeekCurrent); err != nil || after != pos {
			t.Fatalf("failed seek changed position: %d -> %d (%v)", pos, after, err)
		}
	}
}
