// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"runtime"
)

func New() e.Extension {
	return e.Extension{ID: "navigatorinfo", Blocks: []e.Block{
		{Opcode: "navigatorinfo_getOS", Kind: "reporter", Text: "operating system", Handler: func(*e.Context) (e.Result, error) {
			name := "Other"
			switch runtime.GOOS {
			case "windows":
				name = "Windows"
			case "linux":
				name = "Linux"
			case "darwin":
				name = "macOS"
			case "android":
				name = "Android"
			case "ios":
				name = "iOS"
			}
			return e.Result{Value: name}, nil
		}},
		{Opcode: "navigatorinfo_getBrowser", Kind: "reporter", Text: "browser", Handler: func(*e.Context) (e.Result, error) {
			return e.Result{Value: "Other"}, nil
		}},
		{Opcode: "navigatorinfo_getMemory", Kind: "reporter", Text: "device memory", Handler: func(*e.Context) (e.Result, error) {
			return e.Result{Value: "Unsupported"}, nil
		}},
	}}
}
