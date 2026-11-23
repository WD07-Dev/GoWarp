package extension

import (
	"strings"
	"testing"
)

func TestActivityDataValidation(t *testing.T) {
	for name, values := range map[string]map[string]any{
		"encoding":      {"details": make(chan int)},
		"size":          {"details": strings.Repeat("x", 16*1024)},
		"unknown field": {"unknown": true},
		"field type":    {"details": 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := activityFromData(values); err == nil {
				t.Fatal("accepted invalid activity data")
			}
		})
	}
	values := map[string]any{"details": "Battle", "startTimestamp": float64(1700000000), "partySize": float64(1), "partyMax": float64(4)}
	activity, err := activityFromData(values)
	if err != nil {
		t.Fatal(err)
	}
	values["details"] = "changed"
	if activity != (Activity{Details: "Battle", StartTimestamp: 1700000000, PartySize: 1, PartyMax: 4}) {
		t.Fatalf("activity = %+v", activity)
	}
}
