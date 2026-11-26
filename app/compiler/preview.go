package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gowarp/app/config"
)

// PreparePreview keeps the project separate from the cached native runtime.
func PreparePreview(ctx context.Context, t Template, input, dir string, s config.Settings) (string, error) {
	input, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "preview.json")
	t, err = prepareRuntime(ctx, t, input, path, s)
	if err != nil {
		return "", err
	}
	s.Project = input
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	if len(data) > config.MaxSettingsSize {
		return "", fmt.Errorf("settings exceed preview limit")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	return t.Path, nil
}

func ReadPreview(dir string) (*Package, error) {
	file, err := os.Open(filepath.Join(dir, "preview.json"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, config.MaxSettingsSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > config.MaxSettingsSize {
		return nil, fmt.Errorf("settings exceed preview limit")
	}
	var s config.Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(s.Project) {
		return nil, fmt.Errorf("preview project must be an absolute path")
	}
	return &Package{Name: archiveName(s.Project), Settings: s}, nil
}
