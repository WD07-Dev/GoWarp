package compiler

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gowarp/app/assets"
	"gowarp/app/config"
	"gowarp/app/i18n"
	"gowarp/core/project"
	"gowarp/platform/windows/resource"
)

const (
	packageMagic      = "GOWARP"
	packageFooterSize = len(packageMagic) + 16
)

var errPackageArchiveLimit = errors.New("project exceeds package limit")

type Template struct {
	validatorPath string
	Version       int    `json:"version"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	SHA256        string `json:"sha256"`
	SDKHash       string `json:"sdkSHA256,omitempty"`
	Path          string `json:"-"`
}

type Package struct {
	Name     string          `json:"name"`
	Settings config.Settings `json:"settings"`
	Project  []byte          `json:"-"`
}

/*
Compressed media gains little from ZIP deflate, but inflating it again can
stall the first playback or costume change. Only the packaged copy changes.
*/
func packageArchive(ctx context.Context, input string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := project.ArchiveBytes(input)
	if err != nil {
		return nil, err
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		/*
			Low-level packaging also accepts opaque payloads. Project validation
			stays with their callers; this pass only prepares readable ZIPs.
		*/
		return data, ctx.Err()
	}
	store := make([]bool, len(archive.File))
	manifests := make(map[*zip.File][]byte)
	size := int64(len(data))
	changed := false
	for i, entry := range archive.File {
		/*
			Match the asset reader's per-file limit. Do not expand oversized,
			unreferenced entries just because their names look like media.
		*/
		if entry.Method != zip.Deflate || entry.UncompressedSize64 > 64<<20 {
			continue
		}
		switch strings.ToLower(filepath.Ext(entry.Name)) {
		case ".png", ".jpg", ".jpeg", ".mp3", ".ogg":
			store[i] = true
			changed = true
			/*
				Allow for timestamp extras in both headers and a data descriptor
				without triggering a second large buffer allocation.
			*/
			size += 64
			if entry.UncompressedSize64 > entry.CompressedSize64 {
				size += int64(entry.UncompressedSize64 - entry.CompressedSize64)
			}
		}
	}
	if size <= 1<<30 {
		for _, entry := range archive.File {
			if entry.Name != "project.json" || entry.UncompressedSize64 > 64<<20 {
				continue
			}
			compressed, err := compressPackageManifest(ctx, entry)
			if err != nil {
				return nil, err
			}
			if compressed != nil {
				manifests[entry] = compressed
				size += int64(len(compressed)) - int64(entry.CompressedSize64)
				changed = true
			}
		}
	}
	if !changed || size > 1<<30 {
		return data, ctx.Err()
	}
	var output packageArchiveBuffer
	output.Grow(int(size))
	writer := zip.NewWriter(&output)
	if err = writer.SetComment(archive.Comment); err != nil {
		return nil, err
	}
	for i, entry := range archive.File {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if compressed, ok := manifests[entry]; ok {
			header := entry.FileHeader
			header.Method = zip.Deflate
			header.ReaderVersion = max(header.ReaderVersion, 20)
			header.CompressedSize64 = uint64(len(compressed))
			header.CompressedSize = uint32(len(compressed))
			destination, err := writer.CreateRaw(&header)
			if err != nil {
				if errors.Is(err, errPackageArchiveLimit) {
					return data, ctx.Err()
				}
				return nil, err
			}
			if _, err = destination.Write(compressed); err != nil {
				if errors.Is(err, errPackageArchiveLimit) {
					return data, ctx.Err()
				}
				return nil, err
			}
			continue
		}
		if !store[i] {
			if err = writer.Copy(entry); err != nil {
				if errors.Is(err, errPackageArchiveLimit) {
					return data, ctx.Err()
				}
				return nil, err
			}
			continue
		}
		header := entry.FileHeader
		header.Method = zip.Store
		destination, err := writer.CreateHeader(&header)
		if err != nil {
			if errors.Is(err, errPackageArchiveLimit) {
				return data, ctx.Err()
			}
			return nil, err
		}
		source, err := entry.Open()
		if err != nil {
			return nil, err
		}
		_, copyErr := io.Copy(destination, &contextReader{ctx: ctx, reader: source})
		closeErr := source.Close()
		if copyErr != nil {
			if errors.Is(copyErr, errPackageArchiveLimit) {
				return data, ctx.Err()
			}
			return nil, fmt.Errorf("prepare %s: %w", entry.Name, copyErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	if err = writer.Close(); err != nil {
		if errors.Is(err, errPackageArchiveLimit) {
			return data, ctx.Err()
		}
		return nil, err
	}
	return output.Bytes(), ctx.Err()
}

func compressPackageManifest(ctx context.Context, entry *zip.File) ([]byte, error) {
	source, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer source.Close()
	var compressed bytes.Buffer
	writer, err := flate.NewWriter(&compressed, flate.BestCompression)
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(writer, io.LimitReader(&contextReader{ctx: ctx, reader: source}, (64<<20)+1))
	closeErr := writer.Close()
	if err != nil {
		return nil, fmt.Errorf("compress project.json: %w", err)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if n > 64<<20 {
		return nil, fmt.Errorf("project.json exceeds 64 MiB")
	}
	if uint64(compressed.Len()) >= entry.CompressedSize64 {
		return nil, nil
	}
	return compressed.Bytes(), ctx.Err()
}

type packageArchiveBuffer struct {
	bytes.Buffer
}

func (b *packageArchiveBuffer) Write(data []byte) (int, error) {
	if len(data) > 1<<30-b.Len() {
		return 0, errPackageArchiveLimit
	}
	return b.Buffer.Write(data)
}

func Templates(dir string) ([]Template, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*", "player.json"))
	if err != nil {
		return nil, err
	}
	var result []Template
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var t Template
		if err = json.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		name := "player"
		if t.OS == "windows" {
			name += ".exe"
		}
		t.Path = filepath.Join(filepath.Dir(path), name)
		if t.Version != config.Version {
			continue
		}
		if _, err = os.Stat(t.Path); err == nil {
			result = append(result, t)
		}
	}
	return result, nil
}

func Export(ctx context.Context, t Template, input, output string, s config.Settings) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.Validate(); err != nil {
		return err
	}
	if t.Version != config.Version {
		return fmt.Errorf("runtime version mismatch")
	}
	switch t.OS {
	case "linux", "windows":
	default:
		return fmt.Errorf("unsupported runtime OS")
	}
	switch t.Arch {
	case "386", "amd64", "arm64":
	default:
		return fmt.Errorf("unsupported runtime architecture")
	}
	p, err := project.Load(input)
	if err != nil {
		return err
	}
	sources, err := projectSourcePaths(input, p)
	if err != nil {
		return err
	}
	sources = append(sources, t.Path)
	if exe, e := os.Executable(); e == nil {
		sources = append(sources, exe)
	}
	if err = protectOutput(output, sources); err != nil {
		return err
	}
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf(i18n.Tr("error.go_missing"), err)
	}
	extensions, err := resolveExtensionSources(ctx, input, p)
	if err != nil {
		return err
	}
	for _, extension := range extensions {
		if extension.Path != "" {
			sources = append(sources, extension.Path)
		}
	}
	if err = protectOutput(output, sources); err != nil {
		return err
	}
	t, err = extensionRuntime(ctx, t, extensions, output, p)
	if err != nil {
		return err
	}
	if err = protectOutput(output, []string{t.Path, t.validatorPath}); err != nil {
		return err
	}
	return writePackage(ctx, t, input, output, s)
}

func writePackage(ctx context.Context, t Template, input, output string, s config.Settings) error {
	if err := protectOutput(output, []string{input, t.Path}); err != nil {
		return err
	}
	file, err := os.Open(t.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != t.SHA256 {
		return fmt.Errorf("runtime checksum mismatch: %s", t.Path)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	archive, err := packageArchive(ctx, input)
	if err != nil {
		return err
	}
	if len(archive) > 1<<30 {
		return fmt.Errorf("project exceeds package limit")
	}
	s.Project = ""
	meta, err := json.Marshal(Package{Name: archiveName(input), Settings: s})
	if err != nil {
		return err
	}
	if len(meta) > config.MaxSettingsSize {
		return fmt.Errorf("settings exceed package limit")
	}
	if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(output), ".gowarp-export-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	if _, err = io.Copy(out, &contextReader{ctx: ctx, reader: file}); err != nil {
		return err
	}
	if t.OS == "windows" {
		icon := s.Player.Icon
		if len(icon) == 0 {
			icon = assets.DefaultIcon
		}
		if err := resource.AddWindowsIcon(out, icon); err != nil {
			return err
		}
	}
	if _, err = out.Write(archive); err != nil {
		return err
	}
	if _, err = out.Write(meta); err != nil {
		return err
	}
	footer := make([]byte, packageFooterSize)
	copy(footer, packageMagic)
	lengths := footer[len(packageMagic):]
	binary.LittleEndian.PutUint64(lengths, uint64(len(archive)))
	binary.LittleEndian.PutUint64(lengths[8:], uint64(len(meta)))
	if _, err = out.Write(footer); err != nil {
		return err
	}
	if err = out.Chmod(0755); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(out.Name(), output)
}

func ReadPackage(path string) (*Package, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	footerOffset := info.Size() - int64(packageFooterSize)
	if footerOffset < 0 {
		return nil, fmt.Errorf("no packaged project")
	}
	footer := make([]byte, packageFooterSize)
	if _, err = f.ReadAt(footer, footerOffset); err != nil {
		return nil, err
	}
	if string(footer[:len(packageMagic)]) != packageMagic {
		return nil, fmt.Errorf("no supported packaged project; export again with GoWarp")
	}
	lengths := footer[len(packageMagic):]
	size, metaSize := binary.LittleEndian.Uint64(lengths), binary.LittleEndian.Uint64(lengths[8:])
	if size == 0 || size > 1<<30 || metaSize == 0 || metaSize > config.MaxSettingsSize || size+metaSize > uint64(footerOffset) {
		return nil, fmt.Errorf("invalid package sizes")
	}
	meta := make([]byte, int(metaSize))
	metaOffset := footerOffset - int64(metaSize)
	if _, err = f.ReadAt(meta, metaOffset); err != nil {
		return nil, err
	}
	var p Package
	p.Settings.Loading = config.DefaultLoadingSettings()
	if err = json.Unmarshal(meta, &p); err != nil {
		return nil, err
	}
	if filepath.Base(p.Name) != p.Name || p.Name == "" || p.Name == "." {
		return nil, fmt.Errorf("invalid package name")
	}
	if err = p.Settings.Validate(); err != nil {
		return nil, err
	}
	p.Project = make([]byte, int(size))
	_, err = f.ReadAt(p.Project, metaOffset-int64(size))
	return &p, err
}
