package host

import (
	"context"
	"os/exec"

	"gowarp/platform/linux/native"
)

const (
	HasDrives         = native.HasDrives
	FocusRestoreDraws = native.FocusRestoreDraws
	ClipboardUsesMeta = native.ClipboardUsesMeta
)

func SetWindowButtons(disableMinimize, disableMaximize bool) error {
	return native.SetWindowButtons(disableMinimize, disableMaximize)
}

func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	return native.Command(ctx, name, args...)
}

func NextDrive(path string) string {
	return native.NextDrive(path)
}

func ConfigureGamePriority() (string, error) {
	return "", nil
}
