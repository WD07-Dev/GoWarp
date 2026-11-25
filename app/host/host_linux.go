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

func SetWindowProperties(disableMinimize, disableMaximize, disableClose bool, opacity float64) error {
	return native.SetWindowProperties(disableMinimize, disableMaximize, disableClose, opacity)
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
