package project

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
)

/*
ArchiveBytes retains an existing SB3 byte-for-byte. Unpacked projects become
a bounded archive containing the original manifest and only referenced assets.
*/
func ArchiveBytes(path string) ([]byte, error) {
	absolute, root, _, err := unpackedLocation(path)
	if err != nil {
		return nil, err
	}
	if root == "" {
		file, err := os.Open(absolute)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<30 {
			return nil, fmt.Errorf("SB3 must be a regular file no larger than 1 GiB")
		}
		data, err := io.ReadAll(io.LimitReader(file, 1<<30+1))
		if err != nil {
			return nil, err
		}
		if len(data) > 1<<30 {
			return nil, fmt.Errorf("SB3 exceeds 1 GiB")
		}
		return data, nil
	}
	manifest, err := readManifest(absolute)
	if err != nil {
		return nil, err
	}
	p, err := Load(absolute)
	if err != nil {
		return nil, err
	}
	assets, err := ReadAssets(absolute, p)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	write := func(name string, data []byte) error {
		entry, err := archive.Create(name)
		if err != nil {
			return err
		}
		_, err = entry.Write(data)
		return err
	}
	if err = write("project.json", manifest); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(assets))
	for name := range assets {
		if name != "project.json" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err = write(name, assets[name]); err != nil {
			return nil, err
		}
	}
	if err = archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
