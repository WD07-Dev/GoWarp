package media

import (
	"bytes"
	"container/list"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"weak"

	"github.com/hajimehoshi/ebiten/v2/audio/vorbis"

	"gowarp/core/project"
)

type LoadProgress struct {
	Done, Total int
	Label       string
}

type costumeSource struct {
	name string
	key  string
	meta project.Costume
}

type soundSource struct {
	name, format string
}

const preparedMP3Bytes = 1 << 20
const maxStreamSoundBytes = 1 << 30

type cachedAsset struct {
	key         string
	bytes       int
	image       *image.NRGBA
	repeated    bool
	original    weak.Pointer[image.NRGBA]
	sound       *Sound
	soundDecode *soundDecode
	data        []byte
	prepared    *mp3PCM
}

type runtimeCache struct {
	reader       *project.AssetReader
	ctx          context.Context
	costumes     map[*Costume]costumeSource
	sounds       map[*Sound]soundSource
	entries      map[string]*list.Element
	recent       list.List
	bytes, limit int
	err          error
	closed       bool
	display      *displayPreparation
	playback     map[string]*playbackBitmap
	playbackFile *os.File
}

func LoadRuntime(ctx context.Context, path string, p *project.Project, progress func(LoadProgress)) (*Library, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, err := project.OpenAssets(path, p)
	if err != nil {
		return nil, err
	}
	cache := &runtimeCache{reader: reader, ctx: ctx, costumes: map[*Costume]costumeSource{}, sounds: map[*Sound]soundSource{}, entries: map[string]*list.Element{}, limit: 96 << 20}
	lib := &Library{cache: cache}
	success := false
	defer func() {
		if !success {
			lib.Close()
		}
	}()
	total := len(p.Targets) + 2
	for _, target := range p.Targets {
		total += len(target.Costumes) + len(target.Sounds)
	}
	done := 0
	report := func(label string) {
		if progress != nil {
			progress(LoadProgress{Done: done, Total: total, Label: label})
		}
	}
	report("Preparing project")
	if err := lib.loadFonts(p, reader.Read); err != nil {
		return nil, fmt.Errorf("custom font: %w", err)
	}
	geometry := map[string]*Costume{}
	soundInfo := map[string]*Sound{}
	for _, target := range p.Targets {
		costumes := make([]*Costume, 0, len(target.Costumes))
		sounds := make([]*Sound, 0, len(target.Sounds))
		for _, meta := range target.Costumes {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			name := project.AssetName(meta.MD5Ext, meta.AssetID, meta.DataFormat)
			var c *Costume
			if name != "" {
				key := fmt.Sprintf("%s/%s/%g/%g/%g", name, meta.DataFormat, meta.BitmapResolution, meta.RotationCenterX, meta.RotationCenterY)
				if known := geometry[key]; known != nil {
					copy := *known
					c = &copy
				} else {
					data, e := reader.Read(name)
					if e != nil {
						return nil, e
					}
					c, e = inspectCostume(meta, data)
					if e != nil {
						return nil, fmt.Errorf("%s costume %q: %w", target.Name, meta.Name, e)
					}
					geometry[key] = c
				}
				cache.costumes[c] = costumeSource{
					name: name,
					key:  "image:" + meta.DataFormat + ":" + name,
					meta: meta,
				}
			} else {
				c = &Costume{Image: image.NewNRGBA(image.Rect(0, 0, 1, 1))}
			}
			costumes = append(costumes, c)
			done++
			report("Reading costume information")
		}
		for _, meta := range target.Sounds {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			name := project.AssetName(meta.MD5Ext, meta.AssetID, meta.DataFormat)
			key := meta.DataFormat + ":" + name
			s := soundInfo[key]
			if s == nil {
				data, e := reader.Read(name)
				if e != nil {
					return nil, e
				}
				var prepared *mp3PCM
				s, prepared, e = inspectSoundWithMP3(meta.DataFormat, data)
				if e != nil {
					return nil, fmt.Errorf("%s sound %q: %w", target.Name, meta.Name, e)
				}
				soundInfo[key] = s
				if s.Duration() > 10 || s.pcm16WAV {
					key := "sound-source:" + name
					if cache.get(key) == nil {
						entry := &cachedAsset{key: key, bytes: len(data), data: data, prepared: prepared}
						if prepared != nil {
							entry.bytes += preparedMP3Bytes
						}
						cache.put(entry)
					}
				}
			}
			cache.sounds[s] = soundSource{name, meta.DataFormat}
			sounds = append(sounds, s)
			done++
			report("Reading sound information")
		}
		lib.Costumes = append(lib.Costumes, costumes)
		lib.Sounds = append(lib.Sounds, sounds)
	}
	if err := lib.prepareReferencedSounds(p, func() {
		report("Preparing sound effects")
	}); err != nil {
		return nil, err
	}
	done++
	report("Preparing sound effects")
	for i, target := range p.Targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(lib.Costumes[i]) > 0 {
			index := max(0, min(len(lib.Costumes[i])-1, target.CurrentCostume))
			if lib.CostumeImage(lib.Costumes[i][index]) == nil {
				return nil, lib.Err()
			}
		}
		done++
		report("Preparing the first frame")
	}
	if err := lib.prepareReferencedCostumes(p, func(LoadProgress) {
		report("Preparing the first frame")
	}); err != nil {
		return nil, err
	}
	done++
	report("Preparing the first frame")
	success = true
	return lib, nil
}

