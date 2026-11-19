package extension

import (
	"bytes"
	"encoding/json"
	e "gowarp/core/extension"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

/*
Port of the JSON operations used by Dusttrust. Object property order and
string-valued numbers are retained, as in the original JavaScript extension.
*/
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
	return strings.TrimSuffix(b.String(), "\n")
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

/*
Only read operations share parsed values. Setters and list transfers still
parse their own mutable copy. Bound both entry count and source text size.
*/
type readEntry struct {
	value       any
	results     map[string]any
	resultBytes int
}

type readCache struct {
	entries map[string]*readEntry
	order   []string
	bytes   int
}

func (c *readCache) parse(source string) (any, bool) {
	if entry, ok := c.entries[source]; ok {
		return entry.value, true
	}
	value, valid := parse(source)
	if !valid || len(source) > 4096 {
		return value, valid
	}
	if c.entries == nil {
		c.entries = make(map[string]*readEntry)
	}
	for len(c.order) >= 32 || c.bytes+len(source) > 64<<10 {
		oldest := c.order[0]
		delete(c.entries, oldest)
		c.bytes -= len(oldest)
		copy(c.order, c.order[1:])
		c.order = c.order[:len(c.order)-1]
	}
	// A short substring must not pin a much larger caller-owned string.
	source = strings.Clone(source)
	c.entries[source] = &readEntry{value: value}
	c.order = append(c.order, source)
	c.bytes += len(source)
	return value, true
}

/*
Cached parse trees are read-only. Keep serialized children with their
source entry, bounded by the source length and released on its eviction.
*/
func (c *readCache) result(source, key string, value any) any {
	switch value.(type) {
	case *object, []any:
	default:
		return resultValue(value)
	}
	entry := c.entries[source]
	if entry != nil {
		if result, ok := entry.results[key]; ok {
			return result
		}
	}
	result := resultValue(value)
	if entry != nil {
		size := len(key) + len(result.(string))
		if len(entry.results) < 64 && size <= len(source)-entry.resultBytes {
			if entry.results == nil {
				entry.results = make(map[string]any)
			}
			entry.results[strings.Clone(key)] = result
			entry.resultBytes += size
		}
	}
	return result
}

func itemValue(v any) any {
	if s, ok := v.(string); ok && len(s) >= 2 && ((s[0] == '[' && s[len(s)-1] == ']') || (s[0] == '{' && s[len(s)-1] == '}')) {
		if p, valid := parse(s); valid {
			v = p
		}
	}
	if v == nil {
		return ""
	}
	if f, ok := v.(float64); ok && (math.IsNaN(f) || math.IsInf(f, 0)) {
		return e.String(f)
	}
	return v
}
func resultValue(v any) any {
	if v == nil {
		return ""
	}
	switch v.(type) {
	case *object, []any:
		return encode(v)
	}
	return v
}
func get(v any, key string) any {
	switch x := v.(type) {
	case *object:
		for _, p := range *x {
			if p.key == key {
				return p.value
			}
		}
	case []any:
		if key == "length" {
			return float64(len(x))
		}
		if i, ok := index(key); ok && i < len(x) {
			return x[i]
		}
	case string:
		u := utf16.Encode([]rune(x))
		if key == "length" {
			return float64(len(u))
		}
		if i, ok := index(key); ok && i < len(u) {
			return string(utf16.Decode(u[i : i+1]))
		}
	}
	return nil
}
func strictEqual(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case float64:
		switch b.(type) {
		case float64, float32, int, int64:
			return x == e.Number(b)
		}
	}
	return false
}
func set(v any, key string, value any) any {
	switch x := v.(type) {
	case *object:
		x.set(key, value)
		return x
	case []any:
		if i, ok := index(key); ok {
			if i >= len(x) {
				x = append(x, make([]any, i+1-len(x))...)
			}
			x[i] = value
		}
		return x
	}
	return nil
}

