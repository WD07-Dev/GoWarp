package extension

import (
	e "gowarp/core/extension"
	"math"
	"reflect"
	"testing"
)

// These expected values were captured from the preserved JavaScript extension.
// Tests use embedded cases and never load a reference directory or JS engine.
func TestDeclaredBlockSemantics(t *testing.T) {
	cases := []struct {
		Op   string
		Args map[string]any
		Want any
	}{
		{"strings_indexof", map[string]any{"STRING": "A😀B", "SUBSTRING": "b"}, float64(4)},
		{"strings_indexof", map[string]any{"STRING": "Hello", "SUBSTRING": "LL"}, float64(3)},
		{"strings_indexof", map[string]any{"STRING": "Hello", "SUBSTRING": "missing"}, float64(0)},
		{"strings_indexof", map[string]any{"STRING": "Hello", "SUBSTRING": ""}, float64(1)},
		{"strings_indexof", map[string]any{"STRING": "İx", "SUBSTRING": "x"}, float64(3)},
		{"strings_posWith", map[string]any{"STRING": "TurboWarp", "SUBSTRING": "Turbo", "POSITION": "starts"}, true},
		{"strings_posWith", map[string]any{"STRING": "TurboWarp", "SUBSTRING": "warp", "POSITION": "ends"}, false},
		{"strings_posWith", map[string]any{"STRING": "TurboWarp", "SUBSTRING": "Warp", "POSITION": "ends"}, true},
		{"strings_trim", map[string]any{"STRING": "\ufeff \tHello\n ", "METHOD": "start"}, "Hello\n "},
		{"strings_trim", map[string]any{"STRING": " Hello\ufeff", "METHOD": "end"}, " Hello"},
		{"strings_trim", map[string]any{"STRING": " Hello", "METHOD": "both"}, " Hello"},
		{"strings_trim", map[string]any{"STRING": "  Hello　", "METHOD": "both"}, "Hello"},
		{"strings_toCase", map[string]any{"STRING": "hELLO wORLD. how ARE you? fine!", "TEXTCASE": "lowercase"}, "hello world. how are you? fine!"},
		{"strings_toCase", map[string]any{"STRING": "Straße ΟΣ İ", "TEXTCASE": "lowercase"}, "straße ος i̇"},
		{"strings_toCase", map[string]any{"STRING": "hELLO wORLD. how ARE you? fine!", "TEXTCASE": "uppercase"}, "HELLO WORLD. HOW ARE YOU? FINE!"},
		{"strings_toCase", map[string]any{"STRING": "Straße ΟΣ İ", "TEXTCASE": "uppercase"}, "STRASSE ΟΣ İ"},
		{"strings_toCase", map[string]any{"STRING": "hELLO wORLD. how ARE you? fine!", "TEXTCASE": "mixedcase"}, "HeLlO WoRlD. hOw aRe yOu? FiNe!"},
		{"strings_toCase", map[string]any{"STRING": "Straße ΟΣ İ", "TEXTCASE": "mixedcase"}, "StRaSSe οΣ İ"},
		{"strings_toCase", map[string]any{"STRING": "hELLO wORLD. how ARE you? fine!", "TEXTCASE": "titlecase"}, "HELLO WORLD. How ARE You? Fine!"},
		{"strings_toCase", map[string]any{"STRING": "Straße ΟΣ İ", "TEXTCASE": "titlecase"}, "StraSSE ΟΣ İ"},
		{"strings_toCase", map[string]any{"STRING": "hELLO wORLD. how ARE you? fine!", "TEXTCASE": "exacttitlecase"}, "Hello World. How Are You? Fine!"},
		{"strings_toCase", map[string]any{"STRING": "Straße ΟΣ İ", "TEXTCASE": "exacttitlecase"}, "StraSSE ος i̇"},
		{"strings_toCase", map[string]any{"STRING": "hELLO wORLD. how ARE you? fine!", "TEXTCASE": "sentencecase"}, "Hello world. How are you? Fine!"},
		{"strings_toCase", map[string]any{"STRING": "Straße ΟΣ İ", "TEXTCASE": "sentencecase"}, "Straße οσ i̇"},
		{"strings_toCase", map[string]any{"STRING": "hELLO wORLD. how ARE you? fine!", "TEXTCASE": "camelcase"}, "helloWorld.HowAreYou?Fine!"},
		{"strings_toCase", map[string]any{"STRING": "Straße ΟΣ İ", "TEXTCASE": "camelcase"}, "straßeΟσİ"},
		{"strings_toCase", map[string]any{"STRING": "hELLO wORLD. how ARE you? fine!", "TEXTCASE": "unknown"}, "hELLO wORLD. how ARE you? fine!"},
		{"strings_toCase", map[string]any{"STRING": "Straße ΟΣ İ", "TEXTCASE": "unknown"}, "Straße ΟΣ İ"},
		{"strings_isCase", map[string]any{"STRING": "ABC xyz", "TEXTCASE": "mixedcase"}, true},
		{"strings_isCase", map[string]any{"STRING": "ABC Xyz", "TEXTCASE": "titlecase"}, true},
		{"strings_isCase", map[string]any{"STRING": "ABC Xyz", "TEXTCASE": "exacttitlecase"}, false},
		{"strings_isCase", map[string]any{"STRING": "Hello World", "TEXTCASE": "exacttitlecase"}, true},
		{"strings_isCase", map[string]any{"STRING": "Hello. World", "TEXTCASE": "sentencecase"}, true},
		{"strings_isCase", map[string]any{"STRING": "Hello.", "TEXTCASE": "sentencecase"}, false},
		{"strings_isCase", map[string]any{"STRING": "helloWorld", "TEXTCASE": "camelcase"}, true},
		{"strings_isCase", map[string]any{"STRING": "HelloWorld", "TEXTCASE": "camelcase"}, false},
		{"strings_isCase", map[string]any{"STRING": "", "TEXTCASE": "camelcase"}, false},
		{"strings_isCase", map[string]any{"STRING": "123", "TEXTCASE": "lowercase"}, true},
		{"strings_isCase", map[string]any{"STRING": "anything", "TEXTCASE": "randomcase"}, true},
		{"strings_isCase", map[string]any{"STRING": "value", "TEXTCASE": "unknown"}, false},
		{"strings_replaceRegex", map[string]any{"STRING": "aB ab", "REGEX": "(a)(b)", "FLAGS": "i", "REPLACE": "$2$1 $$ $& $` $'"}, "Ba $ aB   ab ab"},
		{"strings_replaceRegex", map[string]any{"STRING": "ab ab", "REGEX": "(a)(b)", "FLAGS": "g", "REPLACE": "$2$1-$12-$02-$9"}, "ba-a2-b-$9 ba-a2-b-$9"},
		{"strings_replaceRegex", map[string]any{"STRING": "a ab", "REGEX": "(a)(b)?", "FLAGS": "g", "REPLACE": "$2:$1"}, ":a b:a"},
		{"strings_replaceRegex", map[string]any{"STRING": "aba", "REGEX": "a", "FLAGS": "", "REPLACE": "x"}, "xba"},
		{"strings_replaceRegex", map[string]any{"STRING": "aba", "REGEX": "a", "FLAGS": "g", "REPLACE": "x"}, "xbx"},
		{"strings_replaceRegex", map[string]any{"STRING": "ab", "REGEX": "", "FLAGS": "g", "REPLACE": "-"}, "-a-b-"},
		{"strings_matchRegexJSON", map[string]any{"STRING": "a", "REGEX": "a*", "FLAGS": ""}, "[\"a\",\"\"]"},
		{"strings_matchRegexJSON", map[string]any{"STRING": "abc", "REGEX": "", "FLAGS": ""}, "[\"\",\"\",\"\",\"\"]"},
		{"strings_matchRegexJSON", map[string]any{"STRING": "a\nb", "REGEX": "^|$", "FLAGS": "gm"}, "[\"\",\"\",\"\",\"\"]"},
		{"strings_matchRegexJSON", map[string]any{"STRING": "a b ab", "REGEX": "\\b[a-z]+\\b", "FLAGS": ""}, "[\"a\",\"b\",\"ab\"]"},
		{"strings_matchRegexJSON", map[string]any{"STRING": "1a22b333", "REGEX": "\\d+", "FLAGS": ""}, "[\"1\",\"22\",\"333\"]"},
		{"strings_matchRegexJSON", map[string]any{"STRING": "a\r\nb", "REGEX": ".", "FLAGS": ""}, "[\"a\",\"b\"]"},
		{"strings_matchRegexJSON", map[string]any{"STRING": "a\r\nb", "REGEX": ".", "FLAGS": "s"}, "[\"a\",\"\\r\",\"\\n\",\"b\"]"},
		{"strings_matchRegexJSON", map[string]any{"STRING": " \t\u000b\f\r\n", "REGEX": "\\s", "FLAGS": ""}, "[\" \",\"\\t\",\"\\u000b\",\"\\f\",\"\\r\",\"\\n\"]"},
		{"strings_matchRegexJSON", map[string]any{"STRING": "a-zb", "REGEX": "[a\\-z]", "FLAGS": ""}, "[\"a\",\"-\",\"z\"]"},
		{"strings_matchRegexJSON", map[string]any{"STRING": "aA", "REGEX": "\\u0061", "FLAGS": "i"}, "[\"a\",\"A\"]"},
		{"strings_matchRegexJSON", map[string]any{"STRING": "aa ba", "REGEX": "a+", "FLAGS": "y"}, "[\"aa\"]"},
		{"strings_matchRegex", map[string]any{"STRING": "a ab abc", "REGEX": "[a-z]+", "FLAGS": "", "ITEM": float64(2)}, "ab"},
		{"strings_matchRegex", map[string]any{"STRING": "a ab abc", "REGEX": "[a-z]+", "FLAGS": "", "ITEM": float64(1.5)}, ""},
		{"strings_countRegex", map[string]any{"STRING": "a", "REGEX": "a*", "FLAGS": ""}, float64(2)},
		{"strings_testRegex", map[string]any{"STRING": "xabc", "REGEX": "abc", "FLAGS": "y"}, false},
		{"strings_testRegex", map[string]any{"STRING": "abc", "REGEX": "a", "FLAGS": "d"}, true},
		{"strings_testRegex", map[string]any{"STRING": "abc", "REGEX": "[", "FLAGS": ""}, false},
		{"strings_replaceRegex", map[string]any{"STRING": "abc", "REGEX": "[", "FLAGS": "", "REPLACE": "x"}, ""},
		{"strings_matchRegexJSON", map[string]any{"STRING": "abc", "REGEX": "a", "FLAGS": "gg"}, ""},
	}
	handlers := map[string]e.Handler{}
	for _, b := range New().Blocks {
		handlers[b.Opcode] = b.Handler
	}
	for _, tc := range cases {
		t.Run(tc.Op, func(t *testing.T) {
			handler := handlers[tc.Op]
			if handler == nil {
				t.Fatalf("missing opcode %s", tc.Op)
			}
			got, err := handler(&e.Context{Args: tc.Args})
			equal := reflect.DeepEqual(got.Value, tc.Want)
			if a, ok := got.Value.(float64); ok {
				if b, ok := tc.Want.(float64); ok {
					equal = a == b || math.Abs(a-b) <= math.Abs(b)*1e-14
				}
			}
			if err != nil || !equal {
				t.Fatalf("args %#v: got %#v (%T), %v; want %#v (%T)", tc.Args, got.Value, got.Value, err, tc.Want, tc.Want)
			}
		})
	}
}
func TestDeclaredBlockMonitorMetadata(t *testing.T) {
	want := map[string]struct{ Kind, Text string }{
		"strings_letters_of":     {"reporter", "letters [LETTER1] to [LETTER2] of [STRING]"},
		"strings_split":          {"reporter", "item [ITEM] of [STRING] split by [SPLIT]"},
		"strings_count":          {"reporter", "count [SUBSTRING] in [STRING]"},
		"strings_indexof":        {"reporter", "index of [SUBSTRING] in [STRING]"},
		"strings_replace":        {"reporter", "replace [SUBSTRING] in [STRING] with [REPLACE]"},
		"strings_repeat":         {"reporter", "repeat [STRING] [REPEAT] times"},
		"strings_unicodeof":      {"reporter", "unicode of [STRING]"},
		"strings_unicodefrom":    {"reporter", "unicode [NUM] as letter"},
		"strings_replaceRegex":   {"reporter", "replace regex /[REGEX]/[FLAGS] in [STRING] with [REPLACE]"},
		"strings_matchRegex":     {"reporter", "item [ITEM] of [STRING] matched by regex /[REGEX]/[FLAGS]"},
		"strings_matchRegexJSON": {"reporter", "matches of [STRING] using regex /[REGEX]/[FLAGS] as array"},
		"strings_testRegex":      {"boolean", "[STRING] matches regex /[REGEX]/[FLAGS]?"},
		"strings_countRegex":     {"reporter", "count regex /[REGEX]/[FLAGS] in [STRING]"},
		"strings_identical":      {"boolean", "is [OPERAND1] identical to [OPERAND2]?"},
		"strings_isCase":         {"boolean", "is [STRING] [TEXTCASE]?"},
		"strings_toCase":         {"reporter", "convert [STRING] to [TEXTCASE]"},
		"strings_posWith":        {"boolean", "[STRING] [POSITION] with [SUBSTRING]?"},
		"strings_reverse":        {"reporter", "reverse text [STRING]"},
		"strings_trim":           {"reporter", "trim whitespace [STRING] from [METHOD]"},
	}
	for _, b := range New().Blocks {
		if spec, ok := want[b.Opcode]; ok {
			if b.Kind != spec.Kind || b.Text != spec.Text {
				t.Errorf("%s: kind %q text %q, want %q %q", b.Opcode, b.Kind, b.Text, spec.Kind, spec.Text)
			}
			delete(want, b.Opcode)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing declared blocks: %v", want)
	}
}

func TestUnsupportedJavaScriptRegexIsExplicit(t *testing.T) {
	for _, tc := range []struct{ pattern, flags, text string }{
		{`(?=a)a`, "", "a"}, {`(a)\1`, "", "aa"}, {`.`, "u", "a"},
		{`.`, "", "😀"}, {`(?<name>a)`, "", "a"}, {`(a(b)?)+`, "", "aba"},
		{`[[:alpha:]]`, "", "a"}, {`[\S]`, "", "a"}, {`^a`, "m", "x\ra"},
	} {
		for _, op := range []string{"replaceRegex", "matchRegex", "matchRegexJSON", "testRegex", "countRegex"} {
			handler := stringBlock(t, New(), "strings_"+op)
			_, err := handler(&e.Context{Args: map[string]any{"STRING": tc.text, "REGEX": tc.pattern, "FLAGS": tc.flags, "ITEM": float64(1), "REPLACE": "x"}})
			if err == nil {
				t.Errorf("%s /%s/%s on %q silently accepted unsupported JS behavior", op, tc.pattern, tc.flags, tc.text)
			}
		}
	}
}

func TestRegexJSONAndInvalidPatternCache(t *testing.T) {
	ext := New()
	match := stringBlock(t, ext, "strings_matchRegexJSON")
	args := map[string]any{"STRING": "<&>", "REGEX": ".", "FLAGS": ""}
	got, err := match(&e.Context{Args: args})
	if err != nil || got.Value != `["<","&",">"]` {
		t.Fatalf("JSON = %v, %v", got.Value, err)
	}
	count := stringBlock(t, ext, "strings_countRegex")
	args["REGEX"] = "["
	got, err = count(&e.Context{Args: args})
	if err != nil || got.Value != float64(3) {
		t.Fatalf("invalid regex should preserve the last successful match cache: %v, %v", got.Value, err)
	}
	args["REGEX"] = "none"
	got, err = count(&e.Context{Args: args})
	if err != nil || got.Value != float64(0) {
		t.Fatalf("new valid regex should reset matches: %v, %v", got.Value, err)
	}
}
