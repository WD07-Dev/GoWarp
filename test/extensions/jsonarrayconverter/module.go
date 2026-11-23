// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"encoding/json"
	"fmt"
	e "gowarp/core/extension"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

type member struct {
	key   string
	value any
}
type object []member

func New() e.Extension {
	return e.Extension{ID: "jsonarrayconverter", Blocks: []e.Block{
		{Opcode: "jsonarrayconverter_mergeObjects", Kind: "reporter", Inputs: []string{"JSON1", "JSON2"}, Handler: func(c *e.Context) (e.Result, error) {
			a, ok := numericValues(c.String("JSON1"))
			b, ok2 := numericValues(c.String("JSON2"))
			if !ok || !ok2 {
				return e.Result{Value: "{}"}, nil
			}
			return e.Result{Value: stringify(valuesObject(append(a, b...)))}, nil
		}},
		{Opcode: "jsonarrayconverter_appendValue", Kind: "reporter", Inputs: []string{"JSON", "VALUE"}, Handler: func(c *e.Context) (e.Result, error) {
			values, ok := numericValues(c.String("JSON"))
			if !ok {
				return e.Result{Value: "{}"}, nil
			}
			return e.Result{Value: stringify(valuesObject(append(values, c.String("VALUE"))))}, nil
		}},
		{Opcode: "jsonarrayconverter_arrayToObject", Kind: "reporter", Inputs: []string{"ARRAY"}, Handler: func(c *e.Context) (e.Result, error) {
			value, _ := parse(c.String("ARRAY"))
			values, ok := value.([]any)
			if !ok {
				return e.Result{Value: "{}"}, nil
			}
			return e.Result{Value: stringify(valuesObject(values))}, nil
		}},
		{Opcode: "jsonarrayconverter_objectToArray", Kind: "reporter", Inputs: []string{"JSON"}, Handler: func(c *e.Context) (e.Result, error) {
			value, _ := parse(c.String("JSON"))
			obj, ok := value.(object)
			if !ok {
				return e.Result{Value: "[]"}, nil
			}
			last := -1
			entries := map[int]any{}
			for _, m := range obj {
				if n, ok := numericKey(m.key); ok {
					// JavaScript's Array constructor rejects lengths of 2^32 or greater.
					if n >= 4294967295 {
						return e.Result{}, fmt.Errorf("invalid array length")
					}
					i := int(n)
					entries[i] = m.value
					if i > last {
						last = i
					}
				}
			}
			var out strings.Builder
			out.WriteByte('[')
			for i := 0; i <= last; i++ {
				if i > 0 {
					out.WriteByte(',')
				}
				out.WriteString(stringify(entries[i]))
			}
			out.WriteByte(']')
			return e.Result{Value: out.String()}, nil
		}},
	}}
}

func numericKey(key string) (uint64, bool) {
	if key == "" || len(key) > 1 && key[0] == '0' {
		return 0, false
	}
	for _, r := range key {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(key, 10, 64)
	return n, err == nil && n <= 9007199254740991
}

func numericValues(text string) ([]any, bool) {
	value, err := parse(text)
	obj, ok := value.(object)
	if err != nil || !ok {
		return nil, false
	}
	keys := make([]member, 0, len(obj))
	for _, m := range obj {
		if _, ok := numericKey(m.key); ok {
			keys = append(keys, m)
		}
	}
	sort.Slice(keys, func(i, j int) bool { a, _ := numericKey(keys[i].key); b, _ := numericKey(keys[j].key); return a < b })
	values := make([]any, len(keys))
	for i, m := range keys {
		values[i] = m.value
	}
	return values, true
}

func valuesObject(values []any) object {
	obj := make(object, len(values))
	for i, v := range values {
		obj[i] = member{strconv.Itoa(i), v}
	}
	return obj
}

// Preserve object insertion order and JavaScript's numeric-key ordering, even
// inside values. Parsing into Go maps would change the reporter's JSON text.
func parse(text string) (any, error) {
	d := json.NewDecoder(strings.NewReader(text))
	d.UseNumber()
	value, err := readValue(d)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON data")
	}
	return value, nil
}

func readValue(d *json.Decoder) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		obj := object{}
		indices := map[string]int{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			value, err := readValue(d)
			if err != nil {
				return nil, err
			}
			name := key.(string)
			if i, exists := indices[name]; exists {
				obj[i].value = value
			} else {
				indices[name] = len(obj)
				obj = append(obj, member{name, value})
			}
		}
		_, err = d.Token()
		return obj, err
	case json.Delim('['):
		values := []any{}
		for d.More() {
			value, err := readValue(d)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		_, err = d.Token()
		return values, err
	}
	if number, ok := token.(json.Number); ok {
		value, _ := strconv.ParseFloat(string(number), 64)
		return value, nil
	}
	return token, nil
}

func stringify(value any) string {
	switch value := value.(type) {
	case nil:
		return "null"
	case string:
		var out strings.Builder
		out.WriteByte('"')
		for _, r := range value {
			switch r {
			case '"', '\\':
				out.WriteByte('\\')
				out.WriteRune(r)
			case '\b':
				out.WriteString(`\b`)
			case '\f':
				out.WriteString(`\f`)
			case '\n':
				out.WriteString(`\n`)
			case '\r':
				out.WriteString(`\r`)
			case '\t':
				out.WriteString(`\t`)
			default:
				if r < 0x20 {
					fmt.Fprintf(&out, `\u%04x`, r)
				} else {
					out.WriteRune(r)
				}
			}
		}
		out.WriteByte('"')
		return out.String()
	case float64:
		if math.IsInf(value, 0) || math.IsNaN(value) {
			return "null"
		}
		return e.String(value)
	case bool:
		return strconv.FormatBool(value)
	case []any:
		parts := make([]string, len(value))
		for i, v := range value {
			parts[i] = stringify(v)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case object:
		ordered := append(object(nil), value...)
		sort.SliceStable(ordered, func(i, j int) bool {
			a, ai := numericKey(ordered[i].key)
			b, bi := numericKey(ordered[j].key)
			ai = ai && a < 4294967295
			bi = bi && b < 4294967295
			if ai != bi {
				return ai
			}
			return ai && a < b
		})
		parts := make([]string, len(ordered))
		for i, m := range ordered {
			parts[i] = stringify(m.key) + ":" + stringify(m.value)
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	return "null"
}
