package resource

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"image/png"
	"io"
	"os"

	"gowarp/app/assets"
)

func iconResources(icon []byte, rva uint32) ([]byte, error) {
	images, err := assets.IconImages(icon)
	if err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("missing icon image")
	}
	count := len(images)
	iconType := 32
	groupType := iconType + 16 + count*8
	names := groupType + 24
	entries := names + (count+1)*24
	data := make([]byte, entries+(count+1)*16)
	put16 := func(offset int, value uint16) {
		binary.LittleEndian.PutUint16(data[offset:], value)
	}
	put32 := func(offset int, value uint32) {
		binary.LittleEndian.PutUint32(data[offset:], value)
	}
	put16(14, 2)
	put32(16, 3) // RT_ICON
	put32(20, uint32(iconType)|0x80000000)
	put32(24, 14) // RT_GROUP_ICON
	put32(28, uint32(groupType)|0x80000000)
	put16(iconType+14, uint16(count))
	put16(groupType+14, 1)
	put32(groupType+16, 1)
	put32(groupType+20, uint32(names+count*24)|0x80000000)
	group := make([]byte, 6+14*count)
	binary.LittleEndian.PutUint16(group[2:], 1)
	binary.LittleEndian.PutUint16(group[4:], uint16(count))
	for i := 0; i <= count; i++ {
		name := names + i*24
		entry := entries + i*16
		put16(name+14, 1)
		put32(name+16, 0) // neutral language
		put32(name+20, uint32(entry))
		var payload []byte
		if i < count {
			put32(iconType+16+i*8, uint32(i+1))
			put32(iconType+20+i*8, uint32(name)|0x80000000)
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, images[i]); err != nil {
				return nil, err
			}
			payload = encoded.Bytes()
			position := 6 + i*14
			group[position] = byte(images[i].Bounds().Dx()) // 0 means 256
			group[position+1] = byte(images[i].Bounds().Dy())
			binary.LittleEndian.PutUint16(group[position+4:], 1)
			binary.LittleEndian.PutUint16(group[position+6:], 32)
			binary.LittleEndian.PutUint32(group[position+8:], uint32(len(payload)))
			binary.LittleEndian.PutUint16(group[position+12:], uint16(i+1))
		} else {
			payload = group
		}
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
		put32(entry, rva+uint32(len(data)))
		put32(entry+4, uint32(len(payload)))
		data = append(data, payload...)
	}
	return data, nil
}

func AddWindowsIcon(file *os.File, icon []byte) error {
	binaryFile, err := pe.NewFile(file)
	if err != nil {
		return fmt.Errorf("read Windows runtime: %w", err)
	}
	defer binaryFile.Close()
	var fileAlignment, sectionAlignment, headerSize uint32
	var directories []pe.DataDirectory
	directoryOffset := 0
	switch header := binaryFile.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		fileAlignment, sectionAlignment, headerSize = header.FileAlignment, header.SectionAlignment, header.SizeOfHeaders
		directories = header.DataDirectory[:]
		directoryOffset = 96
	case *pe.OptionalHeader64:
		fileAlignment, sectionAlignment, headerSize = header.FileAlignment, header.SectionAlignment, header.SizeOfHeaders
		directories = header.DataDirectory[:]
		directoryOffset = 112
	default:
		return fmt.Errorf("unsupported Windows runtime header")
	}
	if directories[2].VirtualAddress != 0 || directories[4].VirtualAddress != 0 {
		return fmt.Errorf("icon export requires an unsigned runtime without existing resources")
	}
	if fileAlignment == 0 || sectionAlignment == 0 || fileAlignment > 65536 || sectionAlignment > 65536 || headerSize < 64 || headerSize > 1<<20 || len(binaryFile.Sections) >= 96 {
		return fmt.Errorf("invalid Windows runtime layout")
	}
	header := make([]byte, headerSize)
	if _, err := file.ReadAt(header, 0); err != nil {
		return err
	}
	peOffset := int(binary.LittleEndian.Uint32(header[60:]))
	optional := peOffset + 24
	section := optional + int(binaryFile.SizeOfOptionalHeader) + len(binaryFile.Sections)*40
	if peOffset < 0 || section < optional || section+40 > len(header) || optional+directoryOffset+24 > len(header) {
		return fmt.Errorf("runtime has no room for an icon section header")
	}
	var virtualEnd uint64
	for _, existing := range binaryFile.Sections {
		if existing.Offset != 0 && uint64(existing.Offset) < uint64(section+40) {
			return fmt.Errorf("icon section header overlaps runtime data")
		}
		end := uint64(existing.VirtualAddress) + uint64(max(existing.VirtualSize, existing.Size))
		virtualEnd = max(virtualEnd, end)
	}
	align := func(value uint64, alignment uint32) uint64 {
		return (value + uint64(alignment) - 1) / uint64(alignment) * uint64(alignment)
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	rva := align(virtualEnd, sectionAlignment)
	rawOffset := align(uint64(info.Size()), fileAlignment)
	if rva > 0xffffffff || rawOffset > 0xffffffff {
		return fmt.Errorf("Windows runtime is too large")
	}
	resources, err := iconResources(icon, uint32(rva))
	if err != nil {
		return err
	}
	rawSize := align(uint64(len(resources)), fileAlignment)
	imageSize := align(rva+uint64(len(resources)), sectionAlignment)
	if rawOffset+rawSize > 0xffffffff || imageSize > 0xffffffff {
		return fmt.Errorf("Windows icon section is too large")
	}
	clear(header[section : section+40])
	copy(header[section:], ".rsrc")
	put32 := func(offset int, value uint32) {
		binary.LittleEndian.PutUint32(header[offset:], value)
	}
	put32(section+8, uint32(len(resources)))
	put32(section+12, uint32(rva))
	put32(section+16, uint32(rawSize))
	put32(section+20, uint32(rawOffset))
	put32(section+36, 0x40000040) // initialized data, readable
	binary.LittleEndian.PutUint16(header[peOffset+6:], uint16(len(binaryFile.Sections)+1))
	put32(optional+8, binary.LittleEndian.Uint32(header[optional+8:])+uint32(rawSize))
	put32(optional+56, uint32(imageSize))
	put32(optional+64, 0) // checksum is optional for an unsigned user application
	put32(optional+directoryOffset+16, uint32(rva))
	put32(optional+directoryOffset+20, uint32(len(resources)))
	if _, err := file.WriteAt(header, 0); err != nil {
		return err
	}
	if _, err := file.Seek(int64(rawOffset), io.SeekStart); err != nil {
		return err
	}
	if _, err := file.Write(resources); err != nil {
		return err
	}
	_, err = file.Write(make([]byte, int(rawSize)-len(resources)))
	return err
}
