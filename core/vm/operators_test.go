package vm

import (
	"context"
	"math"
	"testing"

	"gowarp/core/project"
)

func evalBlock(t *testing.T, r *Runtime, targetIndex int, b *project.Block) (any, error) {
	t.Helper()
	installTestExtensions(t, r, b.Opcode)
	target := r.targets[targetIndex]
	if target.source.Blocks == nil {
		target.source.Blocks = project.Blocks{}
	}
	target.source.Blocks["test-reporter"] = b
	budget := stepsPerFrame
	return r.eval(context.Background(), &thread{target: target}, "test-reporter", 0, &budget)
}

func literalInputs(values map[string]any) map[string][]any {
	inputs := map[string][]any{}
	for name, value := range values {
		inputs[name] = []any{float64(1), []any{float64(10), value}}
	}
	return inputs
}

func TestAdditionalOperators(t *testing.T) {
	tests := []struct {
		op     string
		inputs map[string]any
		fields map[string][]any
		want   any
	}{
		{"operator_letter_of", map[string]any{"LETTER": "2", "STRING": "가나다"}, nil, "나"},
		{"operator_letter_of", map[string]any{"LETTER": "0", "STRING": "abc"}, nil, ""},
		{"operator_letter_of", map[string]any{"LETTER": "2", "STRING": "a😀z"}, nil, "\ufffd"},
		{"operator_letter_of", map[string]any{"LETTER": "3", "STRING": "a😀z"}, nil, "\ufffd"},
		{"operator_letter_of", map[string]any{"LETTER": "4.9", "STRING": "a😀z"}, nil, "z"},
		{"operator_letter_of", map[string]any{"LETTER": "5", "STRING": "a😀z"}, nil, ""},
		{"operator_letter_of", map[string]any{"LETTER": "0.9", "STRING": "abc"}, nil, ""},
		{"operator_letter_of", map[string]any{"LETTER": "Infinity", "STRING": "abc"}, nil, ""},
		{"operator_letter_of", map[string]any{"LETTER": "NaN", "STRING": "abc"}, nil, ""},
		{"operator_letter_of", map[string]any{"LETTER": "1", "STRING": "\xffz"}, nil, "\ufffd"},
		{"operator_length", map[string]any{"STRING": "a😀z"}, nil, float64(4)},
		{"operator_length", map[string]any{"STRING": "가나다"}, nil, float64(3)},
		{"operator_length", map[string]any{"STRING": "\xffz"}, nil, float64(2)},
		{
			"operator_letters_of",
			map[string]any{
				"LETTER1": "2",
				"LETTER2": "3",
				"STRING":  "a😀z",
			},
			nil,
			"😀",
		},
		{"operator_letters_of", map[string]any{
			"LETTER1": "4",
			"LETTER2": "1",
			"STRING":  "abcd",
		}, nil, "bc"},
		{"operator_index_of", map[string]any{"SUBSTRING": "Z", "STRING": "a😀z"}, nil, float64(4)},
		{
			"operator_change_case",
			map[string]any{"STRING": "Hello"},
			map[string][]any{"CASE": {"uppercase"}},
			"HELLO",
		},
		{"operator_repeat", map[string]any{"STRING": "ab", "REPEAT": "2.9"}, nil, "abab"},
		{"operator_repeat", map[string]any{"STRING": "ab", "REPEAT": "Infinity"}, nil, ""},
		{
			"operator_replace",
			map[string]any{
				"SUBSTRING": "A.",
				"STRING":    "a. A. ab",
				"REPLACE":   "$1",
			},
			nil,
			"$1 $1 ab",
		},
		{"operator_trim", map[string]any{"STRING": "\ufeff a \n"}, nil, "a"},
		{"operator_pi", nil, nil, math.Pi}, {"operator_newline", nil, nil, "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.op, func(t *testing.T) {
			r := motionRuntime(t, `{}`)
			got, err := evalBlock(t, r, 1, &project.Block{
				Opcode: tc.op,
				Inputs: literalInputs(tc.inputs),
				Fields: tc.fields,
			})
			if err != nil || got != tc.want {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestMathOperators(t *testing.T) {
	tests := []struct {
		op, value string
		want      float64
	}{
		{"abs", "-3", 3}, {"floor", "2.9", 2}, {"ceiling", "-2.9", -2}, {"sqrt", "9", 3},
		{"sin", "30", .5}, {"cos", "90", 0}, {"tan", "90", math.Inf(1)}, {"tan", "270", math.Inf(-1)},
		{"asin", "1", 90}, {"acos", "-1", 180}, {"atan", "1", 45}, {"ln", "1", 0}, {"log", "10", 1}, {"e ^", "0", 1}, {"10 ^", "3", 1000}, {"unknown", "3", 0},
	}
	for _, tc := range tests {
		t.Run(tc.op, func(t *testing.T) {
			got, err := evalBlock(t, motionRuntime(t, `{}`), 1, &project.Block{
				Opcode: "operator_mathop",
				Inputs: literalInputs(map[string]any{"NUM": tc.value}),
				Fields: map[string][]any{"OPERATOR": {tc.op}},
			})
			if err != nil || got != tc.want {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestRandomReversedAndDecimalRanges(t *testing.T) {
	r := motionRuntime(t, `{}`)
	for _, bounds := range [][2]string{{"3", "-2"}, {"1.5", "1.8"}, {"7", "7"}} {
		for i := 0; i < 100; i++ {
			got, err := evalBlock(t, r, 1, &project.Block{
				Opcode: "operator_random",
				Inputs: literalInputs(map[string]any{"FROM": bounds[0], "TO": bounds[1]}),
			})
			if err != nil {
				t.Fatal(err)
			}
			n := number(got)
			lo, hi := math.Min(number(bounds[0]), number(bounds[1])), math.Max(number(bounds[0]), number(bounds[1]))
			if n < lo || n > hi || (bounds[0] == "3" && n != math.Trunc(n)) {
				t.Fatalf("out of range: %v", n)
			}
		}
	}
}

func TestRepeatAllocationLimit(t *testing.T) {
	_, err := evalBlock(t, motionRuntime(t, `{}`), 1, &project.Block{
		Opcode: "operator_repeat",
		Inputs: literalInputs(map[string]any{"STRING": "abc", "REPEAT": "1000000000"}),
	})
	if err == nil {
		t.Fatal("unbounded allocation accepted")
	}
}

func TestBooleanOperatorsShortCircuit(t *testing.T) {
	for _, tc := range []struct {
		op   string
		left string
		want bool
	}{{"operator_and", "false", false}, {"operator_or", "true", true}} {
		inputs := literalInputs(map[string]any{"OPERAND1": tc.left})
		inputs["OPERAND2"] = []any{float64(2), "must-not-evaluate"}
		got, err := evalBlock(t, motionRuntime(t, `{}`), 1, &project.Block{Opcode: tc.op, Inputs: inputs})
		if err != nil || got != tc.want {
			t.Fatalf("short circuit: %v %v", got, err)
		}
	}
}

func TestLiteralShadowReporters(t *testing.T) {
	for _, tc := range []struct {
		op, field, value string
	}{
		{"math_number", "NUM", "007"},
		{"math_positive_number", "NUM", "1.5"},
		{"math_whole_number", "NUM", "2"},
		{"math_integer", "NUM", "-1"},
		{"math_angle", "NUM", "90"},
		{"text", "TEXT", "hello"},
		{"colour_picker", "COLOUR", "#123456"},
	} {
		got, err := evalBlock(t, motionRuntime(t, `{}`), 1, &project.Block{Opcode: tc.op, Fields: map[string][]any{tc.field: {tc.value}}})
		if err != nil || got != tc.value {
			t.Fatalf("%s: %v %v", tc.op, got, err)
		}
	}
}
