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

var systemFonts [6][4]cachedSystemFont
var systemFontFiles sync.Map
var systemFontFaces sync.Map

var systemFontFamilies = [...]string{"sans", "serif", "mono", "emoji", "cjk", "symbols"}

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
	if strings.Contains(family, "mono") || strings.Contains(family, "courier") || strings.Contains(family, "consolas") {
		return 2
	}
	if strings.Contains(family, "emoji") {
		return 3
	}
	if family == "serif" || strings.Contains(family, "times") || strings.Contains(family, "georgia") {
		return 1
	}
	return 0
}

func browserFont(family, weight, style string) *systemFont {
	return loadSystemFont(fontFamily(family), fontVariant(weight, style))
}

func loadSystemFont(family, variant int) *systemFont {
	cached := &systemFonts[family][variant]
	cached.once.Do(func() {
		bold, italic := variant&1 != 0, variant&2 != 0
		switch runtime.GOOS {
		case "windows":
			for _, path := range windowsfonts.Candidates(systemFontFamilies[family], bold, italic) {
				if cached.font = readSystemFont(path, 0); cached.font != nil {
					return
				}
			}
		case "linux":
			path, index := linuxfonts.Match(systemFontFamilies[family], bold, italic)
			if path != "" {
				cached.font = readSystemFont(path, index)
				if cached.font != nil {
					return
				}
			}
			for _, path := range linuxfonts.Candidates(systemFontFamilies[family]) {
				if cached.font = readSystemFont(path, 0); cached.font != nil {
					return
				}
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
