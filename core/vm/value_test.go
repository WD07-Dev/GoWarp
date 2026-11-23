package vm

import (
	"math"
	"strings"
	"testing"
)

func TestTextComparisonAllocations(t *testing.T) {
	for _, pair := range [][2]string{{"BONE-white", "BONE-blue"}, {"Σ骨", "σ骨"}, {"K", "K"}} {
		want := float64(strings.Compare(strings.ToLower(pair[0]), strings.ToLower(pair[1])))
		allocations := testing.AllocsPerRun(100, func() {
			if got := compare(pair[0], pair[1]); got != want {
				t.Fatalf("comparison=%g, want %g", got, want)
			}
		})
		if allocations != 0 {
			t.Errorf("comparing %q allocated %g objects", pair, allocations)
		}
	}
}

func TestTextComparisonMatchesLowercaseOrdering(t *testing.T) {
	values := []string{"", " ", "BONE-white", "bone-blue", "Σ", "σ", "ς", "K", "k", "İ", "i", "一般", "일반", "𐐀", "𐐨", "\xff", "\xef\xbf\xbd", "\xc0\x80", "A\xffB"}
	for _, left := range values {
		for _, right := range values {
			want := float64(strings.Compare(strings.ToLower(left), strings.ToLower(right)))
			if got := compare(left, right); got != want {
				t.Fatalf("compare(%q,%q)=%g, want %g", left, right, got, want)
			}
		}
	}
}

func TestNumberValuePreservesFloatBits(t *testing.T) {
	values := []float64{-1025, -1024, -1, 0, math.Copysign(0, -1), 1, 1023, 1024, 0.5, -0.5, math.SmallestNonzeroFloat64, math.Inf(1), math.Inf(-1), math.NaN()}
	for _, value := range values {
		got, ok := numberValue(value).(float64)
		if !ok || math.Float64bits(got) != math.Float64bits(value) {
			t.Fatalf("number value changed type or bits: %v -> %v", value, got)
		}
	}
	first := numberValue(10)
	for _, value := range values {
		numberValue(value)
	}
	if first != float64(10) {
		t.Fatal("a later result changed a retained value")
	}
}

func TestIdenticalTextComparisonAllocations(t *testing.T) {
	for _, value := range []string{"일반", "GB", "NaN", "Infinity", "  ", "1.2.3", "Σ"} {
		t.Run(value, func(t *testing.T) {
			allocations := testing.AllocsPerRun(100, func() {
				if compare(value, value) != 0 {
					t.Fatal("identical strings must compare equal")
				}
			})
			if allocations != 0 {
				t.Fatalf("identical text comparison allocated %g objects", allocations)
			}
		})
	}
}

func TestScratchValues(t *testing.T) {
	for _, tc := range []struct {
		in    any
		n     float64
		truth bool
	}{
		{"", 0, false},
		{" ", 0, true},
		{"FALSE", 0, false},
		{"0", 0, false},
		{"00", 0, true},
		{"2.5", 2.5, true},
		{"hello", 0, true},
		{true, 1, true},
		{false, 0, false},
		{"0x10", 16, true},
		{"0b11", 3, true},
		{"Infinity", math.Inf(1), true},
		{"Inf", 0, true},
	} {
		if got := number(tc.in); got != tc.n {
			t.Errorf("number(%v)=%v want %v", tc.in, got, tc.n)
		}
		if got := truth(tc.in); got != tc.truth {
			t.Errorf("truth(%v)=%v", tc.in, got)
		}
	}
	for _, tc := range []struct {
		a, b any
		want bool
	}{
		{"05", float64(5), true},
		{"HELLO", "hello", true},
		{"", float64(0), false},
		{" ", float64(0), false},
		{"\ufeff\u2003", float64(0), false},
		{"-0", float64(0), true},
		{"Infinity", math.Inf(1), true},
		{"a", "b", false},
	} {
		if got := compare(tc.a, tc.b) == 0; got != tc.want {
			t.Errorf("equals(%v,%v)=%v", tc.a, tc.b, got)
		}
	}
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{math.Inf(1), "Infinity"},
		{math.NaN(), "NaN"},
		{1e20, "100000000000000000000"},
		{1e-7, "1e-7"},
		{math.Copysign(0, -1), "0"},
	} {
		if got := text(tc.in); got != tc.want {
			t.Errorf("text(%v)=%s", tc.in, got)
		}
	}
}

