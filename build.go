//go:build ignore

package main

import (
	"fmt"
	"os"

	"gowarp/app/assets"
	"gowarp/platform/windows/resource"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run build.go executable.exe")
		os.Exit(2)
	}
	if err := applyIcon(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func applyIcon(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = resource.AddWindowsIcon(file, assets.DefaultIcon)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
