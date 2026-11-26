package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gowarp/core/project"
)

func ValidateOutputs(input string, p *project.Project, outputs ...string) error {
	var destinations []string
	for _, output := range outputs {
		if output == "" {
			continue
		}
		absolute, err := filepath.Abs(output)
		if err != nil {
			return err
		}
		if _, err := resolvedPath(absolute); err != nil {
			return err
		}
		destinations = append(destinations, absolute)
	}
	if len(destinations) == 0 {
		return nil
	}
	input, err := filepath.Abs(input)
	if err != nil {
		return err
	}
	sources, err := projectSourcePaths(input, p)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	sources = append(sources, executable)
	for _, output := range destinations {
		if err := protectOutput(output, sources); err != nil {
			return err
		}
	}
	return nil
}

func resolvedPath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		tail = append(tail, filepath.Base(path))
		path = parent
	}
}

func projectSourcePaths(input string, p *project.Project) ([]string, error) {
	sources := []string{input}
	info, err := os.Stat(input)
	if err != nil {
		return nil, err
	}
	root := ""
	if info.IsDir() {
		root = input
		sources = append(sources, filepath.Join(root, "project.json"))
	} else if strings.EqualFold(filepath.Ext(input), ".json") {
		root = filepath.Dir(input)
	}
	if root == "" {
		return sources, nil
	}
	if p == nil {
		p, err = project.Load(input)
		if err != nil {
			return nil, err
		}
	}
	add := func(name string) error {
		if name == "" {
			return nil
		}
		if strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
			return fmt.Errorf("invalid asset name %q", name)
		}
		sources = append(sources, filepath.Join(root, name))
		return nil
	}
	for _, target := range p.Targets {
		for _, costume := range target.Costumes {
			if err = add(project.AssetName(costume.MD5Ext, costume.AssetID, costume.DataFormat)); err != nil {
				return nil, err
			}
		}
		for _, sound := range target.Sounds {
			if err = add(project.AssetName(sound.MD5Ext, sound.AssetID, sound.DataFormat)); err != nil {
				return nil, err
			}
		}
	}
	return sources, nil
}

func protectOutput(output string, sources []string) error {
	for _, source := range sources {
		if sameFile(output, source) {
			return fmt.Errorf("output must differ from project sources and the running executable: %s", source)
		}
	}
	return nil
}

func overlapsRuntimeSources(root, destination string) bool {
	for _, name := range []string{"app", "core", "platform", "test"} {
		source := filepath.Join(root, name)
		if within(source, destination) || within(destination, source) {
			return true
		}
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		if within(filepath.Join(root, name), destination) {
			return true
		}
	}
	return false
}
