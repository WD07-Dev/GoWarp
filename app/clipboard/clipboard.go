// Package clipboard provides text copy and paste for the editor.
package clipboard

import (
	"context"
	"errors"
	"fmt"

	native "golang.design/x/clipboard"
)

const textLimit = 64 * 1024

var operationSlot = make(chan struct{}, 1)

func Read(ctx context.Context) (string, error) {
	return ReadWithLimit(ctx, textLimit)
}

func ReadWithLimit(ctx context.Context, limit int) (string, error) {
	if limit <= 0 {
		return "", errors.New("clipboard limit must be positive")
	}
	return runSerialized(ctx, func() (string, error) {
		if err := native.Init(); err != nil {
			return "", err
		}
		data, err := native.Read(ctx, native.FmtText)
		if errors.Is(err, native.ErrNoData) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if len(data) > limit {
			return "", fmt.Errorf("clipboard text exceeds %d KiB", (limit+1023)/1024)
		}
		return string(data), nil
	})
}

func Write(ctx context.Context, text string) error {
	return WriteWithLimit(ctx, text, textLimit)
}

func WriteWithLimit(ctx context.Context, text string, limit int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if limit <= 0 {
		return errors.New("clipboard limit must be positive")
	}
	if len(text) > limit {
		return fmt.Errorf("clipboard text exceeds %d KiB", (limit+1023)/1024)
	}
	_, err := runSerialized(ctx, func() (string, error) {
		if err := native.Init(); err != nil {
			return "", err
		}
		// This channel signals replacement by another copy, not write completion.
		_, err := native.Write(ctx, native.FmtText, []byte(text))
		return "", err
	})
	return err
}

/*
runSerialized returns on cancellation even if a native read cannot stop.
The slot stays occupied until that read ends, preventing blocked goroutines from accumulating.
*/
func runSerialized(ctx context.Context, operation func() (string, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	select {
	case operationSlot <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		defer func() { <-operationSlot }()
		if err := ctx.Err(); err != nil {
			done <- result{err: err}
			return
		}
		text, err := operation()
		done <- result{text: text, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case value := <-done:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return value.text, value.err
	}
}
