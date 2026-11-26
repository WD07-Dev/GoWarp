package config

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"gowarp/core/project"
)

/*
TurboWarp stores only changed settings in a stage comment. Preserve strings
while treating its extended JSON nonfinite numbers as absent numeric values.
*/
var extendedJSONToken = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|-?Infinity|NaN`)

func storedProjectSettings(p *project.Project) map[string]json.RawMessage {
	if p == nil {
		return nil
	}
	for _, target := range p.Targets {
		if target == nil || !target.IsStage {
			continue
		}
		ids := make([]string, 0, len(target.Comments))
		for id := range target.Comments {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			text := target.Comments[id].Text
			if !strings.Contains(text, "_twconfig_") {
				continue
			}
			candidates := []string{}
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, "_twconfig_") {
					candidates = append(candidates, line)
				}
			}
			candidates = append(candidates, text)
			for _, candidate := range candidates {
				start, end := strings.Index(candidate, "{"), strings.LastIndex(candidate, "}")
				if start < 0 || end < start {
					continue
				}
				data := extendedJSONToken.ReplaceAllStringFunc(candidate[start:end+1], func(token string) string {
					if token[0] == '"' {
						return token
					}
					return "null"
				})
				var fields map[string]json.RawMessage
				if json.Unmarshal([]byte(data), &fields) == nil && fields != nil {
					return fields
				}
			}
		}
	}
	return nil
}

func applyProjectSettings(s *Settings, p *project.Project) {
	fields := storedProjectSettings(p)
	var fps *float64
	if json.Unmarshal(fields["framerate"], &fps) == nil && fps != nil && *fps >= 0 && *fps <= 1000 {
		s.FPS = *fps
		if s.FPS == 0 {
			s.FPS = 60
		}
	}
	for key, dest := range map[string]*bool{"turbo": &s.Turbo, "interpolation": &s.Interpolation, "hq": s.HighQualityPen} {
		var value *bool
		if json.Unmarshal(fields[key], &value) == nil && value != nil {
			*dest = *value
		}
	}
	var runtimeOptions struct {
		Fencing    *bool `json:"fencing"`
		MiscLimits *bool `json:"miscLimits"`
	}
	if json.Unmarshal(fields["runtimeOptions"], &runtimeOptions) == nil {
		if runtimeOptions.Fencing != nil {
			*s.RemoveFencing = !*runtimeOptions.Fencing
		}
		if runtimeOptions.MiscLimits != nil {
			*s.RemoveMiscLimits = !*runtimeOptions.MiscLimits
		}
	}
	for _, dimension := range []struct {
		key, alias string
		dest       *int
	}{
		{"width", "stageWidth", &s.Width}, {"height", "stageHeight", &s.Height},
	} {
		data, ok := fields[dimension.key]
		if !ok {
			data = fields[dimension.alias]
		}
		var value *int
		if json.Unmarshal(data, &value) == nil && value != nil && *value >= 1 && *value <= 4096 {
			*dimension.dest = *value
		}
	}
}
