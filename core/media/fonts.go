package media

import (
	"strings"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"

	"gowarp/core/project"
)

func (lib *Library) loadFonts(p *project.Project, read func(string) ([]byte, error)) error {
	lib.fonts = make(map[string]*opentype.Font)
	parsed := make(map[string]*opentype.Font)
	for _, meta := range p.CustomFonts {
		if meta.System || meta.MD5Ext == "" {
			continue
		}
		face := parsed[meta.MD5Ext]
		if face == nil {
			data, err := read(meta.MD5Ext)
			if err != nil {
				return err
			}
			face, err = opentype.Parse(data)
			if err != nil {
				return err
			}
			parsed[meta.MD5Ext] = face
		}
		lib.fonts[meta.Family] = face
	}
	return nil
}

type svgFontResolver func(string) *opentype.Font

func (lib *Library) customFont(family string) *opentype.Font {
	for _, name := range strings.Split(family, ",") {
		name = strings.Trim(strings.TrimSpace(name), "\"'")
		if face := lib.fonts[name]; face != nil {
			return face
		}
	}
	return nil
}

func (lib *Library) TextFont(family string) *opentype.Font {
	if face := lib.customFont(family); face != nil {
		return face
	}
	if lib.fallbackFont == nil {
		lib.fallbackFont, _ = opentype.Parse(goregular.TTF)
	}
	return lib.fallbackFont
}
