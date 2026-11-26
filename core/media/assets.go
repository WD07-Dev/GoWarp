// Package media decodes project assets without opening a window or audio device.
package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"strings"
	"weak"

	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/audio/vorbis"
	"golang.org/x/image/font/opentype"

	"gowarp/core/project"
)

const maxPixels = 16 << 20

const maxDecodedBytes = 256 << 20

// Long music tracks can exceed 64 MiB after conversion to stereo float PCM.
const maxSoundBytes = 128 << 20

/*
A project can contain many costumes and narration clips. Keep this separate
from the per-SVG working limits used while decoding a single costume.
*/
const maxLibraryBytes = 1 << 30

type Costume struct {
	// CacheKey identifies generated pixels after their CPU buffer is evicted.
	CacheKey string
	Image    *image.NRGBA
	// Bitmap costumes preserve source pixels when enlarged; SVG and text stay smooth.
	Bitmap                          bool
	Width, Height, CenterX, CenterY float64
}

type Sound struct {
	Samples []float32
	Rate    int
	frames  int
	// PCM16 WAV can be sampled directly without expanding the entire sound.
	pcm16WAV bool
	mp3      bool
	/*
		Metadata can find PCM held by an active player after LRU eviction,
		without keeping unused music alive outside the cache budget.
	*/
	decoded  weak.Pointer[Sound]
	decoding weak.Pointer[soundDecode]
	// Encoded data can likewise be shared while a streaming player owns it.
	source weak.Pointer[cachedAsset]
}

func (s *Sound) Duration() float64 {
	if s.Samples == nil && s.Rate > 0 {
		return float64(s.frames) / float64(s.Rate)
	}
	return float64(len(s.Samples)/2) / float64(s.Rate)
}

type Library struct {
	noAntialias  bool
	fonts        map[string]*opentype.Font
	fallbackFont *opentype.Font
	Costumes     [][]*Costume
	Sounds       [][]*Sound
	cache        *runtimeCache
}

func Load(path string, p *project.Project) (*Library, error) {
	raw, err := project.ReadAssets(path, p)
	if err != nil {
		return nil, err
	}
	lib := &Library{}
	if err := lib.loadFonts(p, func(name string) ([]byte, error) { return raw[name], nil }); err != nil {
		return nil, fmt.Errorf("custom font: %w", err)
	}
	total := 0
	for _, t := range p.Targets {
		costumes := []*Costume{}
		sounds := []*Sound{}
		for _, c := range t.Costumes {
			name := project.AssetName(c.MD5Ext, c.AssetID, c.DataFormat)
			if name == "" {
				costumes = append(costumes, &Costume{Image: image.NewNRGBA(image.Rect(0, 0, 1, 1))})
				continue
			}
			decoded, err := decodeCostume(c, raw[name], lib.customFont)
			if err != nil {
				return nil, fmt.Errorf("%s costume %q: %w", t.Name, c.Name, err)
			}
			total += len(decoded.Image.Pix)
			if total > maxLibraryBytes {
				return nil, fmt.Errorf("decoded assets exceed %d MiB", maxLibraryBytes>>20)
			}
			costumes = append(costumes, decoded)
		}
		for _, s := range t.Sounds {
			name := project.AssetName(s.MD5Ext, s.AssetID, s.DataFormat)
			decoded, err := DecodeSound(s.DataFormat, raw[name])
			if err != nil {
				return nil, fmt.Errorf("%s sound %q: %w", t.Name, s.Name, err)
			}
			total += len(decoded.Samples) * 4
			if total > maxLibraryBytes {
				return nil, fmt.Errorf("decoded assets exceed %d MiB", maxLibraryBytes>>20)
			}
			sounds = append(sounds, decoded)
		}
		lib.Costumes = append(lib.Costumes, costumes)
		lib.Sounds = append(lib.Sounds, sounds)
	}
	return lib, nil
}

func validSize(w, h float64) bool {
	return w > 0 && h > 0 && w <= 8192 && h <= 8192 &&
		math.Ceil(w)*math.Ceil(h) <= maxPixels
}

func DecodeCostume(meta project.Costume, data []byte) (*Costume, error) {
	return decodeCostume(meta, data, nil)
}

// DecodeCostume decodes an imported costume using project fonts and the selected
// antialias mode, without changing runtime playback or worker configuration.
func (lib *Library) DecodeCostume(meta project.Costume, data []byte, antialias bool) (*Costume, error) {
	return decodeCostumeWithAntialias(meta, data, lib.customFont, !antialias)
}

func decodeCostume(meta project.Costume, data []byte, fonts svgFontResolver) (*Costume, error) {
	return decodeCostumeWithAntialias(meta, data, fonts, false)
}

