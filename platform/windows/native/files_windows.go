package native

import (
	"os"
	"path/filepath"
	"strings"
)

func NextDrive(path string) string {
	current := strings.ToUpper(filepath.VolumeName(path))
	base := byte('C')
	if len(current) > 0 {
		base = current[0]
	}
	for offset := 1; offset <= 26; offset++ {
		letter := byte((int(base-'A')+offset)%26) + 'A'
		next := string(letter) + ":\\"
		if _, err := os.Stat(next); err == nil {
			return next
		}
	}
	return ""
}
