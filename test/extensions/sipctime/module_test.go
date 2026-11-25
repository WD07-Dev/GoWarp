package extension

import (
	e "gowarp/core/extension"
	"math"
	"testing"
	"time"
)

func TestDateReporters(t *testing.T) {
	previous := time.Local
	time.Local = time.FixedZone("test", 9*3600)
	defer func() { time.Local = previous }()
	blocks := map[string]e.Block{}
	for _, b := range New().Blocks {
		blocks[b.Opcode] = b
	}
	run := func(op string, args map[string]any) any {
		t.Helper()
		b, ok := blocks["sipctime_"+op]
		if !ok {
			t.Fatalf("missing %s", op)
		}
		r, err := b.Handler(&e.Context{Args: args})
		if err != nil {
			t.Fatal(err)
		}
		return r.Value
	}
	for _, tc := range []struct {
		op   string
		args map[string]any
		want any
	}{
		{"timezone", nil, "UTC+9"},
		{"TimestampToTime", map[string]any{"timestamp": "0"}, "1970-01-01 09:00:00"},
		{"TimeToTimestamp", map[string]any{"time": "1970-01-01 09:00:01"}, 1000.0},
		{"TimeToTimestamp", map[string]any{"time": "1970-01-01"}, 0.0},
		{"TimeToTimestamp", map[string]any{"time": "2020-06-08T17:59:23Z"}, 1591639163000.0},
		{"Timedata", map[string]any{"timestamp": 0.0, "Timedata": "month"}, "01"},
		{"Timedata", map[string]any{"timestamp": 0.0, "Timedata": "year"}, 1970.0},
		{"differenceBetweenDates", map[string]any{"START": 0.0, "END": 86400000.0, "TIME_MENU": "hours"}, 24.0},
		{"differenceBetweenDates", map[string]any{"START": 86400000.0, "END": 0.0, "TIME_MENU": "days"}, -1.0},
		{"daysInMonth", map[string]any{"YEAR": 2024.0, "MONTH": 2.0}, 29.0},
		{"daysInMonth", map[string]any{"YEAR": 2023.0, "MONTH": 2.0}, 28.0},
		{"daysInMonth", map[string]any{"YEAR": 1.0, "MONTH": 0.0}, 31.0},
		{"daysInMonth", map[string]any{"YEAR": 0.0, "MONTH": 2.0}, 0.0},
		{"TimestampToTime", map[string]any{"timestamp": "invalid"}, "NaN-NaN-NaN NaN:NaN:NaN"},
	} {
		if got := run(tc.op, tc.args); got != tc.want {
			t.Errorf("%s(%v)=%v; want %v", tc.op, tc.args, got, tc.want)
		}
	}
	if got := run("TimeToTimestamp", map[string]any{"time": "invalid"}); !math.IsNaN(got.(float64)) {
		t.Fatal(got)
	}
	before := float64(time.Now().UnixMilli())
	got := run("Timestamp", nil).(float64)
	after := float64(time.Now().UnixMilli())
	if got < before || got > after {
		t.Fatal("timestamp is not Unix milliseconds", got)
	}
	for _, op := range []string{"Timestamp", "timezone", "Timedata", "TimestampToTime", "TimeToTimestamp", "differenceBetweenDateAndNow", "differenceBetweenDates", "daysInMonth", "formatTime"} {
		if blocks["sipctime_"+op].Text == "" {
			t.Error("missing monitor text", op)
		}
	}
}
