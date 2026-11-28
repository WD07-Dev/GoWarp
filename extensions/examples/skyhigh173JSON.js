/*
@gowarp-config {
}

@gowarp-go {
package extension

import (
	"bytes"
	"encoding/json"
	"fmt"
	e "gowarp/core/extension"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

// Port of the JSON operations used by Dusttrust. Object property order and
// string-valued numbers are retained, as in the original JavaScript extension.
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

// Only read operations share parsed values. Setters and list transfers still
// parse their own mutable copy. Bound both entry count and source text size.
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

// Cached parse trees are read-only. Keep serialized children with their
// source entry, bounded by the source length and released on its eviction.
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

func concatText(value any) string {
	switch value := value.(type) {
	case *object:
		return "[object Object]"
	case []any:
		parts := make([]string, len(value))
		for i, v := range value {
			if v != nil {
				parts[i] = concatText(v)
			}
		}
		return strings.Join(parts, ",")
	default:
		return e.String(value)
	}
}

func New() e.Extension {
	x := e.Extension{ID: "skyhigh173JSON"}
	var reads readCache
	labels := map[string]string{
		"skyhigh173JSON_json_is_valid":         "is JSON [json] valid?",
		"skyhigh173JSON_json_is":               "is [json] [types]?",
		"skyhigh173JSON_json_get_all":          "all [Stype] of [json]",
		"skyhigh173JSON_json_new":              "new [json]",
		"skyhigh173JSON_json_has_key":          "[json] contains key [key]?",
		"skyhigh173JSON_json_has_value":        "[json] contains value [value]?",
		"skyhigh173JSON_json_equal":            "[json1] [equal] [json2]",
		"skyhigh173JSON_json_jlength":          "length of json [json]",
		"skyhigh173JSON_json_get":              "value of [item] in [json]",
		"skyhigh173JSON_json_set":              "set [item] in [json] to [value]",
		"skyhigh173JSON_json_delete":           "delete [item] in [json]",
		"skyhigh173JSON_json_length":           "length of array [json]",
		"skyhigh173JSON_json_array_get":        "item [item] of array [json]",
		"skyhigh173JSON_json_array_push":       "add [item] to array [json]",
		"skyhigh173JSON_json_array_set":        "replace item [pos] of [json] with [item]",
		"skyhigh173JSON_json_array_insert":     "insert [item] at [pos] of array [json]",
		"skyhigh173JSON_json_array_delete":     "delete item [item] of array [json]",
		"skyhigh173JSON_json_array_remove_all": "delete all [item] in array [json]",
		"skyhigh173JSON_json_array_itemH":      "item # of [item] in array [json]",
		"skyhigh173JSON_json_array_from":       "array from text [json]",
		"skyhigh173JSON_json_array_fromto":     "items [item] to [item2] of array [json]",
		"skyhigh173JSON_json_array_reverse":    "reverse array [json]",
		"skyhigh173JSON_json_array_flat":       "flat array [json] by depth [depth]",
		"skyhigh173JSON_json_array_concat":     "array concat [json] [json2]",
		"skyhigh173JSON_json_array_filter":     "get all values with key [key] in array [json]",
		"skyhigh173JSON_json_array_setlen":     "set length of array [json] to [len]",
		"skyhigh173JSON_json_array_create":     "create array by [text] with delimiter [d]",
		"skyhigh173JSON_json_array_join":       "join string by array [json] with delimiter [d]",
		"skyhigh173JSON_json_array_sort":       "sort array [list] in [order] order",
		"skyhigh173JSON_json_array_analysis":   "[analysis] of array [list]",
		"skyhigh173JSON_json_vm_getlist":       "get list [list] as array",
		"skyhigh173JSON_json_vm_setlist":       "set list [list] to [json]",
	}
	add := func(op, kind string, inputs []string, h e.Handler) {
		x.Blocks = append(x.Blocks, e.Block{Text: labels["skyhigh173JSON_"+op], Opcode: "skyhigh173JSON_" + op, Kind: kind, Inputs: inputs, Handler: h})
	}

	add("json_is_valid", "boolean", []string{"json"}, func(c *e.Context) (e.Result, error) {
		text, ok := c.Args["json"].(string)
		if !ok {
			return e.Result{Value: false}, nil
		}
		text = strings.TrimFunc(text, func(r rune) bool { return r == '\ufeff' || r != '\u0085' && unicode.IsSpace(r) })
		valid := len(text) > 0 && (text[0] == '[' || text[0] == '{') && json.Valid([]byte(text))
		return e.Result{Value: valid}, nil
	})
	add("json_is", "boolean", []string{"json", "types"}, func(c *e.Context) (e.Result, error) {
		text, ok := c.Args["json"].(string)
		if !ok {
			return e.Result{Value: false}, nil
		}
		if !json.Valid([]byte(text)) {
			return e.Result{Value: false}, nil
		}
		text = strings.TrimLeft(text, " \t\r\n")
		isType := len(text) > 0 && (text[0] == '[' && c.String("types") == "Array" || text[0] == '{' && c.String("types") == "Object")
		return e.Result{Value: isType}, nil
	})
	add("json_array_concat", "reporter", []string{"json", "json2"}, func(c *e.Context) (e.Result, error) {
		first, valid := parse(c.String("json"))
		second, valid2 := parse(c.String("json2"))
		if !valid || !valid2 {
			return e.Result{Value: ""}, nil
		}
		if array, ok := first.([]any); ok {
			if more, ok := second.([]any); ok {
				return e.Result{Value: encode(append(array, more...))}, nil
			}
			return e.Result{Value: encode(append(array, second))}, nil
		}
		if text, ok := first.(string); ok {
			return e.Result{Value: encode(text + concatText(second))}, nil
		}
		return e.Result{Value: ""}, nil
	})
	for _, name := range []string{"types", "get_list", "get_all", "equal", "sort_order", "analysis"} {
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
		{"json_has_value", []string{"json", "value"}}, {"json_length", []string{"json"}}, {"json_jlength", []string{"json"}},
	} {
		op := spec.op
		kind := "reporter"
		if op == "json_has_value" {
			kind = "boolean"
		}
		add(op, kind, spec.inputs, func(c *e.Context) (e.Result, error) {
			var v any
			var valid bool
			switch op {
			case "json_get", "json_array_get", "json_array_itemH", "json_has_value", "json_length", "json_jlength":
				v, valid = reads.parse(c.String("json"))
			default:
				v, valid = parse(c.String("json"))
			}
			out := any("")
			if op == "json_has_value" {
				out = false
			}
			if op == "json_length" || op == "json_jlength" {
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
			case "json_length", "json_jlength":
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
	addJSONOperations(add)
	return x
}

func ownKeys(v any) ([]string, bool) {
	keys := []string{}
	switch x := v.(type) {
	case nil:
		return nil, false
	case *object:
		props := append(object(nil), (*x)...)
		sort.SliceStable(props, func(i, j int) bool {
			a, ai := index(props[i].key)
			b, bi := index(props[j].key)
			return ai && (!bi || a < b)
		})
		for _, p := range props {
			keys = append(keys, p.key)
		}
	case []any:
		for i := range x {
			keys = append(keys, strconv.Itoa(i))
		}
	case string:
		for i := range utf16.Encode([]rune(x)) {
			keys = append(keys, strconv.Itoa(i))
		}
	}
	return keys, true
}
func owns(v any, key string) bool {
	if key == "length" {
		switch v.(type) {
		case []any, string:
			return true
		}
	}
	keys, _ := ownKeys(v)
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}
func sliceIndex(n float64, length int) int {
	if math.IsNaN(n) {
		return 0
	}
	n = math.Trunc(n)
	if n < 0 {
		n += float64(length)
	}
	return int(math.Max(0, math.Min(float64(length), n)))
}
func flatten(a []any, depth float64) []any {
	out := []any{}
	for _, v := range a {
		if nested, ok := v.([]any); ok && depth >= 1 {
			out = append(out, flatten(nested, depth-1)...)
		} else {
			out = append(out, v)
		}
	}
	return out
}
func addJSONOperations(add func(string, string, []string, e.Handler)) {
	for _, spec := range []struct {
		op, kind string
		inputs   []string
	}{
		{"json_get_all", "reporter", []string{"Stype", "json"}},
		{"json_has_key", "boolean", []string{"json", "key"}},
		{"json_equal", "boolean", []string{"json1", "equal", "json2"}},
		{"json_delete", "reporter", []string{"item", "json"}},
		{"json_array_insert", "reporter", []string{"item", "pos", "json"}},
		{"json_array_delete", "reporter", []string{"item", "json"}},
		{"json_array_remove_all", "reporter", []string{"item", "json"}},
		{"json_array_fromto", "reporter", []string{"json", "item", "item2"}},
		{"json_array_reverse", "reporter", []string{"json"}},
		{"json_array_flat", "reporter", []string{"json", "depth"}},
		{"json_array_create", "reporter", []string{"text", "d"}},
		{"json_array_join", "reporter", []string{"json", "d"}},
		{"json_array_filter", "reporter", []string{"key", "json"}},
		{"json_array_setlen", "reporter", []string{"json", "len"}},
		{"json_array_sort", "reporter", []string{"list", "order"}},
		{"json_array_analysis", "reporter", []string{"list", "analysis"}},
	} {
		op := spec.op
		add(op, spec.kind, spec.inputs, func(c *e.Context) (e.Result, error) {
			out := any("")
			if op == "json_has_key" || op == "json_equal" {
				out = false
			}
			if op == "json_array_analysis" {
				out = float64(0)
			}
			done := func() (e.Result, error) { return e.Result{Value: out}, nil }
			if op == "json_array_create" {
				out = encode(strings.Split(c.String("text"), c.String("d")))
				return done()
			}
			source := c.String("json")
			if op == "json_array_sort" || op == "json_array_analysis" {
				source = c.String("list")
			}
			if op == "json_equal" {
				source = c.String("json1")
			}
			v, valid := parse(source)
			if !valid {
				return done()
			}
			a, isArray := v.([]any)
			switch op {
			case "json_get_all":
				keys, ok := ownKeys(v)
				if !ok {
					return done()
				}
				values := []any{}
				mode := c.Field("Stype")
				if mode == "" {
					mode = c.String("Stype")
				}
				for _, key := range keys {
					value := get(v, key)
					if value == nil {
						value = ""
					}
					switch mode {
					case "keys":
						values = append(values, key)
					case "values":
						values = append(values, value)
					case "datas":
						values = append(values, []any{key, value})
					default:
						return done()
					}
				}
				if mode == "keys" || mode == "values" || mode == "datas" {
					out = encode(values)
				}
			case "json_has_key":
				switch v.(type) {
				case *object, []any:
					key := concatText(itemValue(c.Args["key"]))
					out = owns(v, key)
					if key == "constructor" || key == "toString" || key == "valueOf" || key == "hasOwnProperty" || key == "__proto__" {
						out = true
					}
				}
			case "json_equal":
				other, ok := parse(c.String("json2"))
				keys, ok1 := ownKeys(v)
				keys2, ok2 := ownKeys(other)
				if !ok || !ok1 || !ok2 {
					return done()
				}
				equal := len(keys) == len(keys2)
				for _, k := range keys {
					if !owns(other, k) || !strictEqual(get(v, k), get(other, k)) {
						equal = false
						break
					}
				}
				if c.String("equal") == "=" {
					out = equal
				}
				if c.String("equal") == "≠" {
					out = !equal
				}
			case "json_delete":
				key := c.String("item")
				switch x := v.(type) {
				case *object:
					for i, p := range *x {
						if p.key == key {
							*x = append((*x)[:i], (*x)[i+1:]...)
							break
						}
					}
				case []any:
					if i, ok := index(key); ok && i < len(x) {
						x[i] = nil
					}
				case nil:
					return done()
				case string:
					if owns(x, key) {
						return done()
					}
				}
				out = encode(v)
			case "json_array_fromto":
				if s, ok := v.(string); ok {
					chars := utf16.Encode([]rune(s))
					from, to := sliceIndex(c.Number("item")-1, len(chars)), sliceIndex(c.Number("item2"), len(chars))
					if to < from {
						to = from
					}
					out = encode(string(utf16.Decode(chars[from:to])))
					return done()
				}
				if !isArray {
					return done()
				}
				from, to := sliceIndex(c.Number("item")-1, len(a)), sliceIndex(c.Number("item2"), len(a))
				if to < from {
					to = from
				}
				out = encode(a[from:to])
			case "json_array_setlen":
				if obj, ok := v.(*object); ok {
					obj.set("length", c.Args["len"])
					out = encode(obj)
					return done()
				}
				if !isArray {
					return done()
				}
				n := c.Number("len")
				if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n != math.Trunc(n) || n >= 4294967296 {
					return done()
				}
				if n <= float64(len(a)) {
					a = a[:int(n)]
				} else {
					// Unlike JavaScript's sparse arrays, []any allocates every entry.
					if n > 1_000_000 {
						return e.Result{}, fmt.Errorf("JSON array growth exceeds 1,000,000 entries")
					}
					a = append(a, make([]any, int(n)-len(a))...)
				}
				out = encode(a)
			default:
				if !isArray {
					return done()
				}
				switch op {
				case "json_array_insert":
					i := sliceIndex(c.Number("pos")-1, len(a))
					a = append(a, nil)
					copy(a[i+1:], a[i:])
					a[i] = itemValue(c.Args["item"])
				case "json_array_delete":
					i := sliceIndex(c.Number("item")-1, len(a))
					if i < len(a) {
						a = append(a[:i], a[i+1:]...)
					}
				case "json_array_remove_all":
					value := itemValue(c.Args["item"])
					outArray := []any{}
					for _, v := range a {
						if !strictEqual(v, value) {
							outArray = append(outArray, v)
						}
					}
					a = outArray
				case "json_array_reverse":
					for i, j := 0, len(a)-1; i < j; i, j = i+1, j-1 {
						a[i], a[j] = a[j], a[i]
					}
				case "json_array_flat":
					a = flatten(a, math.Trunc(c.Number("depth")))
				case "json_array_join":
					parts := make([]string, len(a))
					for i, v := range a {
						if v != nil {
							parts[i] = concatText(v)
						}
					}
					out = strings.Join(parts, c.String("d"))
					return done()
				case "json_array_filter":
					for i, v := range a {
						if v == nil {
							return done()
						}
						a[i] = get(v, c.String("key"))
					}
				case "json_array_sort":
					sort.SliceStable(a, func(i, j int) bool { return e.Compare(itemCompare(a[i]), itemCompare(a[j])) < 0 })
					if c.String("order") == "descending" {
						for i, j := 0, len(a)-1; i < j; i, j = i+1, j-1 {
							a[i], a[j] = a[j], a[i]
						}
					}
				case "json_array_analysis":
					nums := make([]float64, len(a))
					sum := 0.0
					for i, v := range a {
						nums[i] = e.Number(itemCompare(v))
						sum += nums[i]
					}
					n := float64(len(nums))
					average := sum / n
					switch c.String("analysis") {
					case "sum":
						out = sum
					case "average":
						out = average
					case "maximum":
						out = math.Inf(-1)
						for _, v := range nums {
							out = math.Max(out.(float64), v)
						}
					case "minimum":
						out = math.Inf(1)
						for _, v := range nums {
							out = math.Min(out.(float64), v)
						}
					case "median":
						sort.Float64s(nums)
						if len(nums) == 0 {
							out = math.NaN()
						} else if len(nums)%2 == 0 {
							out = (nums[len(nums)/2-1] + nums[len(nums)/2]) / 2
						} else {
							out = nums[len(nums)/2]
						}
					case "mode":
						freq := map[float64]int{}
						max := 0
						for _, v := range nums {
							freq[v]++
							if freq[v] > max {
								out = v
								max = freq[v]
							}
						}
					case "variance":
						total := 0.0
						for _, v := range nums {
							total += (v - average) * (v - average)
						}
						out = total / n
					}
					return done()
				}
				out = encode(a)
			}
			return done()
		})
	}
}
func itemCompare(v any) any {
	switch v.(type) {
	case *object, []any:
		return concatText(v)
	}
	return v
}
}
*/
// Name: JSON
// ID: skyhigh173JSON
// Description: Handle JSON strings and arrays.
// By: Skyhigh173 <https://scratch.mit.edu/users/Skyhigh173/>
// By: Mio <https://scratch.mit.edu/users/0znzw/>
// License: MIT