func TestNumericStringBoundaries(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  float64
	}{
		{"+12345", 12345}, {"-12345", -12345}, {"-0", math.Copysign(0, -1)},
		{"999999999999999", 999999999999999}, {"9007199254740993", 9007199254740992},
		{"-9007199254740993", -9007199254740992}, {"00000000000000000042", 42},
		{".125", .125}, {"1.", 1}, {"1.25e+2", 125}, {"-1e-9999", math.Copysign(0, -1)},
		{"1e9999", math.Inf(1)}, {"-Infinity", math.Inf(-1)},
		{"0xFF", 255}, {"0o17", 15}, {"0b101", 5},
		{"\t\n\v\f\r 42 \r\n", 42}, {"\ufeff\u00a0\u2003-2\u2029", -2},
		{"\u008542", math.NaN()}, {"42\u0085", math.NaN()},
		{"\x0042", math.NaN()}, {"42\x00", math.NaN()},
		{"1_000", math.NaN()}, {"0x1p2", math.NaN()}, {"+0x10", math.NaN()},
		{"++1", math.NaN()}, {"1-2", math.NaN()}, {"1e", math.NaN()},
		{"１２", math.NaN()}, {"2\xff", math.NaN()},
	} {
		got := numeric(tc.input)
		if math.IsNaN(tc.want) {
			if !math.IsNaN(got) {
				t.Errorf("numeric(%q)=%g, want NaN", tc.input, got)
			}
		} else if math.Float64bits(got) != math.Float64bits(tc.want) {
			t.Errorf("numeric(%q)=%g, want %g", tc.input, got, tc.want)
		}
	}
}

func BenchmarkNumericString(b *testing.B) {
	for _, value := range []string{"12345", "-12345", "123.456", "1.25e+2", "9007199254740993", "action-normal", "일반", " 12345 "} {
		b.Run(value, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				numeric(value)
			}
		})
	}
}

func BenchmarkValueComparison(b *testing.B) {
	for _, pair := range [][2]any{{"12345", float64(12346)}, {"BONE-white", "BONE-blue"}, {"일반", "다른"}, {"  ", float64(0)}} {
		b.Run(text(pair[0]), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				compare(pair[0], pair[1])
			}
		})
	}
}

func TestNumericValuesPreserveSpecialNumbers(t *testing.T) {
	for _, value := range []float64{0, math.Copysign(0, -1), .125, -12345, math.SmallestNonzeroFloat64, math.MaxFloat64, math.Inf(1), math.Inf(-1), math.NaN()} {
		if got := numeric(value); math.Float64bits(got) != math.Float64bits(value) {
			t.Fatalf("numeric(%g) changed its bits", value)
		}
		want := value
		if math.IsNaN(want) {
			want = 0
		}
		if got := number(value); math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("number(%g) = %g, want %g", value, got, want)
		}
	}
}

func TestNumberCoercionFallbacks(t *testing.T) {
	for _, tc := range []struct {
		input any
		want  float64
	}{
		{nil, 0}, {true, 1}, {false, 0}, {"", 0}, {" \t", 0},
		{" 12.5 ", 12.5}, {"-0", math.Copysign(0, -1)},
		{"0xff", 255}, {"0b10", 2}, {"0o17", 15},
		{"Infinity", math.Inf(1)}, {"-Infinity", math.Inf(-1)}, {"1e999", math.Inf(1)},
		{"NaN", 0}, {"Inf", 0}, {"0x1p2", 0}, {"1_000", 0}, {"12x", 0}, {int(12), 0},
		{math.Float64frombits(0x7ff8000000001234), 0},
	} {
		if got := number(tc.input); math.Float64bits(got) != math.Float64bits(tc.want) {
			t.Fatalf("number(%#v) = %g, want %g", tc.input, got, tc.want)
		}
	}
	nan := math.Float64frombits(0xfff8000000001234)
	if math.Float64bits(numeric(nan)) != math.Float64bits(nan) {
		t.Fatal("numeric changed a NaN payload")
	}
}

var numericBenchmarkResult float64

func BenchmarkNumericListArithmetic(b *testing.B) {
	for _, tc := range []struct {
		name  string
		items []any
	}{
		{"numbers", []any{12.5, -31.25, 0.75, 255.5}},
		{"strings", []any{"12.5", "-31.25", "0.75", "255.5"}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			index := 0
			var result float64
			b.ReportAllocs()
			for b.Loop() {
				x, y := number(tc.items[index]), number(tc.items[(index+1)%len(tc.items)])
				result = (x*y + x) / (y + 1)
				index = (index + 1) % len(tc.items)
			}
			numericBenchmarkResult = result
		})
	}
}

func TestDecimalIntegerConversion(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  float64
	}{
		{"+0", 0}, {"-000", math.Copysign(0, -1)},
		{"999999999999999", 999999999999999}, {"-999999999999999", -999999999999999},
		{"9999999999999999", 10000000000000000},
		{"+", math.NaN()}, {"-", math.NaN()}, {"123x", math.NaN()},
		{"12-3", math.NaN()}, {"12.5", 12.5}, {"12e3", 12000},
	} {
		got := numeric(tc.input)
		if math.IsNaN(tc.want) {
			if !math.IsNaN(got) {
				t.Fatalf("numeric(%q) = %g, want NaN", tc.input, got)
			}
		} else if math.Float64bits(got) != math.Float64bits(tc.want) {
			t.Fatalf("numeric(%q) = %g, want %g", tc.input, got, tc.want)
		}
	}
}

func BenchmarkNumericValue(b *testing.B) {
	var value any = 12.5
	for b.Loop() {
		if number(value) != 12.5 {
			b.Fatal("incorrect number")
		}
	}
}
