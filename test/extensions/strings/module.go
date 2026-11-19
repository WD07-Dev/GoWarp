// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

func New() e.Extension {
	var split splitCache
	return e.Extension{ID: "strings", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "strings_letters_of", Kind: "reporter", Inputs: []string{"STRING", "LETTER1", "LETTER2"}, Handler: func(c *e.Context) (e.Result, error) {
			units := utf16.Encode([]rune(c.String("STRING")))
			index := func(value float64) int {
				if math.IsNaN(value) || value <= 0 {
					return 0
				}
				if value >= float64(len(units)) {
					return len(units)
				}
				return int(value)
			}
			start, end := index(c.Number("LETTER1")-1), index(c.Number("LETTER2"))
			if start > end {
				start, end = end, start
			}
			return e.Result{Value: string(utf16.Decode(units[start:end]))}, nil
		}},
		{Opcode: "strings_count", Kind: "reporter", Inputs: []string{"STRING", "SUBSTRING"}, Handler: func(c *e.Context) (e.Result, error) {
			text, separator := c.String("STRING"), c.String("SUBSTRING")
			if separator == "" {
				count := len(utf16.Encode([]rune(text))) - 1
				return e.Result{Value: float64(count)}, nil
			}
			return e.Result{Value: float64(len(split.parts(text, separator)) - 1)}, nil
		}},
		{Opcode: "strings_repeat", Kind: "reporter", Inputs: []string{"STRING", "REPEAT"}, Handler: func(c *e.Context) (e.Result, error) {
			text, count := c.String("STRING"), math.Floor(c.Number("REPEAT"))
			if !finite(count) || count <= 0 || text == "" {
				return e.Result{Value: ""}, nil
			}
			// Match a bounded JavaScript string allocation rather than overflow Go's int.
			if count > float64((1<<29)-24)/float64(len(text)) {
				return e.Result{}, fmt.Errorf("repeated string is too large")
			}
			return e.Result{Value: strings.Repeat(text, int(count))}, nil
		}},
		{Opcode: "strings_reverse", Kind: "reporter", Inputs: []string{"STRING"}, Handler: func(c *e.Context) (e.Result, error) {
			text := []rune(c.String("STRING"))
			for left, right := 0, len(text)-1; left < right; left, right = left+1, right-1 {
				text[left], text[right] = text[right], text[left]
			}
			return e.Result{Value: string(text)}, nil
		}},

		{Opcode: "strings_split", Kind: "reporter", Inputs: []string{"STRING", "SPLIT", "ITEM"}, Handler: func(c *e.Context) (e.Result, error) {
			item := c.Number("ITEM")
			if !finite(item) || item != math.Floor(item) || item < 1 {
				return e.Result{Value: ""}, nil
			}
			parts := split.parts(c.String("STRING"), c.String("SPLIT"))
			if item > float64(len(parts)) {
				return e.Result{Value: ""}, nil
			}
			return e.Result{Value: parts[int(item)-1]}, nil
		}},
		{Opcode: "strings_replace", Kind: "reporter", Inputs: []string{"STRING", "SUBSTRING", "REPLACE"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: extensionReplace(c.String("STRING"), c.String("SUBSTRING"), c.String("REPLACE"))}, nil
		}},
		{Opcode: "strings_toCase", Kind: "reporter", Inputs: []string{"STRING", "TEXTCASE"}, Handler: func(c *e.Context) (e.Result, error) {
			switch c.String("TEXTCASE") {
			case "lowercase":
				return e.Result{Value: strings.ToLower(c.String("STRING"))}, nil
			case "uppercase":
				return e.Result{Value: strings.ToUpper(c.String("STRING"))}, nil
			}
			return e.Result{}, fmt.Errorf("unsupported text case %q", c.String("TEXTCASE"))
		}},
		{Opcode: "strings_unicodeof", Kind: "reporter", Inputs: []string{"STRING"}, Handler: func(c *e.Context) (e.Result, error) {
			parts := []string{}
			for _, ch := range c.String("STRING") {
				unit := ch
				if ch > 0xffff {
					unit, _ = utf16.EncodeRune(ch)
				}
				parts = append(parts, strconv.Itoa(int(unit)))
			}
			return e.Result{Value: strings.Join(parts, " ")}, nil
		}},
		{Opcode: "strings_menu_textCase", Kind: "reporter", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: c.Field("textCase")}, nil
		}},
	}}
}

// Keep only the last bounded result, as the JS extension does for field lookups.
type splitCache struct {
	text, separator string
	result          []string
	pattern         string
	re              *regexp.Regexp
}

func (cache *splitCache) parts(text, separator string) []string {
	if cache.result != nil && cache.text == text && cache.separator == separator {
		return cache.result
	}
	cache.text, cache.separator, cache.result = "", "", nil
	retain := len(text) <= 64<<10 && len(separator) <= 1024
	if retain {
		// Inputs can be slices of much larger strings; retain only these bytes.
		text, separator = strings.Clone(text), strings.Clone(separator)
	}
	literal := separator != ""
	for i := 0; i < len(separator) && literal; i++ {
		ch := separator[i]
		literal = ch < 128 && !(ch >= 'A' && ch <= 'Z') && !(ch >= 'a' && ch <= 'z')
	}
	var parts []string
	if literal {
		parts = strings.Split(text, separator)
	} else {
		re := cache.re
		if re == nil || cache.pattern != separator {
			re = regexp.MustCompile("(?i)" + regexp.QuoteMeta(separator))
			if len(separator) <= 1024 {
				cache.pattern, cache.re = strings.Clone(separator), re
			}
		}
		parts = re.Split(text, -1)
	}
	if retain && len(parts) <= 4096 {
		cache.text, cache.separator, cache.result = text, separator, parts
	}
	return parts
}

func extensionReplace(s, find, replacement string) string {
	re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(find))
	matches := re.FindAllStringIndex(s, -1)
	var out strings.Builder
	last := 0
	for _, m := range matches {
		out.WriteString(s[last:m[0]])
		for i := 0; i < len(replacement); i++ {
			if replacement[i] == '$' && i+1 < len(replacement) {
				switch replacement[i+1] {
				case '$':
					out.WriteByte('$')
					i++
					continue
				case '&':
					out.WriteString(s[m[0]:m[1]])
					i++
					continue
				case '`':
					out.WriteString(s[:m[0]])
					i++
					continue
				case '\'':
					out.WriteString(s[m[1]:])
					i++
					continue
				}
			}
			out.WriteByte(replacement[i])
		}
		last = m[1]
	}
	out.WriteString(s[last:])
	return out.String()
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