func decodeCostumeWithAntialias(meta project.Costume, data []byte, fonts svgFontResolver, noAntialias bool) (*Costume, error) {
	c := &Costume{}
	if strings.EqualFold(meta.DataFormat, "svg") {
		img, box, err := rasterSVGWithAntialias(data, 1, fonts, noAntialias)
		if err != nil {
			return nil, err
		}
		c.Image = img
		c.Width, c.Height = box.W, box.H
		c.CenterX, c.CenterY = meta.RotationCenterX-box.X, meta.RotationCenterY-box.Y
	} else {
		c.Bitmap = true
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if !validSize(float64(config.Width), float64(config.Height)) {
			return nil, fmt.Errorf("invalid or excessive bitmap dimensions")
		}
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if pixels, ok := decoded.(*image.NRGBA); ok && pixels.Bounds().Min == (image.Point{}) {
			c.Image = pixels
		} else {
			c.Image = image.NewNRGBA(image.Rect(0, 0, config.Width, config.Height))
			draw.Draw(c.Image, c.Image.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
		}
		resolution := meta.BitmapResolution
		if resolution <= 0 {
			resolution = 1
		}
		c.Width, c.Height = float64(config.Width)/resolution, float64(config.Height)/resolution
		if !validSize(c.Width, c.Height) {
			return nil, fmt.Errorf("invalid or excessive logical bitmap dimensions")
		}
		c.CenterX, c.CenterY = meta.RotationCenterX/resolution, meta.RotationCenterY/resolution
	}
	return c, nil
}

/*
Scratch's asset extension is not authoritative: imported sounds can retain a
.wav name while the browser decodes their original Ogg or MP3 bytes.
*/
func soundFormat(format string, data []byte) string {
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		return "wav"
	}
	if len(data) >= 4 && string(data[:4]) == "OggS" {
		return "ogg"
	}
	if len(data) >= 3 && string(data[:3]) == "ID3" {
		return "mp3"
	}
	if len(data) >= 2 && data[0] == 0xff && data[1]&0xe0 == 0xe0 {
		return "mp3"
	}
	return strings.ToLower(format)
}

func DecodeSound(format string, data []byte) (*Sound, error) {
	type stream interface {
		io.Reader
		SampleRate() int
		Length() int64
	}
	var decoded stream
	var err error
	switch soundFormat(format, data) {
	case "wav":
		return decodeWAV(data)
	case "mp3":
		decoded, err = openMP3Stream(data)
	case "ogg":
		decoded, err = vorbis.DecodeF32(bytes.NewReader(data))
	default:
		return nil, fmt.Errorf("unsupported sound format %q", format)
	}
	if err != nil {
		return nil, err
	}
	if decoded.SampleRate() < 1 || decoded.SampleRate() > 384000 {
		return nil, fmt.Errorf("invalid sound sample rate")
	}
	samples, err := readSoundSamples(decoded, decoded.Length())
	if err != nil {
		return nil, err
	}
	return &Sound{Rate: decoded.SampleRate(), Samples: samples}, nil
}

// ResampleSound converts decoded PCM once, before repeated playback.
func ResampleSound(sound *Sound, rate int) (*Sound, error) {
	if sound.Rate < 1 || rate < 1 || sound.Rate > 384000 || rate > 384000 {
		return nil, fmt.Errorf("invalid sound sample rate")
	}
	if sound.Rate == rate || len(sound.Samples) == 0 {
		return sound, nil
	}
	length := int64(len(sound.Samples)) * 4
	outputLength := length * int64(rate) / int64(sound.Rate)
	if outputLength > maxSoundBytes {
		return nil, fmt.Errorf("excessive resampled audio")
	}
	source := NewStream(sound, sound.Rate)
	reader := audio.ResampleReaderF32(source, length, sound.Rate, rate)
	samples, err := readSoundSamples(reader, outputLength)
	if err != nil {
		return nil, err
	}
	return &Sound{Rate: rate, Samples: samples}, nil
}

// Decode chunks into the sample buffer without retaining a second full PCM copy.
func readSoundSamples(decoded io.Reader, length int64) ([]float32, error) {
	if length > maxSoundBytes {
		return nil, fmt.Errorf("invalid or excessive decoded audio")
	}
	samples := make([]float32, 0, max(0, length)/4)
	var pcm [32 << 10]byte
	used := 0
	for {
		n, err := decoded.Read(pcm[used:])
		if err != nil && err != io.EOF {
			return nil, err
		}
		used += n
		if used < len(pcm) && err == nil {
			continue
		}
		if used%8 != 0 || len(samples)+used/4 > maxSoundBytes/4 {
			return nil, fmt.Errorf("invalid or excessive decoded audio")
		}
		for offset := 0; offset < used; offset += 4 {
			value := math.Float32frombits(binary.LittleEndian.Uint32(pcm[offset:]))
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				value = 0
			}
			samples = append(samples, value)
		}
		if err != nil {
			return samples, nil
		}
		used = 0
	}
}

// LoadVisual decodes only costumes; headless audio retains metadata timing.
func LoadVisual(path string, p *project.Project) (*Library, error) {
	visual := *p
	visual.Targets = make([]*project.Target, len(p.Targets))
	for i, t := range p.Targets {
		copy := *t
		copy.Sounds = nil
		visual.Targets[i] = &copy
	}
	return Load(path, &visual)
}
