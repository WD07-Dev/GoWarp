package runtimehost

import (
	"os"
	"path/filepath"
	"strings"

	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/render"
	"gowarp/core/vm"
)

func Headless(r *vm.Runtime, path string, p *project.Project) error {
	assets, err := media.LoadVisual(path, p)
	if err != nil {
		return err
	}
	r.SetGraphics(render.New(assets))
	return ConfigureSave(r, path)
}

func ConfigureSave(r *vm.Runtime, path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return r.SetSavePath(filepath.Join(home, ".gowarp", "saves", name+".json"))
}
