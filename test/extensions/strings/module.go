// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"encoding/json"
	"fmt"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	e "gowarp/core/extension"
	"math"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

func New() e.Extension {
	var split splitCache
	var regexCache regexMatchCache
	return e.Extension{ID: "strings", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "strings_identical", Kind: "boolean", Text: "is [OPERAND1] identical to [OPERAND2]?", Inputs: []string{"OPERAND1", "OPERAND2"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: identical(c.Args["OPERAND1"], c.Args["OPERAND2"])}, nil
		}},
		{Opcode: "strings_unicodefrom", Kind: "reporter", Text: "unicode [NUM] as letter", Inputs: []string{"NUM"}, Handler: func(c *e.Context) (e.Result, error) {
			n := c.Number("NUM")
			if math.IsInf(n, 0) || math.IsNaN(n) {
				n = 0
			}
			n = math.Mod(math.Trunc(n), 65536)
			if n < 0 {
				n += 65536
			}
			return e.Result{Value: string(rune(uint16(n)))}, nil
		}},
		{Opcode: "strings_letters_of", Kind: "reporter", Text: "letters [LETTER1] to [LETTER2] of [STRING]", Inputs: []string{"STRING", "LETTER1", "LETTER2"}, Handler: func(c *e.Context) (e.Result, error) {
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
		{Opcode: "strings_count", Kind: "reporter", Text: "count [SUBSTRING] in [STRING]", Inputs: []string{"STRING", "SUBSTRING"}, Handler: func(c *e.Context) (e.Result, error) {
			text, separator := c.String("STRING"), c.String("SUBSTRING")
			if separator == "" {
				count := len(utf16.Encode([]rune(text))) - 1
				return e.Result{Value: float64(count)}, nil
			}
			return e.Result{Value: float64(len(split.parts(text, separator)) - 1)}, nil
		}},
		{Opcode: "strings_repeat", Kind: "reporter", Text: "repeat [STRING] [REPEAT] times", Inputs: []string{"STRING", "REPEAT"}, Handler: func(c *e.Context) (e.Result, error) {
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
		{Opcode: "strings_reverse", Kind: "reporter", Text: "reverse text [STRING]", Inputs: []string{"STRING"}, Handler: func(c *e.Context) (e.Result, error) {
			text := []rune(c.String("STRING"))
			for left, right := 0, len(text)-1; left < right; left, right = left+1, right-1 {
				text[left], text[right] = text[right], text[left]
			}
			return e.Result{Value: string(text)}, nil
		}},

		{Opcode: "strings_split", Kind: "reporter", Text: "item [ITEM] of [STRING] split by [SPLIT]", Inputs: []string{"STRING", "SPLIT", "ITEM"}, Handler: func(c *e.Context) (e.Result, error) {
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
		{Opcode: "strings_replace", Kind: "reporter", Text: "replace [SUBSTRING] in [STRING] with [REPLACE]", Inputs: []string{"STRING", "SUBSTRING", "REPLACE"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: extensionReplace(c.String("STRING"), c.String("SUBSTRING"), c.String("REPLACE"))}, nil
		}},
		{Opcode: "strings_toCase", Kind: "reporter", Text: "convert [STRING] to [TEXTCASE]", Inputs: []string{"STRING", "TEXTCASE"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: toCase(c.String("STRING"), c.String("TEXTCASE"))}, nil
		}},
		{Opcode: "strings_unicodeof", Kind: "reporter", Text: "unicode of [STRING]", Inputs: []string{"STRING"}, Handler: func(c *e.Context) (e.Result, error) {
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
		{Opcode: "strings_indexof", Kind: "reporter", Text: "index of [SUBSTRING] in [STRING]", Inputs: []string{"SUBSTRING", "STRING"}, Handler: func(c *e.Context) (e.Result, error) {
			text, sub := lower(c.String("STRING")), lower(c.String("SUBSTRING"))
			index := strings.Index(text, sub)
			if index < 0 {
				return e.Result{Value: float64(0)}, nil
			}
			return e.Result{Value: float64(len(utf16.Encode([]rune(text[:index]))) + 1)}, nil
		}},
		{Opcode: "strings_posWith", Kind: "boolean", Text: "[STRING] [POSITION] with [SUBSTRING]?", Inputs: []string{"STRING", "POSITION", "SUBSTRING"}, Handler: func(c *e.Context) (e.Result, error) {
			text, sub := c.String("STRING"), c.String("SUBSTRING")
			if c.String("POSITION") == "starts" {
				return e.Result{Value: strings.HasPrefix(text, sub)}, nil
			}
			return e.Result{Value: strings.HasSuffix(text, sub)}, nil
		}},
		{Opcode: "strings_trim", Kind: "reporter", Text: "trim whitespace [STRING] from [METHOD]", Inputs: []string{"STRING", "METHOD"}, Handler: func(c *e.Context) (e.Result, error) {
			text := c.String("STRING")
			switch c.String("METHOD") {
			case "start":
				text = strings.TrimLeftFunc(text, jsSpace)
			case "end":
				text = strings.TrimRightFunc(text, jsSpace)
			default:
				text = strings.TrimFunc(text, jsSpace)
			}
			return e.Result{Value: text}, nil
		}},
		{Opcode: "strings_isCase", Kind: "boolean", Text: "is [STRING] [TEXTCASE]?", Inputs: []string{"STRING", "TEXTCASE"}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: isCase(c.String("STRING"), c.String("TEXTCASE"))}, nil
		}},
		{Opcode: "strings_replaceRegex", Kind: "reporter", Text: "replace regex /[REGEX]/[FLAGS] in [STRING] with [REPLACE]", Inputs: []string{"REGEX", "FLAGS", "STRING", "REPLACE"}, Handler: func(c *e.Context) (e.Result, error) {
			return regexResult(&regexCache, c, "replaceRegex")
		}},
		{Opcode: "strings_matchRegex", Kind: "reporter", Text: "item [ITEM] of [STRING] matched by regex /[REGEX]/[FLAGS]", Inputs: []string{"ITEM", "STRING", "REGEX", "FLAGS"}, Handler: func(c *e.Context) (e.Result, error) {
			return regexResult(&regexCache, c, "matchRegex")
		}},
		{Opcode: "strings_matchRegexJSON", Kind: "reporter", Text: "matches of [STRING] using regex /[REGEX]/[FLAGS] as array", Inputs: []string{"STRING", "REGEX", "FLAGS"}, Handler: func(c *e.Context) (e.Result, error) {
			return regexResult(&regexCache, c, "matchRegexJSON")
		}},
		{Opcode: "strings_testRegex", Kind: "boolean", Text: "[STRING] matches regex /[REGEX]/[FLAGS]?", Inputs: []string{"STRING", "REGEX", "FLAGS"}, Handler: func(c *e.Context) (e.Result, error) {
			return regexResult(&regexCache, c, "testRegex")
		}},
		{Opcode: "strings_countRegex", Kind: "reporter", Text: "count regex /[REGEX]/[FLAGS] in [STRING]", Inputs: []string{"STRING", "REGEX", "FLAGS"}, Handler: func(c *e.Context) (e.Result, error) {
			return regexResult(&regexCache, c, "countRegex")
		}},
		{Opcode: "strings_menu_positions", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field("positions")}, nil }},
		{Opcode: "strings_menu_trimMethod", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field("trimMethod")}, nil }},
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

