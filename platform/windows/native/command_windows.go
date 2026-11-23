package native

import (
	"context"
	"os/exec"
	"syscall"
)

func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, args...)
	/*
		Go and the extension validator are console programs. Keep their output
		in the existing pipes without opening a console from the GUI packager.
	*/
	const createNoWindow = 0x08000000
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	return command
}
