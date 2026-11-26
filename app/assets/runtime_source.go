//go:build gowarp_packager

package assets

import _ "embed"

//go:embed embed/runtime-source.zip
var runtimeSource []byte

func init() {
	RuntimeSource = runtimeSource
}
