package extension

/*
ShaderSource is a lazily compiled stage fragment program. A nil Compiler
means Code is already Kage and uniform names and values are passed unchanged.
*/
type ShaderSource struct {
	Code     string
	Compiler ShaderCompiler
}

/*
ShaderCompiler adapts a source language and its uniform conventions to Kage.
ID must identify all compilation and binding semantics for cache reuse.
Compilers must be immutable and may not retain runtime or renderer state.
*/
type ShaderCompiler interface {
	ID() string
	Compile(string) (ShaderProgram, error)
}

func (s ShaderSource) Key() [2]string {
	id := "kage"
	if s.Compiler != nil {
		id = s.Compiler.ID()
	}
	return [2]string{id, s.Code}
}

func (s ShaderSource) Compile() (ShaderProgram, error) {
	if s.Compiler != nil {
		return s.Compiler.Compile(s.Code)
	}
	return ShaderProgram{Code: s.Code}, nil
}

// ShaderContext contains output dimensions in pixels and clocks in seconds.
type ShaderContext struct {
	Width, Height int
	Time, Elapsed float64
	Nearest       bool
}

/*
ShaderUniformBinding owns reusable output until its next call. Callers must
submit that output before binding another pass and must not modify it.
Binding must not mutate input values.
*/
type ShaderUniformBinding func(map[string]any, ShaderContext) map[string]any

/*
ShaderProgram contains immutable compilation data. NewUniforms creates private
binding state for each renderer; nil passes raw Kage uniforms through unchanged.
*/
type ShaderProgram struct {
	Code        string
	NewUniforms func() ShaderUniformBinding
}
