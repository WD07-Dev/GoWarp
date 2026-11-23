package vm

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

/*
Reuse immutable boxed integers in a fixed shared table; other numbers remain uncached float64
values.
*/
var smallNumbers = func() [2048]any {
	var values [2048]any
	for i := range values {
		values[i] = float64(i - 1024)
	}
	return values
}()

func numberValue(n float64) any {
	if n >= -1024 && n < 1024 {
		i := int(n)
		if float64(i) == n && (n != 0 || !math.Signbit(n)) {
			return smallNumbers[i+1024]
		}
	}
	return n
}

func trim(s string) string {
	if len(s) == 0 || (s[0] > ' ' && s[0] < utf8.RuneSelf && s[len(s)-1] > ' ' && s[len(s)-1] < utf8.RuneSelf) {
		return s
	}
	return strings.TrimFunc(s, func(r rune) bool {
		return r == '\ufeff' || (r != '\u0085' && unicode.IsSpace(r))
	})
}

var invalidNumericValue = math.NaN()

func numeric(v any) float64 {
	if n, ok := v.(float64); ok {
		return n
	}
	return parseNumeric(v, invalidNumericValue)
}

func number(v any) float64 {
	// NaN falls through to Scratch's arithmetic fallback of zero.
	if n, ok := v.(float64); ok && n == n {
		return n
	}
	return parseNumeric(v, 0)
}

func parseNumeric(v any, invalid float64) float64 {
	if v == nil {
		return 0
	}
	switch v := v.(type) {
	case float64:
		if math.IsNaN(v) {
			return invalid
		}
		return v
	case bool:
		if v {
			return 1
		}
		return 0
	case string:
		s := trim(v)
		if s == "" {
			return 0
		}
		if c := s[0]; (c < '0' || c > '9') && c != '+' && c != '-' && c != '.' && c != 'I' {
			return invalid
		}
		/*
			Up to 15 decimal digits are exact in float64. Longer values and
			fractional/exponential forms still use the correctly rounded parser.
		*/
		digits := s
		negative := s[0] == '-'
		if negative || s[0] == '+' {
			digits = s[1:]
		}
		validated := 0
		if len(digits) > 0 && len(digits) <= 15 && digits[0] >= '0' && digits[0] <= '9' {
			var integer uint64
			i := 0
			for ; i < len(digits) && digits[i] >= '0' && digits[i] <= '9'; i++ {
				integer = integer*10 + uint64(digits[i]-'0')
			}
			if i == len(digits) {
				if negative {
					return -float64(integer)
				}
				return float64(integer)
			}
			validated = len(s) - len(digits) + i
		}
		switch s {
		case "Infinity", "+Infinity":
			return math.Inf(1)
		case "-Infinity":
			return math.Inf(-1)
		}
		if len(s) > 2 && s[0] == '0' {
			base := 0
			switch s[1] {
			case 'x', 'X':
				base = 16
			case 'b', 'B':
				base = 2
			case 'o', 'O':
				base = 8
			}
			if base != 0 {
				n, err := strconv.ParseUint(s[2:], base, 64)
				if err == nil {
					return float64(n)
				}
				return invalid
			}
		}
		// Go accepts Inf, hex floats and underscores; JavaScript Number does not.
		for i := validated; i < len(s); i++ {
			c := s[i]
			if c >= '0' && c <= '9' {
				continue
			}
			switch c {
			case '+', '-', '.', 'e', 'E':
			default:
				return invalid
			}
		}
		n, err := strconv.ParseFloat(s, 64)
		if err == nil || math.IsInf(n, 0) {
			return n
		}
	}
	return invalid
}

func truth(v any) bool {
	if s, ok := v.(string); ok {
		return s != "" && s != "0" && !strings.EqualFold(s, "false")
	}
	n := numeric(v)
	return n != 0 && !math.IsNaN(n)
}

func text(v any) string {
	if _, ok := v.(undefinedValue); ok {
		return "undefined"
	}
	if v == nil {
		return "null"
	}
	switch v := v.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		if math.IsNaN(v) {
			return "NaN"
		}
		if math.IsInf(v, 1) {
			return "Infinity"
		}
		if math.IsInf(v, -1) {
			return "-Infinity"
		}
		if v == 0 {
			return "0"
		}
		if math.Abs(v) >= 1e-6 && math.Abs(v) < 1e21 {
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
		s := strconv.FormatFloat(v, 'e', -1, 64)
		s = strings.Replace(s, "e-0", "e-", 1)
		return strings.Replace(s, "e+0", "e+", 1)
	}
	return ""
}

func compare(a, b any) float64 {
	if left, ok := a.(string); ok {
		if right, ok := b.(string); ok && left == right {
			return 0
		}
	}
	an, bn := numeric(a), numeric(b)
	if an == 0 {
		if s, ok := a.(string); ok && trim(s) == "" {
			an = math.NaN()
		}
	}
	if bn == 0 {
		if s, ok := b.(string); ok && trim(s) == "" {
			bn = math.NaN()
		}
	}
	if !math.IsNaN(an) && !math.IsNaN(bn) {
		if an == bn {
			return 0
		}
		return an - bn
	}
	return compareText(text(a), text(b))
}

// Compare lowercase text without allocating copies; matching runes need no conversion.
func compareText(a, b string) float64 {
	if a == b {
		return 0
	}
	for len(a) > 0 && len(b) > 0 {
		left, right := rune(a[0]), rune(b[0])
		leftSize, rightSize := 1, 1
		if left >= utf8.RuneSelf {
			left, leftSize = utf8.DecodeRuneInString(a)
		}
		if right >= utf8.RuneSelf {
			right, rightSize = utf8.DecodeRuneInString(b)
		}
		if left != right {
			left, right = unicode.ToLower(left), unicode.ToLower(right)
			if left < right {
				return -1
			}
			if left > right {
				return 1
			}
		}
		a, b = a[leftSize:], b[rightSize:]
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}
