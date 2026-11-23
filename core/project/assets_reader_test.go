package project

import (
	"archive/zip"
	"bytes"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assetReaderMemoryFixture(t testing.TB) *AssetReader {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "large.bin"), bytes.Repeat([]byte{0x5a}, 4<<20), 0600); err != nil {
		t.Fatal(err)
	}
	p := &Project{Targets: []*Target{{Costumes: []Costume{{MD5Ext: "large.bin"}}}}}
	reader, err := OpenAssets(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	return reader
}

func TestAssetReaderAvoidsRepeatedBufferGrowth(t *testing.T) {
	reader := assetReaderMemoryFixture(t)
	allocations := testing.AllocsPerRun(10, func() {
		data, err := reader.Read("large.bin")
		if err != nil || len(data) != 4<<20 || data[0] != 0x5a || data[len(data)-1] != 0x5a {
			t.Fatalf("asset read: length=%d error=%v", len(data), err)
		}
	})
	if allocations > 16 {
		t.Fatalf("reading a known-size asset allocated %.0f objects; repeated buffer growth must be avoided", allocations)
	}
}

func BenchmarkAssetReaderLargeAsset(b *testing.B) {
	reader := assetReaderMemoryFixture(b)
	b.ReportAllocs()
	b.SetBytes(4 << 20)
	b.ResetTimer()
	for b.Loop() {
		data, err := reader.Read("large.bin")
		if err != nil || len(data) != 4<<20 {
			b.Fatalf("asset read: length=%d error=%v", len(data), err)
		}
	}
}

func TestAssetReaderPreservesStoredArchiveIntegrityChecks(t *testing.T) {
	for _, kind := range []string{"checksum", "truncated", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.sb3")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			payload := []byte("asset")
			header := &zip.FileHeader{
				Name: "shape.svg", Method: zip.Store, CRC32: crc32.ChecksumIEEE(payload),
				CompressedSize64: uint64(len(payload)), UncompressedSize64: uint64(len(payload)),
			}
			switch kind {
			case "checksum":
				header.CRC32++
			case "truncated":
				header.UncompressedSize64++
			case "oversized":
				header.UncompressedSize64--
			}
			z := zip.NewWriter(file)
			w, err := z.CreateRaw(header)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(payload); err != nil {
				t.Fatal(err)
			}
			if err := z.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			p := &Project{Targets: []*Target{{Costumes: []Costume{{MD5Ext: "shape.svg"}}}}}
			reader, err := OpenAssets(path, p)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			if _, err := reader.Read("shape.svg"); err == nil {
				t.Fatalf("accepted asset with %s corruption", kind)
			}
		})
	}
}

