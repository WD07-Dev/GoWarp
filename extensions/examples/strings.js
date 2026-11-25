/*
@gowarp-config {
require golang.org/x/text v0.41.0
}

@gowarp-go {
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
}
*/
// Name: Text
// ID: strings
// Description: Manipulate characters and text.
// By: CST1229 <https://scratch.mit.edu/users/CST1229/>
// By: BludIsAnLemon <https://scratch.mit.edu/users/BludIsAnLemon/>
// By: Man-o-Valor <https://scratch.mit.edu/users/man-o-valor/>
// License: MIT AND MPL-2.0

/* generated l10n code */Scratch.translate.setup({"es":{"_Text":"Texto"},"fi":{"_Exactly Title Case":"Alkukirjaimet Tarkasti Isolla","_MiXeD CaSe":"IsOt jA PiEnEt kIrJaImEt sEkAiSiN","_RAndoMCaSe":"SATuNnaiNEn KirJaInKoKO","_Sentence case":"Virkkeen alkukirjain isolla","_Text":"Teksti","_Title Case":"Alkukirjaimet Isolla","_UPPERCASE":"ISOT KIRJAIMET","_[STRING] matches regex /[REGEX]/[FLAGS]?":"vastaako [STRING] säännöllistä lauseketta /[REGEX]/[FLAGS]?","_apple":"omena","_both sides":"molemmilta puolilta","_camelCase":"karavaaniTyyli","_convert [STRING] to [TEXTCASE]":"muunna [STRING] muotoon [TEXTCASE]","_count [SUBSTRING] in [STRING]":"merkkien [SUBSTRING] määrä merkkijonossa [STRING]","_count regex /[REGEX]/[FLAGS] in [STRING]":"laske säännöllinen lauseke /[REGEX]/[FLAGS] merkkijonossa [STRING]","_index of [SUBSTRING] in [STRING]":"merkkijonon [SUBSTRING] järjestysnumero merkkijonossa [STRING]","_is [OPERAND1] identical to [OPERAND2]?":"onko [OPERAND1] täysin sama kuin [OPERAND2]?","_is [STRING] [TEXTCASE]?":"onko [STRING] muodossa [TEXTCASE]?","_item [ITEM] of [STRING] matched by regex /[REGEX]/[FLAGS]":"merkkijonon [STRING] [ITEM]. kohde, joka vastaa säännöllistä lauseketta /[REGEX]/[FLAGS]","_item [ITEM] of [STRING] split by [SPLIT]":"[ITEM]. kohde merkkijonosta [STRING] jaettuna merkillä [SPLIT]","_letters [LETTER1] to [LETTER2] of [STRING]":"merkkijonon [STRING] merkit [LETTER1] – [LETTER2]","_lowercase":"pienet kirjaimet","_repeat [STRING] [REPEAT] times":"toista [STRING] [REPEAT] kertaa","_replace [SUBSTRING] in [STRING] with [REPLACE]":"korvaa [SUBSTRING] merkkijonossa [STRING] merkkijonolla [REPLACE]","_replace regex /[REGEX]/[FLAGS] in [STRING] with [REPLACE]":"korvaa säännöllinen lauseke /[REGEX]/[FLAGS] merkkijonossa [STRING] lausekkeella [REPLACE]","_reverse text [STRING]":"teksti [STRING] käänteisenä","_the end":"lopusta","_the start":"alusta","_trim whitespace [STRING] from [METHOD]":"poista turhat sanavälit merkkijonon [STRING] [METHOD]","_unicode [NUM] as letter":"Unicode-koodi [NUM] merkkijonona","_unicode of [STRING]":"merkkijonon [STRING] Unicode-koodi"},"he":{"_Text":"טקסט"},"hu":{"_Text":"Szöveg"},"it":{"_Exactly Title Case":"Maiuscolo stile Titolo Esatto","_MiXeD CaSe":"MaIuScOlO MiSto","_RAndoMCaSe":"MaiUSCoLoCaSUaLe","_Sentence case":"Maiuscolo come frase","_Text":"Testo","_Title Case":"Maiuscolo stile TItolo","_UPPERCASE":"MAIUSCOLO","_[STRING] [POSITION] with [SUBSTRING]?":"[STRING] [POSITION] con [SUBSTRING]","_[STRING] matches regex /[REGEX]/[FLAGS]?":"[STRING] corrisponde a regex /[REGEX]/[FLAGS]","_apple":"mela","_both sides":"entrambi i lati","_camelCase":"maiuscoleInterne","_convert [STRING] to [TEXTCASE]":"converti [STRING] in [TEXTCASE]","_count [SUBSTRING] in [STRING]":"occorrenze di [SUBSTRING] in [STRING]","_count regex /[REGEX]/[FLAGS] in [STRING]":"numero occorrenze di regex /[REGEX]/[FLAGS] in [STRING]","_ends":"finisce","_index of [SUBSTRING] in [STRING]":"posizione di [SUBSTRING] in [STRING]","_is [OPERAND1] identical to [OPERAND2]?":"[OPERAND1] è identico a [OPERAND2]","_is [STRING] [TEXTCASE]?":"[STRING] è [TEXTCASE]","_item [ITEM] of [STRING] matched by regex /[REGEX]/[FLAGS]":"elemento [ITEM] di [STRING] corrispondente a regex /[REGEX]/[FLAGS]","_item [ITEM] of [STRING] split by [SPLIT]":"elemento [ITEM] di [STRING] spezzata da [SPLIT]","_letters [LETTER1] to [LETTER2] of [STRING]":"lettere da [LETTER1] a [LETTER2] di [STRING]","_lowercase":"minuscolo","_matches of [STRING] using regex /[REGEX]/[FLAGS] as array":"corrispondenze di [STRING] usando regex /[REGEX]/[FLAGS] come array","_repeat [STRING] [REPEAT] times":"ripeti [STRING] [REPEAT] volte","_replace [SUBSTRING] in [STRING] with [REPLACE]":"sostituisci [SUBSTRING] con [REPLACE]in [STRING]","_replace regex /[REGEX]/[FLAGS] in [STRING] with [REPLACE]":"sostituisci regex /[REGEX]/[FLAGS] in [STRING] con [REPLACE]","_reverse text [STRING]":"inverti testo [STRING]","_starts":"inizia","_the end":"la fine","_the start":"l'inizio","_trim whitespace [STRING] from [METHOD]":"Rimuovi spazi bianchi di [STRING] da [METHOD]","_unicode [NUM] as letter":"[NUM] unicode come lettera","_unicode of [STRING]":"unicode di [STRING]"},"ja":{"_Exactly Title Case":"正確なタイトルケース","_MiXeD CaSe":"ミックスケース","_RAndoMCaSe":"ランダムケース","_Sentence case":"センテンスケース","_Text":"テキスト","_Title Case":"タイトルケース","_UPPERCASE":"大文字","_apple":"りんご","_camelCase":"キャメルケース","_convert [STRING] to [TEXTCASE]":"[STRING]を[TEXTCASE]に変換する","_count [SUBSTRING] in [STRING]":"[STRING]に[SUBSTRING]が出てきた回数","_index of [SUBSTRING] in [STRING]":"[STRING]の[SUBSTRING]の場所","_is [OPERAND1] identical to [OPERAND2]?":"[OPERAND1]と[OPERAND2]が同一","_is [STRING] [TEXTCASE]?":"[STRING]が[TEXTCASE]","_item [ITEM] of [STRING] split by [SPLIT]":"[STRING]を[SPLIT]で区切ったときの[ITEM]番目","_letters [LETTER1] to [LETTER2] of [STRING]":"[STRING]の[LETTER1]から[LETTER2]文字目","_lowercase":"小文字","_repeat [STRING] [REPEAT] times":"[STRING]を[REPEAT]回繰り返す","_replace [SUBSTRING] in [STRING] with [REPLACE]":"[STRING]の[SUBSTRING]を[REPLACE]で置き換える","_unicode [NUM] as letter":"Unicode[NUM]の文字","_unicode of [STRING]":"[STRING]のUnicode"},"ko":{"_Exactly Title Case":"정확한 제목 표기법 (Abc Abc)","_MiXeD CaSe":"섞인 표기법 (AbC AbCd)","_Text":"텍스트","_Title Case":"제목 표기법 (Abc Abc)","_UPPERCASE":"대문자 표기법 (ABC)","_[STRING] matches regex /[REGEX]/[FLAGS]?":"[STRING]이(가) 정규표현식 /[REGEX]/[FLAGS] 에 일치하는가?","_convert [STRING] to [TEXTCASE]":"[STRING]을(를) [TEXTCASE](으)로 변환하기","_count [SUBSTRING] in [STRING]":"[STRING]에서 [SUBSTRING]의 개수","_count regex /[REGEX]/[FLAGS] in [STRING]":"[STRING]에서 정규표현식 /[REGEX]/[FLAGS] 의 결과 개수","_index of [SUBSTRING] in [STRING]":"[STRING]에서 [SUBSTRING]의 번째","_is [OPERAND1] identical to [OPERAND2]?":"[OPERAND1]이(가) [OPERAND2]와(과) 정확히 같지 않은가?","_is [STRING] [TEXTCASE]?":"[STRING]이(가) [TEXTCASE]인가?","_item [ITEM] of [STRING] matched by regex /[REGEX]/[FLAGS]":"[STRING]에서 정규표현식 /[REGEX]/[FLAGS] 의 [ITEM]번째 결과","_item [ITEM] of [STRING] split by [SPLIT]":"[STRING]을(를) [SPLIT](으)로 나눈 것의 [ITEM]번째","_lowercase":"소문자 표기법 (abc)","_repeat [STRING] [REPEAT] times":"[STRING] 문자열 [REPEAT]번 반복","_replace [SUBSTRING] in [STRING] with [REPLACE]":"[STRING]의 [SUBSTRING]을(를) [REPLACE](으)로 바꾸기","_replace regex /[REGEX]/[FLAGS] in [STRING] with [REPLACE]":"정규표현식 /[REGEX]/[FLAGS] (으)로 [STRING]을(를) [REPLACE](으)로 바꾸기","_unicode [NUM] as letter":"유니코드 문자 [NUM]","_unicode of [STRING]":"[STRING]의 유니코드"},"nb":{"_Text":"Tekst"},"nl":{"_Exactly Title Case":"Alleen Beginhoofdletters","_MiXeD CaSe":"DoOr eLkAaR","_Text":"Tekst","_Title Case":"Alles Met Beginhoofdletter","_UPPERCASE":"HOOFDLETTERS","_[STRING] matches regex /[REGEX]/[FLAGS]?":"[STRING] komt overeen met regex /[REGEX]/[FLAGS]?","_apple":"appel","_convert [STRING] to [TEXTCASE]":"zet [STRING] om naar [TEXTCASE]","_count [SUBSTRING] in [STRING]":"aantal [SUBSTRING] in [STRING]","_count regex /[REGEX]/[FLAGS] in [STRING]":"aantal overeenkomsten van regex /[REGEX]/[FLAGS] met [STRING]","_index of [SUBSTRING] in [STRING]":"positie van [SUBSTRING] in [STRING]","_is [OPERAND1] identical to [OPERAND2]?":"[OPERAND1] identiek aan [OPERAND2]?","_item [ITEM] of [STRING] matched by regex /[REGEX]/[FLAGS]":"item [ITEM] van [STRING] waarmee regex /[REGEX]/[FLAGS] overeenkomt","_item [ITEM] of [STRING] split by [SPLIT]":"item [ITEM] van [STRING] gesplitst door [SPLIT]","_letters [LETTER1] to [LETTER2] of [STRING]":"letters [LETTER1] t/m [LETTER2] van [STRING]","_lowercase":"kleine letters","_repeat [STRING] [REPEAT] times":"herhaal [STRING] [REPEAT] keer","_replace [SUBSTRING] in [STRING] with [REPLACE]":"vervang [SUBSTRING] in [STRING] door [REPLACE]","_replace regex /[REGEX]/[FLAGS] in [STRING] with [REPLACE]":"vervang regex /[REGEX]/[FLAGS] in [STRING] door [REPLACE]","_unicode [NUM] as letter":"unicode [NUM] als letter","_unicode of [STRING]":"unicode van [STRING]"},"pl":{"_Text":"Tekst"},"pt-br":{"_Text":"Texto"},"ru":{"_Exactly Title Case":"Настоящий Тайтл Кейс","_MiXeD CaSe":"СмЕшАнНыЙ КеЙс","_RAndoMCaSe":"СЛуЧайнЫЙРеГисТР","_Sentence case":"Смысловой регистр","_Text":"Текст","_Title Case":"Тайтл Кейс","_UPPERCASE":"ВЕРХНИЙ РЕГИСТР","_[STRING] [POSITION] with [SUBSTRING]?":"[STRING] [POSITION] на [SUBSTRING]?","_[STRING] matches regex /[REGEX]/[FLAGS]?":"[STRING] совпадает с regex'ом /[REGEX]/[FLAGS]?","_apple":"яблоко","_both sides":"обе стороны","_camelCase":"Регистр верблюда","_convert [STRING] to [TEXTCASE]":"сконвертировать строку [STRING] в [TEXTCASE]","_count [SUBSTRING] in [STRING]":"количество [SUBSTRING] в [STRING]","_count regex /[REGEX]/[FLAGS] in [STRING]":"посчитать regex /[REGEX]/[FLAGS] в [STRING]","_ends":"заканчивается","_index of [SUBSTRING] in [STRING]":"индекс [SUBSTRING] в [STRING]","_is [OPERAND1] identical to [OPERAND2]?":"[OPERAND1] идентичный с [OPERAND2]?","_is [STRING] [TEXTCASE]?":"строка [STRING] это [TEXTCASE]?","_item [ITEM] of [STRING] matched by regex /[REGEX]/[FLAGS]":"предмет [ITEM] строки [STRING] совпадающий regex'ом /[REGEX]/[FLAGS]","_item [ITEM] of [STRING] split by [SPLIT]":"предмет [ITEM] строки [STRING] распределённый [SPLIT]","_letters [LETTER1] to [LETTER2] of [STRING]":"буквы с [LETTER1] до [LETTER2] строки [STRING]","_lowercase":"нижний регистр","_matches of [STRING] using regex /[REGEX]/[FLAGS] as array":"совпадения [STRING] используя regex /[REGEX]/[FLAGS] как массив","_repeat [STRING] [REPEAT] times":"повторить [STRING] [REPEAT] раз","_replace [SUBSTRING] in [STRING] with [REPLACE]":"заменить [SUBSTRING] в [STRING] на [REPLACE]","_replace regex /[REGEX]/[FLAGS] in [STRING] with [REPLACE]":"заменить regex /[REGEX]/[FLAGS] в [STRING] на [REPLACE]","_reverse text [STRING]":"перевернуть текст [STRING]","_starts":"начинается","_the end":"конца","_the start":"начала","_trim whitespace [STRING] from [METHOD]":"обрезать пробелы [STRING] из [METHOD]","_unicode [NUM] as letter":"юникод [NUM] как букву","_unicode of [STRING]":"юникод [STRING]"},"sl":{"_Text":"Besedilo"},"tr":{"_Text":"Metin"},"uk":{"_Exactly Title Case":"Точно Заголовком","_MiXeD CaSe":"МіШаНиМи","_RAndoMCaSe":"ВИпадКоВИМи","_Sentence case":"Речення","_Text":"Текст","_Title Case":"Заголовком","_UPPERCASE":"ВЕЛИКИМИ БУКВАМИ","_both sides":"обох сторін","_convert [STRING] to [TEXTCASE]":"зробити [STRING] [TEXTCASE]","_count [SUBSTRING] in [STRING]":"кількість [SUBSTRING] у [STRING]","_index of [SUBSTRING] in [STRING]":"номер [SUBSTRING] у [STRING]","_is [OPERAND1] identical to [OPERAND2]?":"[OPERAND1] ідентична до [OPERAND2]?","_is [STRING] [TEXTCASE]?":"[STRING] є [TEXTCASE]?","_item [ITEM] of [STRING] split by [SPLIT]":"елемент [ITEM] у [STRING] розділеного через [SPLIT]","_letters [LETTER1] to [LETTER2] of [STRING]":"букви з [LETTER1] по [LETTER2] у [STRING]","_lowercase":"малими буквами","_repeat [STRING] [REPEAT] times":"повторити [STRING] [REPEAT] рази","_replace [SUBSTRING] in [STRING] with [REPLACE]":"замінити [SUBSTRING] у [STRING] на [REPLACE]","_reverse text [STRING]":"розвернути [STRING]","_the end":"початку","_the start":"кінця","_trim whitespace [STRING] from [METHOD]":"очистити пробіли у [STRING] з [METHOD]","_unicode [NUM] as letter":"символ юнікоду [NUM] ","_unicode of [STRING]":"юнікод [STRING]"},"zh-cn":{"_Exactly Title Case":"精确标题","_MiXeD CaSe":"混合大小写","_RAndoMCaSe":"随机大小写","_Sentence case":"句首大写","_Text":"文本","_Title Case":"标题","_UPPERCASE":"大写","_[STRING] [POSITION] with [SUBSTRING]?":"[STRING]以[SUBSTRING][POSITION]？","_[STRING] matches regex /[REGEX]/[FLAGS]?":"[STRING] 满足正则 /[REGEX]/[FLAGS]？","_apple":"苹果","_both sides":"两边","_camelCase":"驼峰式命名","_convert [STRING] to [TEXTCASE]":"转换[STRING]为[TEXTCASE]","_count [SUBSTRING] in [STRING]":"[STRING]中[SUBSTRING]的数量","_count regex /[REGEX]/[FLAGS] in [STRING]":"[STRING]中匹配正则 /[REGEX]/[FLAGS]的次数","_ends":"结尾","_index of [SUBSTRING] in [STRING]":"[STRING]中[SUBSTRING]的位置","_is [OPERAND1] identical to [OPERAND2]?":"[OPERAND1]完全等价于[OPERAND2]？","_is [STRING] [TEXTCASE]?":"[STRING]是[TEXTCASE]？","_item [ITEM] of [STRING] matched by regex /[REGEX]/[FLAGS]":"[STRING]中匹配正则 /[REGEX]/[FLAGS]的第[ITEM]个部分","_item [ITEM] of [STRING] split by [SPLIT]":"以[SPLIT]分割[STRING]后的第[ITEM]项","_letters [LETTER1] to [LETTER2] of [STRING]":"[STRING]的第[LETTER1]到第[LETTER2]位","_lowercase":"小写","_matches of [STRING] using regex /[REGEX]/[FLAGS] as array":"[STRING]中匹配正则 /[REGEX]/[FLAGS]的所有部分列表","_repeat [STRING] [REPEAT] times":"重复[REPEAT]个[STRING]","_replace [SUBSTRING] in [STRING] with [REPLACE]":"替换[STRING]中的[SUBSTRING]为[REPLACE]","_replace regex /[REGEX]/[FLAGS] in [STRING] with [REPLACE]":"将[STRING]中匹配正则 /[REGEX]/[FLAGS]的部分替换为[REPLACE]","_reverse text [STRING]":"反转文本[STRING]","_starts":"开头","_the end":"末尾","_the start":"开头","_trim whitespace [STRING] from [METHOD]":"去除[STRING]中[METHOD]的空格","_unicode [NUM] as letter":"unicode[NUM]对应的字符","_unicode of [STRING]":"[STRING]的unicode"}});/* end generated l10n code */(function (Scratch) {
  "use strict";

  const Cast = Scratch.Cast;

  const CaseParam = {
    LOWERCASE: "lowercase",
    UPPERCASE: "uppercase",
    MIXEDCASE: "mixedcase",
    TITLECASE: "titlecase",
    EXACTTITLECASE: "exacttitlecase",
    RANDOMCASE: "randomcase",
    SENTENCECASE: "sentencecase",
    CAMELCASE: "camelcase",
  };

  let splitCache;
  let matchCache;

  class StringsExt {
    constructor() {}

    _initCaseMenu() {
      return [
        {
          text: Scratch.translate({
            default: "lowercase",
            description: "If your language has lowercase, style it accordingly",
          }),
          value: CaseParam.LOWERCASE,
        },
        {
          text: Scratch.translate({
            default: "UPPERCASE",
            description: "If your language has uppercase, style it accordingly",
          }),
          value: CaseParam.UPPERCASE,
        },
        {
          text: Scratch.translate({
            default: "Sentence case",
            description:
              "Starts words after ., !, and ? with captialized letters",
          }),
          value: CaseParam.SENTENCECASE,
        },
        {
          text: Scratch.translate({
            default: "Title Case",
            description:
              "If your language has Title Case, style it accordingly. 'Abc' is title case and exactly title case but 'ABC' is only title case.",
          }),
          value: CaseParam.TITLECASE,
        },
        {
          text: Scratch.translate({
            default: "Exactly Title Case",
            description:
              "If your language has Title Case, style it accordingly. 'Abc' is title case and exactly title case but 'ABC' is only title case.",
          }),
          value: CaseParam.EXACTTITLECASE,
        },
        {
          text: Scratch.translate({
            default: "MiXeD CaSe",
            description:
              "If your language has mixed case, style it accordingly",
          }),
          value: CaseParam.MIXEDCASE,
        },
        {
          text: Scratch.translate({
            default: "RAndoMCaSe",
            description:
              "If your language has randomcase, style it accordingly",
          }),
          value: CaseParam.RANDOMCASE,
        },
        {
          text: Scratch.translate({
            default: "camelCase",
            description:
              "Removes all spaces and capitalizes all words after the first",
          }),
          value: CaseParam.CAMELCASE,
        },
      ];
    }

    getInfo() {
      return {
        // id "text" would conflict with Scratch Lab's Animated Text (lab/text.js)
        id: "strings",
        name: Scratch.translate("Text"),
        blocks: [
          {
            opcode: "letters_of",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate(
              "letters [LETTER1] to [LETTER2] of [STRING]"
            ),
            arguments: {
              LETTER1: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 2,
              },
              LETTER2: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 4,
              },
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "apple",
              },
            },
          },
          {
            opcode: "split",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("item [ITEM] of [STRING] split by [SPLIT]"),
            arguments: {
              ITEM: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 3,
              },
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "apple",
              },
              SPLIT: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "p",
              },
            },
          },
          {
            opcode: "count",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate({
              default: "count [SUBSTRING] in [STRING]",
              description:
                "Counts how many time [SUBSTRING] appears in [STRING]",
            }),
            arguments: {
              SUBSTRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "p",
              },
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "apple",
              },
            },
          },
          {
            opcode: "indexof",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate({
              default: "index of [SUBSTRING] in [STRING]",
              description: "Reports where [SUBSTRING] appears in [STRING]",
            }),
            arguments: {
              SUBSTRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "p",
              },
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "apple",
              },
            },
          },

          "---",

          {
            opcode: "replace",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate(
              "replace [SUBSTRING] in [STRING] with [REPLACE]"
            ),
            arguments: {
              SUBSTRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "world",
              },
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "Hello world!",
              },
              REPLACE: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "fellow Scratchers",
              },
            },
          },
          {
            opcode: "repeat",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("repeat [STRING] [REPEAT] times"),
            arguments: {
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "apple ",
              },
              REPEAT: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 3,
              },
            },
          },

          "---",

          {
            opcode: "unicodeof",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("unicode of [STRING]"),
            arguments: {
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "A",
              },
            },
          },
          {
            opcode: "unicodefrom",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("unicode [NUM] as letter"),
            arguments: {
              NUM: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 65,
              },
            },
          },

          "---",
          {
            opcode: "replaceRegex",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate(
              "replace regex /[REGEX]/[FLAGS] in [STRING] with [REPLACE]"
            ),
            arguments: {
              REGEX: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: ".",
              },
              FLAGS: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "g",
              },
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "Hello world!",
              },
              REPLACE: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "$&$&",
              },
            },
          },
          {
            opcode: "matchRegex",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate({
              default:
                "item [ITEM] of [STRING] matched by regex /[REGEX]/[FLAGS]",
              description:
                "/[REGEX]/ is supposed to match the syntax that some actual programming languages used for regular expressions.",
            }),
            arguments: {
              ITEM: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: 1,
              },
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "Hello world!",
              },
              REGEX: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "(.) (.{2})",
              },
              FLAGS: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "g",
              },
            },
          },
          {
            opcode: "matchRegexJSON",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate({
              default:
                "matches of [STRING] using regex /[REGEX]/[FLAGS] as array",
              description:
                "/[REGEX]/ is supposed to match the syntax that some actual programming languages used for regular expressions.",
            }),
            arguments: {
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "Hello world!",
              },
              REGEX: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "(.) (.{2})",
              },
              FLAGS: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "g",
              },
            },
          },
          {
            opcode: "testRegex",
            blockType: Scratch.BlockType.BOOLEAN,
            text: Scratch.translate({
              default: "[STRING] matches regex /[REGEX]/[FLAGS]?",
              description:
                "/[REGEX]/ is supposed to match the syntax that some actual programming languages used for regular expressions.",
            }),
            arguments: {
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "Hello world!",
              },
              REGEX: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "hello",
              },
              FLAGS: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "i",
              },
            },
          },
          {
            opcode: "countRegex",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate({
              default: "count regex /[REGEX]/[FLAGS] in [STRING]",
              description:
                "/[REGEX]/ is supposed to match the syntax that some actual programming languages used for regular expressions.",
            }),
            arguments: {
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "Hello world!",
              },
              REGEX: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "[AEIOU]",
              },
              FLAGS: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "i",
              },
            },
          },

          "---",

          {
            opcode: "identical",
            blockType: Scratch.BlockType.BOOLEAN,
            text: Scratch.translate("is [OPERAND1] identical to [OPERAND2]?"),
            arguments: {
              OPERAND1: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "A",
              },
              OPERAND2: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "a",
              },
            },
          },

          "---",

          {
            opcode: "isCase",
            blockType: Scratch.BlockType.BOOLEAN,
            text: Scratch.translate({
              default: "is [STRING] [TEXTCASE]?",
              description: "Example block context: <is [hello] [lowercase] ?>",
            }),
            arguments: {
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "apple",
              },
              TEXTCASE: {
                type: Scratch.ArgumentType.STRING,
                menu: "textCase",
                defaultValue: CaseParam.LOWERCASE,
              },
            },
          },
          {
            opcode: "toCase",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate({
              default: "convert [STRING] to [TEXTCASE]",
              description:
                "Example block context: (convert [HELLO] to [lowercase])",
            }),
            arguments: {
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "apple",
              },
              TEXTCASE: {
                type: Scratch.ArgumentType.STRING,
                menu: "textCase",
                defaultValue: CaseParam.UPPERCASE,
              },
            },
          },

          "---",
          {
            opcode: "posWith",
            blockType: Scratch.BlockType.BOOLEAN,
            text: Scratch.translate({
              default: "[STRING] [POSITION] with [SUBSTRING]?",
              description:
                "[POSITION] is a dropdown with 'starts' and 'ends'. The block then takes the form '[STRING] starts with [SUBSTRING]?' or '[STRING] ends with [SUBSTRING]?'",
            }),
            arguments: {
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "turbowarp",
              },
              POSITION: {
                type: Scratch.ArgumentType.STRING,
                menu: "positions",
              },
              SUBSTRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "turbo",
              },
            },
          },

          "---",

          {
            opcode: "reverse",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("reverse text [STRING]"),
            arguments: {
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: Scratch.translate("apple"),
              },
            },
          },

          "---",

          {
            opcode: "trim",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("trim whitespace [STRING] from [METHOD]"),
            arguments: {
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: `    ${Scratch.translate("apple")}    `,
              },
              METHOD: {
                type: Scratch.ArgumentType.STRING,
                menu: "trimMethod",
              },
            },
          },
        ],
        menus: {
          textCase: {
            acceptReporters: true,
            items: this._initCaseMenu(),
          },
          positions: {
            acceptReporters: true,
            items: [
              {
                text: Scratch.translate("starts"),
                value: "starts",
              },
              {
                text: Scratch.translate("ends"),
                value: "ends",
              },
            ],
          },
          trimMethod: {
            acceptReporters: true,
            items: [
              {
                text: Scratch.translate("both sides"),
                value: "both",
              },
              {
                text: Scratch.translate("the end"),
                value: "end",
              },
              {
                text: Scratch.translate("the start"),
                value: "start",
              },
            ],
          },
        },
      };
    }

    identical(args, util) {
      // Purposefully no casting, because
      // types ARE differentiated in this block
      return args.OPERAND1 === args.OPERAND2;
    }

    unicodeof(args, util) {
      const chars = Array.from(Cast.toString(args.STRING));
      return chars.map((char) => char.charCodeAt(0)).join(" ");
    }

    unicodefrom(args, util) {
      return String.fromCharCode(Cast.toNumber(args.NUM));
    }

    letters_of(args, util) {
      const string = Cast.toString(args.STRING);
      const letter1 = Cast.toNumber(args.LETTER1);
      const letter2 = Cast.toNumber(args.LETTER2);
      return string.substring(letter1 - 1, letter2);
    }

    _caseInsensitiveRegex(str) {
      return new RegExp(str.replaceAll(/[^a-zA-Z0-9]/g, "\\$&"), "gi");
    }

    split(args, util) {
      const string = Cast.toString(args.STRING);
      const split = Cast.toString(args.SPLIT);
      const item = Cast.toNumber(args.ITEM);

      // Cache the last split
      if (
        !(
          splitCache &&
          splitCache.string === string &&
          splitCache.split === split
        )
      ) {
        const regex = this._caseInsensitiveRegex(split);

        splitCache = {
          string,
          split,
          arr: string.split(regex),
        };
      }
      return splitCache.arr[item - 1] || "";
    }

    count(args, util) {
      // Fill cache
      this.split(
        {
          SPLIT: args.SUBSTRING,
          STRING: args.STRING,
          ITEM: 0,
        },
        util
      );
      return splitCache.arr.length - 1 || 0;
    }

    replace(args, util) {
      const string = Cast.toString(args.STRING);
      const substring = Cast.toString(args.SUBSTRING);
      const replace = Cast.toString(args.REPLACE);

      const regex = this._caseInsensitiveRegex(substring);

      return string.replace(regex, replace);
    }

    indexof(args, util) {
      // .toLowerCase() for case insensitivity
      const string = Cast.toString(args.STRING).toLowerCase();
      const substring = Cast.toString(args.SUBSTRING).toLowerCase();

      // Since both arguments are casted to strings beforehand,
      // we don't have to worry about type differences
      // like in the item number of in list block
      const found = string.indexOf(substring);

      // indexOf returns -1 when no matches are found, we can just +1
      return found + 1;
    }

    repeat(args, util) {
      const string = Cast.toString(args.STRING);
      const repeat = Math.floor(Cast.toNumber(args.REPEAT));
      if (repeat < 0 || !Number.isFinite(repeat)) {
        return "";
      }
      return string.repeat(repeat);
    }

    replaceRegex(args, util) {
      try {
        const string = Cast.toString(args.STRING);
        const replacer = Cast.toString(args.REPLACE);
        const regex = Cast.toString(args.REGEX);
        const flags = Cast.toString(args.FLAGS);

        return string.replace(new RegExp(regex, flags), replacer);
      } catch (e) {
        console.error(e);
        return "";
      }
    }

    matchRegex(args, util) {
      try {
        const string = Cast.toString(args.STRING);
        const uncleanRegex = Cast.toString(args.REGEX);
        const flags = Cast.toString(args.FLAGS);
        const item = Cast.toNumber(args.ITEM);

        // Cache the last matched string
        if (
          !(
            matchCache &&
            matchCache.string === string &&
            matchCache.regex === uncleanRegex &&
            matchCache.flags === flags
          )
        ) {
          const newFlags = flags.includes("g") ? flags : flags + "g";
          const regex = new RegExp(uncleanRegex, newFlags);

          matchCache = {
            string,
            regex: uncleanRegex,
            flags,
            arr: string.match(regex) || [],
          };
        }
        return matchCache.arr[item - 1] || "";
      } catch (e) {
        console.error(e);
        return "";
      }
    }
    matchRegexJSON(args, util) {
      // matchRegex but it returns an array
      try {
        const string = Cast.toString(args.STRING);
        const uncleanRegex = Cast.toString(args.REGEX);
        const flags = Cast.toString(args.FLAGS);

        // Cache the last matched string
        if (
          !(
            matchCache &&
            matchCache.string === string &&
            matchCache.regex === uncleanRegex &&
            matchCache.flags === flags
          )
        ) {
          const newFlags = flags.includes("g") ? flags : flags + "g";
          const regex = new RegExp(uncleanRegex, newFlags);

          matchCache = {
            string,
            regex: uncleanRegex,
            flags,
            arr: string.match(regex) || [],
          };
        }
        return JSON.stringify(matchCache.arr) || "[]";
      } catch (e) {
        console.error(e);
        return "";
      }
    }

    countRegex(args, util) {
      // Fill cache
      // (ITEM is casted into 0,
      // but we don't care about the return value)
      this.matchRegex(args, util);
      return matchCache.arr.length || 0;
    }

    testRegex(args, util) {
      try {
        const string = Cast.toString(args.STRING);
        const regex = Cast.toString(args.REGEX);
        const flags = Cast.toString(args.FLAGS);

        return new RegExp(regex, flags).test(string);
      } catch (e) {
        console.error(e);
        return false;
      }
    }

    isCase(args, util) {
      const string = Cast.toString(args.STRING);
      const textCase = Cast.toString(args.TEXTCASE);
      switch (textCase) {
        case CaseParam.LOWERCASE:
          return string.toLowerCase() === string;
        case CaseParam.UPPERCASE:
          return string.toUpperCase() === string;
        case CaseParam.MIXEDCASE:
          return !(
            string.toUpperCase() === string || string.toLowerCase() === string
          );
        case CaseParam.TITLECASE:
          return string.split(/\b/g).every((word) => {
            if (!word) return true;
            const titleCased = word[0].toUpperCase() + word.substring(1);
            return word === titleCased;
          });
        case CaseParam.EXACTTITLECASE:
          return string.split(/\b/g).every((word) => {
            if (!word) return true;
            const titleCased =
              word[0].toUpperCase() + word.substring(1).toLowerCase();
            return word === titleCased;
          });
        case CaseParam.CAMELCASE:
          return /^[^A-Z\s][^\s]*$/.test(string);
        case CaseParam.RANDOMCASE:
          return true;
        case CaseParam.SENTENCECASE:
          return /^[A-Z][^?.!]*(?:[?.!]\s+[A-Z][^?.!]*)*$/.test(string);
        default:
          return false;
      }
    }

    toCase(args, util) {
      const string = Cast.toString(args.STRING);
      const textCase = Cast.toString(args.TEXTCASE);
      let workingText = "";
      let sentenceCapitalFlag = false;
      switch (textCase) {
        case CaseParam.LOWERCASE:
          return string.toLowerCase();
        case CaseParam.UPPERCASE:
          return string.toUpperCase();
        case CaseParam.MIXEDCASE:
          return Array.from(string)
            .map((char, index) =>
              index % 2 === 0 ? char.toUpperCase() : char.toLowerCase()
            )
            .join("");
        case CaseParam.TITLECASE:
          return string
            .split(/\b/g)
            .map((word) => {
              if (!word) return "";
              return word[0].toUpperCase() + word.substring(1);
            })
            .join("");
        case CaseParam.EXACTTITLECASE:
          return string
            .split(/\b/g)
            .map((word) => {
              if (!word) return "";
              return word[0].toUpperCase() + word.substring(1).toLowerCase();
            })
            .join("");
        case CaseParam.SENTENCECASE:
          for (let i = 0; i < string.length; i++) {
            if (
              /^\s*$/.test(string[i - 1] ?? " ") &&
              !sentenceCapitalFlag &&
              string[i].toUpperCase() != string[i].toLowerCase()
            ) {
              workingText += string[i].toUpperCase();
              sentenceCapitalFlag = true;
            } else {
              if (string[i] == "." || string[i] == "!" || string[i] == "?") {
                sentenceCapitalFlag = false;
              }
              workingText += string[i].toLowerCase();
            }
          }
          return workingText;
        case CaseParam.RANDOMCASE:
          for (let i = 0; i < string.length; i++) {
            if (Math.random() > 0.5) {
              workingText += string[i].toUpperCase();
            } else {
              workingText += string[i].toLowerCase();
            }
          }
          return workingText;
        case CaseParam.CAMELCASE:
          for (let i = 0; i < string.length; i++) {
            if (/^\s*$/.test(string[i - 1] ?? "x")) {
              workingText += string[i].toUpperCase();
            } else {
              workingText += string[i].toLowerCase();
            }
          }
          return workingText.replace(/\s/g, "");
        default:
          return string;
      }
    }
    posWith(args) {
      const STRING = Cast.toString(args.STRING);
      const SUBSTRING = Cast.toString(args.SUBSTRING);
      if (Cast.toString(args.POSITION) === "starts") {
        return STRING.startsWith(SUBSTRING);
      }
      return STRING.endsWith(SUBSTRING);
    }
    reverse(args) {
      return Array.from(Cast.toString(args.STRING)).reverse().join("");
    }
    trim(args) {
      const STRING = Cast.toString(args.STRING);
      switch (Cast.toString(args.METHOD)) {
        case "start":
          return STRING.trimStart();
        case "end":
          return STRING.trimEnd();
        case "both":
        default:
          return STRING.trim();
      }
    }
  }

  Scratch.extensions.register(new StringsExt());
})(Scratch);
