package extension

import (
	"testing"

	e "gowarp/core/extension"
)

type shaderHost struct{}

func (shaderHost) Call(string, ...any) (any, error) { return nil, nil }

func TestModeMonitorTracksPipelineCommands(t *testing.T) {
	def := New()
	c := &e.Context{State: map[string]any{}, Host: shaderHost{}}
	call := func(name string, args map[string]any) any {
		t.Helper()
		c.Args = args
		for _, block := range def.Blocks {
			if block.Opcode == "postprocessingv5_"+name {
				result, err := block.Handler(c)
				if err != nil {
					t.Fatal(err)
				}
				return result.Value
			}
		}
		t.Fatalf("missing %s", name)
		return nil
	}
	if got := call("opcodeGetPostProcess", nil); got != "none" {
		t.Fatalf("initial mode = %v", got)
	}
	call("opcodePipelineAdd", map[string]any{"Menu": "fisheye"})
	if got := call("opcodeGetPostProcess", nil); got != "pipeline" {
		t.Fatalf("mode after adding pass = %v", got)
	}
	call("opcodePipelineClear", nil)
	if got := call("opcodeGetPostProcess", nil); got != "pipeline" {
		t.Fatalf("clear changed mode: %v", got)
	}
	if got := call("opcodeGetPipeline", nil); got != "" {
		t.Fatalf("cleared pipeline = %v", got)
	}
}
