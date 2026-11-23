package vm

import (
	"gowarp/core/extension"
	penplus "gowarp/test/extensions/penP"
	"testing"
)

func TestGenericShaderPipelinePreservesNamesAndDefersInvalidSource(t *testing.T) {
	r := fixture(t, "{}")
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	call := func(operation string, args ...any) {
		t.Helper()
		_, err := c.Call(operation, args...)
		if err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
	}
	call("graphics.shader.define", "native", "invalid but unused", map[string]any{"Amount": 2.0})
	if r.View().Shader != nil {
		t.Fatal("defining an unused shader selected it")
	}
	call("graphics.shader.uniform", "native", "Amount", 3.0)
	call("graphics.shader.pipeline", []any{map[string]any{"name": "native", "uniforms": map[string]any{"Amount": 4.0}}})
	shader := r.View().Shader
	if shader == nil || shader.Uniforms["Amount"] != float32(3) || shader.Overrides["Amount"] != float32(4) {
		t.Fatalf("generic pipeline = %+v", shader)
	}
	if shader.Compiler != nil {
		t.Fatal("raw shader received an extension compiler")
	}
	call("graphics.shader.select", "missing")
	if r.View().Shader != nil {
		t.Fatal("selecting a missing program did not clear the shader")
	}
}

func TestPenPlusShaderDefinitionIsLazyAndOwnsCompiler(t *testing.T) {
	r := fixture(t, "{}")
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	if _, err := c.Call("graphics.shader.define", "unused", extension.ShaderSource{Code: "invalid shader", Compiler: penplus.ShaderCompiler{}}); err != nil {
		t.Fatal(err)
	}
	sources := r.StageShaderSources()
	if len(sources) != 1 || sources[0].Compiler == nil {
		t.Fatalf("Pen+ sources = %+v", sources)
	}
	if r.View().Shader != nil {
		t.Fatal("definition selected unused shader")
	}
	if _, err := sources[0].Compile(); err == nil {
		t.Fatal("invalid shader did not fail on compilation")
	}
}

func TestUnscopedShaderAPIIsNotRegistered(t *testing.T) {
	r := fixture(t, "{}")
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	for _, operation := range []string{"shader.define", "shader.select", "shader.uniform", "shader.pipeline"} {
		if _, err := c.Call(operation); err == nil {
			t.Errorf("unscoped API %s is still registered", operation)
		}
	}
}

func TestShaderRedefinitionUpdatesActivePasses(t *testing.T) {
	for _, pipeline := range []bool{false, true} {
		name := "selected"
		if pipeline {
			name = "pipeline"
		}
		t.Run(name, func(t *testing.T) {
			r := fixture(t, "{}")
			c := &extensionContext{r: r, th: &thread{target: r.stage}}
			call := func(op string, args ...any) {
				t.Helper()
				if _, err := c.Call(op, args...); err != nil {
					t.Fatal(err)
				}
			}
			call("graphics.shader.define", "effect", "old", map[string]any{"Amount": 1.0})
			call("graphics.shader.define", "other", "old", map[string]any{"Amount": 9.0})
			if pipeline {
				call("graphics.shader.pipeline", []any{"effect", "other", map[string]any{"name": "effect", "uniforms": map[string]any{"Amount": 4.0}}})
			} else {
				call("graphics.shader.select", "effect")
			}
			before := r.View().Shader
			r.redrawRequested = false
			replacement := extension.ShaderSource{Code: "new", Compiler: penplus.ShaderCompiler{}}
			call("graphics.shader.define", "effect", replacement, map[string]any{"Amount": 2.0})
			after := r.View().Shader
			if after.Fragment != "new" || after.Compiler == nil || after.Uniforms["Amount"] != float32(2) || !r.redrawRequested {
				t.Fatalf("redefinition did not update active shader: %+v", after)
			}
			if before.Fragment != "old" || before.Compiler != nil || before.Uniforms["Amount"] != float32(1) {
				t.Fatal("redefinition mutated an earlier view")
			}
			if pipeline {
				if after.Next.Fragment != "old" || after.Next.Uniforms["Amount"] != float32(9) || after.Next.Next.Fragment != "new" || after.Next.Next.Overrides["Amount"] != float32(4) || after.Next.Next.Next != nil {
					t.Fatal("redefinition changed pipeline order, overrides, or a different program")
				}
				if before.Next.Next.Fragment != "old" {
					t.Fatal("redefinition mutated an earlier pipeline")
				}
			}
			call("graphics.shader.uniform", "effect", "Amount", 3.0)
			if after.Uniforms["Amount"] != float32(3) || (pipeline && after.Next.Next.Uniforms["Amount"] != float32(3)) {
				t.Fatal("active passes no longer share current program uniforms")
			}
			call("graphics.shader.define", "effect", "newer", map[string]any{"Amount": 5.0})
			if r.View().Shader.Fragment != "newer" || after.Fragment != "new" {
				t.Fatal("a second redefinition lost the program link or changed the previous view")
			}
			before = r.View().Shader
			r.redrawRequested = false
			call("graphics.shader.define", "unused", "invalid but unused")
			if r.View().Shader != before || r.redrawRequested {
				t.Fatal("unused definition changed the active pipeline")
			}
		})
	}
}
