package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"text/template"

	"gowarp/app/host"
	"gowarp/core/project"
)

const extensionValidationSuccess = "gowarp-extension-validation-ok\n"

func writeExtensionProgram(root, name, source string, sources []extensionSource) error {
	program, err := template.New(name).Parse(source)
	if err != nil {
		return err
	}
	data := struct {
		Sources []extensionSource
		Success string
	}{sources, extensionValidationSuccess}
	var output bytes.Buffer
	if err := program.Execute(&output, data); err != nil {
		return err
	}
	code, err := format.Source(output.Bytes())
	if err != nil {
		return err
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "main.go"), code, 0644)
}

/*
Validation runs on the author's host in a separate process. Factories are
native user code, not sandboxed code; handlers and lifecycle events are not run.
Preparation runs before native generation and also on cache hits.
*/
type extensionRequirements struct{ IDs, Opcodes []string }

func projectExtensionRequirements(p *project.Project) extensionRequirements {
	request := extensionRequirements{}
	for _, id := range p.Extensions {
		if id != "pen" {
			request.IDs = append(request.IDs, id)
		}
	}
	opcodes := map[string]bool{}
	for _, target := range p.Targets {
		for _, block := range target.Blocks {
			if block != nil {
				opcodes[block.Opcode] = true
			}
		}
	}
	for _, monitor := range p.Monitors {
		if monitor.Opcode != "" {
			opcodes[monitor.Opcode] = true
		}
	}
	for opcode := range opcodes {
		request.Opcodes = append(request.Opcodes, opcode)
	}
	sort.Strings(request.Opcodes)
	return request
}

type extensionPreparationRequest struct {
	extensionRequirements
	Project     *project.Project
	ScriptOrder [][]string
	NativePath  string
}

func preparationRequest(p *project.Project) extensionPreparationRequest {
	request := extensionPreparationRequest{Project: p}
	if p != nil {
		request.extensionRequirements = projectExtensionRequirements(p)
		request.ScriptOrder = make([][]string, len(p.Targets))
		for i, target := range p.Targets {
			request.ScriptOrder[i] = target.ScriptOrder
		}
	}
	return request
}

func prepareExtensionProject(ctx context.Context, path string, p *project.Project, nativePath string) error {
	request := preparationRequest(p)
	request.NativePath = nativePath
	return runExtensionValidator(ctx, path, request)
}

func validateExtensionProject(ctx context.Context, path string, p *project.Project) error {
	return runExtensionValidator(ctx, path, projectExtensionRequirements(p))
}

func runExtensionValidator(ctx context.Context, path string, request any) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	command := host.Command(ctx, path)
	command.Stdin = bytes.NewReader(data)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("Go extension validation/preparation failed: %w\n%s", err, output)
	}
	if !bytes.HasSuffix(output, []byte(extensionValidationSuccess)) {
		return fmt.Errorf("Go extension validator exited without confirming registration\n%s", output)
	}
	return nil
}