func identical(a, b any) bool {
	switch a := a.(type) {
	case nil:
		return b == nil
	case string:
		v, ok := b.(string)
		return ok && a == v
	case bool:
		v, ok := b.(bool)
		return ok && a == v
	}
	numeric := func(v any) (float64, bool) {
		switch v := v.(type) {
		case int:
			return float64(v), true
		case int64:
			return float64(v), true
		case float32:
			return float64(v), true
		case float64:
			return v, true
		}
		return 0, false
	}
	x, ok := numeric(a)
	y, ok2 := numeric(b)
	return ok && ok2 && x == y
}

func lower(s string) string { return cases.Lower(language.Und).String(s) }
func upper(s string) string { return cases.Upper(language.Und).String(s) }
func jsSpace(r rune) bool   { return r == '\ufeff' || (r != '\u0085' && unicode.IsSpace(r)) }

var wordBoundary = regexp.MustCompile(`\b`)
var sentenceCase = regexp.MustCompile(`^[A-Z][^?.!]*(?:[?.!][\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+[A-Z][^?.!]*)*$`)

func titleCase(s string, exact bool) string {
	parts := wordBoundary.Split(s, -1)
	for i, part := range parts {
		if part != "" {
			_, size := utf8.DecodeRuneInString(part)
			tail := part[size:]
			if exact {
				tail = lower(tail)
			}
			parts[i] = upper(part[:size]) + tail
		}
	}
	return strings.Join(parts, "")
}
func isCase(s, kind string) bool {
	switch kind {
	case "lowercase":
		return lower(s) == s
	case "uppercase":
		return upper(s) == s
	case "mixedcase":
		return lower(s) != s && upper(s) != s
	case "titlecase":
		return titleCase(s, false) == s
	case "exacttitlecase":
		return titleCase(s, true) == s
	case "randomcase":
		return true
	case "sentencecase":
		return sentenceCase.MatchString(s)
	case "camelcase":
		if s == "" || s[0] >= 'A' && s[0] <= 'Z' {
			return false
		}
		return strings.IndexFunc(s, jsSpace) < 0
	}
	return false
}
func toCase(s, kind string) string {
	switch kind {
	case "lowercase":
		return lower(s)
	case "uppercase":
		return upper(s)
	case "titlecase":
		return titleCase(s, false)
	case "exacttitlecase":
		return titleCase(s, true)
	case "mixedcase", "randomcase", "sentencecase", "camelcase":
		var out strings.Builder
		capital := false
		previous := ' '
		if kind == "camelcase" {
			previous = 'x'
		}
		for i, r := range []rune(s) {
			part := string(r)
			switch kind {
			case "mixedcase":
				if i%2 == 0 {
					part = upper(part)
				} else {
					part = lower(part)
				}
			case "randomcase":
				if rand.Float64() > 0.5 {
					part = upper(part)
				} else {
					part = lower(part)
				}
			case "camelcase":
				if jsSpace(previous) {
					part = upper(part)
				} else {
					part = lower(part)
				}
				if jsSpace(r) {
					part = ""
				}
			case "sentencecase":
				if jsSpace(previous) && !capital && upper(part) != lower(part) {
					part = upper(part)
					capital = true
				} else {
					if strings.ContainsRune(".!?", r) {
						capital = false
					}
					part = lower(part)
				}
			}
			out.WriteString(part)
			previous = r
		}
		return out.String()
	}
	return s
}

