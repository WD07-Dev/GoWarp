package extension

import (
	e "gowarp/core/extension"
	"math"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestAdditionalProjectOperations(t *testing.T) {
	for _, tc := range []struct {
		op   string
		args map[string]any
		want any
	}{
		{"strings_letters_of", map[string]any{"STRING": "abcdef", "LETTER1": float64(5), "LETTER2": float64(2)}, "cd"},
		{"strings_letters_of", map[string]any{"STRING": "A😀B", "LETTER1": float64(2), "LETTER2": float64(3)}, "😀"},
		{"strings_count", map[string]any{"STRING": "aaAA", "SUBSTRING": "aa"}, float64(2)},
		{"strings_count", map[string]any{"STRING": "abc", "SUBSTRING": ""}, float64(2)},
		{"strings_repeat", map[string]any{"STRING": "ha", "REPEAT": float64(2.8)}, "haha"},
		{"strings_reverse", map[string]any{"STRING": "A😀B"}, "B😀A"},
	} {
		t.Run(tc.op, func(t *testing.T) {
			handler := stringBlock(t, New(), tc.op)
			got, err := handler(&e.Context{Args: tc.args, State: map[string]any{}})
			if err != nil || !reflect.DeepEqual(got.Value, tc.want) {
				t.Fatalf("got %v (%T), %v; want %v (%T)", got.Value, got.Value, err, tc.want, tc.want)
			}
		})
	}
}

func stringBlock(t testing.TB, ext e.Extension, opcode string) e.Handler {
	t.Helper()
	for _, block := range ext.Blocks {
		if block.Opcode == opcode {
			return block.Handler
		}
	}
	t.Fatalf("missing opcode %s", opcode)
	return nil
}

func TestSplitResultsAfterInputChanges(t *testing.T) {
	ext := New()
	split := stringBlock(t, ext, "strings_split")
	for _, tc := range []struct {
		text, separator string
		item            float64
		want            string
	}{
		{"10||30|", "|", 2, ""},
		{"10||30|", "|", 3, "30"},
		{"10||30|", "|", 4, ""},
		{"10||30|", "||", 2, "30|"},
		{"changed|value", "|", 1, "changed"},
		{"aXXbxxC", "xX", 2, "b"},
		{"AΣBσCςD", "σ", 4, "D"},
		{"a.*b.*c", ".*", 2, "b"},
		{"A😀B", "", 2, "😀"},
		{"", "", 1, ""},
		{"", "|", 1, ""},
		{"a|b", "|", math.NaN(), ""},
		{"a|b", "|", math.Inf(1), ""},
		{"a|b", "|", 1.5, ""},
		{"a|b", "|", 0, ""},
		{"a|b", "|", 3, ""},
	} {
		got, err := split(&e.Context{Args: map[string]any{"STRING": tc.text, "SPLIT": tc.separator, "ITEM": tc.item}})
		if err != nil || got.Value != tc.want {
			t.Fatalf("split(%q, %q, %v) = %v, %v; want %q", tc.text, tc.separator, tc.item, got.Value, err, tc.want)
		}
	}
}

func TestRepeatedSplitAllocations(t *testing.T) {
	split := stringBlock(t, New(), "strings_split")
	ctx := &e.Context{Args: map[string]any{
		"STRING": strings.Repeat("123|456|", 64), "SPLIT": "|", "ITEM": float64(2),
	}}
	allocs := testing.AllocsPerRun(100, func() {
		result, err := split(ctx)
		if err != nil || result.Value != "456" {
			t.Fatalf("split = %v, %v", result.Value, err)
		}
	})
	if allocs > 2 {
		t.Fatalf("repeated field lookup allocates %.0f objects; want at most 2", allocs)
	}
}

func TestSplitCachePreservesCaseInsensitiveResults(t *testing.T) {
	var cache splitCache
	for _, separator := range []string{"_", "*", ".", "[]", "123", "", "a", "σ", "한"} {
		for _, text := range []string{"", "A_a_aa", "__12__34_", "a*a*", ".*[]123", "AΣBσCςD", "한글한", "A😀B", string([]byte{0xff, '_', 0xfe})} {
			want := regexp.MustCompile("(?i)"+regexp.QuoteMeta(separator)).Split(text, -1)
			got := cache.parts(text, separator)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("split(%q, %q) = %q; want %q", text, separator, got, want)
			}
		}
	}
}

func TestSplitCacheDoesNotRetainLargeInputs(t *testing.T) {
	var cache splitCache
	cache.parts("first_second", "_")
	large := strings.Repeat("x", 128<<10)
	if got := cache.parts(large+"_end", "_"); len(got) != 2 || got[1] != "end" {
		t.Fatalf("large split has %d fields", len(got))
	}
	if len(cache.text) != 0 || len(cache.result) != 0 {
		t.Fatal("large input retained in split cache")
	}
	cache.parts(strings.Repeat("_", 8000), "_")
	if len(cache.result) != 0 {
		t.Fatal("large field array retained in split cache")
	}
	if got := cache.parts("new_value", "_"); !reflect.DeepEqual(got, []string{"new", "value"}) {
		t.Fatalf("split after large input = %q", got)
	}
}

func BenchmarkSplitFields(b *testing.B) {
	for _, changing := range []bool{false, true} {
		name := "repeated"
		if changing {
			name = "changing"
		}
		b.Run(name, func(b *testing.B) {
			split := stringBlock(b, New(), "strings_split")
			inputs := []string{"12|34|56|78|90|12|34|56", "13|35|57|79|91|13|35|57"}
			ctx := &e.Context{Args: map[string]any{"STRING": inputs[0], "SPLIT": "|", "ITEM": float64(4)}}
			b.ReportAllocs()
			b.ResetTimer()
			i := 0
			for b.Loop() {
				if changing {
					ctx.Args["STRING"] = inputs[i%len(inputs)]
					i++
				}
				if _, err := split(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
