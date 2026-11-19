package vm

import (
	"context"
	"testing"

	"gowarp/core/project"
)

func TestPlayerSettingsOverrideProjectWithoutChangingTickSpeed(t *testing.T) {
	r := New(&project.Project{Targets: []*project.Target{{IsStage: true, Name: "Stage"}}})
	if err := r.Configure(1.5, true, true, 640, 480, "tester", "#123456"); err != nil {
		t.Fatal(err)
	}
	if err := r.Tick(context.Background(), 30, false); err != nil {
		t.Fatal(err)
	}
	if r.FPS() != 1.5 || !r.turbo || !r.Options().Interpolation || r.View().Width != 640 || r.username != "tester" {
		t.Fatal("settings not applied")
	}
	if err := r.Configure(0, false, false, 480, 360, "", "#ffffff"); err == nil {
		t.Fatal("invalid FPS accepted")
	}
}
