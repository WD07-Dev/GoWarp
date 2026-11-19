package extension

import (
	"math"
	"strconv"
	"strings"
	"unicode"
)

func trim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return r == '\ufeff' || (r != '\u0085' && unicode.IsSpace(r))
	})
}

func numeric(v any) float64 {
	if v == nil {
		return 0
	}
	switch v := v.(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case float32:
		return float64(v)
	case float64:
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
				return math.NaN()
			}
		}
		// Go accepts Inf, hex floats and underscores; JavaScript Number does not.
		for _, r := range s {
			if !strings.ContainsRune("0123456789+-.eE", r) {
				return math.NaN()
			}
		}
		n, err := strconv.ParseFloat(s, 64)
		if err == nil || math.IsInf(n, 0) {
			return n
		}
	}
	return math.NaN()
}

func Number(v any) float64 {
	n := numeric(v)
	if math.IsNaN(n) {
		return 0
	}
	return n
}

func Bool(v any) bool {
	if s, ok := v.(string); ok {
		return s != "" && s != "0" && !strings.EqualFold(s, "false")
	}
	n := numeric(v)
	return n != 0 && !math.IsNaN(n)
}

func String(v any) string {
	if v == nil {
		return "null"
	}
	switch v := v.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float32:
		return String(float64(v))
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

func Compare(a, b any) float64 {
	an, bn := numeric(a), numeric(b)
	if s, ok := a.(string); ok && trim(s) == "" {
		an = math.NaN()
	}
	if s, ok := b.(string); ok && trim(s) == "" {
		bn = math.NaN()
	}
	if !math.IsNaN(an) && !math.IsNaN(bn) {
		if an == bn {
			return 0
		}
		return an - bn
	}
	return float64(strings.Compare(strings.ToLower(String(a)), strings.ToLower(String(b))))
}

func Clamp(n, low, high float64) float64 {
	return math.Max(low, math.Min(high, n))
}
