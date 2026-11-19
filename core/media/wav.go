package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// WAV frames become two float32 samples, matching the existing 64 MiB limit.
const maxWAVFrames = (64 << 20) / 8

type wavFormat struct {
	kind, channels, rate, align, bits, samplesPerBlock int
}

/*
Long 16-bit PCM tracks can be sampled directly from their WAV payload.
This avoids expanding a whole track to stereo float32 before playback starts.
*/
type wavPCM struct {
	data     []byte
	channels int
}

func (w *wavPCM) pair(at, next int) [4]float32 {
	var samples [4]float32
	for i, frame := range [2]int{at, next} {
		offset := frame * w.channels * 2
		left := float32(int16(binary.LittleEndian.Uint16(w.data[offset:]))) / 32768
		right := left
		if w.channels == 2 {
			right = float32(int16(binary.LittleEndian.Uint16(w.data[offset+2:]))) / 32768
		}
		samples[i*2], samples[i*2+1] = left, right
	}
	return samples
}

func readWAV(data []byte) (wavFormat, []byte, *uint32, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return wavFormat{}, nil, nil, fmt.Errorf("invalid RIFF WAVE header")
	}
	end64 := uint64(binary.LittleEndian.Uint32(data[4:8])) + 8
	if end64 < 12 || end64 > uint64(len(data)) {
		return wavFormat{}, nil, nil, fmt.Errorf("truncated RIFF WAVE data")
	}
	end := int(end64)
	var format, payload []byte
	var fact *uint32
	for offset := 12; offset < end; {
		if end-offset < 8 {
			return wavFormat{}, nil, nil, fmt.Errorf("truncated WAV chunk header")
		}
		name := string(data[offset : offset+4])
		size := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8
		padded := size + size%2
		/*
			Accept one trailing zero alignment byte omitted from RIFF length. Payload frames must still be
			complete.
		*/
		if name == "data" && size%2 == 1 && size == uint64(end-offset) && len(data) == end+1 && data[end] == 0 {
			padded = size
		}
		if padded > uint64(end-offset) {
			return wavFormat{}, nil, nil, fmt.Errorf("truncated WAV %q chunk", name)
		}
		chunk := data[offset : offset+int(size)]
		switch name {
		case "fmt ":
			if format != nil {
				return wavFormat{}, nil, nil, fmt.Errorf("duplicate WAV format chunk")
			}
			format = chunk
		case "data":
			if payload != nil {
				return wavFormat{}, nil, nil, fmt.Errorf("duplicate WAV data chunk")
			}
			payload = chunk
		case "fact":
			if len(chunk) < 4 || fact != nil {
				return wavFormat{}, nil, nil, fmt.Errorf("invalid WAV fact chunk")
			}
			n := binary.LittleEndian.Uint32(chunk)
			fact = &n
		}
		offset += int(padded)
	}
	if format == nil || payload == nil {
		return wavFormat{}, nil, nil, fmt.Errorf("WAV requires format and data chunks")
	}
	f, err := parseWAVFormat(format)
	if err != nil {
		return wavFormat{}, nil, nil, err
	}
	return f, payload, fact, nil
}

func decodeWAV(data []byte) (*Sound, error) {
	f, payload, fact, err := readWAV(data)
	if err != nil {
		return nil, err
	}
	if f.kind == 0x11 {
		return decodeIMAWAV(f, payload, fact)
	}
	if len(payload)%f.align != 0 {
		return nil, fmt.Errorf("partial WAV PCM frame")
	}
	frames := len(payload) / f.align
	if frames > maxWAVFrames {
		return nil, fmt.Errorf("excessive decoded audio")
	}
	sound := &Sound{Rate: f.rate, Samples: make([]float32, frames*2)}
	stride := f.bits / 8
	for frame := 0; frame < frames; frame++ {
		for channel := 0; channel < f.channels; channel++ {
			p := payload[frame*f.align+channel*stride:]
			var sample float32
			if f.kind == 3 {
				if f.bits == 32 {
					sample = math.Float32frombits(binary.LittleEndian.Uint32(p))
				} else {
					sample = float32(math.Float64frombits(binary.LittleEndian.Uint64(p)))
				}
			} else {
				switch f.bits {
				case 8:
					sample = (float32(p[0]) - 127.5) / 128 // Preserve the existing unsigned 8-bit conversion.
				case 16:
					sample = float32(int16(binary.LittleEndian.Uint16(p))) / 32768
				case 24:
					n := int32(p[0]) | int32(p[1])<<8 | int32(p[2])<<16
					sample = float32(n<<8>>8) / 8388608
				case 32:
					sample = float32(float64(int32(binary.LittleEndian.Uint32(p))) / 2147483648)
				}
			}
			if math.IsNaN(float64(sample)) || math.IsInf(float64(sample), 0) {
				sample = 0
			}
			sound.Samples[frame*2+channel] = sample
			if f.channels == 1 {
				sound.Samples[frame*2+1] = sample
			}
		}
	}
	return sound, nil
}

