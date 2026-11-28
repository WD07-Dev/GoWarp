package assets

import _ "embed"

//go:embed extensions/music.js
var musicExtension []byte

func BuiltinExtensionSource(id string) []byte {
	switch id {
	case "music":
		return musicExtension
	}
	return nil
}
