package native

import "fmt"

const (
	normalPriorityClass      = 0x20
	aboveNormalPriorityClass = 0x8000
)

func configureGamePriority(get func() (uint32, error), set func(uint32) error) (string, error) {
	before, err := get()
	if err != nil {
		return "", fmt.Errorf("read process priority: %w", err)
	}
	after := before
	if before == normalPriorityClass {
		if err := set(aboveNormalPriorityClass); err != nil {
			return "", fmt.Errorf("set Above Normal process priority: %w", err)
		}
		after, err = get()
		if err != nil {
			return "", fmt.Errorf("verify process priority: %w", err)
		}
	}
	return fmt.Sprintf("process_priority before=0x%x after=0x%x", before, after), nil
}
