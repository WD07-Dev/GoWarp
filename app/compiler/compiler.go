// Package compiler builds standalone Go executables using the shared GoWarp VM.
package compiler

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gowarp/core/project"
)

const (
	magic      = "GOWARP"
	footerSize = len(magic) + 16
)

type Embedded struct {
	Project []byte
	Name    string
	Args    []string
}

type metadata struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

type Options struct {
	Project, Output string
	Args            []string
}

// ReadEmbedded returns nil when the executable has no attached project.
func ReadEmbedded(path string) (*Embedded, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	size, projectSize, optionsSize, err := payload(file)
	if err != nil || projectSize == 0 {
		return nil, err
	}
	data := make([]byte, projectSize)
	if _, err = file.ReadAt(data, size-projectSize-optionsSize-int64(footerSize)); err != nil {
		return nil, err
	}
	options := make([]byte, optionsSize)
	if _, err = file.ReadAt(options, size-optionsSize-int64(footerSize)); err != nil {
		return nil, err
	}
	var meta metadata
	if err = json.Unmarshal(options, &meta); err != nil {
		return nil, fmt.Errorf("embedded options: %w", err)
	}
	if filepath.Base(meta.Name) != meta.Name || meta.Name == "." || meta.Name == "" {
		return nil, fmt.Errorf("invalid embedded project name")
	}
	return &Embedded{
		Project: data,
		Name:    meta.Name,
		Args:    meta.Args,
	}, nil
}

func payload(file *os.File) (size, projectSize, optionsSize int64, err error) {
	info, err := file.Stat()
	if err != nil {
		return 0, 0, 0, err
	}
	size = info.Size()
	footerOffset := size - int64(footerSize)
	if footerOffset < 0 {
		return size, 0, 0, nil
	}
	footer := make([]byte, footerSize)
	if _, err = file.ReadAt(footer, footerOffset); err != nil {
		return
	}
	if string(footer[:len(magic)]) != magic {
		return size, 0, 0, nil
	}
	lengths := footer[len(magic):]
	p, o := binary.LittleEndian.Uint64(lengths), binary.LittleEndian.Uint64(lengths[8:])
	if p == 0 || p > 1<<30 || o > 4<<20 || p+o > uint64(footerOffset) {
		return size, 0, 0, fmt.Errorf("invalid embedded project footer")
	}
	return size, int64(p), int64(o), nil
}

/*
Build packages the current executable with a project. Failed or
canceled builds leave existing output intact.
*/
func Build(ctx context.Context, options Options) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	input, err := filepath.Abs(options.Project)
	if err != nil {
		return "", err
	}
	loaded, err := project.Load(input)
	if err != nil {
		return "", err
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	if options.Output == "" {
		options.Output = strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
		if runtime.GOOS == "windows" {
			options.Output += ".exe"
		}
	}
	output, err := filepath.Abs(options.Output)
	if err != nil {
		return "", err
	}
	sources, err := projectSourcePaths(input, loaded)
	if err != nil {
		return "", err
	}
	sources = append(sources, executable)
	if err = protectOutput(output, sources); err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return "", err
	}
	staged, err := os.CreateTemp(filepath.Dir(output), ".gowarp-build-*")
	if err != nil {
		return "", err
	}
	stagedPath := staged.Name()
	staged.Close()
	defer os.Remove(stagedPath)
	err = pack(ctx, executable, input, stagedPath, options.Args)
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	info, err := os.Stat(stagedPath)
	if err != nil {
		return "", err
	}
	if info.Size() == 0 {
		return "", fmt.Errorf("compiler produced an empty executable")
	}
	if err = os.Chmod(stagedPath, 0755); err != nil {
		return "", err
	}
	if err = os.Rename(stagedPath, output); err != nil {
		return "", err
	}
	return output, nil
}

func sameFile(a, b string) bool {
	if resolved, err := resolvedPath(a); err == nil {
		a = resolved
	}
	if resolved, err := resolvedPath(b); err == nil {
		b = resolved
	}
	if a == b {
		return true
	}
	ai, ae := os.Stat(a)
	bi, be := os.Stat(b)
	return ae == nil && be == nil && os.SameFile(ai, bi)
}

func within(path, dir string) bool {
	if resolved, err := resolvedPath(path); err == nil {
		path = resolved
	}
	if resolved, err := resolvedPath(dir); err == nil {
		dir = resolved
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pack(ctx context.Context, executable, input, output string, args []string) error {
	sources, err := projectSourcePaths(input, nil)
	if err != nil {
		return err
	}
	if err = protectOutput(output, append(sources, executable)); err != nil {
		return err
	}
	file, err := os.Open(executable)
	if err != nil {
		return err
	}
	defer file.Close()
	size, p, o, err := payload(file)
	if err != nil {
		return err
	}
	if p > 0 {
		size -= p + o + int64(footerSize)
	}
	data, err := packageArchive(ctx, input)
	if err != nil {
		return err
	}
	options, err := json.Marshal(metadata{Name: archiveName(input), Args: args})
	if err != nil {
		return err
	}
	if len(data) > 1<<30 || len(options) > 4<<20 {
		return fmt.Errorf("embedded project is too large")
	}
	out, err := os.OpenFile(output, os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err = io.CopyN(out, &contextReader{ctx: ctx, reader: file}, size); err != nil {
		return err
	}
	if _, err = out.Write(data); err != nil {
		return err
	}
	if _, err = out.Write(options); err != nil {
		return err
	}
	footer := make([]byte, footerSize)
	copy(footer, magic)
	lengths := footer[len(magic):]
	binary.LittleEndian.PutUint64(lengths, uint64(len(data)))
	binary.LittleEndian.PutUint64(lengths[8:], uint64(len(options)))
	if _, err = out.Write(footer); err != nil {
		return err
	}
	return out.Close()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func sourceRoot() (string, error) {
	var candidates []string
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	for _, start := range []string{cwd, filepath.Dir(exe)} {
		for dir := start; ; dir = filepath.Dir(dir) {
			candidates = append(candidates, dir)
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	for _, candidate := range candidates {
		mod, err := os.ReadFile(filepath.Join(candidate, "go.mod"))
		if err == nil && strings.HasPrefix(string(mod), "module gowarp\n") {
			if _, err = os.Stat(filepath.Join(candidate, "app", "player", "main.go")); err == nil {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("GoWarp source tree not found")
}

// Unpacked inputs acquire an SB3 suffix while retaining their save-file stem.
func archiveName(path string) string {
	name := filepath.Base(path)
	if strings.EqualFold(filepath.Ext(name), ".sb3") {
		return name
	}
	return strings.TrimSuffix(name, filepath.Ext(name)) + ".sb3"
}
