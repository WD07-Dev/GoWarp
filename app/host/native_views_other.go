//go:build !windows || !amd64

package host

import (
	"fmt"
	"gowarp/core/window"
)

func OpenNativeWindows([]window.View, bool) (window.Backend, string, error) {
	return nil, "", fmt.Errorf("native multiwindow is supported only on Windows amd64 with DirectX 11")
}
