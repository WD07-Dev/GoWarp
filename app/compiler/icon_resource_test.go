package compiler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gowarp/app/assets"

	"gowarp/app/config"
)

func TestWindowsIconResourcesPreserveRuntimeSections(t *testing.T) {
	var icon bytes.Buffer
	if err := png.Encode(&icon, image.NewRGBA(image.Rect(0, 0, 32, 32))); err != nil {
		t.Fatal(err)
	}
	input, err := filepath.Abs("../../test/fixtures/projects/counter.sb3")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		arch        string
		machine     uint16
		defaultIcon bool
	}{
		{arch: "386", machine: pe.IMAGE_FILE_MACHINE_I386},
		{arch: "amd64", machine: pe.IMAGE_FILE_MACHINE_AMD64},
		{arch: "arm64", machine: pe.IMAGE_FILE_MACHINE_ARM64},
		{arch: "amd64", machine: pe.IMAGE_FILE_MACHINE_AMD64, defaultIcon: true},
	} {
		name := target.arch
		if target.defaultIcon {
			name += "/default"
		}
		t.Run(name, func(t *testing.T) {
			original := iconTestPE(target.machine)
			path := filepath.Join(t.TempDir(), "player.exe")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(original)
			template := Template{
				Version: config.Version,
				OS:      "windows",
				Arch:    target.arch,
				SHA256:  hex.EncodeToString(hash[:]),
				Path:    path,
			}
			settings := config.Default(nil)
			if !target.defaultIcon {
				settings.Player.Icon = icon.Bytes()
			}
			path = filepath.Join(filepath.Dir(path), "game.exe")
			if err := writePackage(context.Background(), template, input, path, settings); err != nil {
				t.Fatal(err)
			}
			packaged, err := ReadPackage(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(packaged.Settings.Player.Icon, settings.Player.Icon) || len(packaged.Project) == 0 {
				t.Fatal("icon or project lost during export")
			}
			modified, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(original[512:1024], modified[512:1024]) {
				t.Fatal("runtime code section changed")
			}
			parsed, err := pe.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer parsed.Close()
			section := parsed.Section(".rsrc")
			if section == nil {
				t.Fatal("missing Windows resource section")
			}
			resources, err := section.Data()
			if err != nil {
				t.Fatal(err)
			}
			lookup := func(kind, id uint32) []byte {
				t.Helper()
				offset := uint32(0)
				for _, key := range []uint32{kind, id, 0} {
					count := int(binary.LittleEndian.Uint16(resources[offset+14:]))
					found := false
					for i := 0; i < count; i++ {
						entry := int(offset) + 16 + i*8
						if binary.LittleEndian.Uint32(resources[entry:]) == key {
							offset = binary.LittleEndian.Uint32(resources[entry+4:]) & 0x7fffffff
							found = true
							break
						}
					}
					if !found {
						t.Fatalf("missing resource %d/%d", kind, id)
					}
				}
				start := binary.LittleEndian.Uint32(resources[offset:]) - section.VirtualAddress
				size := binary.LittleEndian.Uint32(resources[offset+4:])
				return resources[start : start+size]
			}
			group := lookup(14, 1)
			if binary.LittleEndian.Uint16(group[2:]) != 1 || binary.LittleEndian.Uint16(group[4:]) != 4 {
				t.Fatal("invalid icon group")
			}
			for i, size := range []int{16, 32, 48, 256} {
				entry := group[6+i*14:]
				id := binary.LittleEndian.Uint16(entry[12:])
				data := lookup(3, uint32(id))
				info, err := png.DecodeConfig(bytes.NewReader(data))
				if err != nil || info.Width != size || info.Height != size || len(data) != int(binary.LittleEndian.Uint32(entry[8:])) {
					t.Fatalf("invalid %d-pixel resource: %+v, %v", size, info, err)
				}
				if size == 256 {
					expectedData := icon.Bytes()
					if target.defaultIcon {
						expectedData = assets.DefaultIcon
					}
					expected, err := png.Decode(bytes.NewReader(expectedData))
					if err != nil {
						t.Fatal(err)
					}
					actual, err := png.Decode(bytes.NewReader(data))
					if err != nil {
						t.Fatal(err)
					}
					r, g, b, a := actual.At(128, 128).RGBA()
					wr, wg, wb, wa := expected.At(expected.Bounds().Dx()/2, expected.Bounds().Dy()/2).RGBA()
					if r != wr || g != wg || b != wb || a != wa {
						t.Fatal("exported icon does not match the selected project or default icon")
					}
				}
			}
		})
	}
}

func iconTestPE(machine uint16) []byte {
	data := make([]byte, 1024)
	put16 := func(offset int, value uint16) {
		binary.LittleEndian.PutUint16(data[offset:], value)
	}
	put32 := func(offset int, value uint32) {
		binary.LittleEndian.PutUint32(data[offset:], value)
	}
	copy(data, "MZ")
	put32(60, 128)
	copy(data[128:], "PE\x00\x00")
	put16(132, machine)
	put16(134, 1)
	optional, optionalSize, magic, directories := 152, 240, uint16(0x20b), 108
	if machine == pe.IMAGE_FILE_MACHINE_I386 {
		optionalSize, magic, directories = 224, 0x10b, 92
	}
	put16(148, uint16(optionalSize))
	put16(optional, magic)
	put32(optional+8, 512)
	put32(optional+32, 4096)
	put32(optional+36, 512)
	put32(optional+56, 8192)
	put32(optional+60, 512)
	put32(optional+directories, 16)
	section := optional + optionalSize
	copy(data[section:], ".text")
	put32(section+8, 512)
	put32(section+12, 4096)
	put32(section+16, 512)
	put32(section+20, 512)
	put32(section+36, 0x60000020)
	for i := 512; i < len(data); i++ {
		data[i] = byte(i)
	}
	return data
}
