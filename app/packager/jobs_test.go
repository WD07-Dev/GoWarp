package packager

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJobLocksEditingAndAllowsCancellation(t *testing.T) {
	a := renderedApplication(t)
	a.ctx = context.Background()
	a.results = make(chan result, 1)
	a.editor = &a.form.fields[0].value
	a.job("테스트 작업 중…", func(ctx context.Context) result {
		<-ctx.Done()
		// Even work that finishes just after cancellation must not publish success.
		return result{status: "finished", form: a.form}
	})
	defer a.jobCancel()
	if a.editor != nil || len(a.hits) != 0 {
		t.Fatal("background job left stale editable controls active")
	}
	a.render()
	if len(a.hits) != 1 || a.hits[0].action == nil {
		t.Fatal("busy view must expose only its cancel action")
	}
	a.hits[0].action()
	select {
	case r := <-a.results:
		if r.err != context.Canceled {
			t.Fatalf("canceled work published success: %v", r.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("job cancellation did not finish")
	}
}

func TestSaveValidatesSettingsInBackground(t *testing.T) {
	a := renderedApplication(t)
	a.ctx = context.Background()
	a.results = make(chan result, 1)
	for i := range a.form.fields {
		if a.form.fields[i].key == "fps" {
			a.form.fields[i].value = "invalid"
		}
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	a.save(path, false)
	if !a.busy {
		t.Fatal("save validated settings on the UI thread before starting its job")
	}
	select {
	case r := <-a.results:
		if r.err == nil {
			t.Fatal("invalid settings were saved")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("validation did not finish")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid save created output: %v", err)
	}
}
