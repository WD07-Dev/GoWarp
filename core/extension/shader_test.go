package extension_test

import (
	"testing"

	"gowarp/core/extension"
	penplus "gowarp/test/extensions/penP"
)

func TestRawShaderPreservesSourceAndUniforms(t *testing.T) {
	source := extension.ShaderSource{Code: "package main"}
	program, err := source.Compile()
	if err != nil || program.Code != source.Code {
		t.Fatalf("raw shader = %+v, %v", program, err)
	}
	if program.NewUniforms != nil {
		t.Fatal("raw Kage unexpectedly adapts uniform values")
	}
	adapted := extension.ShaderSource{Code: source.Code, Compiler: penplus.ShaderCompiler{}}
	if adapted.Key() == source.Key() {
		t.Fatal("different source languages share a cache key")
	}
}
