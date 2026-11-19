package project

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

const maxAssetSize = 64 << 20
const maxAssetsSize = 256 << 20

// Indexed assets are read on demand; their combined size is not resident memory.
const maxIndexedAssetsSize = 1 << 30

type assetEntry struct {
	file *zip.File
	size int64
}

/*
AssetReader reads referenced assets on demand without retaining their bytes.
It is intended for a single caller; Read and Close must not run concurrently.
*/
type AssetReader struct {
	archive *zip.ReadCloser
	root    *os.Root
	assets  map[string]assetEntry
	closed  bool
}

/*
OpenAssets indexes and validates all referenced assets without reading their
contents. The caller must Close the reader when finished.
*/
func OpenAssets(path string, p *Project) (*AssetReader, error) {
	r := &AssetReader{assets: make(map[string]assetEntry)}
	for _, t := range p.Targets {
		for _, c := range t.Costumes {
			r.assets[AssetName(c.MD5Ext, c.AssetID, c.DataFormat)] = assetEntry{}
		}
		for _, s := range t.Sounds {
			r.assets[AssetName(s.MD5Ext, s.AssetID, s.DataFormat)] = assetEntry{}
		}
	}
	for _, font := range p.CustomFonts {
		if !font.System {
			r.assets[font.MD5Ext] = assetEntry{}
		}
	}
	delete(r.assets, "")
	for name := range r.assets {
		if strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
			return nil, fmt.Errorf("invalid asset name %q", name)
		}
	}
	absolute, root, _, err := unpackedLocation(path)
	if err != nil {
		return nil, err
	}
	valid := false
	defer func() {
		if !valid {
			r.Close()
		}
	}()
	var total uint64
	checkSize := func(name string, size uint64) error {
		if size > maxAssetSize {
			return fmt.Errorf("asset %q exceeds asset size limit", name)
		}
		if size > maxIndexedAssetsSize-total {
			return fmt.Errorf("asset %q exceeds archive asset size limit", name)
		}
		total += size
		return nil
	}
	if root != "" {
		r.root, err = os.OpenRoot(root)
		if err != nil {
			return nil, err
		}
		for name := range r.assets {
			info, err := r.root.Stat(name)
			if err != nil {
				return nil, fmt.Errorf("asset %q: %w", name, err)
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("asset %q is not a regular file", name)
			}
			if err := checkSize(name, uint64(info.Size())); err != nil {
				return nil, err
			}
			r.assets[name] = assetEntry{size: info.Size()}
		}
	} else {
		r.archive, err = zip.OpenReader(absolute)
		if err != nil {
			return nil, err
		}
		for _, file := range r.archive.File {
			entry, wanted := r.assets[file.Name]
			if !wanted {
				continue
			}
			if entry.file != nil {
				return nil, fmt.Errorf("duplicate asset %q", file.Name)
			}
			if err := checkSize(file.Name, file.UncompressedSize64); err != nil {
				return nil, err
			}
			r.assets[file.Name] = assetEntry{file: file, size: int64(file.UncompressedSize64)}
		}
		for name, entry := range r.assets {
			if entry.file == nil {
				return nil, fmt.Errorf("missing asset %q", name)
			}
		}
	}
	valid = true
	return r, nil
}

/*
Read returns a fresh byte slice for a referenced asset. Unpacked assets may
not grow beyond their indexed size, preserving the validated total limit.
*/
func (r *AssetReader) Read(name string) ([]byte, error) {
	if r.closed {
		return nil, fmt.Errorf("asset reader is closed")
	}
	entry, ok := r.assets[name]
	if !ok {
		return nil, fmt.Errorf("unreferenced asset %q", name)
	}
	var reader io.ReadCloser
	var err error
	if r.root != nil {
		file, openErr := r.root.Open(name)
		if openErr != nil {
			return nil, fmt.Errorf("asset %q: %w", name, openErr)
		}
		info, statErr := file.Stat()
		if statErr != nil {
			file.Close()
			return nil, statErr
		}
		if !info.Mode().IsRegular() || info.Size() > entry.size {
			file.Close()
			return nil, fmt.Errorf("asset %q is not a regular file within its indexed size", name)
		}
		reader = file
	} else {
		reader, err = entry.file.Open()
		if err != nil {
			return nil, err
		}
	}
	/*
		The index already bounds the asset size. Reserve once, including the
		EOF probe used by ReadFrom, instead of repeatedly copying growing data.
	*/
	buffer := bytes.NewBuffer(make([]byte, 0, int(entry.size)+bytes.MinRead))
	_, readErr := buffer.ReadFrom(io.LimitReader(reader, entry.size+1))
	data := buffer.Bytes()
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(data)) > entry.size {
		return nil, fmt.Errorf("asset %q exceeds its indexed size", name)
	}
	return data, nil
}

// Close releases the open archive or project directory. It is safe to repeat.
func (r *AssetReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.archive != nil {
		return r.archive.Close()
	}
	if r.root != nil {
		return r.root.Close()
	}
	return nil
}
