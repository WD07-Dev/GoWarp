// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"encoding/json"
	"fmt"
	e "gowarp/core/extension"
	"sort"
)

func New() e.Extension {
	return e.Extension{ID: "SPmbpCST", Prepare: prepare, State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "SPmbpCST_setParam", Kind: "command", Inputs: []string{"VALUE"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("procedures.setParam", c.Field("PARAM"), c.Args["VALUE"])
			return e.Result{}, err
		}},
		{Opcode: "SPmbpCST_$-,.___SPmbpCST_TEMP_BLOCK", Kind: "command", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{}, nil
		}},
	}}
}

func prepare(p *e.Project) error {
	type global struct {
		owner      int
		definition string
		code       string
	}
	var globals []global
	seen := map[string]bool{}
	for i, src := range p.Targets {
		declarations := map[string]bool{}
		var storage map[string]struct {
			Global bool `json:"global"`
		}
		if raw := src.ExtensionStorage["SPmbpCST"]; len(raw) != 0 {
			if err := json.Unmarshal(raw, &storage); err != nil {
				return fmt.Errorf("target %s global procedures: %w", src.Name, err)
			}
			for code, value := range storage {
				if value.Global {
					declarations[code] = true
				}
			}
		}
		var ids []string
		for id := range src.Blocks {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			b := src.Blocks[id]
			if b == nil || b.Opcode != "procedures_definition" {
				continue
			}
			input := b.Inputs["custom_block"]
			if len(input) < 2 {
				continue
			}
			prototype, _ := input[1].(string)
			proto := src.Blocks[prototype]
			if proto == nil {
				continue
			}
			var code string
			if json.Unmarshal(proto.Mutation["proccode"], &code) != nil || !declarations[code] {
				continue
			}
			if seen[code] {
				return fmt.Errorf("duplicate global procedure %q", code)
			}
			seen[code] = true
			globals = append(globals, global{i, id, code})
		}
	}
	for destination, target := range p.Targets {
		local := map[string]bool{}
		needed := map[string]bool{}
		for _, b := range target.Blocks {
			if b != nil && b.Opcode == "procedures_call" {
				var code string
				_ = json.Unmarshal(b.Mutation["proccode"], &code)
				needed[code] = true
			}
			if b != nil && b.Opcode == "procedures_prototype" {
				var code string
				_ = json.Unmarshal(b.Mutation["proccode"], &code)
				local[code] = true
			}
		}
		copied := make(map[string]bool)
		for changed := true; changed; {
			changed = false
			for _, g := range globals {
				if g.owner == destination || local[g.code] || !needed[g.code] {
					continue
				}
				source := p.Targets[g.owner]
				prefix := fmt.Sprintf("\x00SPmbpCST:%d:", g.owner)
				var copyBlock func(string) (string, error)
				copyBlock = func(id string) (string, error) {
					if id == "" {
						return "", nil
					}
					key := prefix + id
					if copied[key] {
						return key, nil
					}
					if _, exists := target.Blocks[key]; exists {
						return "", fmt.Errorf("global procedure block collision %q", key)
					}
					b := source.Blocks[id]
					if b == nil {
						return "", fmt.Errorf("global procedure %q references missing block %q", g.code, id)
					}
					encoded, err := json.Marshal(b)
					if err != nil {
						return "", err
					}
					var cloned e.ProjectBlock
					if err = json.Unmarshal(encoded, &cloned); err != nil {
						return "", err
					}
					if cloned.Opcode == "procedures_call" {
						var code string
						_ = json.Unmarshal(cloned.Mutation["proccode"], &code)
						needed[code] = true
					}
					target.Blocks[key] = &cloned
					copied[key] = true
					cloned.Next, err = copyBlock(b.Next)
					if err != nil {
						return "", err
					}
					for _, input := range cloned.Inputs {
						for index := 1; index < len(input); index++ {
							if reference, ok := input[index].(string); ok && reference != "" {
								input[index], err = copyBlock(reference)
								if err != nil {
									return "", err
								}
							}
						}
					}
					return key, nil
				}
				if _, err := copyBlock(g.definition); err != nil {
					return err
				}
				local[g.code] = true
				changed = true
			}
		}
	}
	return nil
}