func parseWAVFormat(data []byte) (wavFormat, error) {
	var f wavFormat
	if len(data) < 16 {
		return f, fmt.Errorf("short WAV format chunk")
	}
	f = wavFormat{
		kind:     int(binary.LittleEndian.Uint16(data)),
		channels: int(binary.LittleEndian.Uint16(data[2:])),
		rate:     int(binary.LittleEndian.Uint32(data[4:])),
		align:    int(binary.LittleEndian.Uint16(data[12:])),
		bits:     int(binary.LittleEndian.Uint16(data[14:])),
	}
	if f.channels != 1 && f.channels != 2 {
		return f, fmt.Errorf("unsupported WAV channel count %d", f.channels)
	}
	if f.rate < 1 || f.rate > 384000 {
		return f, fmt.Errorf("invalid sound sample rate")
	}
	if len(data) > 16 && (len(data) < 18 || int(binary.LittleEndian.Uint16(data[16:])) > len(data)-18) {
		return f, fmt.Errorf("truncated WAV format extension")
	}
	if f.kind == 0xfffe {
		if len(data) < 40 || binary.LittleEndian.Uint16(data[16:]) < 22 {
			return f, fmt.Errorf("short extensible WAV format")
		}
		guidTail := []byte{0, 0, 0x10, 0, 0x80, 0, 0, 0xaa, 0, 0x38, 0x9b, 0x71}
		if !bytes.Equal(data[28:40], guidTail) {
			return f, fmt.Errorf("unsupported extensible WAV subtype")
		}
		f.kind = int(binary.LittleEndian.Uint32(data[24:28]))
		valid := int(binary.LittleEndian.Uint16(data[18:20]))
		if valid < 1 || valid > f.bits || (f.kind == 3 && valid != f.bits) {
			return f, fmt.Errorf("invalid extensible WAV valid bits")
		}
		if f.kind != 1 && f.kind != 3 {
			return f, fmt.Errorf("unsupported extensible WAV format %d", f.kind)
		}
	}
	switch f.kind {
	case 1:
		if f.bits != 8 && f.bits != 16 && f.bits != 24 && f.bits != 32 {
			return f, fmt.Errorf("unsupported PCM WAV bit depth %d", f.bits)
		}
	case 3:
		if f.bits != 32 && f.bits != 64 {
			return f, fmt.Errorf("unsupported float WAV bit depth %d", f.bits)
		}
	case 0x11:
		if f.bits != 4 || len(data) < 20 || binary.LittleEndian.Uint16(data[16:]) < 2 {
			return f, fmt.Errorf("invalid IMA ADPCM format")
		}
		if f.align < 4*f.channels || (f.channels == 2 && (f.align-8)%8 != 0) {
			return f, fmt.Errorf("invalid IMA ADPCM block alignment")
		}
		f.samplesPerBlock = int(binary.LittleEndian.Uint16(data[18:]))
		if f.samplesPerBlock != 1+(f.align-4*f.channels)*2/f.channels {
			return f, fmt.Errorf("IMA ADPCM samples per block do not match alignment")
		}
		return f, nil
	default:
		return f, fmt.Errorf("unsupported WAV format 0x%x", f.kind)
	}
	if f.align != f.channels*(f.bits/8) {
		return f, fmt.Errorf("invalid WAV PCM block alignment")
	}
	return f, nil
}

