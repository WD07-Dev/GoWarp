package desktop

import (
	"github.com/hajimehoshi/ebiten/v2"
	penplus "gowarp/test/extensions/penP"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStageShaderTranslation(t *testing.T) {
	_, err := ebiten.NewShader([]byte(`package main
func Fragment(position vec4, texCoord vec2, color vec4) vec4 { return color }
`))
	if err != nil {
		t.Fatal(err)
	}
	/*
		Exercise live function dependencies, uniforms, lower-left framebuffer
		coordinates and a fragment output without relying on a shader's name.
	*/
	source := `uniform float amount;
uniform vec2 u_res;
uniform sampler2D u_skin;
float offset(float x) { return x + amount; }
void main() { vec2 uv = gl_FragCoord.xy / u_res; gl_FragColor = texture2D(u_skin, vec2(offset(uv.x),uv.y)); }`
	program, err := (penplus.ShaderCompiler{}).Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ebiten.NewShader([]byte(program.Code)); err != nil {
		t.Fatalf("%v\n%s", err, program.Code)
	}
}

func TestStagePostShaderCompilation(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	for _, name := range []string{"canvas_gpu.kage", "split_gpu.kage"} {
		source, err := os.ReadFile(filepath.Join(filepath.Dir(file), name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ebiten.NewShader(source); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestStageShaderMultilineArrayAndConditional(t *testing.T) {
	source := `uniform float power;
float unused(float v){return v;}
vec2 unused(vec2 v){return v;}
float factor(float a) {bool positive=a>0.;return positive ? a : 0.;}
void main(){
 const int count=4;float values[4];values[0]=.1;values[1]=.2;values[2]=.3;values[3]=.4;
 float sum=0.;for(int i=0;i<count;i++){sum+=values[i];}
 gl_FragColor=vec4(
 factor(sum)*power,
 sin(sum)
 *cos(sum),
 0.,1.
 );
}`
	program, err := (penplus.ShaderCompiler{}).Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ebiten.NewShader([]byte(program.Code)); err != nil {
		t.Fatalf("%v\n%s", err, program.Code)
	}
}