// Go's RE2 is not a JavaScript RegExp engine. The portable subset below supports
// ASCII input/patterns, captures, alternation, ordinary quantifiers, and g/i/m/s/y/d.
// Unsupported valid JS features return an explicit runtime error; they must never
// silently produce RE2 results for lookaround, backreferences or UTF-16 matching.
// Invalid expressions/flags retain the original extension's empty/false fallback.
type unsupportedRegex struct{ reason string }

func (err unsupportedRegex) Error() string {
	return "unsupported JavaScript regular expression: " + err.reason
}
func jsRegex(pattern, flags, text string) (string, error) {
	seen := map[rune]bool{}
	for _, flag := range flags {
		if seen[flag] || !strings.ContainsRune("dgimsuvy", flag) {
			return "", fmt.Errorf("invalid regular expression flags")
		}
		seen[flag] = true
	}
	if seen['u'] || seen['v'] {
		return "", unsupportedRegex{"Unicode flags"}
	}
	for _, s := range []string{pattern, text} {
		for _, r := range s {
			if r > 127 {
				return "", unsupportedRegex{"non-ASCII pattern or input"}
			}
		}
	}
	if seen['m'] && strings.ContainsRune(text, '\r') {
		return "", unsupportedRegex{"multiline carriage returns"}
	}
	var out strings.Builder
	class := false
	// A repeated capture can retain values differently in RE2 and JavaScript.
	captures := []bool{}
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		if ch == '\\' {
			if i+1 >= len(pattern) {
				return "", fmt.Errorf("trailing escape")
			}
			i++
			next := pattern[i]
			switch next {
			case '1', '2', '3', '4', '5', '6', '7', '8', '9':
				return "", unsupportedRegex{"backreferences or octal escapes"}
			case '0':
				if i+1 < len(pattern) && pattern[i+1] >= '0' && pattern[i+1] <= '9' {
					return "", unsupportedRegex{"octal escapes"}
				}
				out.WriteString(`\x00`)
			case 'd', 'D', 'w', 'W', 't', 'n', 'v', 'f', 'r':
				out.WriteByte('\\')
				out.WriteByte(next)
			case 'b':
				if class {
					out.WriteString(`\x08`)
				} else {
					out.WriteString(`\b`)
				}
			case 'B':
				if class {
					out.WriteByte('B')
				} else {
					out.WriteString(`\B`)
				}
			case 's':
				if class {
					out.WriteString(`\t\n\v\f\r `)
				} else {
					out.WriteString(`[\t\n\v\f\r ]`)
				}
			case 'S':
				if class {
					return "", unsupportedRegex{"negated whitespace escape inside a character class"}
				}
				out.WriteString(`[^\t\n\v\f\r ]`)
			case 'x', 'u':
				size := 2
				if next == 'u' {
					size = 4
				}
				if i+size >= len(pattern) {
					return "", unsupportedRegex{"legacy hexadecimal escape"}
				}
				n, err := strconv.ParseUint(pattern[i+1:i+1+size], 16, 16)
				if err != nil {
					return "", unsupportedRegex{"legacy hexadecimal escape"}
				}
				if n > 127 {
					return "", unsupportedRegex{"non-ASCII escape"}
				}
				fmt.Fprintf(&out, `\x%02x`, n)
				i += size
			case 'c':
				if i+1 >= len(pattern) || !((pattern[i+1] >= 'a' && pattern[i+1] <= 'z') || (pattern[i+1] >= 'A' && pattern[i+1] <= 'Z')) {
					return "", unsupportedRegex{"legacy control escape"}
				}
				i++
				fmt.Fprintf(&out, `\x%02x`, pattern[i]%32)
			default:
				if class && strings.ContainsRune(`\]-^`, rune(next)) {
					out.WriteByte('\\')
					out.WriteByte(next)
				} else {
					out.WriteString(regexp.QuoteMeta(string(next)))
				}
			}
			continue
		}
		if ch == '[' && !class {
			class = true
			if strings.HasPrefix(pattern[i:], "[[:") || strings.HasPrefix(pattern[i:], "[]") || strings.HasPrefix(pattern[i:], "[^]") {
				return "", unsupportedRegex{"legacy or POSIX character class"}
			}
		} else if ch == ']' {
			class = false
		}
		if !class {
			if ch == '(' {
				capture := true
				if i+1 < len(pattern) && pattern[i+1] == '?' {
					if !strings.HasPrefix(pattern[i:], "(?:") {
						return "", unsupportedRegex{"lookaround, named groups or inline flags"}
					}
					capture = false
				}
				captures = append(captures, capture)
			}
			if ch == ')' && len(captures) > 0 {
				capture := captures[len(captures)-1]
				captures = captures[:len(captures)-1]
				if capture && len(captures) > 0 {
					captures[len(captures)-1] = true
				}
				if capture && i+1 < len(pattern) && strings.ContainsRune("*+{", rune(pattern[i+1])) {
					return "", unsupportedRegex{"quantified capturing groups"}
				}
			}
			if ch == '.' && !seen['s'] {
				out.WriteString(`[^\n\r]`)
				continue
			}
		}
		out.WriteByte(ch)
	}
	modes := ""
	for _, flag := range "ims" {
		if seen[flag] {
			modes += string(flag)
		}
	}
	compiled := out.String()
	if modes != "" {
		compiled = "(?" + modes + ")" + compiled
	}
	if _, err := regexp.Compile(compiled); err != nil {
		if strings.Contains(err.Error(), "invalid repeat count") {
			return "", unsupportedRegex{"repeat count outside RE2 limits"}
		}
		return "", err
	}
	return compiled, nil
}
func regexMatches(pattern, text string, global, sticky bool) [][]int {
	matches := [][]int{}
	search := ""
	if !sticky {
		search = `(?s:.*?)`
	}
	first := regexp.MustCompile(`\A` + search + "(" + pattern + ")")
	next := regexp.MustCompile(`(?s:\A.)` + search + "(" + pattern + ")")
	for cursor := 0; cursor <= len(text); {
		// Preserve one preceding ASCII byte for ^ and word-boundary assertions.
		// Keeping the suffix's real end also preserves $. Reuse compiled patterns
		// instead of recompiling an ever-growing prefix for every global match.
		base, re := 0, first
		if cursor > 0 {
			base = cursor - 1
			re = next
		}
		indices := re.FindStringSubmatchIndex(text[base:])
		if indices == nil {
			break
		}
		indices = indices[2:]
		for i := range indices {
			if indices[i] >= 0 {
				indices[i] += base
			}
		}
		matches = append(matches, indices)
		if !global {
			break
		}
		cursor = indices[1]
		if indices[0] == indices[1] {
			cursor++
		}
	}
	return matches
}

