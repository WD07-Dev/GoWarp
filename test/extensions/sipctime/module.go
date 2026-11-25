// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

func New() e.Extension {
	x := e.Extension{ID: "sipctime", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "sipctime_formatTime", Text: "format [VALUE] seconds as [ROUND] time", Kind: "reporter", Inputs: []string{"VALUE", "ROUND"}, Handler: func(c *e.Context) (e.Result, error) {
			value := c.Number("VALUE")
			seconds := extensionFixed(math.Mod(value, 60), 3)
			width := 6
			if c.String("ROUND") == "rounded" {
				seconds = e.String(math.Floor(math.Mod(value, 60) + .5))
				width = 2
			}
			return e.Result{Value: extensionPad(e.String(math.Floor(value/3600)), 2) + ":" + extensionPad(e.String(math.Floor(math.Mod(value/60, 60))), 2) + ":" + extensionPad(seconds, width)}, nil
		}},
		{Opcode: "sipctime_menu_TimeFormat", Kind: "reporter", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: c.Field("TimeFormat")}, nil
		}},
	}}
	for _, entry := range []struct {
		op, text string
		inputs   []string
	}{
		{"Timestamp", "current timestamp", nil}, {"timezone", "current time zone", nil},
		{"Timedata", "[Timedata] from [timestamp]", []string{"Timedata", "timestamp"}},
		{"TimestampToTime", "convert [timestamp] to YYYY-MM-DD HH:MM:SS", []string{"timestamp"}},
		{"TimeToTimestamp", "convert [time] to timestamp", []string{"time"}},
		{"differenceBetweenDateAndNow", "difference between [DATE] and now in [TIME_MENU]", []string{"DATE", "TIME_MENU"}},
		{"differenceBetweenDates", "difference between [START] and [END] in [TIME_MENU]", []string{"START", "END", "TIME_MENU"}},
		{"daysInMonth", "number of days in [MONTH] [YEAR]", []string{"MONTH", "YEAR"}},
	} {
		entry := entry
		x.Blocks = append(x.Blocks, e.Block{Opcode: "sipctime_" + entry.op, Text: entry.text, Kind: "reporter", Inputs: entry.inputs, Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: dateReporter(c, entry.op)}, nil }})
	}
	for _, menu := range []string{"Time", "DurationUnit", "Months"} {
		menu := menu
		x.Blocks = append(x.Blocks, e.Block{Opcode: "sipctime_menu_" + menu, Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field(menu)}, nil }})
	}
	return x
}
func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
func extensionFixed(n float64, digits int) string {
	if !finite(n) || math.Abs(n) >= 1e21 {
		return e.String(n)
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	value := new(big.Rat).SetFloat64(n)
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)
	numerator := new(big.Int).Mul(value.Num(), scale)
	whole, remainder := new(big.Int), new(big.Int)
	whole.QuoRem(numerator, value.Denom(), remainder)
	if remainder.Lsh(remainder, 1).Cmp(value.Denom()) >= 0 {
		whole.Add(whole, big.NewInt(1))
	}
	s := whole.String()
	if digits > 0 {
		s = extensionPad(s, digits+1)
		s = s[:len(s)-digits] + "." + s[len(s)-digits:]
	}
	return sign + s
}
func extensionPad(s string, width int) string {
	if len(s) < width {
		return strings.Repeat("0", width-len(s)) + s
	}
	return s
}

// parseDate accepts numeric Unix milliseconds and the ISO/local date formats
// exposed by the extension. Unrecognized date strings produce an invalid date.
func parseDate(value any) (time.Time, bool) {
	s, ok := value.(string)
	if !ok {
		if number, ok := value.(float64); ok {
			return milliseconds(number)
		}
		return milliseconds(e.Number(value))
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return milliseconds(0)
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return milliseconds(n)
	}
	if len(s) > 2 && s[0] == '0' && strings.ContainsAny(s[1:2], "xXbBoO") {
		if _, err := strconv.ParseUint(s, 0, 64); err == nil {
			return milliseconds(e.Number(s))
		}
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02", "2006-01", "2006", time.RFC1123, time.RFC1123Z, time.RFC822, time.RFC822Z} {
		if d, err := time.Parse(layout, s); err == nil {
			return d.In(time.Local), true
		}
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04", "2006/01/02 15:04:05", "2006/01/02", "1/2/2006", "Jan 2, 2006", "January 2, 2006"} {
		if d, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return d, true
		}
	}
	return time.Time{}, false
}
func milliseconds(n float64) (time.Time, bool) {
	if !finite(n) || math.Abs(n) > 8.64e15 {
		return time.Time{}, false
	}
	return time.UnixMilli(int64(n)).In(time.Local), true
}
func dateReporter(c *e.Context, op string) any {
	if op == "Timedata" {
		switch c.String("Timedata") {
		case "year", "month", "day", "hour", "minute", "second":
		default:
			return float64(0)
		}
	}
	switch op {
	case "Timestamp":
		return float64(time.Now().UnixMilli())
	case "timezone":
		_, offset := time.Now().Zone()
		sign := ""
		if offset >= 0 {
			sign = "+"
		}
		return "UTC" + sign + e.String(float64(offset)/3600)
	case "daysInMonth":
		year, month := math.Floor(c.Number("YEAR")+.5), math.Floor(c.Number("MONTH")+.5)
		if year <= 0 || month < 0 || month >= 13 {
			return float64(0)
		}
		if !finite(year) || !finite(month) || year > 275760 {
			return math.NaN()
		}
		if year < 100 {
			year += 1900
		}
		return float64(time.Date(int(year), time.Month(int(month)+1), 0, 0, 0, 0, 0, time.Local).Day())
	case "differenceBetweenDateAndNow", "differenceBetweenDates":
		key := "START"
		if op == "differenceBetweenDateAndNow" {
			key = "DATE"
		}
		start, validStart := parseDate(c.Args[key])
		end, validEnd := time.Now(), true
		if op == "differenceBetweenDates" {
			end, validEnd = parseDate(c.Args["END"])
		}
		divisor, ok := map[string]float64{"years": 86400000 * 365, "months": 86400000 * 30.436875, "days": 86400000, "hours": 3600000, "minutes": 60000, "seconds": 1000}[c.String("TIME_MENU")]
		if !ok {
			return float64(0)
		}
		if !validStart || !validEnd {
			return math.NaN()
		}
		return float64(end.UnixMilli()-start.UnixMilli()) / divisor
	}
	key := "timestamp"
	if op == "TimeToTimestamp" {
		key = "time"
	}
	date, valid := parseDate(c.Args[key])
	if !valid {
		if op == "TimestampToTime" {
			return "NaN-NaN-NaN NaN:NaN:NaN"
		}
		return math.NaN()
	}
	switch op {
	case "TimeToTimestamp":
		return float64(date.UnixMilli())
	case "TimestampToTime":
		return fmt.Sprintf("%d-%02d-%02d %02d:%02d:%02d", date.Year(), date.Month(), date.Day(), date.Hour(), date.Minute(), date.Second())
	case "Timedata":
		if c.String("Timedata") == "year" {
			return float64(date.Year())
		}
		n, ok := map[string]int{"month": int(date.Month()), "day": date.Day(), "hour": date.Hour(), "minute": date.Minute(), "second": date.Second()}[c.String("Timedata")]
		if !ok {
			return float64(0)
		}
		if n < 10 {
			return fmt.Sprintf("%02d", n)
		}
		return float64(n)
	}
	return float64(0)
}
