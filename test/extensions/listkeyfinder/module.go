// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"bytes"
	"encoding/json"
	e "gowarp/core/extension"
	"sort"
	"strconv"
	"strings"
)

type property struct {
	key   string
	value any
}
type object []property

func (o *object) set(key string, value any) {
	for i := range *o {
		if (*o)[i].key == key {
			(*o)[i].value = value
			return
		}
	}
	*o = append(*o, property{key, value})
}
func index(key string) (int, bool) {
	n, err := strconv.ParseUint(key, 10, 32)
	return int(n), err == nil && n < 4294967295 && strconv.FormatUint(n, 10) == key
}
func (o *object) MarshalJSON() ([]byte, error) {
	props := append(object(nil), (*o)...)
	sort.SliceStable(props, func(i, j int) bool {
		a, ai := index(props[i].key)
		b, bi := index(props[j].key)
		return ai && (!bi || a < b)
	})
	var b strings.Builder
	b.WriteByte('{')
	for i, p := range props {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(encode(p.key))
		b.WriteByte(':')
		b.WriteString(encode(p.value))
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}
func encode(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) != nil {
		return ""
	}
	// JSON.stringify leaves line separators literal; preserve escaped backslashes.
	text := strings.TrimSuffix(b.String(), "\n")
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) {
			if strings.HasPrefix(text[i:], `\u2028`) {
				out.WriteRune('\u2028')
				i += 5
				continue
			}
			if strings.HasPrefix(text[i:], `\u2029`) {
				out.WriteRune('\u2029')
				i += 5
				continue
			}
			out.WriteByte(text[i])
			i++
		}
		out.WriteByte(text[i])
	}
	return out.String()
}
func readValue(d *json.Decoder) (any, error) {
	tok, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch tok {
	case json.Delim('{'):
		o := object{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			v, err := readValue(d)
			if err != nil {
				return nil, err
			}
			o.set(key.(string), v)
		}
		_, err = d.Token()
		return &o, err
	case json.Delim('['):
		a := []any{}
		for d.More() {
			v, err := readValue(d)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		_, err = d.Token()
		return a, err
	}
	return tok, nil
}
func parse(s string) (any, bool) {
	if !json.Valid([]byte(s)) {
		return nil, false
	}
	v, err := readValue(json.NewDecoder(strings.NewReader(s)))
	return v, err == nil
}

func listID(c *e.Context, token string) (string, string, error) {
	current, err := c.Call("list.list")
	if err != nil {
		return "", "", err
	}
	stage, err := c.Call("list.list", "_stage_")
	if err != nil {
		return "", "", err
	}
	for _, key := range []string{"id", "name"} {
		for i, records := range []any{current, stage} {
			for _, record := range records.([]any) {
				m := record.(map[string]any)
				if e.String(m[key]) == token {
					ref := "_myself_"
					if i == 1 {
						ref = "_stage_"
					}
					return e.String(m["id"]), ref, nil
				}
			}
		}
	}
	return "", "", nil
}
func comparable(v any) any {
	if v == nil {
		return ""
	}
	switch v.(type) {
	case *object, map[string]any, []any:
		return encode(v)
	}
	return v
}
func lookup(v any, key string) (any, bool) {
	if m, ok := v.(map[string]any); ok {
		v, ok := m[key]
		return v, ok
	}
	if _, ok := v.(*object); !ok {
		parsed, valid := parse(e.String(v))
		if !valid {
			return nil, false
		}
		v = parsed
	}
	if o, ok := v.(*object); ok {
		for _, p := range *o {
			if p.key == key {
				return p.value, true
			}
		}
	}
	return nil, false
}
func New() e.Extension {
	return e.Extension{ID: "listkeyfinder", Blocks: []e.Block{
		{Opcode: "listkeyfinder_menu_lists", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field("lists")}, nil }},
		{Opcode: "listkeyfinder_findItemNumber", Kind: "reporter", Inputs: []string{"LIST", "KEY", "VALUE"}, Handler: func(c *e.Context) (e.Result, error) {
			id, ref, err := listID(c, c.String("LIST"))
			if err != nil || id == "" {
				return e.Result{Value: float64(0)}, err
			}
			value, err := c.Call("list.get", id, ref)
			if err != nil {
				return e.Result{}, err
			}
			wanted := comparable(c.Args["VALUE"])
			for i, item := range value.([]any) {
				actual, ok := lookup(item, c.String("KEY"))
				if ok && e.Compare(comparable(actual), wanted) == 0 {
					return e.Result{Value: float64(i + 1)}, nil
				}
			}
			return e.Result{Value: float64(0)}, nil
		}},
	}}
}
