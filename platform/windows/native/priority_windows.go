package native

import "syscall"

func ConfigureGamePriority() (string, error) {
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		return "", err
	}
	kernel := syscall.NewLazyDLL("kernel32.dll")
	get := kernel.NewProc("GetPriorityClass")
	set := kernel.NewProc("SetPriorityClass")
	return configureGamePriority(func() (uint32, error) {
		class, _, err := get.Call(uintptr(process))
		if class == 0 {
			return 0, err
		}
		return uint32(class), nil
	}, func(class uint32) error {
		ok, _, err := set.Call(uintptr(process), uintptr(class))
		if ok == 0 {
			return err
		}
		return nil
	})
}
