package compiler

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gowarp/core/extension"
	"gowarp/core/project"
)

/*
ConvertExtensions atomically embeds annotated JS, preserving blocks, assets and unknown fields.
Failed or canceled conversions leave the destination intact.
*/
func ConvertExtensions(ctx context.Context, input, output string, sources map[string][]byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	input, err := filepath.Abs(input)
	if err != nil {
		return err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return err
	}
	protected := []string{input}
	if executable, err := os.Executable(); err == nil {
		protected = append(protected, executable)
	}
	if err = protectOutput(output, protected); err != nil {
		return err
	}
	return rewriteExtensions(ctx, input, output, sources, true, nil)
}

func rewriteExtensions(ctx context.Context, input, output string, sources map[string][]byte, requireGo bool, revision *[32]byte) error {
	archive, err := zip.OpenReader(input)
	if err != nil {
		return err
	}
	defer archive.Close()
	var manifest *zip.File
	for _, entry := range archive.File {
		if entry.Name == "project.json" {
			if manifest != nil {
				return fmt.Errorf("duplicate project.json in archive")
			}
			manifest = entry
		}
	}
	if manifest == nil {
		return fmt.Errorf("project archive has no project.json")
	}
	reader, err := manifest.Open()
	if err != nil {
		return err
	}
	// Use the same manifest limit as project.Load, separate from the JS limit.
	const maxManifest = 64 << 20
	data, err := limitedSource(&contextReader{ctx: ctx, reader: reader}, maxManifest)
	reader.Close()
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return fmt.Errorf("project.json must contain a project object")
	}
	var p project.Project
	if err = json.Unmarshal(data, &p); err != nil {
		return err
	}
	declared := map[string]bool{}
	urls := p.ExtensionURLs
	if urls == nil {
		urls = map[string]string{}
	}
	for _, id := range p.Extensions {
		if declared[id] {
			return fmt.Errorf("duplicate extension ID %q", id)
		}
		declared[id] = true
		if id == "pen" {
			continue
		}
		source, ok := sources[id]
		if !ok {
			if requireGo {
				return fmt.Errorf("extension %s has no annotated JS source", id)
			}
			continue
		}
		if len(source) > extensionSourceLimit {
			return fmt.Errorf("extension %s exceeds 16 MiB", id)
		}
		if requireGo {
			if _, err = extension.ExtractGo(id+".js", source); err != nil {
				return err
			}
		}
		urls[id] = "data:application/javascript;base64," + base64.StdEncoding.EncodeToString(source)
	}
	for id := range sources {
		if !declared[id] || id == "pen" {
			return fmt.Errorf("source %s is not a declared custom extension", id)
		}
	}
	fields["extensionURLs"], err = json.Marshal(urls)
	if err != nil {
		return err
	}
	data, err = json.Marshal(fields)
	if err != nil {
		return err
	}
	if len(data) > maxManifest {
		return fmt.Errorf("converted project.json exceeds 64 MiB")
	}
	if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	staged, err := os.CreateTemp(filepath.Dir(output), ".gowarp-convert-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	writer := zip.NewWriter(&extensionContextWriter{ctx: ctx, writer: staged})
	defer writer.Close()
	for _, entry := range archive.File {
		if err = ctx.Err(); err != nil {
			return err
		}
		if entry != manifest {
			if err = writer.Copy(entry); err != nil {
				return err
			}
			continue
		}
		header := entry.FileHeader
		header.CRC32 = 0
		header.CompressedSize = 0
		header.CompressedSize64 = 0
		header.UncompressedSize = 0
		header.UncompressedSize64 = 0
		target, err := writer.CreateHeader(&header)
		if err != nil {
			return err
		}
		if _, err = target.Write(data); err != nil {
			return err
		}
	}
	if err = writer.Close(); err != nil {
		return err
	}
	if err = staged.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = archive.Close(); err != nil {
		return err
	}
	return replaceExtensionArchive(ctx, staged.Name(), output, revision)
}

type extensionContextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (w *extensionContextWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(data)
}