/*
IMA's standard tables and shift/add rounding are also used by Scratch's
ADPCMSoundDecoder and dr_wav. Low nibble comes first; stereo uses four-byte
packets per channel, rather than alternating individual compressed bytes.
*/
var imaSteps = [...]int{
	7,
	8,
	9,
	10,
	11,
	12,
	13,
	14,
	16,
	17,
	19,
	21,
	23,
	25,
	28,
	31,
	34,
	37,
	41,
	45,
	50,
	55,
	60,
	66,
	73,
	80,
	88,
	97,
	107,
	118,
	130,
	143,
	157,
	173,
	190,
	209,
	230,
	253,
	279,
	307,
	337,
	371,
	408,
	449,
	494,
	544,
	598,
	658,
	724,
	796,
	876,
	963,
	1060,
	1166,
	1282,
	1411,
	1552,
	1707,
	1878,
	2066,
	2272,
	2499,
	2749,
	3024,
	3327,
	3660,
	4026,
	4428,
	4871,
	5358,
	5894,
	6484,
	7132,
	7845,
	8630,
	9493,
	10442,
	11487,
	12635,
	13899,
	15289,
	16818,
	18500,
	20350,
	22385,
	24623,
	27086,
	29794,
	32767,
}

var imaIndexChange = [...]int{-1, -1, -1, -1, 2, 4, 6, 8}

type imaChannel struct {
	predictor, index int
}

func (c *imaChannel) sample(code byte) float32 {
	step := imaSteps[c.index]
	delta := step >> 3
	if code&1 != 0 {
		delta += step >> 2
	}
	if code&2 != 0 {
		delta += step >> 1
	}
	if code&4 != 0 {
		delta += step
	}
	if code&8 != 0 {
		delta = -delta
	}
	c.predictor = max(-32768, min(32767, c.predictor+delta))
	c.index = max(0, min(88, c.index+imaIndexChange[code&7]))
	return float32(c.predictor) / 32768
}

func imaFrameCount(f wavFormat, data []byte, fact *uint32) (int, error) {
	frames := uint64(len(data)/f.align) * uint64(f.samplesPerBlock)
	if remainder := len(data) % f.align; remainder != 0 {
		payload := remainder - 4*f.channels
		if payload < 0 || (f.channels == 2 && payload%8 != 0) {
			return 0, fmt.Errorf("partial IMA ADPCM channel block")
		}
		frames += uint64(1 + payload*2/f.channels)
	}
	if frames > maxWAVFrames {
		return 0, fmt.Errorf("excessive decoded audio")
	}
	if fact != nil && *fact > 0 {
		if uint64(*fact) > frames {
			return 0, fmt.Errorf("WAV fact count exceeds IMA ADPCM samples")
		}
		frames = uint64(*fact)
	}
	return int(frames), nil
}

func decodeIMAWAV(f wavFormat, data []byte, fact *uint32) (*Sound, error) {
	frames, err := imaFrameCount(f, data, fact)
	if err != nil {
		return nil, err
	}
	sound := &Sound{Rate: f.rate, Samples: make([]float32, int(frames)*2)}
	write := func(frame, channel int, sample float32) {
		if frame >= int(frames) {
			return
		}
		sound.Samples[frame*2+channel] = sample
		if f.channels == 1 {
			sound.Samples[frame*2+1] = sample
		}
	}
	base := 0
	for offset := 0; offset < len(data); offset += f.align {
		block := data[offset:min(offset+f.align, len(data))]
		var channels [2]imaChannel
		for i := 0; i < f.channels; i++ {
			header := block[4*i : 4*i+4]
			if header[2] > 88 || header[3] != 0 {
				return nil, fmt.Errorf("invalid IMA ADPCM predictor header")
			}
			channels[i] = imaChannel{predictor: int(int16(binary.LittleEndian.Uint16(header))), index: int(header[2])}
			write(base, i, float32(channels[i].predictor)/32768)
		}
		payload := block[4*f.channels:]
		if f.channels == 1 {
			for i, b := range payload {
				write(base+1+2*i, 0, channels[0].sample(b&15))
				write(base+2+2*i, 0, channels[0].sample(b>>4))
			}
		} else {
			for group := 0; group < len(payload)/8; group++ {
				for channel := 0; channel < 2; channel++ {
					for j := 0; j < 4; j++ {
						b := payload[group*8+channel*4+j]
						write(base+1+group*8+j*2, channel, channels[channel].sample(b&15))
						write(base+2+group*8+j*2, channel, channels[channel].sample(b>>4))
					}
				}
			}
		}
		base += 1 + len(payload)*2/f.channels
	}
	return sound, nil
}
