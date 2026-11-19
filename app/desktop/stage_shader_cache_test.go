package desktop

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"
)

func cacheTestShader(index int) *vm.StageShader {
	return &vm.StageShader{Fragment: fmt.Sprintf("// variant %d\n", index) + `//kage:unit pixels
package main
func Fragment(position vec4, texCoord vec2, color vec4) vec4 { return vec4(0.25, 0, 0, 1) }
`}
}

func TestStageShaderCacheRetainsActiveAndRecentPrograms(t *testing.T) {
	runGPUFrame(t, func() {
		var gpu stageGPU
		defer gpu.Close()
		renderer := render.New(&media.Library{})
		dst := ebiten.NewImage(2, 2)
		defer dst.Deallocate()
		draw := func(shader *vm.StageShader) {
			t.Helper()
			renderer.SetView(vm.View{Width: 2, Height: 2, Zoom: 100, Direction: 90, Shader: shader})
			if err := gpu.Draw(dst, renderer, nil); err != nil {
				t.Fatal(err)
			}
			if shader != nil {
				pixel := color.RGBAModel.Convert(dst.At(0, 0)).(color.RGBA)
				if pixel.R < 63 || pixel.R > 64 || pixel.G != 0 || pixel.B != 0 || pixel.A != 255 {
					t.Fatalf("shader pixel=%v", pixel)
				}
			}
		}
		first := cacheTestShader(0)
		draw(first)
		original := gpu.canvas.programs[first.Source().Key()].shader
		for i := 1; i < 40; i++ {
			draw(cacheTestShader(i))
		}
		if len(gpu.canvas.programs) > 32 {
			t.Fatalf("dynamic shader cache grew to %d", len(gpu.canvas.programs))
		}
		recent := cacheTestShader(8)
		cached := gpu.canvas.programs[recent.Source().Key()].shader
		draw(recent)
		draw(cacheTestShader(40))
		if gpu.canvas.programs[recent.Source().Key()].shader != cached {
			t.Fatal("recently used shader was evicted")
		}
		if _, ok := gpu.canvas.programs[cacheTestShader(9).Source().Key()]; ok {
			t.Fatal("least recently used shader was retained")
		}
		draw(first)
		if gpu.canvas.programs[first.Source().Key()].shader == original {
			t.Fatal("retired shader was not recompiled")
		}
		var head *vm.StageShader
		for i := 0; i < 36; i++ {
			pass := cacheTestShader(100 + i)
			pass.Next = head
			head = pass
		}
		draw(head)
		for pass := head; pass != nil; pass = pass.Next {
			if _, ok := gpu.canvas.programs[pass.Source().Key()]; !ok {
				t.Fatal("an active pipeline program was evicted")
			}
		}
		draw(nil)
		if len(gpu.canvas.programs) > 32 {
			t.Fatal("cache did not shrink after the pipeline was cleared")
		}
		gpu.canvas.close()
		if len(gpu.canvas.programs) != 0 {
			t.Fatal("close retained shader programs")
		}
	})
}

func TestStageShaderCacheBoundsRetainedSourceBytes(t *testing.T) {
	runGPUFrame(t, func() {
		var gpu stageGPU
		defer gpu.Close()
		renderer := render.New(&media.Library{})
		dst := ebiten.NewImage(1, 1)
		defer dst.Deallocate()
		for i := 0; i < 20; i++ {
			shader := cacheTestShader(i)
			shader.Fragment += "//" + strings.Repeat("x", 160<<10) + "\n"
			renderer.SetView(vm.View{Width: 1, Height: 1, Zoom: 100, Direction: 90, Shader: shader})
			if err := gpu.Draw(dst, renderer, nil); err != nil {
				t.Fatal(err)
			}
		}
		retained := 0
		for key, program := range gpu.canvas.programs {
			retained += len(key[1]) + len(program.program.Code)
		}
		if retained > 4<<20 {
			t.Fatalf("retained shader source bytes=%d", retained)
		}
	})
}
