package media

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func wavFixture(format, channels, align, bits uint16, extra, payload []byte, fact *uint32) []byte {
	var fmtChunk bytes.Buffer
	for _, v := range []any{format, channels, uint32(8000), uint32(8000) * uint32(align), align, bits} {
		_ = binary.Write(&fmtChunk, binary.LittleEndian, v)
	}
	if extra != nil {
		_ = binary.Write(&fmtChunk, binary.LittleEndian, uint16(len(extra)))
		fmtChunk.Write(extra)
	}
	var chunks bytes.Buffer
	chunk := func(id string, data []byte) {
		chunks.WriteString(id)
		_ = binary.Write(&chunks, binary.LittleEndian, uint32(len(data)))
		chunks.Write(data)
		if len(data)%2 != 0 {
			chunks.WriteByte(0)
		}
	}
	chunk("JUNK", []byte{1, 2, 3}) // Unknown odd-sized chunks must retain RIFF padding.
	chunk("fmt ", fmtChunk.Bytes())
	if fact != nil {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], *fact)
		chunk("fact", b[:])
	}
	chunk("data", payload)
	var result bytes.Buffer
	result.WriteString("RIFF")
	_ = binary.Write(&result, binary.LittleEndian, uint32(chunks.Len()+4))
	result.WriteString("WAVE")
	result.Write(chunks.Bytes())
	return result.Bytes()
}

func TestWAVIMAMonoKnownNibbleVector(t *testing.T) {
	data := wavFixture(0x11, 1, 8, 4, []byte{9, 0}, []byte{0, 0, 0, 0, 0x10, 0x32, 0x54, 0x76}, nil)
	sound, err := DecodeSound("wav", data)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 0, 1, 4, 8, 15, 27, 47, 88}
	if sound.Rate != 8000 || len(sound.Samples) != 2*len(want) {
		t.Fatalf("decoded shape: rate=%d samples=%d", sound.Rate, len(sound.Samples))
	}
	for i, n := range want {
		for channel := 0; channel < 2; channel++ {
			if got := sound.Samples[2*i+channel]; got != float32(n)/32768 {
				t.Fatalf("sample %d/%d=%v want %v", i, channel, got, float32(n)/32768)
			}
		}
	}
}

func TestWAVIMAStereoGroupsAndBlockReset(t *testing.T) {
	block := []byte{0xe8, 3, 0, 0, 0x18, 0xfc, 0, 0, 0x10, 0x32, 0x54, 0x76, 0x98, 0xba, 0xdc, 0xfe}
	payload := append(append([]byte{}, block...), []byte{123, 0, 0, 0, 0x85, 0xff, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}...)
	sound, err := DecodeSound("wav", wavFixture(0x11, 2, 16, 4, []byte{9, 0}, payload, nil))
	if err != nil {
		t.Fatal(err)
	}
	want := []int{
		1000,
		-1000,
		1000,
		-1000,
		1001,
		-1001,
		1004,
		-1004,
		1008,
		-1008,
		1015,
		-1015,
		1027,
		-1027,
		1047,
		-1047,
		1088,
		-1088,
	}
	if len(sound.Samples) != 36 {
		t.Fatalf("stereo block frames=%d", len(sound.Samples)/2)
	}
	for i, n := range want {
		if got := sound.Samples[i]; got != float32(n)/32768 {
			t.Fatalf("interleaved sample %d=%v want %v", i, got, float32(n)/32768)
		}
	}
	for i := 18; i < 36; i += 2 {
		if sound.Samples[i] != 123.0/32768 || sound.Samples[i+1] != -123.0/32768 {
			t.Fatal("second block retained predictor history")
		}
	}
}

