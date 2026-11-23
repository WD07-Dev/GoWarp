// Package dialog opens file selection dialogs through the native system UI.
package dialog

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/ncruces/zenity"
)

var ErrCancelled = errors.New("file selection cancelled")

var ErrUnavailable = errors.New("native file selection unavailable")

type Options struct {
	Title, Directory, Filename string
	Patterns                   []string
	Save                       bool
}

func Select(ctx context.Context, o Options) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !zenity.IsAvailable() {
		return "", ErrUnavailable
	}
	initial := filepath.Join(o.Directory, o.Filename)
	if o.Filename == "" {
		initial += string(filepath.Separator)
	}
	options := []zenity.Option{zenity.Context(ctx), zenity.Title(o.Title), zenity.Filename(initial)}
	if len(o.Patterns) > 0 {
		options = append(options,
			zenity.FileFilter{Name: "Projects", Patterns: append([]string(nil), o.Patterns...)},
			zenity.FileFilter{Name: "All files", Patterns: []string{"*"}},
		)
	}
	var path string
	var err error
	if o.Save {
		path, err = zenity.SelectFileSave(options...)
	} else {
		path, err = zenity.SelectFile(options...)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if errors.Is(err, zenity.ErrCanceled) || err == nil && path == "" {
		return "", ErrCancelled
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "", ErrUnavailable
	}
	if err != nil {
		return "", fmt.Errorf("file dialog: %w", err)
	}
	return path, nil
}
