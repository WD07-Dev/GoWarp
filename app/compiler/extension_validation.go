package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gowarp/app/host"
	"gowarp/core/project"
)

const extensionValidationSuccess = "gowarp-extension-validation-ok\n"

func writeExtensionValidator(root string, sources []extensionSource) error {
	var main strings.Builder
	main.WriteString("package main\nimport (\n\"encoding/json\"\n\"fmt\"\n\"os\"\n\"gowarp/core/extension\"\n")
	for i := range sources {
		fmt.Fprintf(&main, "extension%d %s\n", i, strconv.Quote(fmt.Sprintf("gowarp/generatedextensions/extension%d", i)))
	}
	main.WriteString(")\nfunc main(){\nvar request struct { IDs, Opcodes []string }; if err:=json.NewDecoder(os.Stdin).Decode(&request);err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}\nif err:=extension.Validate(extension.Bundle{")
	for i := range sources {
		fmt.Fprintf(&main, "extension%d.New,", i)
	}
	fmt.Fprintf(&main, "}, request.IDs, request.Opcodes);err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}\nfmt.Print(%s)\n}\n", strconv.Quote(extensionValidationSuccess))
	dir := filepath.Join(root, "generatedvalidator")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "main.go"), []byte(main.String()), 0644)
}

/*
Validation runs on the author's host in a separate process. Factories are
native user code, not sandboxed code; handlers and lifecycle events are not run.
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

func validateExtensionProject(ctx context.Context, path string, p *project.Project) error {
	request := projectExtensionRequirements(p)
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
		return fmt.Errorf("Go extension registration validation failed: %w\n%s", err, output)
	}
	if !bytes.HasSuffix(output, []byte(extensionValidationSuccess)) {
		return fmt.Errorf("Go extension validator exited without confirming registration\n%s", output)
	}
	return nil
}
