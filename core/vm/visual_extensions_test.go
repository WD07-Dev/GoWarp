package vm

import (
	"context"
	"encoding/json"
	"gowarp/core/project"
	"testing"
)

func TestVisualDispatchUnrelatedOpcodeDoesNotAllocate(t *testing.T) {
	r := motionRuntime(t, "{}")
	b := &project.Block{Opcode: "motion_setx"}
	allocations := testing.AllocsPerRun(100, func() {
		_, handled, err := r.invokeExtension(context.Background(), nil, b, nil, false, 0, nil)
		if handled || err != nil {
			t.Fatal("unrelated opcode was handled", err)
		}
	})
	if allocations != 0 {
		t.Fatalf("unrelated opcode allocated %g objects", allocations)
	}
}

func TestCanvasEffectsChangeUsesReferenceDefaults(t *testing.T) {
	r := motionRuntime(t, `{}`)
	th := &thread{target: r.targets[1]}
	input := func(key string) (any, error) {
		if key == "EFFECT" {
			return "brightness", nil
		}
		return 25.0, nil
	}
	if _, handled, err := nativeTestCall(t, r, th, &project.Block{Opcode: "theshovelcanvaseffects_changeEffect"}, input, false); !handled || err != nil {
		t.Fatalf("canvas change: handled=%v err=%v", handled, err)
	}
	got, handled, err := nativeTestCall(t, r, th, &project.Block{Opcode: "theshovelcanvaseffects_geteffect"}, input, true)
	if !handled || err != nil || got.Value != 125.0 {
		t.Fatalf("brightness=%v handled=%v err=%v", got, handled, err)
	}
	if _, handled, err := nativeTestCall(t, r, th, &project.Block{Opcode: "theshovelcanvaseffects_cleareffects"}, input, false); !handled || err != nil {
		t.Fatal(handled, err)
	}
	got, _, _ = nativeTestCall(t, r, th, &project.Block{Opcode: "theshovelcanvaseffects_geteffect"}, input, true)
	if got.Value != 100.0 {
		t.Fatalf("reset brightness=%v", got)
	}
}

func TestStageShaderSavedUniformsAndUpdates(t *testing.T) {
	r := motionRuntime(t, `{}`)
	r.project.ExtensionStorage = map[string]json.RawMessage{"penP": json.RawMessage(`{"shaders":{"custom":{"projectData":{"fragShader":"void main(){}","projectData":{"savedVarState":{"strength":"2","color":["1","2","3","4"]}}}}}}`)}
	installTestExtensions(t, r, "penP_setNumberInShader", "OACShaded_setStageShaderAlt")
	th := &thread{target: r.targets[1]}
	call := func(op string, args map[string]any) {
		t.Helper()
		_, handled, err := nativeTestCall(t, r, th, &project.Block{Opcode: op}, func(k string) (any, error) { return args[k], nil }, false)
		if !handled || err != nil {
			t.Fatal(op, handled, err)
		}
	}
	call("OACShaded_setStageShaderAlt", map[string]any{"shader": "custom"})
	s := r.View().Shader
	if s == nil || s.Uniforms["strength"] != float32(2) {
		t.Fatalf("saved shader missing: %#v", s)
	}
	call("penP_setNumberInShader", map[string]any{"shader": "custom", "uniformName": "strength", "number": 5.0})
	if s.Uniforms["strength"] != float32(5) {
		t.Fatal(s.Uniforms)
	}
	call("OACShaded_setStageShaderAlt", map[string]any{"shader": "missing"})
	if r.View().Shader != nil {
		t.Fatal("reference clears framebuffer for an absent shader")
	}
}

func TestSpriteFilterClearPreservesScratchEffects(t *testing.T) {
	r := motionRuntime(t, `{}`)
	target := r.targets[1]
	target.state.Effects.Ghost = 70
	_, handled, err := nativeTestCall(t, r, &thread{target: target}, &project.Block{Opcode: "SPspriteEffects_removeAllFilters"}, nil, false)
	if !handled || err != nil || target.state.Effects.Ghost != 70 {
		t.Fatal(handled, err, target.state.Effects)
	}
}