func TestWAVIMAClippingPartialBlockAndFact(t *testing.T) {
	data := []byte{0xf8, 0x7f, 88, 0, 0xf7, 0xff}
	sound, err := DecodeSound("wav", wavFixture(0x11, 1, 8, 4, []byte{9, 0}, data, nil))
	if err != nil {
		t.Fatal(err)
	}
	want := []int{32760, 32767, -28669, -32768, -32768}
	if len(sound.Samples) != 10 {
		t.Fatalf("partial block count %d", len(sound.Samples))
	}
	for i, n := range want {
		if sound.Samples[2*i] != float32(n)/32768 {
			t.Fatalf("clipping sample %d=%v", i, sound.Samples[2*i])
		}
	}
	fact := uint32(3)
	sound, err = DecodeSound("wav", wavFixture(0x11, 1, 8, 4, []byte{9, 0}, data, &fact))
	if err != nil {
		t.Fatal(err)
	}
	if len(sound.Samples) != 6 || sound.Duration() != 3.0/8000 {
		t.Fatal("fact sample count did not trim padding")
	}
}

func TestWAVPCMWidthsAndFloatSamples(t *testing.T) {
	for _, tc := range []struct {
		name         string
		format, bits uint16
		payload      []byte
		want         []float32
	}{
		{"u8", 1, 8, []byte{0, 128, 255}, []float32{-127.5 / 128, .5 / 128, 127.5 / 128}},
		{"s16", 1, 16, []byte{0, 128, 0, 0, 255, 127}, []float32{-1, 0, 32767.0 / 32768}},
		{"s24", 1, 24, []byte{0, 0, 128, 0, 0, 64, 0, 0, 192}, []float32{-1, .5, -.5}},
		{"s32", 1, 32, []byte{0, 0, 0, 128, 0, 0, 0, 64, 0, 0, 0, 192}, []float32{-1, .5, -.5}},
		{
			"f32",
			3,
			32,
			[]byte{0, 0, 0, 191, 0, 0, 0, 63, 0, 0, 128, 127, 0, 0, 192, 127},
			[]float32{-.5, .5, 0, 0},
		},
		{"f64", 3, 64, []byte{0, 0, 0, 0, 0, 0, 224, 191, 0, 0, 0, 0, 0, 0, 224, 63}, []float32{-.5, .5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sound, err := DecodeSound("wav", wavFixture(tc.format, 1, tc.bits/8, tc.bits, nil, tc.payload, nil))
			if err != nil {
				t.Fatal(err)
			}
			if len(sound.Samples) != 2*len(tc.want) {
				t.Fatalf("sample count %d", len(sound.Samples))
			}
			for i, want := range tc.want {
				if sound.Samples[2*i] != want || sound.Samples[2*i+1] != want {
					t.Fatalf("sample %d=%v want %v", i, sound.Samples[2*i], want)
				}
			}
		})
	}
}

func TestWAVExtensiblePCMAndFloat(t *testing.T) {
	for _, format := range []uint16{1, 3} {
		extra := make([]byte, 22)
		binary.LittleEndian.PutUint16(extra, 32)
		binary.LittleEndian.PutUint32(extra[2:], 3)
		copy(extra[6:], []byte{byte(format), 0, 0, 0, 0, 0, 0x10, 0, 0x80, 0, 0, 0xaa, 0, 0x38, 0x9b, 0x71})
		payload := []byte{0, 0, 0, 64, 0, 0, 0, 192}
		if format == 3 {
			payload = []byte{0, 0, 0, 63, 0, 0, 0, 191}
		}
		sound, err := DecodeSound("wav", wavFixture(0xfffe, 2, 8, 32, extra, payload, nil))
		if err != nil {
			t.Fatal(err)
		}
		if len(sound.Samples) != 2 || sound.Samples[0] != .5 || sound.Samples[1] != -.5 {
			t.Fatalf("extensible subtype %d: %v", format, sound.Samples)
		}
	}
}