func (lib *Library) prepareReferencedCostumes(p *project.Project, progress func(LoadProgress)) error {
	cache := lib.cache
	budget := min(16<<20, cache.limit-cache.bytes)
	if budget <= 0 {
		return cache.ctx.Err()
	}
	backdrops := make(map[string]bool)
	names := make([]map[string]bool, len(p.Targets))
	stamps := make([]bool, len(p.Targets))
	for i, target := range p.Targets {
		names[i] = make(map[string]bool)
		for _, block := range target.Blocks {
			if block == nil {
				continue
			}
			var field string
			switch block.Opcode {
			case "pen_stamp":
				stamps[i] = true
				continue
			case "looks_switchcostumeto":
				names[i][literalAssetName(block, "COSTUME")] = true
				continue
			case "looks_switchbackdropto", "looks_switchbackdroptoandwait":
				backdrops[literalAssetName(block, "BACKDROP")] = true
				continue
			case "looks_costume":
				field = "COSTUME"
			case "looks_backdrops":
				field = "BACKDROP"
			default:
				continue
			}
			if value := block.Fields[field]; len(value) > 0 {
				if name, ok := value[0].(string); ok {
					if field == "BACKDROP" {
						backdrops[name] = true
					} else {
						names[i][name] = true
					}
				}
			}
		}
	}
	type candidate struct {
		source       costumeSource
		bytes        int
		minimumBytes int
		referenced   bool
	}
	var candidates []candidate
	seen := make(map[string]int)
	for i, target := range p.Targets {
		for j, meta := range target.Costumes {
			referenced := names[i][meta.Name] || target.IsStage && backdrops[meta.Name]
			if !referenced && !stamps[i] {
				continue
			}
			costume := lib.Costumes[i][j]
			source, ok := cache.costumes[costume]
			if !ok || cache.entries[source.key] != nil {
				continue
			}
			if index, exists := seen[source.key]; exists {
				candidates[index].referenced = candidates[index].referenced || referenced
				continue
			}
			w, h := math.Ceil(costume.Width), math.Ceil(costume.Height)
			if costume.Bitmap {
				size := lib.BitmapSize(costume)
				w, h = float64(size.X), float64(size.Y)
			}
			if !validSize(w, h) || w*h*4 > 4<<20 {
				continue
			}
			size := int(w * h * 4)
			minimum := size
			if costume.Bitmap && size >= 1<<20 && int(w)%2 == 0 && int(h)%2 == 0 {
				// Identical 2x2 pixels can fit even when their source buffer cannot.
				minimum /= 4
			}
			if minimum > budget {
				continue
			}
			seen[source.key] = len(candidates)
			candidates = append(candidates, candidate{source: source, bytes: size, minimumBytes: minimum, referenced: referenced})
		}
	}
	// Prepare small effects first; stable ordering resolves equal-size candidates.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].referenced != candidates[j].referenced {
			return candidates[i].referenced
		}
		return candidates[i].bytes < candidates[j].bytes
	})
	candidates = candidates[:min(1024, len(candidates))]
	workBudget := 64 << 20
	for i, candidate := range candidates {
		if err := cache.ctx.Err(); err != nil {
			return err
		}
		work := max(candidate.bytes, 64<<10)
		if strings.EqualFold(candidate.source.meta.DataFormat, "svg") {
			work = max(work, 1<<20)
		}
		if candidate.minimumBytes <= budget && work <= workBudget {
			workBudget -= work
			source := candidate.source
			data, err := cache.reader.Read(source.name)
			if err == nil {
				decoded, err := decodeCostumeWithAntialias(source.meta, data, lib.customFont, lib.noAntialias)
				if err == nil {
					asset := newCostumeAsset(source.key, decoded.Image, !strings.EqualFold(source.meta.DataFormat, "svg"))
					if asset.bytes <= budget && asset.bytes <= cache.limit-cache.bytes {
						cache.put(asset)
						budget -= asset.bytes
					}
				}
			}
		}
		if progress != nil {
			progress(LoadProgress{Done: i + 1, Total: len(candidates), Label: "Preparing the first frame"})
		}
	}
	return cache.ctx.Err()
}

