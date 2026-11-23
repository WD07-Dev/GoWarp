package fonts

import (
	"os"
	"path/filepath"
)

func Malgun() string {
	if windows := os.Getenv("WINDIR"); filepath.IsAbs(windows) {
		return filepath.Join(windows, "Fonts", "malgun.ttf")
	}
	return ""
}
