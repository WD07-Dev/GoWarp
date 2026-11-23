package desktop

import (
	"bytes"
	penplus "gowarp/test/extensions/penP"
	"image/color"
	"image/png"
	"testing"

	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"
)

func TestStageGPUSnapshotIncludesDeferredPenAndShader(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 32, Height: 24, Zoom: 100, Direction: 90, Color: "#ffffff", Shader: &vm.StageShader{Compiler: penplus.ShaderCompiler{}, Fragment: `precision mediump float;
   uniform sampler2D u_skin;
   varying vec2 v_texCoord;
   void main(){vec4 c=texture2D(u_skin,v_texCoord);gl_FragColor=vec4(c.b,c.r,c.g,c.a);}`}})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		deferred := deferredPen{pen}
		deferred.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{-5, 0, 5, 0}, Size: 6, Color: color.NRGBA{R: 255, A: 255}})
		data, err := deferred.Snapshot(nil)
		if err != nil {
			t.Fatal(err)
		}
		pixels, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		center := color.NRGBAModel.Convert(pixels.At(16, 12)).(color.NRGBA)
		if center.G < 250 || center.R > 5 || center.B > 5 {
			t.Fatalf("snapshot did not include deferred red pen transformed by stage shader: %v", center)
		}
	})
}