/* generated l10n code */Scratch.translate.setup({"ca":{"_Advanced":"Avançat"},"cs":{"_Advanced":"Pokročilé"},"de":{"_Advanced":"Erweitert"},"es":{"_Advanced":"Avanzado"},"fi":{"_Advanced":"Edistykselliset","_Array":"Taulukko","_General Utils":"Yleiset työkalut","_Lists":"Listat","_Object":"Olio","_[analysis] of array [list]":"taulukon [list] [analysis]","_[json] contains key [key]?":"sisältääkö [json] avaimen [key]?","_[json] contains value [value]?":"sisältääkö [json] arvon [value]?","_add [item] to array [json]":"lisää [item] taulukkoon [json]","_all [Stype] of [json]":"kaikki JSO-koodin [json] [Stype] ","_array concat [json] [json2]":"yhdistä taulukot [json] ja [json2]","_array from text [json]":"taulukko tekstistä [json]","_ascending":"nousevaan","_average":"keskiarvo","_create array by [text] with delimiter [d]":"luo taulukko tekstistä [text] erottimella [d] ","_datas":"datat","_delete [item] in [json]":"poista [item] JSON-koodissa [json]","_delete all [item] in array [json]":"poista jokainen [item] taulukosta [json]","_delete item [item] of array [json]":"poista taulukon [json] [item]. kohde","_descending":"laskevaan","_flat array [json] by depth [depth]":"alenna taulukkoa [json] [depth] tasolla","_get all values with key [key] in array [json]":"kaikki taulukon [json] arvot avaimella [key]","_get list [list] as array":"lista [list] taulukkona","_insert [item] at [pos] of array [json]":"lisää [item] taulukon [json] [pos]. kohtaan","_is JSON [json] valid?":"onko JSON-koodi [json] kelvollinen?","_is [json] [types]?":"onko [json] [types]?","_item # of [item] in array [json]":"taulukon [json] kohteen [item] numero","_item [item] of array [json]":"taulukon [json] [item]. kohde","_items [item] to [item2] of array [json]":"taulukon [json] kohteet [item] – [item2]","_join string by array [json] with delimiter [d]":"tee taulukosta [json] merkkijono erottimella [d]","_keys":"avaimet","_length of array [json]":"taulukon [json] pituus","_length of json [json]":"JSON-koodin [json] pituus","_maximum value":"suurin arvo","_median":"mediaani","_minimum value":"pienin arvo","_mode":"moodi","_new [json]":"uusi [json]","_replace item [pos] of [json] with [item]":"korvaa taulukon [json] [pos]. kohde arvolla [item]","_reverse array [json]":"taulukko [json] käänteisenä","_select a list":"valitse lista","_set [item] in [json] to [value]":"aseta [item] JSON-koodissa [json] arvoon [value]","_set length of array [json] to [len]":"aseta taulukon [json] pituus arvoon [len]","_set list [list] to [json]":"aseta lista [list] arvoon [json]","_sort array [list] in [order] order":"järjestä taulukko [list] [order] järjestykseen","_sum":"summa","_value of [item] in [json]":"avaimen [item] arvo JSON-koodissa [json]","_values":"arvot","_variance":"varianssi"},"fr":{"_Advanced":"Avancé"},"he":{"_Advanced":"מתקדם"},"hu":{"_Advanced":"Haladó"},"it":{"_Advanced":"Impostazioni Avanzate","_General Utils":"Utilità Generali","_Lists":"Liste","_Object":"Oggetto","_[analysis] of array [list]":"[analysis] di array [list]","_[json] contains key [key]?":"[json] contiene chiave [key]","_[json] contains value [value]?":"[json] contiene valore [value]","_add [item] to array [json]":"aggiungi [item] a array [json]","_all [Stype] of [json]":"ogni [Stype] di [json]","_array concat [json] [json2]":"concatena array [json] e [json2]","_array from text [json]":"array da testo [json]","_ascending":"ascendente","_average":"media","_create array by [text] with delimiter [d]":"crea array da [text] delimitato da [d]","_datas":"dati","_delete [item] in [json]":"rimuovi [item] da [json]","_delete all [item] in array [json]":"rimuovi ogni [item] da array [json]","_delete item [item] of array [json]":"rimuovi elemento [item] da array [json]","_descending":"discendente","_flat array [json] by depth [depth]":"array [json] appiattito di [depth] ","_get all values with key [key] in array [json]":"tutti i valori con chiave [key] di array [json]","_get list [list] as array":"converti lista [list] in array","_insert [item] at [pos] of array [json]":"inserisci [item] alla posizione [pos] di array [json]","_is JSON [json] valid?":"JSON [json] valido","_is [json] [types]?":"[json] è un [types]","_item # of [item] in array [json]":"posizione di [item] di array [json]","_item [item] of array [json]":"elemento [item] di array [json]","_items [item] to [item2] of array [json]":"elementi da [item] a [item2] di array [json]","_join string by array [json] with delimiter [d]":"crea stringa da array [json] delimitato da [d]","_keys":"chiave","_length of array [json]":"lunghezza array [json]","_length of json [json]":"lunghezza di json [json]","_maximum value":"valore massimo","_median":"mediana","_minimum value":"valore minimo","_mode":"moda","_new [json]":"nuovo [json]","_replace item [pos] of [json] with [item]":"sostituisci elemento [pos] di [json] con [item]","_reverse array [json]":"inverti array [json]","_select a list":"seleziona una lista","_set [item] in [json] to [value]":"imposta [item]a [value] in [json]","_set length of array [json] to [len]":"imposta lunghezza di array [json] a [len]","_set list [list] to [json]":"porta contenuto lista [list] a json [json]","_sort array [list] in [order] order":"ordina array [list] in ordine [order]","_sum":"somma","_value of [item] in [json]":"valore di [item] in [json]","_values":"valore","_variance":"varianza"},"ja":{"_Advanced":"詳細設定","_General Utils":"汎用ユーティリティ","_Lists":"リスト","_Object":"オブジェクト","_[analysis] of array [list]":"array[list]の[analysis]","_[json1] [equal] [json2]":"[json1][equal][json2]","_[json] contains key [key]?":"[json]がキー[key]を含む","_[json] contains value [value]?":"[json]が値[value]を含む","_add [item] to array [json]":"[item]をArray[json]に追加する","_all [Stype] of [json]":"[json]のすべての[Stype]","_array concat [json] [json2]":"Array[json]と[json2]をつなぐ","_array from text [json]":"テキスト[json]からのArray","_ascending":"数が増えていく","_average":"平均","_create array by [text] with delimiter [d]":"Arrayを[text]から区切り文字[d]で作る","_datas":"データ","_delete [item] in [json]":"[json]の[item]を削除する","_delete all [item] in array [json]":"Array[json]の[item]をすべて削除する","_delete item [item] of array [json]":"Array[json]の[item]番目を削除する","_descending":"数が減少","_flat array [json] by depth [depth]":"Array[json]を深さ[depth]で平らにする","_get all values with key [key] in array [json]":"キー[key]のすべての値をArray[json]から取得する","_get list [list] as array":"リスト[list]のArray","_insert [item] at [pos] of array [json]":"Array[json]の[pos]番目に[item]を挿入する","_is JSON [json] valid?":"JSON[json]が有効","_is [json] [types]?":"[json]が[types]","_item # of [item] in array [json]":"Array[json]の[item]の場所","_item [item] of array [json]":"Array[json]の[item]番目","_items [item] to [item2] of array [json]":"Array[json]の[item]から[item2]番目","_join string by array [json] with delimiter [d]":"Array[json]を区切り文字[d]でつなげる","_keys":"キー","_length of array [json]":"Array[json]の長さ","_length of json [json]":"JSON[json]の長さ","_maximum value":"一番数が大きい値","_median":"中央値","_minimum value":"一番数が小さい値","_mode":"最頻値","_new [json]":"新しい[json]","_replace item [pos] of [json] with [item]":"[json]の[pos]番目を[item]で置き換える","_reverse array [json]":"Array[json]の順番を逆にする","_select a list":"リストを選択","_set [item] in [json] to [value]":"[json]の[item]を[value]にする","_set length of array [json] to [len]":"Array[json]の長さを[len]にする","_set list [list] to [json]":"リスト[list]を[json]にする","_sort array [list] in [order] order":"Array[list]を[order]ように並べる","_sum":"和","_value of [item] in [json]":"[json]の[item]の値","_values":"値","_variance":"分散"},"ja-hira":{"_Advanced":"こうどなせってい"},"ko":{"_Advanced":"고급 설정","_Array":"배열","_General Utils":"일반 유틸","_Lists":"리스트","_Object":"객체","_[json] contains key [key]?":"[json]이(가) [key] 키를 포함하는가?","_[json] contains value [value]?":"[json]이(가) [value] 값을 포함하는가?","_add [item] to array [json]":"배열 [json]에 [item] 추가하기","_all [Stype] of [json]":"[json]의 모든 [Stype]","_array concat [json] [json2]":"배열 합치기 [json] [json2]","_array from text [json]":"텍스트를 배열화 [json]","_ascending":"오름차순","_create array by [text] with delimiter [d]":"[text]에서 구분자 [d](으)로 배열 생성","_datas":"데이터","_delete [item] in [json]":"[json]의 [item]을(를) 삭제하기","_delete all [item] in array [json]":"배열 [json]의 [item]을(를) 모두 삭제하기","_delete item [item] of array [json]":"배열 [json]의 [item]번째를 삭제하기","_descending":"내림차순","_flat array [json] by depth [depth]":"배열을 깊이 [depth]까지 플랫화 [json]","_get all values with key [key] in array [json]":"배열 [json]에서 키가 [key]인 항목","_get list [list] as array":"리스트 [list]을(를) 배열로","_insert [item] at [pos] of array [json]":"배열 [json]의 [pos]번째에 [item] 삽입하기","_is JSON [json] valid?":"JSON [json]이(가) 유효한가?","_is [json] [types]?":"[json]이(가) [types]인가?","_item # of [item] in array [json]":"배열 [json]에서 [item] 항목의 번째","_item [item] of array [json]":"배열 [json]의 [item]번째 값","_items [item] to [item2] of array [json]":"배열 [json]의 [item]번째부터 [item2]번째까지의 항목","_join string by array [json] with delimiter [d]":"배열 [json]을(를) 구분자 [d](으)로 붙이기","_keys":"키","_length of array [json]":"배열 [json]의 길이","_length of json [json]":"JSON [json]의 길이","_new [json]":"새로운 [json]","_replace item [pos] of [json] with [item]":"배열 [json]의 [pos]번째 값을 [item](으)로 정하기","_reverse array [json]":"역방향 배열 [json]","_select a list":"(리스트 선택하기)","_set [item] in [json] to [value]":"[json]에서 [item]의 값을 [value](으)로 정하기","_set length of array [json] to [len]":"배열 [json]의 길이를 [len](으)로 정하기","_set list [list] to [json]":"리스트 [list]을(를) [json](으)로 정하기","_sort array [list] in [order] order":"배열 [list]을(를) [order]으로 정렬하기","_value of [item] in [json]":"[json]에서 [item]의 값","_values":"값"},"lt":{"_Advanced":"Išplėstiniai"},"nb":{"_Advanced":"Avansert","_keys":"nøkler","_values":"verdier"},"nl":{"_Advanced":"Geavanceerd","_General Utils":"Algemeen","_Lists":"Lijsten","_[json] contains key [key]?":"[json] bevat sleutel [key]?","_[json] contains value [value]?":"[json] bevat waarde [value]?","_add [item] to array [json]":"voeg [item] toe aan array [json]","_all [Stype] of [json]":"alle [Stype] van [json]","_array concat [json] [json2]":"voeg arrays [json] en [json2] samen","_array from text [json]":"array van tekst [json]","_ascending":"oplopende","_create array by [text] with delimiter [d]":"creëer array uit [text] met scheidingsteken [d]","_datas":"gegevens","_delete [item] in [json]":"verwijder [item] van [json]","_delete all [item] in array [json]":"verwijder alle [item] van array [json]","_delete item [item] of array [json]":"verwijder item [item] van array [json]","_descending":"aflopende","_flat array [json] by depth [depth]":"vlak array [json] af op diepte [depth]","_get all values with key [key] in array [json]":"alle waarden met sleutel [key] in array [json]","_get list [list] as array":"lijst [list] als array","_insert [item] at [pos] of array [json]":"voeg [item] in op [pos] van array [json]","_is JSON [json] valid?":"is JSON [json] geldig?","_is [json] [types]?":"is [json] [types] ?","_item # of [item] in array [json]":"item # van [item] in array [json]","_item [item] of array [json]":"item [item] van array [json]","_items [item] to [item2] of array [json]":"items [item] t/m [item2] van array [json]","_join string by array [json] with delimiter [d]":"voeg array [json] samen tot string met scheidingsteken [d]","_keys":"sleutels","_length of array [json]":"lengte van array [json]","_length of json [json]":"lengte van JSON [json]","_new [json]":"nieuw(e) [json]","_replace item [pos] of [json] with [item]":"vervang item [pos] van [json] door [item]","_reverse array [json]":"keer array [json] om","_select a list":"selecteer een lijst","_set [item] in [json] to [value]":"maak [item] in [json] [value]","_set length of array [json] to [len]":"maak lengte van array [json] [len]","_set list [list] to [json]":"maak lijst [list] [json]","_sort array [list] in [order] order":"sorteer array [list] in [order] volgorde","_value of [item] in [json]":"waarde van [item] in [json]","_values":"waarden"},"pl":{"_Advanced":"Zaawansowane"},"pt":{"_Advanced":"Avançado"},"pt-br":{"_Advanced":"Avançado"},"ro":{"_Advanced":"Avansat"},"ru":{"_Advanced":"Дополнительно","_Array":"Массив","_General Utils":"Главные Утилиты","_Lists":"Списки","_Object":"Объект","_[analysis] of array [list]":"[analysis] массива [list]","_[json] contains key [key]?":"[json] содержит ключ [key]?","_[json] contains value [value]?":"[json] содержит значение [value]?","_add [item] to array [json]":"добавить [item] к массиву [json]","_all [Stype] of [json]":"все [Stype] [json]","_array concat [json] [json2]":"соединить массив [json] и [json2]","_array from text [json]":"массив из текста [json]","_ascending":"нарастающей","_average":"среднее","_create array by [text] with delimiter [d]":"создать массив из [text] с разделителем [d]","_datas":"данные","_delete [item] in [json]":"удалить [item] из [json]","_delete all [item] in array [json]":"удалить все [item] в массиве [json]","_delete item [item] of array [json]":"удалить [item] в массиве [json]","_descending":"опускающей","_flat array [json] by depth [depth]":"плоский массив [json] по глубине [depth]","_get all values with key [key] in array [json]":"получить все значения с ключом [key] в массиве [json]","_get list [list] as array":"получить список [list] как массив","_insert [item] at [pos] of array [json]":"вставить [item] на позицию [pos] массива [json]","_is JSON [json] valid?":"JSON [json] правилен?","_is [json] [types]?":"[json] [types]?","_item # of [item] in array [json]":"значение # [item] массива [json]","_item [item] of array [json]":"[item] массива [json]","_items [item] to [item2] of array [json]":"значения с [item] до [item2] массива [json]","_join string by array [json] with delimiter [d]":"присоединить строку массива [json] с разделителем [d]","_keys":"ключи","_length of array [json]":"длина массива [json]","_length of json [json]":"длина json [json]","_maximum value":"максимальное значение","_median":"медиан","_minimum value":"минимальное значение","_mode":"режим","_new [json]":"новый [json]","_replace item [pos] of [json] with [item]":"заменить число [pos] матрицы [json] на [item]","_reverse array [json]":"обернуть массив [json]","_select a list":"выберите список","_set [item] in [json] to [value]":"задать [item] в [json] на [value]","_set length of array [json] to [len]":"задать длину массива [json] на [len]","_set list [list] to [json]":"задать список [list] в [json]","_sort array [list] in [order] order":"сортировать массив [list] в порядке [order]","_sum":"сумма","_value of [item] in [json]":"значение [item] в [json]","_values":"значения","_variance":"расхождение"},"sl":{"_Advanced":"Napredno"},"sr":{"_Advanced":"Напредно"},"sv":{"_Advanced":"Avancerat"},"tr":{"_Advanced":"Gelişmiş"},"uk":{"_ascending":"зростання","_descending":"спадання"},"vi":{"_Advanced":"Nâng cao"},"zh-cn":{"_Advanced":"高级","_Array":"数组","_General Utils":"通用","_Lists":"原版列表","_[analysis] of array [list]":"数组[list]的[analysis]","_[json1] [equal] [json2]":"[json1][equal][json2]","_[json] contains key [key]?":"[json]包括键[key]？","_[json] contains value [value]?":"[json]包括值[value]？","_add [item] to array [json]":"将[item]加入数组[json]","_all [Stype] of [json]":"[json]的所有[Stype]","_array concat [json] [json2]":"合并数组[json]和[json2]","_array from text [json]":"来自文本[json]的数组","_ascending":"正序","_average":"平均值","_create array by [text] with delimiter [d]":"以[d]分割字符串[text]为数组","_datas":"数据","_delete [item] in [json]":"删除[json]中的[item]","_delete all [item] in array [json]":"删除数组[json]中的所有[item]","_delete item [item] of array [json]":"删除数组[json]的第[item]项","_descending":"倒序","_flat array [json] by depth [depth]":"扁平化数组[json][depth]层","_get all values with key [key] in array [json]":"获取数组[json]中所有[key]的值","_get list [list] as array":"获取原版列表[list]","_insert [item] at [pos] of array [json]":"在数组[json]的第[pos]项前插入[item]","_is JSON [json] valid?":"[json]是合法JSON？","_is [json] [types]?":"[json]是[types]？","_item # of [item] in array [json]":"数组[json]中[item]的位置","_item [item] of array [json]":"数组[json]的第[item]项","_items [item] to [item2] of array [json]":"数组[json]的[item]到[item2]项","_join string by array [json] with delimiter [d]":"将数组[json]用分隔符[d]连成字符串","_keys":"键","_length of array [json]":"数组[json]的长度","_length of json [json]":"JSON[json]的长度","_maximum value":"最大值","_median":"中位数","_minimum value":"最小值","_mode":"众数","_new [json]":"新[json]","_replace item [pos] of [json] with [item]":"替换数组[json]的第[pos]项为[item]","_reverse array [json]":"反转数组[json]","_select a list":"请选择一个列表","_set [item] in [json] to [value]":"将[json]中的[item]设为[value]","_set length of array [json] to [len]":"将数组[json]的长度设为[len]","_set list [list] to [json]":"将列表[list]设为[json]","_sort array [list] in [order] order":"以[order]排列数组[list]","_sum":"和","_value of [item] in [json]":"[json]中的[item]","_values":"值","_variance":"方差"},"zh-tw":{"_Advanced":"個別更改"}});/* end generated l10n code */(function (Scratch) {
  "use strict";
  /*
   * JSON extension v2.6 by skyhigh173 (English Version)
   * Do not remove this comment
   */

  const vm = Scratch.vm;
  const hasOwn = (obj, property) =>
    Object.prototype.hasOwnProperty.call(obj, property);

  const makeLabel = (text) => ({
    blockType: "label",
    text: text,
  });

  class JSONS {
    getInfo() {
      return {
        id: "skyhigh173JSON",
        // eslint-disable-next-line extension/should-translate
        name: "JSON",
        color1: "#3271D0",
        blocks: [
          makeLabel(Scratch.translate("General Utils")),
          {
            opcode: "json_is_valid",
            blockType: Scratch.BlockType.BOOLEAN,
            text: Scratch.translate("is JSON [json] valid?"),
            arguments: {
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '{"key":"value"}',
              },
            },
          },
          {
            opcode: "json_is",
            blockType: Scratch.BlockType.BOOLEAN,
            text: Scratch.translate("is [json] [types]?"),
            arguments: {
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '{"key":"value"}',
              },
              types: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "Object",
                menu: "types",
              },
            },
          },
          "---",
          {
            opcode: "json_get_all",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("all [Stype] of [json]"),
            arguments: {
              Stype: {
                type: Scratch.ArgumentType.STRING,
                menu: "get_all",
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '{"key":"value","key2":"value2"}',
              },
            },
          },
          {
            opcode: "json_new",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("new [json]"),
            arguments: {
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "Object",
                menu: "types",
              },
            },
          },
          "---",
          {
            opcode: "json_has_key",
            blockType: Scratch.BlockType.BOOLEAN,
            text: Scratch.translate("[json] contains key [key]?"),
            arguments: {
              key: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "key2",
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '{"key":"value"}',
              },
            },
          },
          {
            opcode: "json_has_value",
            blockType: Scratch.BlockType.BOOLEAN,
            text: Scratch.translate("[json] contains value [value]?"),
            arguments: {
              value: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "scratch",
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["TurboWarp","scratch"]',
              },
            },
          },
          {
            opcode: "json_equal",
            blockType: Scratch.BlockType.BOOLEAN,
            text: Scratch.translate("[json1] [equal] [json2]"),
            arguments: {
              json1: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '{"a":0,"b":1}',
              },
              json2: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '{"b":1,"a":0}',
              },
              equal: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "=",
                menu: "equal",
              },
            },
          },
          makeLabel("JSON Strings"),
          {
            opcode: "json_jlength",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("length of json [json]"),
            arguments: {
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '{"key":"value","key2":"value2"}',
              },
            },
          },
          {
            opcode: "json_get",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("value of [item] in [json]"),
            arguments: {
              item: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "key",
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '{"key":"value"}',
              },
            },
          },
          {
            opcode: "json_set",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("set [item] in [json] to [value]"),
            arguments: {
              item: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "key",
              },
              value: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "new value",
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '{"key":"value"}',
              },
            },
          },
          {
            opcode: "json_delete",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("delete [item] in [json]"),
            arguments: {
              item: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "key2",
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '{"key":"value","key2":"value2"}',
              },
            },
          },
          makeLabel(Scratch.translate("Array")),
          {
            opcode: "json_length",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("length of array [json]"),
            arguments: {
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "[1,2,3]",
              },
            },
          },
          {
            opcode: "json_array_get",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("item [item] of array [json]"),
            arguments: {
              item: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 1,
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["scratch","TurboWarp"]',
              },
            },
          },
          {
            opcode: "json_array_push",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("add [item] to array [json]"),
            arguments: {
              item: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "TurboWarp",
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["scratch"]',
              },
            },
          },
          {
            opcode: "json_array_set",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("replace item [pos] of [json] with [item]"),
            arguments: {
              item: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "fav",
              },
              pos: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 2,
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["love","heart","follow"]',
              },
            },
          },
          {
            opcode: "json_array_insert",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("insert [item] at [pos] of array [json]"),
            arguments: {
              item: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "fav",
              },
              pos: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 2,
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["love","follow"]',
              },
            },
          },
          "---",
          {
            opcode: "json_array_delete",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("delete item [item] of array [json]"),
            arguments: {
              item: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 2,
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["scratch","a","TurboWarp"]',
              },
            },
          },
          {
            opcode: "json_array_remove_all",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("delete all [item] in array [json]"),
            arguments: {
              item: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "a",
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["scratch","a","TurboWarp","a","a"]',
              },
            },
          },
          "---",
          {
            opcode: "json_array_itemH",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("item # of [item] in array [json]"),
            arguments: {
              item: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "scratch",
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["scratch","TurboWarp"]',
              },
            },
          },
          makeLabel(Scratch.translate("Advanced")),
          {
            opcode: "json_array_from",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("array from text [json]"),
            arguments: {
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "abcd",
              },
            },
          },
          {
            opcode: "json_array_fromto",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("items [item] to [item2] of array [json]"),
            arguments: {
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "[1,2,3,4]",
              },
              item: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 2,
              },
              item2: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 3,
              },
            },
          },
          {
            opcode: "json_array_reverse",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("reverse array [json]"),
            arguments: {
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["a","b","c","d","e","f"]',
              },
            },
          },
          {
            opcode: "json_array_flat",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("flat array [json] by depth [depth]"),
            arguments: {
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "[[1],2,[3,4],[5,[6]]]",
              },
              depth: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 2,
              },
            },
          },
          {
            opcode: "json_array_concat",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("array concat [json] [json2]"),
            arguments: {
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["a","b"]',
              },
              json2: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["c","d"]',
              },
            },
          },
          {
            opcode: "json_array_filter",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate(
              "get all values with key [key] in array [json]"
            ),
            arguments: {
              key: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "id",
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '[{"id":12},{"id":24}]',
              },
            },
          },
          {
            opcode: "json_array_setlen",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("set length of array [json] to [len]"),
            arguments: {
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["a","b","c"]',
              },
              len: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 2,
              },
            },
          },
          "---",
          {
            opcode: "json_array_create",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate(
              "create array by [text] with delimiter [d]"
            ),
            arguments: {
              text: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "a,b,c",
              },
              d: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: ",",
              },
            },
          },
          {
            opcode: "json_array_join",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate(
              "join string by array [json] with delimiter [d]"
            ),
            arguments: {
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["a","b","c"]',
              },
              d: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: ",",
              },
            },
          },
          "---",
          {
            opcode: "json_array_sort",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("sort array [list] in [order] order"),
            disableMonitor: true,
            arguments: {
              list: {
                type: Scratch.ArgumentType.STRING,
                defaultValue:
                  "[5.23, 214, 522, 61, 5.24, 62.2, 1, 51212, 0, 0]",
              },
              order: {
                type: Scratch.ArgumentType.STRING,
                menu: "sort_order",
              },
            },
          },
          {
            opcode: "json_array_analysis",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("[analysis] of array [list]"),
            disableMonitor: true,
            arguments: {
              list: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "[17, 1, 2017, 0, 120, 14]",
              },
              analysis: {
                type: Scratch.ArgumentType.STRING,
                menu: "analysis",
              },
            },
          },
          makeLabel(Scratch.translate("Lists")),
          {
            opcode: "json_vm_getlist",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("get list [list] as array"),
            arguments: {
              list: {
                type: Scratch.ArgumentType.STRING,
                menu: "get_list",
              },
            },
          },
          {
            opcode: "json_vm_setlist",
            blockType: Scratch.BlockType.COMMAND,
            text: Scratch.translate("set list [list] to [json]"),
            arguments: {
              list: {
                type: Scratch.ArgumentType.STRING,
                menu: "get_list",
              },
              json: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["apple","banana"]',
              },
            },
          },
        ],
        menus: {
          get_all: {
            items: [
              {
                text: Scratch.translate("keys"),
                value: "keys",
              },
              {
                text: Scratch.translate("values"),
                value: "values",
              },
              {
                text: Scratch.translate("datas"),
                value: "datas",
              },
            ],
          },
          get_list: {
            acceptReporters: true,
            items: "getLists",
          },
          types: {
            acceptReporters: true,
            items: [
              {
                text: Scratch.translate("Object"),
                value: "Object",
              },
              {
                text: Scratch.translate("Array"),
                value: "Array",
              },
            ],
          },
          equal: {
            acceptReporters: true,
            items: ["=", "≠"],
          },
          sort_order: {
            acceptReporters: true,
            items: [
              {
                text: Scratch.translate("ascending"),
                value: "ascending",
              },
              {
                text: Scratch.translate("descending"),
                value: "descending",
              },
            ],
          },
          analysis: {
            acceptReporters: true,
            items: [
              {
                text: Scratch.translate("minimum value"),
                value: "minimum",
              },
              {
                text: Scratch.translate("maximum value"),
                value: "maximum",
              },
              {
                text: Scratch.translate("sum"),
                value: "sum",
              },
              {
                text: Scratch.translate("average"),
                value: "average",
              },
              {
                text: Scratch.translate("median"),
                value: "median",
              },
              {
                text: Scratch.translate("mode"),
                value: "mode",
              },
              {
                text: Scratch.translate("variance"),
                value: "variance",
              },
            ],
          },
        },
      };
    }

    getLists() {
      const globalLists = Object.values(
        vm.runtime.getTargetForStage().variables
      ).filter((x) => x.type === "list");
      const localLists = vm.editingTarget
        ? Object.values(vm.editingTarget.variables).filter(
            (x) => x.type === "list"
          )
        : [];
      const uniqueLists = [...new Set([...globalLists, ...localLists])];
      if (uniqueLists.length === 0) {
        return [
          {
            text: Scratch.translate("select a list"),
            value: "select a list",
          },
        ];
      }
      return uniqueLists.map((i) => ({
        text: i.name,
        value: i.id,
      }));
    }

    lookupList(list, util) {
      const byId = util.target.lookupVariableById(list);
      if (byId && byId.type === "list") {
        return byId;
      }
      const byName = util.target.lookupVariableByNameAndType(list, "list");
      if (byName) {
        return byName;
      }
      return null;
    }

    json_is_valid({ json }) {
      if (typeof json != "string") {
        return false;
      }
      json = json.trim();
      if (
        (json.slice(0, 1) != "[" || json.slice(-1) != "]") &&
        (json.slice(0, 1) != "{" || json.slice(-1) != "}")
      ) {
        return false;
      } else {
        try {
          JSON.parse(json);
          return true;
        } catch {
          return false;
        }
      }
    }

    // return object if its json else string
    json_valid_return(json) {
      if (typeof json != "string") {
        return json;
      } else if (
        (json.slice(0, 1) != "[" || json.slice(-1) != "]") &&
        (json.slice(0, 1) != "{" || json.slice(-1) != "}")
      ) {
        return json;
      } else {
        try {
          return JSON.parse(json) ?? "";
        } catch {
          return json;
        }
      }
    }

    json_is({ json, types }) {
      if (!this.json_is_valid({ json: json })) return false;
      try {
        json = JSON.parse(json);
        switch (types) {
          case "Object":
            return !Array.isArray(json);
          case "Array":
            return Array.isArray(json);
          default:
            return false;
        }
      } catch {
        return false;
      }
    }

    json_length({ json }) {
      try {
        json = JSON.parse(json);
        return Object.keys(json).length;
      } catch {
        return " ";
      }
    }

    json_new({ json }) {
      switch (json) {
        case "Object":
          return "{}";
        case "Array":
          return "[]";
        default:
          return "";
      }
    }

    json_has_key({ json, key }) {
      try {
        return (
          this._fixInvalidJSONValues(this.json_valid_return(key)) in
          JSON.parse(json)
        );
      } catch {
        return false;
      }
    }

    json_has_value({ json, value }) {
      try {
        json = JSON.parse(json);
        value = this.json_valid_return(value);
        return json.includes(value);
      } catch {
        return false;
      }
    }

    json_equal({ json1, equal, json2 }) {
      try {
        json1 = JSON.parse(json1);
        json2 = JSON.parse(json2);

        const keys1 = Object.keys(json1);
        const keys2 = Object.keys(json2);
        const result =
          keys1.length === keys2.length &&
          Object.keys(json1).every((key) => json1[key] === json2[key]);
        if (equal === "=") return result;
        if (equal === "≠") return !result;
      } catch {
        // ignore
      }
      return false;
    }

    json_get_all({ Stype, json }) {
      try {
        json = JSON.parse(json);
        switch (Stype) {
          case "keys":
            return JSON.stringify(Object.keys(json).map((key) => key ?? ""));
          case "values":
            return JSON.stringify(
              Object.keys(json).map((key) => json[key] ?? "")
            );
          case "datas":
            return JSON.stringify(
              Object.keys(json).map((key) => [key, json[key] ?? ""])
            );
          default:
            return "";
        }
      } catch {
        return "";
      }
    }

    json_get({ item, json }) {
      try {
        json = JSON.parse(json);
        if (hasOwn(json, item)) {
          const result = json[item] ?? "";
          if (typeof result === "object") {
            return JSON.stringify(result);
          } else {
            return result;
          }
        }
      } catch {
        // ignore
      }
      return "";
    }

    _fixInvalidJSONValues(value) {
      // JSON does not support these values, so convert to string.
      if (Number.isNaN(value)) return "NaN";
      if (value === Infinity) return "Infinity";
      if (value === -Infinity) return "-Infinity";
      // null and undefined -> empty
      return value ?? "";
    }

    json_set({ item, value, json }) {
      try {
        json = JSON.parse(json);
        value = this.json_valid_return(value);
        value = this._fixInvalidJSONValues(value);
        json[item] = value;
        return JSON.stringify(json);
      } catch {
        return "";
      }
    }

    json_delete({ item, json }) {
      try {
        json = JSON.parse(json);
        delete json[item];
        return JSON.stringify(json);
      } catch {
        return "";
      }
    }

    json_jlength({ json }) {
      // same function
      return this.json_length({ json: json });
    }

    json_array_get({ item, json }) {
      // 1...length : array content, -1...-length : reverse array content, 0 : ERROR
      try {
        item = Scratch.Cast.toNumber(item);
        if (item == 0) return "";
        if (item > 0) {
          item--;
        }
        json = JSON.parse(json);
        let result;
        if (item >= 0) {
          result = json[item];
        } else {
          const length = Scratch.Cast.toNumber(json.length);
          result = json[length + item];
        }
        result = result ?? "";
        if (typeof result == "object") {
          return JSON.stringify(result);
        } else {
          return result;
        }
      } catch {
        return "";
      }
    }

    json_array_itemH({ item, json }) {
      try {
        json = JSON.parse(json);
        item = this._fixInvalidJSONValues(this.json_valid_return(item));
        let result = JSON.stringify(json.indexOf(item) + 1);
        return result;
      } catch {
        return "";
      }
    }

    json_array_from({ json }) {
      try {
        return JSON.stringify(Array.from(Scratch.Cast.toString(json)));
      } catch {
        return "";
      }
    }

    json_array_concat({ json, json2 }) {
      try {
        json = JSON.parse(json);
        json2 = JSON.parse(json2);
        return JSON.stringify(json.concat(json2));
      } catch {
        return "";
      }
    }

    json_array_push({ item, json }) {
      try {
        json = JSON.parse(json);
        item = this._fixInvalidJSONValues(this.json_valid_return(item));
        json.push(item);
        return JSON.stringify(json);
      } catch {
        return "";
      }
    }

    json_array_insert({ item, pos, json }) {
      try {
        json = JSON.parse(json);
        item = this._fixInvalidJSONValues(this.json_valid_return(item));
        json.splice(pos - 1, 0, item);
        return JSON.stringify(json);
      } catch {
        return "";
      }
    }

    json_array_set({ item, pos, json }) {
      try {
        json = JSON.parse(json);
        json[pos - 1] = this._fixInvalidJSONValues(
          this.json_valid_return(item)
        );
        return JSON.stringify(json);
      } catch {
        return "";
      }
    }

    json_array_delete({ item, json }) {
      try {
        json = JSON.parse(json);
        json.splice(item - 1, 1);
        return JSON.stringify(json);
      } catch {
        return "";
      }
    }

    json_array_remove_all({ item, json }) {
      try {
        json = JSON.parse(json);
        item = this._fixInvalidJSONValues(this.json_valid_return(item));
        let i = 0;
        while (i < json.length) {
          if (json[i] === item) {
            json.splice(i, 1);
          } else {
            ++i;
          }
        }
        return JSON.stringify(json);
      } catch {
        return "";
      }
    }

    json_array_fromto({ json, item, item2 }) {
      try {
        return JSON.stringify(JSON.parse(json).slice(item - 1, item2));
      } catch {
        return "";
      }
    }

    json_array_reverse({ json }) {
      try {
        return JSON.stringify(JSON.parse(json).reverse());
      } catch {
        return "";
      }
    }

    json_array_flat({ json, depth }) {
      try {
        return JSON.stringify(JSON.parse(json).flat(depth));
      } catch {
        return "";
      }
    }

    json_array_create({ text, d }) {
      return JSON.stringify(Scratch.Cast.toString(text).split(d));
    }

    json_array_join({ json, d }) {
      try {
        return JSON.parse(json).join(d);
      } catch {
        return "";
      }
    }

    json_array_filter({ key, json }) {
      try {
        json = JSON.parse(json);
        return JSON.stringify(
          json.map((x) => {
            if (hasOwn(x, key)) {
              return x[key];
            }
            return null;
          })
        );
      } catch (e) {
        return "";
      }
    }

    json_array_setlen({ json, len }) {
      try {
        json = JSON.parse(json);
        json.length = len;
        return JSON.stringify(json);
      } catch {
        return "";
      }
    }

    json_vm_getlist({ list }, util) {
      try {
        let listVariable = this.lookupList(list, util);
        if (listVariable) {
          return JSON.stringify(listVariable.value);
        }
      } catch (e) {
        // ignore
      }
      return "";
    }
    json_vm_setlist({ list, json }, util) {
      try {
        let listVariable = this.lookupList(list, util);
        if (listVariable) {
          const array = JSON.parse(json);
          if (Array.isArray(array)) {
            const safeArray = array.map((i) => {
              if (typeof i === "object") return JSON.stringify(i);
              return i ?? "";
            });
            listVariable.value = safeArray;
          }
        }
      } catch (e) {
        // ignore
      }
      return "";
    }

    json_array_sort(args) {
      let list;
      try {
        list = JSON.parse(args.list);
      } catch {
        return "";
      }
      if (!Array.isArray(list)) {
        return "";
      }
      list.sort(Scratch.Cast.compare);
      if (args.order === "descending") list.reverse();
      return JSON.stringify(list);
    }
    json_array_analysis(args) {
      let list;
      try {
        list = JSON.parse(args.list);
      } catch {
        return 0;
      }
      if (!Array.isArray(list)) {
        return 0;
      }
      list = list.map(Scratch.Cast.toNumber);
      const listLength = list.length;
      switch (Scratch.Cast.toString(args.analysis)) {
        case "maximum": {
          let max = -Infinity;
          for (let i = 0; i < list.length; i++)
            if (list[i] > max) max = list[i];
          return max;
        }
        case "minimum": {
          let min = Infinity;
          for (let i = 0; i < list.length; i++)
            if (list[i] < min) min = list[i];
          return min;
        }
        case "sum":
          return list.reduce((a, b) => a + b, 0);
        case "average":
          return list.reduce((a, b) => a + b, 0) / listLength;
        case "median": {
          const list2 = list.sort(Scratch.Cast.compare);
          const list2Length = list2.length;
          const c = Math.floor(list2Length / 2);
          const e = list2Length % 2 === 0;
          if (e) return (list2[c - 1] + list2[c]) / 2;
          return list2[c];
        }
        case "mode": {
          const freqMap = new Map(),
            mode = [0, 0]; // current mode, max
          for (
            let i = 0, num = list[0], count = null;
            i < listLength;
            i++, num = list[i], count = freqMap.get(num)
          ) {
            count ||= 1;
            if (freqMap.has(num)) ++count;
            if (count > mode[1]) ((mode[0] = num), (mode[1] = count));
            freqMap.set(num, count);
          }
          return mode[0];
        }
        case "variance": {
          const average = list.reduce((a, b) => a + b, 0) / listLength;
          const list2 = list.map((a) => (a - average) ** 2);
          return list2.reduce((a, b) => a + b, 0) / listLength;
        }
      }
      return 0;
    }
  }
  Scratch.extensions.register(new JSONS());
})(Scratch);