func regexReplacement(text, replacement string, m []int) string {
	var out strings.Builder
	for i := 0; i < len(replacement); i++ {
		if replacement[i] == '$' && i+1 < len(replacement) {
			next := replacement[i+1]
			switch next {
			case '$':
				out.WriteByte('$')
				i++
				continue
			case '&':
				out.WriteString(text[m[0]:m[1]])
				i++
				continue
			case '`':
				out.WriteString(text[:m[0]])
				i++
				continue
			case '\'':
				out.WriteString(text[m[1]:])
				i++
				continue
			}
			if next >= '0' && next <= '9' {
				n := int(next - '0')
				digits := 1
				if i+2 < len(replacement) && replacement[i+2] >= '0' && replacement[i+2] <= '9' {
					two := n*10 + int(replacement[i+2]-'0')
					if two > 0 && two < len(m)/2 {
						n = two
						digits = 2
					}
				}
				if n > 0 && n < len(m)/2 {
					if m[2*n] >= 0 {
						out.WriteString(text[m[2*n]:m[2*n+1]])
					}
					i += digits
					continue
				}
			}
		}
		out.WriteByte(replacement[i])
	}
	return out.String()
}

// Cache only bounded strings/match arrays; retain the last count for the original
// countRegex invalid-pattern behavior without holding large input strings alive.
type regexMatchCache struct {
	text, pattern, flags string
	matches              [][]int
	count                int
	initialized          bool
}

