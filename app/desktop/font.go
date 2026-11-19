package desktop

import (
	"os"
	"path/filepath"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"

	linuxfonts "gowarp/platform/linux/fonts"
	windowsfonts "gowarp/platform/windows/fonts"
)

func loadFont() font.Face {
	paths := []string{
		linuxfonts.NotoSansCJK,
		linuxfonts.NanumGothic,
	}
	if windows := windowsfonts.Malgun(); windows != "" {
		paths = append([]string{windows}, paths...)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		collection, err := opentype.ParseCollection(data)
		if err != nil {
			continue
		}
		// Noto Sans CJK collections use Korean at index 1; other fonts use index 0.
		index := 0
		if filepath.Base(path) == "NotoSansCJK-Regular.ttc" && collection.NumFonts() > 1 {
			index = 1
		}
		f, err := collection.Font(index)
		if err != nil {
			continue
		}
		face, err := opentype.NewFace(f, &opentype.FaceOptions{
			Size:    14,
			DPI:     72,
			Hinting: font.HintingFull,
		})
		if err == nil {
			return face
		}
	}
	f, _ := opentype.Parse(goregular.TTF)
	face, _ := opentype.NewFace(f, &opentype.FaceOptions{Size: 14, DPI: 72})
	return face
}