func inspectCostume(meta project.Costume, data []byte) (*Costume, error) {
	c := &Costume{}
	if strings.EqualFold(meta.DataFormat, "svg") {
		decoder := xml.NewDecoder(bytes.NewReader(data))
		for {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			start, ok := token.(xml.StartElement)
			if !ok {
				continue
			}
			if start.Name.Local != "svg" {
				return nil, fmt.Errorf("expected SVG root")
			}
			header, err := svgXML(&svgNode{start: start})
			if err != nil {
				return nil, err
			}
			header, err = normalizeSVG(header)
			if err != nil {
				return nil, err
			}
			root, err := parseSVGTree(header)
			if err != nil {
				return nil, err
			}
			box, empty := svgEmptyViewport(root)
			if !empty {
				box, err = vectorSVG(header, nil)
				if err != nil {
					return nil, err
				}
			}
			c.Width, c.Height = box.W, box.H
			c.CenterX, c.CenterY = meta.RotationCenterX-box.X, meta.RotationCenterY-box.Y
			return c, nil
		}
	}
	c.Bitmap = true
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if !validSize(float64(config.Width), float64(config.Height)) {
		return nil, fmt.Errorf("invalid or excessive bitmap dimensions")
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
	return c, nil
}

func inspectSound(format string, data []byte) (*Sound, error) {
	sound, _, err := inspectSoundWithMP3(format, data)
	return sound, err
}

func inspectSoundWithMP3(format string, data []byte) (*Sound, *mp3PCM, error) {
	format = soundFormat(format, data)
	switch format {
	case "wav":
		f, payload, fact, err := readWAV(data)
		if err != nil {
			return nil, nil, err
		}
		frames := len(payload) / f.align
		if f.kind == 0x11 {
			frames, err = imaFrameCount(f, payload, fact)
		} else if len(payload)%f.align != 0 {
			err = fmt.Errorf("partial WAV PCM frame")
		}
		if err != nil {
			return nil, nil, err
		}
		if frames > maxWAVFrames {
			return nil, nil, fmt.Errorf("excessive decoded audio")
		}
		return &Sound{Rate: f.rate, frames: frames, pcm16WAV: f.kind == 1 && f.bits == 16}, nil, nil
	case "mp3", "ogg":
		var stream interface {
			Length() int64
			SampleRate() int
		}
		var err error
		var prepared *mp3PCM
		if format == "ogg" {
			stream, err = vorbis.DecodeF32(bytes.NewReader(data))
		} else {
			prepared, err = openMP3Stream(data)
			stream = prepared
		}
		if err != nil {
			return nil, nil, err
		}
		length, rate := stream.Length(), stream.SampleRate()
		if length < 0 || length > maxStreamSoundBytes || length%8 != 0 || rate < 1 || rate > 384000 {
			return nil, nil, fmt.Errorf("invalid or excessive decoded audio")
		}
		return &Sound{Rate: rate, frames: int(length / 8), mp3: format == "mp3"}, prepared, nil
	default:
		return nil, nil, fmt.Errorf("unsupported sound format %q", format)
	}
}

func newCostumeAsset(key string, pixels *image.NRGBA, bitmap bool) *cachedAsset {
	asset := &cachedAsset{key: key, image: pixels, bytes: len(pixels.Pix)}
	if bitmap {
		asset.image, asset.repeated = CompactRepeatedPixels(pixels)
		if asset.repeated {
			asset.bytes = len(asset.image.Pix)
			asset.original = weak.Make(pixels)
		}
	}
	return asset
}

func (asset *cachedAsset) originalPixels() *image.NRGBA {
	if !asset.repeated {
		return asset.image
	}
	if pixels := asset.original.Value(); pixels != nil {
		return pixels
	}
	compact := asset.image
	w, h := compact.Bounds().Dx(), compact.Bounds().Dy()
	pixels := image.NewNRGBA(image.Rect(0, 0, w*2, h*2))
	for y := 0; y < h; y++ {
		row := pixels.Pix[y*2*pixels.Stride : (y*2+1)*pixels.Stride]
		for x := 0; x < w; x++ {
			src := compact.Pix[y*compact.Stride+x*4 : y*compact.Stride+x*4+4]
			copy(row[x*8:x*8+4], src)
			copy(row[x*8+4:x*8+8], src)
		}
		copy(pixels.Pix[(y*2+1)*pixels.Stride:], row)
	}
	asset.original = weak.Make(pixels)
	return pixels
}

func (c *runtimeCache) get(key string) *cachedAsset {
	if entry := c.entries[key]; entry != nil {
		c.recent.MoveToFront(entry)
		return entry.Value.(*cachedAsset)
	}
	return nil
}

func (c *runtimeCache) put(asset *cachedAsset) {
	if asset.bytes > c.limit {
		return
	}
	for c.bytes+asset.bytes > c.limit {
		oldest := c.recent.Back()
		if oldest == nil {
			break
		}
		value := oldest.Value.(*cachedAsset)
		delete(c.entries, value.key)
		c.bytes -= value.bytes
		c.recent.Remove(oldest)
	}
	c.entries[asset.key] = c.recent.PushFront(asset)
	c.bytes += asset.bytes
}

func (c *runtimeCache) putPrefetched(asset *cachedAsset) {
	if c.bytes+asset.bytes > c.limit {
		return
	}
	c.entries[asset.key] = c.recent.PushBack(asset)
	c.bytes += asset.bytes
}

func (c *runtimeCache) ready() bool {
	if c.err != nil {
		return false
	}
	if c.closed {
		c.err = fmt.Errorf("asset library is closed")
		return false
	}
	c.err = c.ctx.Err()
	return c.err == nil
}

// Reuse inspected sound sources within the shared LRU; active streams retain their own reference.
func (c *runtimeCache) soundSource(name string) (*cachedAsset, error) {
	key := "sound-source:" + name
	if entry := c.get(key); entry != nil {
		return entry, nil
	}
	data, err := c.reader.Read(name)
	if err != nil {
		return nil, err
	}
	entry := &cachedAsset{key: key, bytes: len(data), data: data}
	c.put(entry)
	return entry, nil
}

func (lib *Library) CostumeImage(costume *Costume) *image.NRGBA {
	cache := lib.cache
	if cache == nil {
		return costume.Image
	}
	if !cache.ready() {
		return nil
	}
	if costume.Image != nil {
		return costume.Image
	}
	source, ok := cache.costumes[costume]
	if !ok {
		cache.err = fmt.Errorf("unknown costume")
		return nil
	}
	key := source.key
	if display := cache.display; display != nil {
		delete(display.bitmapUnused, key)
	}
	if entry := cache.get(key); entry != nil {
		if display := cache.display; display != nil {
			if work := display.bitmapPending[key]; work != nil {
				work.state.CompareAndSwap(bitmapQueued, bitmapHostOwned)
			}
		}
		return entry.originalPixels()
	}
	if entry, err := lib.preparedDisplayBitmap(key); err != nil {
		cache.err = fmt.Errorf("costume %q: %w", source.meta.Name, err)
		return nil
	} else if entry != nil {
		return entry.originalPixels()
	}
	if entry := cache.playbackAsset(key); entry != nil {
		return entry.originalPixels()
	}
	data, err := cache.reader.Read(source.name)
	if err != nil {
		cache.err = err
		return nil
	}
	decoded, err := decodeCostumeWithAntialias(source.meta, data, lib.customFont, lib.noAntialias)
	if err != nil {
		cache.err = fmt.Errorf("costume %q: %w", source.meta.Name, err)
		return nil
	}
	cache.put(newCostumeAsset(key, decoded.Image, costume.Bitmap))
	return decoded.Image
}

/*
BitmapSize returns the source pixel dimensions without decoding the image.
Non-bitmap costumes return a zero size.
*/
func (lib *Library) BitmapSize(costume *Costume) image.Point {
	if !costume.Bitmap {
		return image.Point{}
	}
	if costume.Image != nil {
		return costume.Image.Bounds().Size()
	}
	if lib.cache == nil {
		return image.Point{}
	}
	source, ok := lib.cache.costumes[costume]
	if !ok {
		return image.Point{}
	}
	resolution := source.meta.BitmapResolution
	if resolution <= 0 {
		resolution = 1
	}
	return image.Pt(int(math.Round(costume.Width*resolution)), int(math.Round(costume.Height*resolution)))
}

/*
CostumeImageCacheKey returns the pixel identity and bounded SVG density without decoding. Bitmap
keys are scale-independent.
*/
func (lib *Library) CostumeImageCacheKey(costume *Costume, scale float64) (string, float64) {
	if costume.CacheKey != "" {
		return costume.CacheKey, 1
	}
	cache := lib.cache
	if cache == nil {
		return fmt.Sprintf("eager:%p", costume.Image), 1
	}
	source, ok := cache.costumes[costume]
	if !ok {
		return fmt.Sprintf("eager:%p", costume.Image), 1
	}
	if !strings.EqualFold(source.meta.DataFormat, "svg") || scale <= 1 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return source.key, 1
	}
	density := 1.0
	for density < scale {
		next := density * 2
		w, h := math.Ceil(costume.Width*next), math.Ceil(costume.Height*next)
		if !validSize(w, h) || w*h*4 > float64(cache.limit) {
			break
		}
		density = next
	}
	if density == 1 {
		return source.key, 1
	}
	return fmt.Sprintf("svg:%s:%g", source.name, density), density
}