func TestWAVRejectMalformedAndExcessiveData(t *testing.T) {
	base := wavFixture(0x11, 1, 8, 4, []byte{9, 0}, []byte{0, 0, 0, 0, 0x10, 0x32, 0x54, 0x76}, nil)
	modify := func(offset int, value byte) []byte {
		b := append([]byte{}, base...)
		b[offset] = value
		return b
	}
	fmtOffset := bytes.Index(base, []byte("fmt ")) + 8
	dataOffset := bytes.Index(base, []byte("data")) + 8
	fact := uint32(99)
	for name, data := range map[string][]byte{
		"short":             base[:11],
		"riff-size":         modify(4, 255),
		"truncated-chunk":   base[:len(base)-1],
		"channels":          modify(fmtOffset+2, 3),
		"rate-zero":         modify(fmtOffset+4, 0),
		"alignment":         modify(fmtOffset+12, 3),
		"sample-count":      modify(fmtOffset+18, 8),
		"step-index":        modify(dataOffset+2, 89),
		"reserved":          modify(dataOffset+3, 1),
		"extension":         modify(fmtOffset+16, 20),
		"unsupported":       modify(fmtOffset, 2),
		"partial-header":    wavFixture(0x11, 1, 8, 4, []byte{9, 0}, []byte{0, 0, 0}, nil),
		"stereo-group":      wavFixture(0x11, 2, 16, 4, []byte{9, 0}, make([]byte, 12), nil),
		"fact-overflow":     wavFixture(0x11, 1, 8, 4, []byte{9, 0}, make([]byte, 8), &fact),
		"pcm-partial-frame": wavFixture(1, 2, 4, 16, nil, []byte{0, 0}, nil),
	} {
		t.Run(name, func(t *testing.T) {
			if name == "rate-zero" {
				binary.LittleEndian.PutUint32(data[fmtOffset+4:], 0)
			}
			if _, err := DecodeSound("wav", data); err == nil {
				t.Fatal("accepted malformed WAV")
			}
		})
	}
	tooLong := wavFixture(1, 1, 1, 8, nil, make([]byte, (64<<20)/8+1), nil)
	if _, err := DecodeSound("wav", tooLong); err == nil || !strings.Contains(err.Error(), "excessive") {
		t.Fatalf("decoded size limit: %v", err)
	}
}

func TestWAVFloatOverflowBecomesSilence(t *testing.T) {
	var payload [8]byte
	binary.LittleEndian.PutUint64(payload[:], math.Float64bits(math.MaxFloat64))
	sound, err := DecodeSound("wav", wavFixture(3, 1, 8, 64, nil, payload[:], nil))
	if err != nil {
		t.Fatal(err)
	}
	if sound.Samples[0] != 0 || sound.Samples[1] != 0 {
		t.Fatal("float conversion emitted nonfinite samples")
	}
}

func TestWAVRejectsTruncationAtEveryByte(t *testing.T) {
	data := wavFixture(0x11, 2, 16, 4, []byte{9, 0}, make([]byte, 16), nil)
	for end := 0; end < len(data); end++ {
		if _, err := DecodeSound("wav", data[:end]); err == nil {
			t.Fatalf("accepted truncation at byte %d", end)
		}
	}
	offset := bytes.Index(data, []byte("fmt ")) + 8
	binary.LittleEndian.PutUint32(data[offset+4:], 384001)
	if _, err := DecodeSound("wav", data); err == nil {
		t.Fatal("accepted excessive sample rate")
	}
}

func TestWAVAcceptsFinalZeroPadExcludedFromRIFFLength(t *testing.T) {
	data := wavFixture(1, 1, 3, 24, nil, []byte{0, 0, 64}, nil)
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-9))
	sound, err := DecodeSound("wav", data)
	if err != nil {
		t.Fatal(err)
	}
	if sound.Rate != 8000 || len(sound.Samples) != 2 || sound.Samples[0] != .5 || sound.Samples[1] != .5 {
		t.Fatalf("complete PCM frame changed: %+v", sound)
	}
	for _, kind := range []string{"missing-pad", "nonzero-pad", "payload-truncated"} {
		t.Run(kind, func(t *testing.T) {
			invalid := append([]byte{}, data...)
			switch kind {
			case "missing-pad":
				invalid = invalid[:len(invalid)-1]
			case "nonzero-pad":
				invalid[len(invalid)-1] = 1
			case "payload-truncated":
				binary.LittleEndian.PutUint32(invalid[4:], uint32(len(invalid)-10))
			}
			if _, err := DecodeSound("wav", invalid); err == nil {
				t.Fatal("accepted more than the final zero-padding discrepancy")
			}
		})
	}
}
