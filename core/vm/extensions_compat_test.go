package vm

import (
	"context"
	"strings"
	"testing"

	"gowarp/core/extension"
	"gowarp/core/project"
	fixtures "gowarp/test/extensions"
)

// Install only modules requested by a fixture, preserving unrelated VM defaults.
func installTestExtensions(t *testing.T, r *Runtime, opcodes ...string) {
	t.Helper()
	if len(r.extensions) > 0 {
		return
	}
	wanted := map[string]bool{}
	if r.project != nil {
		for _, id := range r.project.Extensions {
			wanted[id] = true
		}
	}
	for _, opcode := range opcodes {
		id, _, _ := strings.Cut(opcode, "_")
		wanted[id] = true
	}
	for _, target := range r.targets {
		for _, block := range target.source.Blocks {
			id, _, _ := strings.Cut(block.Opcode, "_")
			wanted[id] = true
		}
	}
	var bundle extension.Bundle
	for _, factory := range fixtures.Bundle() {
		if wanted[factory().ID] {
			bundle = append(bundle, factory)
		}
	}
	if len(bundle) == 0 {
		return
	}
	if err := r.SetExtensions(bundle); err != nil {
		t.Fatal(err)
	}
}

// Exercise registered module handlers and normal input/Local handling directly.
func nativeTestCall(t *testing.T, r *Runtime, th *thread, b *project.Block, input func(string) (any, error), reporter bool) (extension.Result, bool, error) {
	t.Helper()
	installTestExtensions(t, r, b.Opcode)
	budget := stepsPerFrame
	return r.invokeExtension(context.Background(), th, b, input, reporter, 0, &budget)
}
