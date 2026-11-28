package media

import (
	"encoding/binary"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font/sfnt"
	linuxfonts "gowarp/platform/linux/fonts"
	windowsfonts "gowarp/platform/windows/fonts"
)

type systemFont struct {
	face   *sfnt.Font
	data   []byte
	offset int
}

type cachedSystemFont struct {
	once sync.Once
	font *systemFont
}

const (
	fontSans = iota
	fontSerif
	fontMono
	fontEmoji
	fontCJK
	fontSymbols
	fontYaHei
)

var systemFontFamilies = [...]string{
	fontSans: "sans", fontSerif: "serif", fontMono: "mono",
	fontEmoji: "emoji", fontCJK: "cjk", fontSymbols: "symbols", fontYaHei: "yahei",
}

var systemFonts [len(systemFontFamilies)][4]cachedSystemFont
var systemFontFiles sync.Map
var systemFontFaces sync.Map

func fontVariant(weight, style string) int {
	variant := 0
	weight = strings.ToLower(strings.TrimSpace(weight))
	n, _ := strconv.ParseFloat(weight, 64)
	if weight == "bold" || weight == "bolder" || n >= 600 {
		variant |= 1
	}
	style = strings.ToLower(strings.TrimSpace(style))
	if style == "italic" || strings.HasPrefix(style, "oblique") {
		variant |= 2
	}
	return variant
}

func fontFamily(family string) int {
	family = strings.ToLower(family)
	for _, name := range strings.Split(family, ",") {
		switch strings.Trim(strings.TrimSpace(name), "\"'") {
		case "microsoft yahei", "微软雅黑":
			return fontYaHei
		}
	}
	if strings.Contains(family, "mono") || strings.Contains(family, "courier") || strings.Contains(family, "consolas") {
		return fontMono
	}
	if strings.Contains(family, "emoji") {
		return fontEmoji
	}
	if family == "serif" || strings.Contains(family, "times") || strings.Contains(family, "georgia") {
		return fontSerif
	}
	return fontSans
}

func browserFont(family, weight, style string) *systemFont {
	variant := fontVariant(weight, style)
	kind := fontFamily(family)
	face := loadSystemFont(kind, variant)
	if face == nil && kind == fontYaHei {
		return loadSystemFont(fontSans, variant)
	}
	return face
}

func loadSystemFont(family, variant int) *systemFont {
	cached := &systemFonts[family][variant]
	cached.once.Do(func() {
		bold, italic := variant&1 != 0, variant&2 != 0
		var candidates []string
		switch runtime.GOOS {
		case "windows":
			candidates = windowsfonts.Candidates(systemFontFamilies[family], bold, italic)
		case "linux":
			path, index := linuxfonts.Match(systemFontFamilies[family], bold, italic)
			if path != "" {
				cached.font = readSystemFont(path, index)
				if cached.font != nil {
					return
				}
			}
			candidates = linuxfonts.Candidates(systemFontFamilies[family])
		}
		for _, path := range candidates {
			if cached.font = readSystemFont(path, 0); cached.font != nil {
				return
			}
		}
	})
	return cached.font
}

func readSystemFont(path string, index int) *systemFont {
	key := path + "#" + strconv.Itoa(index)
	value, _ := systemFontFiles.LoadOrStore(key, &cachedSystemFont{})
	cached := value.(*cachedSystemFont)
	cached.once.Do(func() {
		info, err := os.Stat(path)
		if err != nil || info.Size() > 64<<20 || info.Size() < 12 {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		collection, err := sfnt.ParseCollection(data)
		if err != nil || index < 0 || index >= collection.NumFonts() {
			return
		}
		face, err := collection.Font(index)
		if err != nil {
			return
		}
		offset := 0
		if string(data[:4]) == "ttcf" {
			record := 12 + index*4
			if record > len(data)-4 {
				return
			}
			offset = int(binary.BigEndian.Uint32(data[record:]))
		}
		cached.font = &systemFont{face: face, data: data, offset: offset}
		systemFontFaces.Store(face, cached.font)
	})
	return cached.font
}

func (f *systemFont) table(tag string) []byte {
	directory := fontBytes(f.data, f.offset, 12)
	if directory == nil {
		return nil
	}
	count := int(binary.BigEndian.Uint16(directory[4:]))
	for i := 0; i < count; i++ {
		record := fontBytes(f.data, f.offset+12+i*16, 16)
		if record == nil {
			return nil
		}
		if string(record[:4]) == tag {
			return fontBytes(f.data, int(binary.BigEndian.Uint32(record[8:])), int(binary.BigEndian.Uint32(record[12:])))
		}
	}
	return nil
}

func fontBytes(data []byte, offset, length int) []byte {
	if offset < 0 || length < 0 || offset > len(data) || length > len(data)-offset {
		return nil
	}
	return data[offset : offset+length]
}
