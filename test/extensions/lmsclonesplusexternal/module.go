// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"crypto/rand"
	"fmt"
	e "gowarp/core/extension"
	"math"
	randv2 "math/rand/v2"
	"sort"
	"strings"
)

type targetSet struct {
	ids  []string
	used uint64
}
type registry struct {
	session string
	serial  uint64
	sets    map[string]*targetSet
	first   map[string]string
}

func state(c *e.Context) *registry {
	s, _ := c.State["sets"].(*registry)
	if s == nil {
		s = &registry{session: rand.Text(), sets: map[string]*targetSet{}, first: map[string]string{}}
		c.State["sets"] = s
	}
	return s
}
func targets(c *e.Context) ([]map[string]any, error) {
	v, err := c.Call("target.list")
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, v := range v.([]any) {
		out = append(out, v.(map[string]any))
	}
	return out, nil
}
func original(ts []map[string]any, name string) map[string]any {
	for _, t := range ts {
		if e.String(t["name"]) == name && e.Number(t["cloneId"]) == 0 && !e.Bool(t["isStage"]) {
			return t
		}
	}
	return nil
}
func clones(ts []map[string]any, name string) []string {
	o := original(ts, name)
	if o == nil {
		return nil
	}
	ids := []string{}
	// Clone IDs increase at creation; layer changes must not change first-clone selection.
	sort.SliceStable(ts, func(i, j int) bool { return e.Number(ts[i]["cloneId"]) < e.Number(ts[j]["cloneId"]) })
	for _, t := range ts {
		if t["source"] == o["source"] && e.Number(t["cloneId"]) != 0 {
			ids = append(ids, e.String(t["id"]))
		}
	}
	return ids
}
func resolve(c *e.Context, token string, ts []map[string]any) []string {
	s := state(c)
	set := s.sets[token]
	if set == nil {
		return nil
	}
	s.serial++
	set.used = s.serial
	alive := map[string]bool{}
	for _, t := range ts {
		if e.Number(t["cloneId"]) != 0 {
			alive[e.String(t["id"])] = true
		}
	}
	ids := set.ids[:0]
	for _, id := range set.ids {
		if alive[id] {
			ids = append(ids, id)
		}
	}
	set.ids = ids
	return ids
}
func localID(c *e.Context, kind, token, target string) (string, error) {
	v, err := c.Call(kind+".list", target)
	if err != nil {
		return "", err
	}
	records := v.([]any)
	for _, r := range records {
		m := r.(map[string]any)
		if e.String(m["id"]) == token {
			return token, nil
		}
	}
	for _, r := range records {
		m := r.(map[string]any)
		if e.String(m["name"]) == token {
			return e.String(m["id"]), nil
		}
	}
	return "", nil
}
func localValue(c *e.Context, kind, token, target string) (any, string, error) {
	id, err := localID(c, kind, token, target)
	if err != nil || id == "" {
		return nil, id, err
	}
	v, err := c.Call(kind+".get", id, target)
	return v, id, err
}
func store(c *e.Context, ids []string, ts []map[string]any) (string, error) {
	s := state(c)
	s.serial++
	token := fmt.Sprintf("@CPXSET:%s:%d", s.session, s.serial)
	s.sets[token] = &targetSet{ids: append([]string{}, ids...), used: s.serial}
	entries := 0
	for _, set := range s.sets {
		entries += len(set.ids)
	}
	if len(s.sets) <= 4096 && entries <= 262144 {
		return token, nil
	}
	// Preserve handles explicitly retained in Scratch variables or list items.
	protected := map[string]bool{token: true}
	remember := func(v any) {
		if token, ok := v.(string); ok {
			protected[token] = true
		}
	}
	for _, t := range ts {
		ref := e.String(t["id"])
		for _, kind := range []string{"variable", "list"} {
			records, err := c.Call(kind+".list", ref)
			if err != nil {
				return "", err
			}
			for _, r := range records.([]any) {
				v, err := c.Call(kind+".get", r.(map[string]any)["id"], ref)
				if err != nil {
					return "", err
				}
				if kind == "list" {
					for _, item := range v.([]any) {
						remember(item)
					}
				} else {
					remember(v)
				}
			}
		}
	}
	keys := make([]string, 0, len(s.sets))
	for k := range s.sets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return s.sets[keys[i]].used < s.sets[keys[j]].used })
	for _, k := range keys {
		if len(s.sets) <= 3072 && entries <= 196608 {
			break
		}
		if !protected[k] {
			entries -= len(s.sets[k].ids)
			delete(s.sets, k)
		}
	}
	for k, v := range s.first {
		if s.sets[v] == nil {
			delete(s.first, k)
		}
	}
	return token, nil
}
func index(value any, length int) int {
	switch strings.ToLower(strings.TrimSpace(e.String(value))) {
	case "last", "마지막":
		return length - 1
	case "random", "랜덤", "임의":
		if length > 0 {
			return randv2.IntN(length)
		}
		return -1
	case "all", "모두":
		return -1
	}
	n := math.Floor(e.Number(value))
	if math.IsInf(n, 0) || n < 1 || n > float64(length) {
		return -1
	}
	return int(n) - 1
}
func New() e.Extension {
	x := e.Extension{ID: "lmsclonesplusexternal", State: map[string]any{}}
	add := func(op, kind string, inputs []string) {
		x.Blocks = append(x.Blocks, e.Block{Opcode: "lmsclonesplusexternal_" + op, Kind: kind, Inputs: inputs, Handler: func(c *e.Context) (e.Result, error) { v, err := run(c, op); return e.Result{Value: v}, err }})
	}
	add("menu_sprites", "reporter", nil)
	for _, op := range []string{"selectClonesByVariable", "selectFirstCloneByVariable"} {
		add(op, "reporter", []string{"SPRITE", "MATCH_VALUE"})
	}
	add("selectClonesByListItem", "reporter", []string{"SPRITE", "MATCH_INDEX", "MATCH_VALUE"})
	add("selectClonesByListContaining", "reporter", []string{"SPRITE", "ITEM"})
	add("intersectTargetSets", "reporter", []string{"TARGETS_A", "TARGETS_B"})
	for _, op := range []string{"targetSetCount", "targetSetHasAny", "getVariableOfFirstInTargetSet", "getLengthOfListOfFirstInTargetSet", "getThingOfFirstInTargetSet"} {
		add(op, "reporter", []string{"TARGETS"})
	}
	add("getItemOfListOfFirstInTargetSet", "reporter", []string{"TARGETS", "INDEX"})
	add("setVariableOfTargetSet", "command", []string{"TARGETS", "VALUE"})
	add("replaceInListOfTargetSet", "command", []string{"TARGETS", "INDEX", "ITEM"})
	add("deleteTargetSet", "command", []string{"TARGETS"})
	add("createCloneInSpriteWithVar", "command", []string{"SPRITE", "MATCH_VALUE"})
	reset := func(c *e.Context) (e.Result, error) { delete(c.State, "sets"); return e.Result{}, nil }
	x.Events = map[string]e.Handler{"flag": reset, "cancel": reset, "stop": reset, "dispose": reset}
	return x
}
func run(c *e.Context, op string) (any, error) {
	if op == "menu_sprites" {
		return c.Field("sprites"), nil
	}
	ts, err := targets(c)
	if err != nil {
		return nil, err
	}
	if op == "createCloneInSpriteWithVar" {
		o := original(ts, c.String("SPRITE"))
		if o == nil {
			return nil, nil
		}
		id, err := c.Call("target.clone", o["id"])
		if err != nil || id == nil {
			return nil, err
		}
		v, err := localID(c, "variable", c.Field("MATCH_VAR"), e.String(id))
		if err != nil || v == "" {
			return nil, err
		}
		return c.Call("variable.set", v, c.Args["MATCH_VALUE"], id)
	}
	if strings.HasPrefix(op, "select") {
		matches := []string{}
		for _, id := range clones(ts, c.String("SPRITE")) {
			kind, field := "variable", "MATCH_VAR"
			if strings.Contains(op, "List") {
				kind, field = "list", "MATCH_LIST"
			}
			v, key, err := localValue(c, kind, c.Field(field), id)
			if err != nil {
				return nil, err
			}
			if key == "" {
				continue
			}
			match := false
			if kind == "variable" {
				match = e.Compare(v, c.Args["MATCH_VALUE"]) == 0
			} else {
				items := v.([]any)
				if op == "selectClonesByListItem" {
					i := index(c.Args["MATCH_INDEX"], len(items))
					match = i >= 0 && e.Compare(items[i], c.Args["MATCH_VALUE"]) == 0
				} else {
					for _, item := range items {
						if e.Compare(item, c.Args["ITEM"]) == 0 {
							match = true
							break
						}
					}
				}
			}
			if match {
				matches = append(matches, id)
				if op == "selectFirstCloneByVariable" {
					break
				}
			}
		}
		if op == "selectFirstCloneByVariable" && len(matches) > 0 {
			key := fmt.Sprintf("%q:%q:%T:%v", c.String("SPRITE"), c.Field("MATCH_VAR"), c.Args["MATCH_VALUE"], c.Args["MATCH_VALUE"])
			s := state(c)
			if token := s.first[key]; token != "" {
				ids := resolve(c, token, ts)
				if len(ids) == 1 && ids[0] == matches[0] {
					return token, nil
				}
			}
			token, err := store(c, matches, ts)
			if err == nil {
				s.first[key] = token
			}
			return token, err
		}
		return store(c, matches, ts)
	}
	if op == "intersectTargetSets" {
		left := resolve(c, c.String("TARGETS_A"), ts)
		right := resolve(c, c.String("TARGETS_B"), ts)
		other := map[string]bool{}
		for _, id := range right {
			other[id] = true
		}
		out := []string{}
		for _, id := range left {
			if other[id] {
				out = append(out, id)
			}
		}
		return store(c, out, ts)
	}
	ids := resolve(c, c.String("TARGETS"), ts)
	switch op {
	case "targetSetCount":
		return float64(len(ids)), nil
	case "targetSetHasAny":
		return len(ids) > 0, nil
	case "deleteTargetSet":
		for i := len(ids) - 1; i >= 0; i-- {
			if _, err := c.Call("target.delete", ids[i]); err != nil {
				return nil, err
			}
		}
		return nil, nil
	case "setVariableOfTargetSet":
		for _, target := range ids {
			id, err := localID(c, "variable", c.Field("VALUE_VAR"), target)
			if err != nil {
				return nil, err
			}
			if id != "" {
				if _, err := c.Call("variable.set", id, c.Args["VALUE"], target); err != nil {
					return nil, err
				}
			}
		}
		return nil, nil
	case "replaceInListOfTargetSet":
		for _, target := range ids {
			v, id, err := localValue(c, "list", c.Field("VALUE_LIST"), target)
			if err != nil {
				return nil, err
			}
			if id != "" {
				items := v.([]any)
				i := index(c.Args["INDEX"], len(items))
				if i >= 0 {
					if _, err := c.Call("list.replace", id, float64(i+1), c.Args["ITEM"], target); err != nil {
						return nil, err
					}
				}
			}
		}
		return nil, nil
	}
	if len(ids) == 0 {
		if op == "getLengthOfListOfFirstInTargetSet" {
			return float64(0), nil
		}
		return "", nil
	}
	target := ids[0]
	switch op {
	case "getVariableOfFirstInTargetSet":
		v, _, err := localValue(c, "variable", c.Field("VALUE_VAR"), target)
		if v == nil {
			v = ""
		}
		return v, err
	case "getItemOfListOfFirstInTargetSet", "getLengthOfListOfFirstInTargetSet":
		v, id, err := localValue(c, "list", c.Field("VALUE_LIST"), target)
		if err != nil {
			return nil, err
		}
		items := []any{}
		if id != "" {
			items = v.([]any)
		}
		if op == "getLengthOfListOfFirstInTargetSet" {
			return float64(len(items)), nil
		}
		i := index(c.Args["INDEX"], len(items))
		if i < 0 || items[i] == nil {
			return "", nil
		}
		return items[i], nil
	case "getThingOfFirstInTargetSet":
		thing := c.Field("THING")
		property := map[string]string{"x position": "x", "y position": "y", "direction": "direction", "size": "size", "volume": "volume", "costume num": "currentCostume", "costume name": "currentCostume"}[thing]
		if property == "" {
			return "", nil
		}
		v, err := c.Call("target.get", property, target)
		if err != nil {
			return nil, err
		}
		if thing == "costume num" {
			return e.Number(v) + 1, nil
		}
		if thing == "costume name" {
			costumes, err := c.Call("target.costumes", target)
			if err != nil {
				return nil, err
			}
			items := costumes.([]any)
			i := int(e.Number(v))
			if i < 0 || i >= len(items) {
				return "", nil
			}
			return items[i].(map[string]any)["name"], nil
		}
		return v, nil
	}
	return nil, fmt.Errorf("unsupported opcode %s", op)
}
