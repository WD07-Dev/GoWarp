package vm

import (
	"slices"

	"gowarp/core/extension"
)

type targetQueryKey struct {
	source     int
	variable   string
	value      any
	clonesOnly bool
	first      bool
}

type targetQueryEntry struct {
	key      targetQueryKey
	revision uint64
	ids      []string
	values   [4]struct {
		value any
		match bool
	}
	valueCount int
}

type targetQueryCache struct {
	revision *uint64
	sources  map[int][]*target
	entries  [8]*targetQueryEntry
	next     int
}

func queryScalar(value any) bool {
	switch value.(type) {
	case nil, bool, string, int, int64, float32, float64:
		return true
	}
	return false
}

func (v *Variable) set(value any) {
	if v.queryRevision != nil && (!queryScalar(v.Value) || !queryScalar(value) || v.Value != value) {
		*v.queryRevision++
	}
	v.Value = value
}

func (r *Runtime) invalidateTargetQueries() {
	if cache := r.targetQueries; cache != nil {
		*cache.revision++
		cache.sources = nil
	}
}

func (entry *targetQueryEntry) matches(value any) bool {
	if !queryScalar(value) {
		return extension.Compare(value, entry.key.value) == 0
	}
	for i := range entry.valueCount {
		if entry.values[i].value == value {
			return entry.values[i].match
		}
	}
	match := extension.Compare(value, entry.key.value) == 0
	if entry.valueCount < len(entry.values) {
		entry.values[entry.valueCount].value = value
		entry.values[entry.valueCount].match = match
		entry.valueCount++
	}
	return match
}

func (r *Runtime) queryTargets(source int, q extension.TargetQuery) []string {
	cache := r.targetQueries
	if cache == nil {
		cache = &targetQueryCache{revision: new(uint64)}
		*cache.revision = 1
		r.targetQueries = cache
	}
	key := targetQueryKey{source: source, variable: q.Variable, value: q.Value, clonesOnly: q.ClonesOnly, first: q.First}
	if q.Variable == "" {
		key.value = nil
	}
	var entry *targetQueryEntry
	cacheable := queryScalar(key.value)
	if cacheable {
		for _, candidate := range cache.entries {
			if candidate != nil && candidate.key == key {
				entry = candidate
				break
			}
		}
		if entry != nil && entry.revision == *cache.revision {
			return slices.Clone(entry.ids)
		}
	}
	if entry == nil {
		entry = &targetQueryEntry{key: key}
		if cacheable {
			cache.entries[cache.next] = entry
			cache.next = (cache.next + 1) % len(cache.entries)
		}
	}
	targets := r.targets
	if source >= 0 {
		if cache.sources == nil {
			cache.sources = make(map[int][]*target)
			for _, t := range r.targets {
				if !t.dead {
					cache.sources[t.sourceIndex] = append(cache.sources[t.sourceIndex], t)
				}
			}
		}
		targets = cache.sources[source]
	}
	clear(entry.ids)
	entry.ids = entry.ids[:0]
	for _, t := range targets {
		if t.dead || (q.ClonesOnly && t.state.CloneID == 0) {
			continue
		}
		if q.Variable != "" {
			v := t.variables[q.Variable]
			if v == nil && r.stage != nil {
				v = r.stage.variables[q.Variable]
			}
			if v == nil {
				continue
			}
			v.queryRevision = cache.revision
			cacheable = cacheable && queryScalar(v.Value)
			if !entry.matches(v.Value) {
				continue
			}
		}
		entry.ids = append(entry.ids, logicTargetID(t))
		if q.First {
			break
		}
	}
	entry.revision = 0
	if len(entry.ids) == 0 {
		entry.ids = nil
	}
	if cacheable {
		entry.revision = *cache.revision
	}
	return slices.Clone(entry.ids)
}
