package extension

import (
	"testing"

	e "gowarp/core/extension"
)

func TestMemoryReportsUnsupportedWithoutHost(t *testing.T) {
	for _, block := range New().Blocks {
		if block.Opcode == "navigatorinfo_getMemory" {
			result, err := block.Handler(&e.Context{})
			if err != nil || result.Value != "Unsupported" {
				t.Fatalf("memory reporter = %v, %v", result.Value, err)
			}
			return
		}
	}
	t.Fatal("missing memory reporter")
}
