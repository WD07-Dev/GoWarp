package desktop

import (
	"image/color"
	"testing"

	"gowarp/core/extension"
	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestGenericStageShaderUsesExactUniformNames(t *testing.T) {
	runGPUFrame(t, func() {
		var gpu stageGPU
		defer gpu.Close()
		source := extension.ShaderSource{Code: `//kage:unit pixels
package main
var Amount float
func Fragment(position vec4, texCoord vec2, color vec4) vec4 { return vec4(Amount, 0, 0, 1) }`}
		if count := gpu.canvas.warmupPrograms([]extension.ShaderSource{source}); count != 1 {
			t.Fatalf("prepared=%d", count)
		}
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 4, Height: 4, Zoom: 100, Direction: 90, Shader: &vm.StageShader{Fragment: source.Code, Uniforms: map[string]any{"Amount": float32(.5)}}})
		dst := ebiten.NewImage(4, 4)
		defer dst.Deallocate()
		if err := gpu.Draw(dst, r, nil); err != nil {
			t.Fatal(err)
		}
		got := color.RGBAModel.Convert(dst.At(2, 2)).(color.RGBA)
		if got.R < 127 || got.R > 128 || got.G != 0 || got.B != 0 || got.A != 255 {
			t.Fatalf("raw shader pixel = %v", got)
		}
	})
}
