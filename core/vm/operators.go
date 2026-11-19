package vm

import (
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"gowarp/core/project"
)

// Bound generated text independently of the instruction budget.
const maxGeneratedText = 16 << 20

func extraOperator(b *project.Block, input func(string) (any, error)) (any, error) {
	args := map[string]any{}
	var names []string
	switch b.Opcode {
	case "operator_pi":
		return math.Pi, nil
	case "operator_newline":
		return "\n", nil
	case "operator_random":
		names = []string{"FROM", "TO"}
	case "operator_letter_of":
		names = []string{"LETTER", "STRING"}
	case "operator_letters_of":
		names = []string{"STRING", "LETTER1", "LETTER2"}
	case "operator_index_of":
		names = []string{"STRING", "SUBSTRING"}
	case "operator_change_case", "operator_trim":
		names = []string{"STRING"}
	case "operator_repeat":
		names = []string{"STRING", "REPEAT"}
	case "operator_replace":
		names = []string{"SUBSTRING", "STRING", "REPLACE"}
	case "operator_mathop":
		names = []string{"NUM"}
	}
	for _, name := range names {
		v, err := input(name)
		if err != nil {
			return nil, err
		}
		args[name] = v
	}
	switch b.Opcode {
	case "operator_random":
		a, c := number(args["FROM"]), number(args["TO"])
		lo, hi := math.Min(a, c), math.Max(a, c)
		if lo == hi {
			return lo, nil
		}
		if integerValue(args["FROM"]) && integerValue(args["TO"]) {
			return lo + math.Floor(rand.Float64()*(hi+1-lo)), nil
		}
		return lo + rand.Float64()*(hi-lo), nil
	case "operator_letter_of":
		return utf16Letter(text(args["STRING"]), number(args["LETTER"])-1), nil
	case "operator_letters_of":
		units := utf16.Encode([]rune(text(args["STRING"])))
		clamp := func(n float64) int {
			return int(math.Max(0, math.Min(float64(len(units)), n)))
		}
		start, end := clamp(number(args["LETTER1"])-1), clamp(number(args["LETTER2"]))
		if start > end {
			start, end = end, start
		}
		return string(utf16.Decode(units[start:end])), nil
	case "operator_index_of":
		s, part := strings.ToLower(text(args["STRING"])), strings.ToLower(text(args["SUBSTRING"]))
		pos := strings.Index(s, part)
		if pos < 0 {
			return float64(0), nil
		}
		return float64(utf16Length(s[:pos]) + 1), nil
	case "operator_change_case":
		if field(b, "CASE", 0) == "uppercase" {
			return strings.ToUpper(text(args["STRING"])), nil
		}
		return strings.ToLower(text(args["STRING"])), nil
	case "operator_trim":
		return trim(text(args["STRING"])), nil
	case "operator_repeat":
		s, count := text(args["STRING"]), math.Floor(number(args["REPEAT"]))
		if s == "" || count <= 0 || math.IsInf(count, 0) {
			return "", nil
		}
		if count > float64(maxGeneratedText/len(s)) {
			return nil, fmt.Errorf("generated text exceeds %d bytes", maxGeneratedText)
		}
		return strings.Repeat(s, int(count)), nil
	case "operator_replace":
		re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(text(args["SUBSTRING"])))
		s, replacement := text(args["STRING"]), text(args["REPLACE"])
		// Check the result size before allocating it. Literal replacement preserves $.
		matches := re.FindAllStringIndex(s, -1)
		size := len(s)
		for _, m := range matches {
			size += len(replacement) - (m[1] - m[0])
			if size > maxGeneratedText {
				return nil, fmt.Errorf("generated text exceeds %d bytes", maxGeneratedText)
			}
		}
		return re.ReplaceAllStringFunc(s, func(string) string {
			return replacement
		}), nil
	case "operator_mathop":
		return mathOperator(strings.ToLower(field(b, "OPERATOR", 0)), number(args["NUM"])), nil
	}
	return nil, fmt.Errorf("unsupported operator %q", b.Opcode)
}

func integerValue(v any) bool {
	// Scratch treats strings containing a decimal point as non-integer inputs.
	if s, ok := v.(string); ok {
		return !strings.Contains(s, ".")
	}
	n := number(v)
	return math.Trunc(n) == n
}

func roundTrig(n float64) float64 {
	if !finite(n) || math.Abs(n) >= 1e21 {
		return n
	}
	var buffer [64]byte
	digits := strconv.AppendFloat(buffer[:0], n, 'f', 10, 64)
	rounded, _ := strconv.ParseFloat(string(digits), 64)
	return rounded
}

type trigResult struct {
	input uint64
	value float64
	valid bool
}

// Cache a fixed number of results per function, including Scratch's decimal rounding.
func (r *Runtime) mathOperator(op string, n float64) float64 {
	var function int
	switch op {
	case "sin":
		function = 0
	case "cos":
		function = 1
	case "tan":
		function = 2
	default:
		return mathOperator(op, n)
	}
	bits := math.Float64bits(n)
	index := ((bits ^ (bits >> 32)) * 0x9e3779b97f4a7c15) >> 60
	entry := &r.trigResults[function][index]
	if entry.valid && entry.input == bits {
		return entry.value
	}
	value := mathOperator(op, n)
	*entry = trigResult{input: bits, value: value, valid: true}
	return value
}

func mathOperator(op string, n float64) float64 {
	switch op {
	case "abs":
		return math.Abs(n)
	case "floor":
		return math.Floor(n)
	case "ceiling":
		return math.Ceil(n)
	case "sqrt":
		return math.Sqrt(n)
	case "sin":
		return roundTrig(math.Sin(math.Pi * n / 180))
	case "cos":
		return roundTrig(math.Cos(math.Pi * n / 180))
	case "tan":
		turn := math.Mod(n, 360)
		if turn == 90 || turn == -270 {
			return math.Inf(1)
		}
		if turn == -90 || turn == 270 {
			return math.Inf(-1)
		}
		return roundTrig(math.Tan(math.Pi * turn / 180))
	case "asin":
		return math.Asin(n) * 180 / math.Pi
	case "acos":
		return math.Acos(n) * 180 / math.Pi
	case "atan":
		return math.Atan(n) * 180 / math.Pi
	case "ln":
		return math.Log(n)
	case "log":
		return math.Log(n) / math.Ln10
	case "e ^":
		return math.Exp(n)
	case "10 ^":
		return math.Pow(10, n)
	}
	return 0
}

/*
Scratch indexes UTF-16 code units. Scan the UTF-8 string without allocating
a rune and UTF-16 buffer for every character requested by a text loop.
*/
func utf16Letter(s string, index float64) string {
	if index < 0 || index >= float64(len(s)) {
		return ""
	}
	unit := int(index)
	for _, ch := range s {
		width := 1
		if ch > 0xffff {
			width = 2
		}
		if unit < width {
			/*
				Existing string results replace an isolated surrogate, or invalid
				UTF-8, with the replacement character.
			*/
			if width == 2 {
				return "\ufffd"
			}
			return string(ch)
		}
		unit -= width
	}
	return ""
}

func utf16Length(s string) int {
	length := 0
	for _, ch := range s {
		length++
		if ch > 0xffff {
			length++
		}
	}
	return length
}
