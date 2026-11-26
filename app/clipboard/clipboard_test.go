package clipboard

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestClipboardTimeoutDoesNotStartMoreBlockedOperations(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := runSerialized(ctx, func() (string, error) {
		close(started)
		<-release
		return "late", nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked operation: %v", err)
	}
	select {
	case <-started:
	default:
		t.Fatal("operation did not start")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	called := make(chan struct{}, 1)
	_, err = runSerialized(ctx2, func() (string, error) { called <- struct{}{}; return "", nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("occupied clipboard: %v", err)
	}
	select {
	case <-called:
		t.Fatal("started another blocked operation")
	default:
	}
}