func New() e.Extension {
	x := e.Extension{ID: "skyhigh173JSON"}
	var reads readCache
	add := func(op, kind string, inputs []string, h e.Handler) {
		x.Blocks = append(x.Blocks, e.Block{Opcode: "skyhigh173JSON_" + op, Kind: kind, Inputs: inputs, Handler: h})
	}
	for _, name := range []string{"types", "get_list"} {
		name := name
		add("menu_"+name, "reporter", nil, func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field(name)}, nil })
	}
	add("json_new", "reporter", []string{"json"}, func(c *e.Context) (e.Result, error) {
		v := ""
		switch c.String("json") {
		case "Object":
			v = "{}"
		case "Array":
			v = "[]"
		}
		return e.Result{Value: v}, nil
	})
	add("json_array_from", "reporter", []string{"json"}, func(c *e.Context) (e.Result, error) {
		a := []string{}
		for _, r := range c.String("json") {
			a = append(a, string(r))
		}
		return e.Result{Value: encode(a)}, nil
	})
	for _, spec := range []struct {
		op     string
		inputs []string
	}{
		{"json_get", []string{"json", "item"}}, {"json_set", []string{"json", "item", "value"}},
		{"json_array_get", []string{"json", "item"}}, {"json_array_push", []string{"json", "item"}},
		{"json_array_set", []string{"json", "pos", "item"}}, {"json_array_itemH", []string{"json", "item"}},
		{"json_has_value", []string{"json", "value"}}, {"json_length", []string{"json"}},
	} {
		op := spec.op
		add(op, "reporter", spec.inputs, func(c *e.Context) (e.Result, error) {
			var v any
			var valid bool
			switch op {
			case "json_get", "json_array_get", "json_array_itemH", "json_has_value", "json_length":
				v, valid = reads.parse(c.String("json"))
			default:
				v, valid = parse(c.String("json"))
			}
			out := any("")
			if op == "json_has_value" {
				out = false
			}
			if op == "json_length" {
				out = " "
			}
			if !valid {
				return e.Result{Value: out}, nil
			}
			a, isArray := v.([]any)
			switch op {
			case "json_get":
				out = reads.result(c.String("json"), c.String("item"), get(v, c.String("item")))
			case "json_set":
				if value := set(v, c.String("item"), itemValue(c.Args["value"])); value != nil {
					out = encode(value)
				}
			case "json_length":
				switch v := v.(type) {
				case *object:
					out = float64(len(*v))
				case []any:
					out = float64(len(v))
				case string:
					out = float64(len(utf16.Encode([]rune(v))))
				case float64, bool:
					out = 0.0
				}
			case "json_array_get":
				n := c.Number("item")
				if n != 0 {
					if n > 0 {
						n--
					} else {
						n += e.Number(get(v, "length"))
					}
					key := e.String(n)
					out = reads.result(c.String("json"), key, get(v, key))
				}
			case "json_array_push":
				if isArray {
					out = encode(append(a, itemValue(c.Args["item"])))
				}
			case "json_array_set":
				if value := set(v, e.String(c.Number("pos")-1), itemValue(c.Args["item"])); value != nil {
					out = encode(value)
				}
			case "json_array_itemH":
				if isArray {
					n := 0
					for i, val := range a {
						if strictEqual(val, itemValue(c.Args["item"])) {
							n = i + 1
							break
						}
					}
					out = strconv.Itoa(n)
				} else if s, ok := v.(string); ok {
					needle := e.String(itemValue(c.Args["item"]))
					offset := strings.Index(s, needle)
					n := 0
					if offset >= 0 {
						n = len(utf16.Encode([]rune(s[:offset]))) + 1
					}
					out = strconv.Itoa(n)
				}
			case "json_has_value":
				value := itemValue(c.Args["value"])
				if isArray {
					for _, val := range a {
						if strictEqual(val, value) {
							out = true
							break
						}
					}
				} else if s, ok := v.(string); ok {
					out = strings.Contains(s, e.String(value))
				}
			}
			return e.Result{Value: out}, nil
		})
	}
	add("json_vm_getlist", "reporter", []string{"list"}, func(c *e.Context) (e.Result, error) {
		v, err := c.Call("list.get", c.String("list"))
		if err != nil {
			return e.Result{Value: ""}, nil
		}
		return e.Result{Value: encode(v)}, nil
	})
	add("json_vm_setlist", "command", []string{"list", "json"}, func(c *e.Context) (e.Result, error) {
		v, valid := parse(c.String("json"))
		a, ok := v.([]any)
		if valid && ok {
			for i, v := range a {
				switch v.(type) {
				case nil, *object, []any:
					a[i] = encode(v)
				}
			}
			_, _ = c.Call("list.set", c.String("list"), a)
		}
		return e.Result{Value: ""}, nil
	})
	return x
}
