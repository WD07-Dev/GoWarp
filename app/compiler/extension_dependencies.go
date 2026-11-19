package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"gowarp/app/host"
	"gowarp/core/extension"
)

func collectExtensionModules(sources []extensionSource) ([]extension.ModuleRequirement, error) {
	modules := make(map[string]extension.ModuleRequirement)
	owners := make(map[string]string)
	for _, source := range sources {
		for _, requirement := range source.Requirements {
			if previous, ok := modules[requirement.Path]; ok && previous.Version != requirement.Version {
				return nil, fmt.Errorf("Go extension dependencies: %s requires %s %s, but %s requires %s",
					source.ID, requirement.Path, requirement.Version, owners[requirement.Path], previous.Version)
			}
			modules[requirement.Path] = requirement
			owners[requirement.Path] = source.ID
		}
	}
	requirements := make([]extension.ModuleRequirement, 0, len(modules))
	for _, requirement := range modules {
		requirements = append(requirements, requirement)
	}
	slices.SortFunc(requirements, func(a, b extension.ModuleRequirement) int {
		return strings.Compare(a.Path, b.Path)
	})
	return requirements, nil
}

func prepareExtensionModules(ctx context.Context, goTool, work string, env []string, requirements []extension.ModuleRequirement) error {
	if len(requirements) == 0 {
		return nil
	}
	run := func(args ...string) ([]byte, error) {
		command := host.Command(ctx, goTool, args...)
		command.Dir, command.Env = work, env
		var diagnostics bytes.Buffer
		command.Stderr = &diagnostics
		output, err := command.Output()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, fmt.Errorf("Go extension dependencies (%s): %w\n%s%s", strings.Join(args, " "), err, output, diagnostics.Bytes())
		}
		return output, nil
	}
	type moduleRequirement struct {
		Path     string
		Version  string
		Indirect bool
	}
	readRequirements := func() (map[string]moduleRequirement, error) {
		data, err := run("mod", "edit", "-json")
		if err != nil {
			return nil, err
		}
		var mod struct {
			Require []moduleRequirement
		}
		if err := json.Unmarshal(data, &mod); err != nil {
			return nil, fmt.Errorf("Go extension dependencies: %w", err)
		}
		versions := make(map[string]moduleRequirement, len(mod.Require))
		for _, requirement := range mod.Require {
			versions[requirement.Path] = requirement
		}
		return versions, nil
	}
	pinned, err := readRequirements()
	if err != nil {
		return err
	}
	args := []string{"mod", "edit"}
	for _, requirement := range requirements {
		if previous, ok := pinned[requirement.Path]; ok && previous.Version != requirement.Version {
			return fmt.Errorf("Go extension dependencies: %s %s conflicts with runtime version %s",
				requirement.Path, requirement.Version, previous.Version)
		}
		pinned[requirement.Path] = moduleRequirement{Path: requirement.Path, Version: requirement.Version}
		args = append(args, "-require="+requirement.Path+"@"+requirement.Version)
	}
	if _, err := run(args...); err != nil {
		return err
	}
	data, err := run("list", "-mod=mod", "-m", "-json", "all")
	if err != nil {
		return err
	}
	graph := make(map[string]string)
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var module extension.ModuleRequirement
		if err := decoder.Decode(&module); err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("Go extension dependency graph: %w", err)
		}
		graph[module.Path] = module.Version
	}
	// Tidy covers platform-specific imports before either cross-compiled binary is built.
	if _, err := run("mod", "tidy"); err != nil {
		return err
	}
	resolved, err := readRequirements()
	if err != nil {
		return err
	}
	for module, selected := range resolved {
		if required, ok := pinned[module]; ok && selected.Version != required.Version {
			return fmt.Errorf("Go extension dependencies: %s resolves to %s, but %s is pinned; use compatible library versions",
				module, selected.Version, required.Version)
		} else if !ok && !selected.Indirect {
			return fmt.Errorf("Go extension dependencies: imported module %s needs its own require entry in @gowarp-config", module)
		}
		if graph[module] != selected.Version {
			return fmt.Errorf("Go extension dependencies: %s %s was not pinned by the declared module graph; add a require entry in @gowarp-config",
				module, selected.Version)
		}
	}
	return nil
}
