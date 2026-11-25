//go:build windows && amd64

package host

import (
	"gowarp/core/window"
	"gowarp/platform/windows/multiwindow"
)

func OpenNativeWindows(views []window.View, centerMain bool) (window.Backend, string, error) {
	backend, err := multiwindow.Open(views, centerMain)
	return backend, multiwindow.SourceTitle, err
}
