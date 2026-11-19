package extension

import (
	e "gowarp/core/extension"
	"testing"
)

func TestSpriteShaderCapability(t *testing.T) {
	def := New()
	for _, block := range def.Blocks {
		if block.Opcode != "OACShaded_shaderCompiledForSprites" {
			continue
		}
		if block.Kind != "boolean" {
			t.Fatalf("kind = %s", block.Kind)
		}
		for _, name := range []string{"____PEN_PLUS__NO__SHADER____", "custom"} {
			result, err := block.Handler(&e.Context{Args: map[string]any{"shader": name}})
			if err != nil || result.Value != false {
				t.Fatalf("%s: result=%+v error=%v", name, result, err)
			}
		}
		return
	}
	t.Fatal("missing sprite shader capability reporter")
}