func regexResult(cache *regexMatchCache, c *e.Context, op string) (e.Result, error) {
	text, flags := c.String("STRING"), c.String("FLAGS")
	pattern, err := jsRegex(c.String("REGEX"), flags, text)
	if err != nil {
		if _, unsupported := err.(unsupportedRegex); unsupported {
			return e.Result{}, err
		}
		switch op {
		case "testRegex":
			return e.Result{Value: false}, nil
		case "countRegex":
			if !cache.initialized {
				return e.Result{}, fmt.Errorf("countRegex has no previous successful match: %w", err)
			}
			return e.Result{Value: float64(cache.count)}, nil
		}
		return e.Result{Value: ""}, nil
	}
	global := strings.Contains(flags, "g") || op == "matchRegex" || op == "matchRegexJSON" || op == "countRegex"
	cached := op == "matchRegex" || op == "matchRegexJSON" || op == "countRegex"
	var matches [][]int
	if cached && cache.matches != nil && cache.text == text && cache.pattern == pattern && cache.flags == flags {
		matches = cache.matches
	} else {
		matches = regexMatches(pattern, text, global, strings.Contains(flags, "y"))
		if cached {
			*cache = regexMatchCache{count: len(matches), initialized: true}
			if len(text) <= 64<<10 && len(pattern) <= 1024 && len(matches) <= 4096 {
				cache.text = strings.Clone(text)
				cache.pattern = strings.Clone(pattern)
				cache.flags = strings.Clone(flags)
				cache.matches = matches
			}
		}
	}
	switch op {
	case "testRegex":
		return e.Result{Value: len(matches) > 0}, nil
	case "countRegex":
		return e.Result{Value: float64(len(matches))}, nil
	case "matchRegex":
		n := c.Number("ITEM")
		if finite(n) && n == math.Trunc(n) && n >= 1 && n <= float64(len(matches)) {
			m := matches[int(n)-1]
			return e.Result{Value: text[m[0]:m[1]]}, nil
		}
		return e.Result{Value: ""}, nil
	case "matchRegexJSON":
		values := make([]string, 0, len(matches))
		for _, m := range matches {
			values = append(values, text[m[0]:m[1]])
		}
		var encoded strings.Builder
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(values)
		return e.Result{Value: strings.TrimSuffix(encoded.String(), "\n")}, nil
	case "replaceRegex":
		var out strings.Builder
		last := 0
		for _, m := range matches {
			out.WriteString(text[last:m[0]])
			out.WriteString(regexReplacement(text, c.String("REPLACE"), m))
			last = m[1]
		}
		out.WriteString(text[last:])
		return e.Result{Value: out.String()}, nil
	}
	return e.Result{}, fmt.Errorf("unknown regex operation %q", op)
}
