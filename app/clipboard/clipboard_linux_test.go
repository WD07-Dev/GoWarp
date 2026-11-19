package clipboard

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestClipboardTextAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Read(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("read canceled: %v", err)
	}
	if err := Write(ctx, "ignored"); !errors.Is(err, context.Canceled) {
		t.Fatalf("write canceled: %v", err)
	}
	if os.Getenv("DISPLAY") == "" {
		t.Skip("clipboard integration requires X11; use xvfb-run")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const text = "Hello 한글 🙂\nsecond line"
	if err := Write(ctx, text); err != nil {
		t.Fatal(err)
	}
	got, err := Read(ctx)
	if err != nil || got != text {
		t.Fatalf("round trip: %q, %v", got, err)
	}
	if err := Write(ctx, strings.Repeat("x", 64*1024+1)); err == nil {
		t.Fatal("accepted oversized clipboard text")
	}
	got, err = Read(ctx)
	if err != nil || got != text {
		t.Fatalf("rejected write changed clipboard: %q, %v", got, err)
	}
	if err := Write(ctx, ""); err != nil {
		t.Fatal(err)
	}
	got, err = Read(ctx)
	if err != nil || got != "" {
		t.Fatalf("empty text: %q, %v", got, err)
	}
}

func TestClipboardCodeSourceLimit(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("requires X11")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	source := strings.Repeat("// extension source\n", 6000)
	if err := WriteWithLimit(ctx, source, 16<<20); err != nil {
		t.Fatal(err)
	}
	got, err := ReadWithLimit(ctx, 16<<20)
	if err != nil || got != source {
		t.Fatalf("large source round trip: len=%d err=%v", len(got), err)
	}
	if _, err = Read(ctx); err == nil {
		t.Fatal("ordinary input lost its clipboard limit")
	}
	if err = WriteWithLimit(ctx, source, 64<<10); err == nil {
		t.Fatal("ignored requested limit")
	}
	if _, err = ReadWithLimit(ctx, 0); err == nil {
		t.Fatal("accepted invalid limit")
	}
}