/*
CostumeImageAtScale caches SVG display rasters at bounded power-of-two densities. Collision
geometry stays unchanged; bitmaps use base pixels.
*/
func (lib *Library) CostumeImageAtScale(costume *Costume, scale float64) *image.NRGBA {
	cache := lib.cache
	if cache == nil {
		return costume.Image
	}
	if !cache.ready() {
		return nil
	}
	key, density := lib.CostumeImageCacheKey(costume, scale)
	if density == 1 {
		return lib.CostumeImage(costume)
	}
	if entry := cache.get(key); entry != nil {
		return entry.image
	}
	source := cache.costumes[costume]
	data, err := cache.reader.Read(source.name)
	if err != nil {
		cache.err = err
		return nil
	}
	decoded, _, err := rasterSVGWithAntialias(data, density, lib.customFont, lib.noAntialias)
	if !cache.ready() {
		return nil
	}
	if err != nil {
		// Cache base pixels when high-density compositing fails, avoiding repeated retries.
		decoded = lib.CostumeImage(costume)
		if decoded == nil {
			return nil
		}
	}
	cache.put(&cachedAsset{key: key, bytes: len(decoded.Pix), image: decoded})
	return decoded
}

func (lib *Library) Sound(source, index int) (*Sound, error) {
	if source < 0 || source >= len(lib.Sounds) || index < 0 || index >= len(lib.Sounds[source]) {
		return nil, fmt.Errorf("invalid sound asset %d/%d", source, index)
	}
	sound := lib.Sounds[source][index]
	cache := lib.cache
	if cache == nil {
		return sound, nil
	}
	if !cache.ready() {
		return nil, cache.err
	}
	asset := cache.sounds[sound]
	key := "sound:" + asset.format + ":" + asset.name
	if entry := cache.get(key); entry != nil {
		if entry.soundDecode != nil {
			<-entry.soundDecode.done
			return entry.soundDecode.sound, entry.soundDecode.err
		}
		return entry.sound, nil
	}
	if decoded := sound.decoded.Value(); decoded != nil {
		return decoded, nil
	}
	data, err := cache.reader.Read(asset.name)
	if err != nil {
		cache.err = err
		return nil, err
	}
	decoded, err := DecodeSound(asset.format, data)
	if err != nil {
		cache.err = fmt.Errorf("sound %q: %w", asset.name, err)
		return nil, cache.err
	}
	cache.put(&cachedAsset{key: key, bytes: len(decoded.Samples) * 4, sound: decoded})
	sound.decoded = weak.Make(decoded)
	return decoded, nil
}

