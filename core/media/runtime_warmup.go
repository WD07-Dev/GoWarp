package media

import (
	"sort"
	"weak"

	"gowarp/core/project"
)

// Only literal inputs are safe to prepare without executing project code.
func literalAssetName(block *project.Block, input string) string {
	value := block.Inputs[input]
	if len(value) < 2 {
		return ""
	}
	literal, ok := value[1].([]any)
	if !ok || len(literal) < 2 {
		return ""
	}
	switch literal[0] {
	case 4, 5, 6, 7, 8, 10, float64(4), float64(5), float64(6), float64(7), float64(8), float64(10):
		name, _ := literal[1].(string)
		return name
	default:
		return ""
	}
}

func (lib *Library) prepareReferencedSounds(p *project.Project, progress func()) error {
	cache := lib.cache
	budget := min(8<<20, cache.limit-cache.bytes)
	if budget <= 0 {
		return cache.ctx.Err()
	}
	type candidate struct {
		meta  *Sound
		asset soundSource
		key   string
		bytes int
	}
	var candidates []candidate
	seen := make(map[string]bool)
	for i, target := range p.Targets {
		names := make(map[string]bool)
		for _, block := range target.Blocks {
			if block == nil {
				continue
			}
			switch block.Opcode {
			case "sound_sounds_menu":
				if field := block.Fields["SOUND_MENU"]; len(field) > 0 {
					if name, ok := field[0].(string); ok {
						names[name] = true
					}
				}
			case "sound_play", "sound_playuntildone":
				names[literalAssetName(block, "SOUND_MENU")] = true
			}
		}
		for j, sound := range target.Sounds {
			meta := lib.Sounds[i][j]
			if !names[sound.Name] || meta.pcm16WAV || meta.Duration() > 10 || meta.frames <= 0 || meta.frames > (2<<20)/8 {
				continue
			}
			asset := cache.sounds[meta]
			key := "sound:" + asset.format + ":" + asset.name
			if seen[key] || cache.entries[key] != nil {
				continue
			}
			seen[key] = true
			candidates = append(candidates, candidate{meta, asset, key, meta.frames * 8})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].bytes < candidates[j].bytes
	})
	for _, candidate := range candidates[:min(32, len(candidates))] {
		if err := cache.ctx.Err(); err != nil {
			return err
		}
		if candidate.bytes > budget {
			continue
		}
		data, err := cache.reader.Read(candidate.asset.name)
		if err != nil {
			continue
		}
		decoded, err := DecodeSound(candidate.asset.format, data)
		if err != nil {
			continue
		}
		if err := cache.ctx.Err(); err != nil {
			return err
		}
		size := len(decoded.Samples) * 4
		if size <= budget && size <= cache.limit-cache.bytes {
			cache.put(&cachedAsset{key: candidate.key, bytes: size, sound: decoded})
			candidate.meta.decoded = weak.Make(decoded)
			budget -= size
		}
		if progress != nil {
			progress()
		}
	}
	return cache.ctx.Err()
}
