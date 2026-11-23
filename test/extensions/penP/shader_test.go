package extension

import (
	"reflect"
	"strings"
	"testing"

	"gowarp/core/extension"
)

func TestShaderUniformsPreserveDefaultsAndFrameValues(t *testing.T) {
	program, err := (ShaderCompiler{}).Compile(`uniform float amount; uniform vec2 offset; uniform vec3 tint; uniform vec4 color; uniform mat4 matrix; void main(){gl_FragColor=vec4(amount);}`)
	if err != nil {
		t.Fatal(err)
	}
	bind := program.NewUniforms()
	values := map[string]any{"amount": float32(.25)}
	got := bind(values, extension.ShaderContext{Width: 320, Height: 180, Time: 2, Elapsed: 3, Nearest: true})
	want := map[string]any{
		"StageResolution": []float32{320, 180}, "StageTime": []float32{2}, "StageElapsed": []float32{3}, "StageNearest": 1,
		"U_amount": float32(.25), "U_offset": []float32{0, 0}, "U_tint": []float32{0, 0, 0}, "U_color": []float32{0, 0, 0, 0},
		"U_matrix": []float32{1, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniform values = %#v", got)
	}
	got["U_matrix"].([]float32)[0] = 99
	if next := bind(nil, extension.ShaderContext{}); next["U_amount"] != float32(0) || next["U_matrix"].([]float32)[0] != 1 {
		t.Fatalf("frame values leaked: %#v", next)
	}
	if len(values) != 1 {
		t.Fatal("binding changed input uniforms")
	}
}

func TestShaderRejectsOnlyLiveUnsupportedHelpers(t *testing.T) {
	program, err := (ShaderCompiler{}).Compile(`float unused(float a){ return unsupported(a); } void main(){gl_FragColor=vec4(1.);}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(program.Code, "unsupported") {
		t.Fatal("unused unsupported function included")
	}
	for _, source := range []string{"invalid shader", "void main(){", "uniform sampler2D extra;void main(){}"} {
		if _, err := (ShaderCompiler{}).Compile(source); err == nil {
			t.Fatalf("accepted %q", source)
		}
	}
}

func TestShaderUniformBindingAllocations(t *testing.T) {
	program, err := (ShaderCompiler{}).Compile(`uniform float amount; uniform vec2 offset; uniform vec3 tint; uniform vec4 color; uniform mat4 matrix; void main(){gl_FragColor=vec4(amount);}`)
	if err != nil {
		t.Fatal(err)
	}
	bind := program.NewUniforms()
	values := map[string]any{"amount": float32(.25)}
	context := extension.ShaderContext{Width: 640, Height: 480, Time: 1.5, Elapsed: .025}
	allocs := testing.AllocsPerRun(1000, func() {
		context.Time += .016
		if bind(values, context)["U_amount"] != float32(.25) {
			t.Fatal("uniform changed")
		}
	})
	if allocs != 0 {
		t.Fatalf("uniform binding allocated %.0f objects per pass", allocs)
	}
}

func BenchmarkShaderUniformBinding(b *testing.B) {
	program, err := (ShaderCompiler{}).Compile(`uniform float amount; uniform vec2 offset; uniform vec3 tint; uniform vec4 color; uniform mat4 matrix; void main(){gl_FragColor=vec4(amount);}`)
	if err != nil {
		b.Fatal(err)
	}
	bind := program.NewUniforms()
	values := map[string]any{"amount": float32(.25)}
	context := extension.ShaderContext{Width: 640, Height: 480, Time: 1.5, Elapsed: .025}
	b.ReportAllocs()
	for b.Loop() {
		context.Time += .016
		bind(values, context)
	}
}

func TestShaderBindingsAreRendererLocal(t *testing.T) {
	program, err := (ShaderCompiler{}).Compile(`uniform vec2 offset; void main(){gl_FragColor=vec4(offset,0.,1.);}`)
	if err != nil {
		t.Fatal(err)
	}
	first, second := program.NewUniforms(), program.NewUniforms()
	values := map[string]any{"offset": []float32{3, 4}}
	a := first(values, extension.ShaderContext{Width: 320, Height: 180, Time: 2})
	b := second(nil, extension.ShaderContext{Width: 640, Height: 480, Time: 5, Nearest: true})
	first(nil, extension.ShaderContext{Width: 800, Height: 600, Time: 9})
	if a["StageTime"].([]float32)[0] != 9 || b["StageTime"].([]float32)[0] != 5 || b["StageResolution"].([]float32)[0] != 640 || b["StageNearest"] != 1 {
		t.Fatal("renderer binding state leaked")
	}
	if !reflect.DeepEqual(a["U_offset"], []float32{0, 0}) || !reflect.DeepEqual(values["offset"], []float32{3, 4}) {
		t.Fatal("missing uniform did not reset or input was modified")
	}
}
