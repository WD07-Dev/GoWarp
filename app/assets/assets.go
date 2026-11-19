// Package assets embeds shared application resources.
package assets

import (
	_ "embed"
	"encoding/binary"
)

//go:embed embed/fonts/Pretendard-Regular.otf
var PretendardRegular []byte

//go:embed embed/fonts/LICENSE.txt
var FontLicense string

//go:embed embed/translations.csv
var Translations []byte

//go:embed embed/templates/extension.go.tmpl
var ExtensionGoTemplate string

/*
RuntimeSource is populated only in authoring builds. Treat it as read-only.
Player binaries and ordinary development builds do not carry the source SDK.
*/
var RuntimeSource []byte

//go:embed embed/gowarp-icon.ico
var defaultIconICO []byte

// DefaultIcon is the PNG payload of the bundled 256-pixel application icon.
var DefaultIcon = func() []byte {
	size := binary.LittleEndian.Uint32(defaultIconICO[14:18])
	offset := binary.LittleEndian.Uint32(defaultIconICO[18:22])
	return defaultIconICO[offset : offset+size]
}()
