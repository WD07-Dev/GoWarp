package vm

import (
	"context"
	"fmt"

	"gowarp/core/extension"
)

type filePickerState struct {
	pick   func(context.Context, string) (string, error)
	ctx    context.Context
	cancel context.CancelFunc
}

/*
SetFilePicker installs the desktop's file reader before execution. The callback
runs on a worker and should respect cancellation; it must not access the VM.
It returns file contents as text or a data URL according to encoding.
*/
func (r *Runtime) SetFilePicker(pick func(context.Context, string) (string, error)) {
	r.cancelFiles()
	r.host.files.pick = pick
}

func (r *Runtime) cancelFiles() {
	if r.host.files.cancel != nil {
		r.host.files.cancel()
		r.host.files.cancel = nil
		r.host.files.ctx = nil
	}
}

func (r *Runtime) closeFiles() {
	r.cancelFiles()
	r.host.files.pick = nil
}

func (c *extensionContext) filesCall(operation string, args []any) (any, error) {
	switch operation {
	case "files.cancel":
		if len(args) != 0 {
			return nil, fmt.Errorf("files.cancel requires no arguments")
		}
		c.r.cancelFiles()
		return nil, nil
	case "files.pick":
		if len(args) != 1 {
			return nil, fmt.Errorf("files.pick requires an encoding")
		}
		state := &c.r.host.files
		if state.pick == nil {
			return nil, fmt.Errorf("file picker is unavailable in this host")
		}
		if state.ctx == nil {
			state.ctx, state.cancel = context.WithCancel(context.Background())
		}
		ctx, pick, encoding := state.ctx, state.pick, extension.String(args[0])
		result := make(chan string, 1)
		go func() {
			value, err := pick(ctx, encoding)
			if err != nil || ctx.Err() != nil {
				value = ""
			}
			// Buffered delivery never blocks when a stopped script abandons its reporter.
			result <- value
			close(result)
		}()
		return (<-chan string)(result), nil
	default:
		return nil, fmt.Errorf("unsupported extension host operation %q", operation)
	}
}
