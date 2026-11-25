package native

import "golang.org/x/sys/windows"

func ConfigureGamePriority() (string, error) {
	process := windows.CurrentProcess()
	return configureGamePriority(func() (uint32, error) {
		return windows.GetPriorityClass(process)
	}, func(class uint32) error {
		return windows.SetPriorityClass(process, class)
	})
}
