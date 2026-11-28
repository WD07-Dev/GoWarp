package fonts

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	NotoSansCJK = "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"
	NanumGothic = "/usr/share/fonts/truetype/nanum/NanumGothic.ttf"
)

func Match(family string, bold, italic bool) (string, int) {
	pattern := map[string]string{
		"sans": "sans-serif", "serif": "serif", "mono": "monospace",
		"emoji": "emoji", "cjk": "sans-serif:lang=ko", "symbols": "Symbola",
	}[family]
	if pattern == "" {
		return "", 0
	}
	if bold {
		pattern += ":weight=bold"
	}
	if italic {
		pattern += ":slant=italic"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "fc-match", "-f", "%{file}\n%{index}\n", "--", pattern).Output()
	if err != nil {
		return "", 0
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || !filepath.IsAbs(lines[0]) {
		return "", 0
	}
	index, err := strconv.Atoi(lines[1])
	if err != nil || index < 0 {
		return "", 0
	}
	return lines[0], index
}

func Candidates(family string) []string {
	switch family {
	case "sans":
		return []string{"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", "/usr/share/fonts/truetype/liberation2/LiberationSans-Regular.ttf"}
	case "serif":
		return []string{"/usr/share/fonts/truetype/dejavu/DejaVuSerif.ttf"}
	case "mono":
		return []string{"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"}
	case "emoji":
		return []string{"/usr/share/fonts/truetype/noto/NotoColorEmoji.ttf"}
	case "cjk":
		return []string{NotoSansCJK, NanumGothic}
	case "symbols":
		return []string{"/usr/share/fonts/truetype/ancient-scripts/Symbola_hint.ttf"}
	}
	return nil
}
