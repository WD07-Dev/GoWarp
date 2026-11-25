// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
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
