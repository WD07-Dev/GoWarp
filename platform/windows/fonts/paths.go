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

func Candidates(family string, bold, italic bool) []string {
	windows := os.Getenv("WINDIR")
	if !filepath.IsAbs(windows) {
		return nil
	}
	var names []string
	suffix := ""
	if bold && italic {
		suffix = "bi"
	} else if bold {
		suffix = "bd"
	} else if italic {
		suffix = "i"
	}
	switch family {
	case "serif":
		names = []string{"times" + suffix + ".ttf"}
	case "mono":
		names = []string{"cour" + suffix + ".ttf"}
	case "emoji":
		names = []string{"seguiemj.ttf", "seguisym.ttf"}
	case "yahei":
		name := "msyh"
		if bold {
			name += "bd"
		}
		names = []string{name + ".ttc", name + ".ttf"}
	case "cjk":
		names = []string{"malgun.ttf", "meiryo.ttc", "msyh.ttc"}
	case "symbols":
		names = []string{"seguisym.ttf"}
	default:
		names = []string{"arial" + suffix + ".ttf"}
	}
	for i, name := range names {
		names[i] = filepath.Join(windows, "Fonts", name)
	}
	return names
}
