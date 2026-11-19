package desktop

import (
	"encoding/json"
	penplus "gowarp/test/extensions/penP"
	"os"
	"strconv"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"gowarp/core/media"
	"gowarp/core/render"
	"gowarp/core/vm"
)

func TestStageShaderProject(t *testing.T) {
	path := os.Getenv("GOWARP_SHADER_CHECK")
	if path == "" {
		t.Skip("set GOWARP_SHADER_CHECK to project.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Targets []struct {
			Blocks map[string]json.RawMessage `json:"blocks"`
		} `json:"targets"`
		ExtensionStorage struct {
			Pen struct {
				Shaders map[string]struct {
					ProjectData struct {
						Fragment    string `json:"fragShader"`
						ProjectData struct {
							Uniforms map[string]any `json:"savedVarState"`
						} `json:"projectData"`
					} `json:"projectData"`
				} `json:"shaders"`
			} `json:"penP"`
		} `json:"extensionStorage"`
	}
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{}
	for _, target := range p.Targets {
		for _, raw := range target.Blocks {
			var b struct {
				Opcode string           `json:"opcode"`
				Fields map[string][]any `json:"fields"`
			}
			if json.Unmarshal(raw, &b) == nil && b.Opcode == "OACShaded_menu_shadersAndStageALT" {
				for _, field := range b.Fields {
					if len(field) > 0 {
						if name, ok := field[0].(string); ok {
							used[name] = true
						}
					}
				}
			}
		}
	}
	runGPUFrame(t, func() {
		for name, s := range p.ExtensionStorage.Pen.Shaders {
			if !used[name] {
				continue
			}
			t.Run(name, func(t *testing.T) {
				program, err := (penplus.ShaderCompiler{}).Compile(s.ProjectData.Fragment)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = ebiten.NewShader([]byte(program.Code)); err != nil {
					t.Fatalf("%v\n%s", err, program.Code)
				}
				uniforms := map[string]any{}
				number := func(v any) float32 {
					switch n := v.(type) {
					case float64:
						return float32(n)
					case string:
						f, _ := strconv.ParseFloat(n, 32)
						return float32(f)
					}
					return 0
				}
				for key, value := range s.ProjectData.ProjectData.Uniforms {
					if values, ok := value.([]any); ok {
						v := make([]float32, len(values))
						for i, n := range values {
							v[i] = number(n)
						}
						uniforms[key] = v
					} else {
						uniforms[key] = number(value)
					}
				}
				r := render.New(&media.Library{})
				r.SetView(vm.View{Width: 16, Height: 12, Zoom: 100, Direction: 90, Color: "#804020", Shader: &vm.StageShader{Compiler: penplus.ShaderCompiler{}, Fragment: s.ProjectData.Fragment, Uniforms: uniforms}, ShaderTime: 1})
				var g stageGPU
				defer g.Close()
				dst := ebiten.NewImage(16, 12)
				defer dst.Deallocate()
				if err := g.Draw(dst, r, nil); err != nil {
					t.Fatal(err)
				}
				dst.ReadPixels(make([]byte, 16*12*4))
			})
		}
	})
}