/*
Stream samples PCM16 WAV directly. Cold MP3 effects share background decoding;
long compressed music keeps only a small decoded window.
*/
func (lib *Library) Stream(source, index, rate int) (*Stream, error) {
	if source < 0 || source >= len(lib.Sounds) || index < 0 || index >= len(lib.Sounds[source]) {
		return nil, fmt.Errorf("invalid sound asset %d/%d", source, index)
	}
	meta := lib.Sounds[source][index]
	cache := lib.cache
	if cache != nil && (meta.Duration() > 10 || meta.pcm16WAV || meta.mp3) {
		if !cache.ready() {
			return nil, cache.err
		}
		asset := cache.sounds[meta]
		key := "sound:" + asset.format + ":" + asset.name
		if entry := cache.get(key); entry != nil {
			if entry.soundDecode != nil {
				return entry.soundDecode.stream(meta, rate)
			}
			return NewStream(entry.sound, rate), nil
		}
		if decoded := meta.decoded.Value(); decoded != nil {
			return NewStream(decoded, rate), nil
		}
		if pending := meta.decoding.Value(); pending != nil {
			return pending.stream(meta, rate)
		}
		if meta.mp3 && meta.Duration() <= 10 {
			data, err := cache.reader.Read(asset.name)
			if err != nil {
				return nil, err
			}
			pending := &soundDecode{done: make(chan struct{})}
			meta.decoding = weak.Make(pending)
			// Reserve decoded PCM before starting work; eviction leaves active voices valid.
			cache.put(&cachedAsset{key: key, bytes: len(data) + meta.frames*8, soundDecode: pending})
			go func() {
				pending.sound, pending.err = DecodeSound("mp3", data)
				if pending.err == nil && (pending.sound.Rate != meta.Rate || len(pending.sound.Samples)/2 > meta.frames) {
					pending.err = fmt.Errorf("MP3 samples changed since loading")
				}
				close(pending.done)
			}()
			return pending.stream(meta, rate)
		}
		source := meta.source.Value()
		var err error
		if source == nil {
			source, err = cache.soundSource(asset.name)
			if err != nil {
				return nil, err
			}
			meta.source = weak.Make(source)
		}
		data := source.data
		var decoded interface {
			io.ReadSeeker
			SampleRate() int
			Length() int64
		}
		switch soundFormat(asset.format, data) {
		case "mp3":
			if source.prepared != nil {
				// Transfer the prepared decoder once; overlapping players need separate cursors.
				decoded = source.prepared
				source.prepared = nil
				source.bytes -= preparedMP3Bytes
				cache.bytes -= preparedMP3Bytes
			} else {
				// Defer replacement decoder initialization to the read-ahead worker's first read.
				decoded = &streamMP3{data: data, rate: meta.Rate, length: int64(meta.frames) * 8}
			}
		case "ogg":
			decoded, err = vorbis.DecodeF32(bytes.NewReader(data))
		case "wav":
			format, payload, _, wavErr := readWAV(data)
			if wavErr != nil {
				return nil, wavErr
			}
			if format.kind == 1 && format.bits == 16 {
				if len(payload)%format.align != 0 {
					return nil, fmt.Errorf("partial WAV PCM frame")
				}
				frames := len(payload) / format.align
				if frames > maxWAVFrames {
					return nil, fmt.Errorf("excessive decoded audio")
				}
				stream := NewStream(&Sound{Rate: format.rate}, rate)
				stream.source = source
				stream.frames, stream.end = frames, float64(frames)
				stream.wav = &wavPCM{data: payload, channels: format.channels}
				return stream, nil
			}
		}
		if err != nil {
			return nil, fmt.Errorf("sound %q: %w", asset.name, err)
		}
		if decoded != nil {
			length := decoded.Length()
			if length < 0 || length > maxStreamSoundBytes || length%8 != 0 || decoded.SampleRate() < 1 || decoded.SampleRate() > 384000 {
				return nil, fmt.Errorf("invalid or excessive decoded audio")
			}
			stream := NewStream(&Sound{Rate: decoded.SampleRate()}, rate)
			stream.source = source
			stream.frames = int(length / 8)
			stream.end = float64(stream.frames)
			stream.pcm = &streamPCM{reader: decoded, data: make([]byte, 32<<10)}
			return stream, nil
		}
		// Other WAV formats retain the PCM cache, using the bytes already read.
		sound, err := DecodeSound(asset.format, data)
		if err != nil {
			return nil, err
		}
		cache.put(&cachedAsset{key: key, bytes: len(sound.Samples) * 4, sound: sound})
		meta.decoded = weak.Make(sound)
		return NewStream(sound, rate), nil
	}
	sound, err := lib.Sound(source, index)
	if err != nil {
		return nil, err
	}
	return NewStream(sound, rate), nil
}

func (lib *Library) Err() error {
	if lib.cache != nil {
		return lib.cache.err
	}
	return nil
}

func (lib *Library) Close() error {
	if lib.cache == nil || lib.cache.closed {
		return nil
	}
	lib.cache.closed = true
	if display := lib.cache.display; display != nil {
		display.cancel()
		<-display.done
		if display.bitmapDone != nil {
			<-display.bitmapDone
		}
		lib.cache.display = nil
	}
	lib.cache.entries = nil
	lib.cache.playback = nil
	lib.cache.recent.Init()
	lib.cache.bytes = 0
	err := lib.cache.reader.Close()
	if file := lib.cache.playbackFile; file != nil {
		lib.cache.playbackFile = nil
		err = errors.Join(err, file.Close(), os.Remove(file.Name()))
	}
	return err
}
