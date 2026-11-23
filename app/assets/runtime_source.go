//go:build gowarp_packager

package assets

import _ "embed"

// Generated temporarily by build.sh; never included in the source archive.
//
//go:embed embed/runtime-source.zip
var runtimeSource []byte

func init() {
	RuntimeSource = runtimeSource
}
