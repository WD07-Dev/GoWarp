package vm

import (
	"fmt"

	"gowarp/core/extension"
)

func (c *extensionContext) shaderCall(operation string, args []any) (any, error) {
	r := c.r
	a := hostArgs(args)
	switch operation {
	case "graphics.shader.pipeline":
		names, ok := a.get(0).([]any)
		if !ok && a.get(0) != nil {
			return nil, fmt.Errorf("graphics.shader.pipeline requires an array")
		}
		var head, tail *StageShader
		for _, entry := range names {
			name := extension.String(entry)
			overrides := map[string]any{}
			if row, ok := entry.(map[string]any); ok {
				name = extension.String(row["name"])
				if values, ok := row["uniforms"].(map[string]any); ok {
					for key, value := range values {
						overrides[key] = shaderUniform(value)
					}
				}
			}
			shader := r.visual.shaders[name]
			if shader == nil {
				return nil, fmt.Errorf("unknown shader %q", name)
			}
			copy := *shader
			copy.program = shader
			copy.Next = nil
			copy.Overrides = overrides
			if head == nil {
				head = &copy
			} else {
				tail.Next = &copy
			}
			tail = &copy
		}
		r.options.View.Shader = head
		r.extensionViewChanged()
		return nil, nil

	case "graphics.shader.define":
		source, ok := a.get(1).(extension.ShaderSource)
		if !ok {
			source.Code = a.string(1)
		}
		uniforms := map[string]any{}
		if values, ok := a.get(2).(map[string]any); ok {
			for k, v := range values {
				uniforms[k] = shaderUniform(v)
			}
		}
		if r.visual.shaders == nil {
			r.visual.shaders = map[string]*StageShader{}
		}
		previous := r.visual.shaders[a.string(0)]
		replacement := &StageShader{
			Fragment: source.Code,
			Compiler: source.Compiler,
			Uniforms: uniforms,
		}
		r.visual.shaders[a.string(0)] = replacement
		if previous != nil {
			updated := replaceStageShader(r.options.View.Shader, previous, replacement)
			if updated != r.options.View.Shader {
				r.options.View.Shader = updated
				r.extensionViewChanged()
			}
		}
		return nil, nil
	case "graphics.shader.select":
		r.options.View.Shader = r.visual.shaders[a.string(0)]
		r.extensionViewChanged()
		return nil, nil
	case "graphics.shader.uniform":
		if shader := r.visual.shaders[a.string(0)]; shader != nil {
			shader.Uniforms[a.string(1)] = shaderUniform(a.get(2))
		}
		r.extensionViewChanged()
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported extension host operation %q", operation)
}

func shaderUniform(value any) any {
	switch values := value.(type) {
	case []any:
		out := make([]float32, len(values))
		for i, v := range values {
			out[i] = float32(extension.Number(v))
		}
		return out
	case []float64:
		out := make([]float32, len(values))
		for i, v := range values {
			out[i] = float32(v)
		}
		return out
	case []float32:
		return append([]float32(nil), values...)
	}
	return float32(extension.Number(value))
}

func replaceStageShader(head, previous, replacement *StageShader) *StageShader {
	used := false
	for pass := head; pass != nil; pass = pass.Next {
		if pass == previous || pass.program == previous {
			used = true
			break
		}
	}
	if !used {
		return head
	}
	var updated, tail *StageShader
	for pass := head; pass != nil; pass = pass.Next {
		copy := *pass
		copy.Next = nil
		if pass == previous || pass.program == previous {
			copy.Fragment = replacement.Fragment
			copy.Compiler = replacement.Compiler
			copy.Uniforms = replacement.Uniforms
			copy.program = replacement
		}
		if updated == nil {
			updated = &copy
		} else {
			tail.Next = &copy
		}
		tail = &copy
	}
	return updated
}
