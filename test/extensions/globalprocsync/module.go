// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"encoding/json"
	"fmt"
	e "gowarp/core/extension"
	"regexp"
	"slices"
	"sort"
)

func New() e.Extension {
	return e.Extension{ID: "globalprocsync", State: map[string]any{"lastResult": "아직 실행하지 않음"}, Blocks: []e.Block{
		{Opcode: "globalprocsync_syncAll", Kind: "command", Handler: syncAll},
		{Opcode: "globalprocsync_getLastResult", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: c.State["lastResult"]}, nil }},
	}}
}

type block struct {
	Opcode   string           `json:"opcode"`
	Mutation map[string]any   `json:"mutation"`
	Inputs   map[string][]any `json:"inputs"`
}
type target struct {
	Index   int                        `json:"target"`
	Name    string                     `json:"name"`
	Storage map[string]json.RawMessage `json:"extensionStorage"`
	Blocks  map[string]*block          `json:"blocks"`
}
type prototype struct {
	code, owner string
	ids, names  []string
	defaults    []any
	types       []string
	mutation    map[string]any
}

func stringArray(value any) []string {
	var result []string
	_ = json.Unmarshal([]byte(e.String(value)), &result)
	return result
}
func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
func choose(call *block, prototypes []prototype, local map[string]bool) *prototype {
	code := e.String(call.Mutation["proccode"])
	for i := range prototypes {
		if prototypes[i].code == code {
			return &prototypes[i]
		}
	}
	if local[code] {
		return nil
	}
	ids := stringArray(call.Mutation["argumentids"])
	best, score, tied := -1, -1, false
	for i, p := range prototypes {
		shared := 0
		for _, id := range ids {
			if contains(p.ids, id) {
				shared++
			}
		}
		if len(ids) == 0 || shared != len(ids) {
			continue
		}
		delta := len(p.ids) - len(ids)
		if delta < 0 {
			delta = -delta
		}
		candidate := 100000 + shared*1000 - delta
		if candidate > score {
			best, score, tied = i, candidate, false
		} else if candidate == score {
			tied = true
		}
	}
	if best < 0 || tied {
		return nil
	}
	return &prototypes[best]
}
func syncAll(c *e.Context) (e.Result, error) {
	summary, err := synchronize(c)
	if err != nil {
		summary = "오류: " + err.Error()
	}
	c.State["lastResult"] = summary
	return e.Result{}, nil
}
func synchronize(c *e.Context) (string, error) {
	snapshot, err := c.Call("procedures.snapshot")
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	var targets []target
	if err = json.Unmarshal(encoded, &targets); err != nil {
		return "", err
	}
	var prototypes []prototype
	owners := map[string]string{}
	types := regexp.MustCompile(`%[sbn]`)
	for _, t := range targets {
		var storage map[string]struct {
			Global bool `json:"global"`
		}
		_ = json.Unmarshal(t.Storage["SPmbpCST"], &storage)
		ids := make([]string, 0, len(t.Blocks))
		for id := range t.Blocks {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			b := t.Blocks[id]
			if b == nil || b.Opcode != "procedures_prototype" {
				continue
			}
			code := e.String(b.Mutation["proccode"])
			if code == "" || !storage[code].Global {
				continue
			}
			p := prototype{code: code, owner: t.Name, ids: stringArray(b.Mutation["argumentids"]), names: stringArray(b.Mutation["argumentnames"]), mutation: b.Mutation, types: types.FindAllString(code, -1)}
			if len(p.ids) != len(p.names) {
				continue
			}
			_ = json.Unmarshal([]byte(e.String(b.Mutation["argumentdefaults"])), &p.defaults)
			for len(p.defaults) < len(p.ids) {
				p.defaults = append(p.defaults, "")
			}
			p.defaults = p.defaults[:len(p.ids)]
			if owner, exists := owners[code]; exists {
				return "", fmt.Errorf("이름이 중복된 전역 함수가 있습니다: %s (%s, %s)", code, owner, t.Name)
			}
			owners[code] = t.Name
			prototypes = append(prototypes, p)
		}
	}
	changes := []any{}
	skipped := 0
	for _, t := range targets {
		local := map[string]bool{}
		for _, b := range t.Blocks {
			if b != nil && b.Opcode == "procedures_prototype" {
				local[e.String(b.Mutation["proccode"])] = true
			}
		}
		ids := make([]string, 0, len(t.Blocks))
		for id := range t.Blocks {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			b := t.Blocks[id]
			if b == nil || b.Opcode != "procedures_call" {
				continue
			}
			p := choose(b, prototypes, local)
			if p == nil {
				shared := false
				for _, arg := range stringArray(b.Mutation["argumentids"]) {
					for _, p := range prototypes {
						if contains(p.ids, arg) {
							shared = true
						}
					}
				}
				if shared {
					skipped++
				}
				continue
			}
			before, _ := json.Marshal(b)
			if b.Mutation == nil {
				b.Mutation = map[string]any{}
			}
			if b.Inputs == nil {
				b.Inputs = map[string][]any{}
			}
			b.Mutation["proccode"] = p.code
			if !slices.Equal(stringArray(b.Mutation["argumentids"]), p.ids) {
				b.Mutation["argumentids"] = p.mutation["argumentids"]
			}
			if warp, exists := p.mutation["warp"]; exists {
				b.Mutation["warp"] = warp
			} else {
				delete(b.Mutation, "warp")
			}
			if result, exists := p.mutation["return"]; exists {
				if e.String(b.Mutation["return"]) != e.String(result) {
					b.Mutation["return"] = e.String(result)
				}
			} else {
				delete(b.Mutation, "return")
			}
			for i, arg := range p.ids {
				if _, exists := b.Inputs[arg]; exists {
					continue
				}
				if i < len(p.types) && p.types[i] == "%b" {
					b.Inputs[arg] = []any{2, nil}
				} else {
					value := ""
					if p.defaults[i] != nil {
						value = e.String(p.defaults[i])
					}
					b.Inputs[arg] = []any{1, []any{10, value}}
				}
			}
			after, _ := json.Marshal(b)
			if string(before) != string(after) {
				changes = append(changes, map[string]any{"target": t.Index, "id": id, "mutation": b.Mutation, "inputs": b.Inputs})
			}
		}
	}
	if _, err = c.Call("procedures.updateCalls", changes); err != nil {
		return "", err
	}
	summary := fmt.Sprintf("%d개 전역 함수 확인, %d개 호출 갱신, 전체 코드 캐시 새로고침 완료", len(prototypes), len(changes))
	if skipped > 0 {
		summary += fmt.Sprintf(", %d개 안전상 건너뜀", skipped)
	}
	return summary, nil
}
