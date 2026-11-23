package vm

import (
	"testing"

	"gowarp/core/project"
)

func TestCloudUpdatesOnlyMarkedStageVariables(t *testing.T) {
	p := &project.Project{Targets: []*project.Target{{
		IsStage: true,
		Name:    "Stage",
		Variables: map[string][]any{
			"score":  {"score", 0, true},
			"local":  {"ordinary", 1},
			"prefix": {"☁ prefixed", 2},
		},
	}}}
	r := New(p)
	values := r.CloudValues()
	if len(values) != 2 || values["score"].Name != "score" {
		t.Fatalf("cloud flags lost: %v", values)
	}
	r.ApplyCloud("score", "42")
	r.ApplyCloud("local", "99")
	if r.CloudValues()["score"].Value != "42" || r.Snapshot()[0].Variables["local"].Value != 1 {
		t.Fatal("incorrect cloud update")
	}
}