func TestAssetReader(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			dir := t.TempDir()
			path := dir
			if packed {
				path = filepath.Join(dir, "test.sb3")
				file, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				z := zip.NewWriter(file)
				for _, name := range []string{"shape.svg", "ignored.txt"} {
					w, err := z.Create(name)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = w.Write([]byte("<svg/>")); err != nil {
						t.Fatal(err)
					}
				}
				if err := z.Close(); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(filepath.Join(dir, "shape.svg"), []byte("<svg/>"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p := &Project{Targets: []*Target{{Costumes: []Costume{{MD5Ext: "shape.svg"}}}}}
			r, err := OpenAssets(path, p)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			for i := 0; i < 2; i++ {
				data, err := r.Read("shape.svg")
				if err != nil || string(data) != "<svg/>" {
					t.Fatalf("read: %q, %v", data, err)
				}
			}
			if _, err := r.Read("ignored.txt"); err == nil {
				t.Fatal("unreferenced read accepted")
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Read("shape.svg"); err == nil {
				t.Fatal("read after close accepted")
			}
		})
	}
}

func TestOpenAssetsValidatesArchiveIndex(t *testing.T) {
	for _, kind := range []string{"missing", "duplicate", "oversized", "total", "traversal"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.sb3")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(file)
			p := &Project{Targets: []*Target{{}}}
			count := 1
			if kind == "total" {
				count = maxIndexedAssetsSize/maxAssetSize + 1
			}
			for i := 0; i < count; i++ {
				name := fmt.Sprintf("%d.svg", i)
				if kind == "traversal" {
					name = "../shape.svg"
				}
				p.Targets[0].Costumes = append(p.Targets[0].Costumes, Costume{MD5Ext: name})
				if kind == "missing" {
					continue
				}
				size := uint64(0)
				if kind == "oversized" {
					size = 64<<20 + 1
				}
				if kind == "total" {
					size = 64 << 20
				}
				if _, err := z.CreateRaw(&zip.FileHeader{Name: name, UncompressedSize64: size}); err != nil {
					t.Fatal(err)
				}
				if kind == "duplicate" {
					if _, err := z.Create(name); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := z.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			if r, err := OpenAssets(path, p); err == nil {
				r.Close()
				t.Fatalf("accepted %s", kind)
			}
		})
	}
}

func TestOpenAssetsUnpackedTotalLimit(t *testing.T) {
	dir := t.TempDir()
	p := &Project{Targets: []*Target{{}}}
	for i := 0; i < maxIndexedAssetsSize/maxAssetSize+1; i++ {
		name := fmt.Sprintf("%d.svg", i)
		p.Targets[0].Costumes = append(p.Targets[0].Costumes, Costume{MD5Ext: name})
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(64 << 20); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if r, err := OpenAssets(dir, p); err == nil {
		r.Close()
		t.Fatal("accepted excessive total size")
	}
}

func TestAssetReaderDefersPayloadRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sb3")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(file)
	w, err := z.CreateRaw(&zip.FileHeader{
		Name: "shape.svg", Method: zip.Deflate,
		CompressedSize64: 1, UncompressedSize64: 6,
	})
	if err != nil {
		t.Fatal(err)
	}
	// This is an invalid deflate block: opening the index must not decode it.
	if _, err := w.Write([]byte{0xff}); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	p := &Project{Targets: []*Target{{Costumes: []Costume{{MD5Ext: "shape.svg"}}}}}
	r, err := OpenAssets(path, p)
	if err != nil {
		t.Fatalf("index read the corrupt payload: %v", err)
	}
	defer r.Close()
	if _, err := r.Read("shape.svg"); err == nil {
		t.Fatal("corrupt payload was accepted")
	}
}

func TestAssetReaderChecksUnpackedChanges(t *testing.T) {
	for _, kind := range []string{"growth", "symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "shape.svg")
			if err := os.WriteFile(path, []byte("<svg/>"), 0600); err != nil {
				t.Fatal(err)
			}
			p := &Project{Targets: []*Target{{Costumes: []Costume{{MD5Ext: "shape.svg"}}}}}
			r, err := OpenAssets(dir, p)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "growth":
				err = os.WriteFile(path, []byte("<svg>larger</svg>"), 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "symlink":
				outside := filepath.Join(t.TempDir(), "private.svg")
				if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(outside, path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.Read("shape.svg"); err == nil {
				t.Fatalf("accepted %s", kind)
			}
		})
	}
}

func TestLargeProjectAssetsStayLazy(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			dir := t.TempDir()
			path := dir
			p := &Project{Targets: []*Target{{Costumes: []Costume{{MD5Ext: "small.svg"}}}}}
			var writer *zip.Writer
			var file *os.File
			if packed {
				path = filepath.Join(dir, "large.sb3")
				var err error
				file, err = os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				writer = zip.NewWriter(file)
				entry, err := writer.Create("small.svg")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write([]byte("<svg/>")); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(dir, "small.svg"), []byte("<svg/>"), 0600); err != nil {
				t.Fatal(err)
			}
			// Index 320 MiB of other assets without allocating or reading them.
			for i := 0; i < 5; i++ {
				name := fmt.Sprintf("large%d.png", i)
				p.Targets[0].Costumes = append(p.Targets[0].Costumes, Costume{MD5Ext: name})
				if packed {
					if _, err := writer.CreateRaw(&zip.FileHeader{Name: name, UncompressedSize64: 64 << 20}); err != nil {
						t.Fatal(err)
					}
				} else {
					f, err := os.Create(filepath.Join(dir, name))
					if err != nil {
						t.Fatal(err)
					}
					err = f.Truncate(64 << 20)
					closeErr := f.Close()
					if err != nil {
						t.Fatal(err)
					}
					if closeErr != nil {
						t.Fatal(closeErr)
					}
				}
			}
			if packed {
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			}
			reader, err := OpenAssets(path, p)
			if err != nil {
				t.Fatalf("lazy reader rejected total asset size: %v", err)
			}
			defer reader.Close()
			data, err := reader.Read("small.svg")
			if err != nil || string(data) != "<svg/>" {
				t.Fatalf("small asset read: %q/%v", data, err)
			}
			if _, err := ReadAssets(path, p); err == nil || !strings.Contains(err.Error(), "memory limit") {
				t.Fatalf("eager read must enforce its memory budget before reading payloads: %v", err)
			}
		})
	}
}
