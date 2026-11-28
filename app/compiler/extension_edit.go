package compiler

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"gowarp/app/assets"
	"gowarp/core/project"
)

type EditableExtension struct {
	ID, Source, Error string
}

type ExtensionDocument struct {
	Path     string
	Sources  []EditableExtension
	revision [32]byte
}

func OpenExtensionDocument(ctx context.Context, path string) (*ExtensionDocument, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	revision, err := extensionArchiveHash(ctx, path)
	if err != nil {
		return nil, err
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	var manifest []byte
	for _, file := range archive.File {
		if file.Name != "project.json" {
			continue
		}
		if manifest != nil {
			return nil, fmt.Errorf("duplicate project.json in archive")
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		manifest, err = readSourceWithLimit(&contextReader{ctx: ctx, reader: reader}, 64<<20)
		reader.Close()
		if err != nil {
			return nil, err
		}
	}
	var p project.Project
	if err = json.Unmarshal(manifest, &p); err != nil {
		return nil, err
	}
	doc := &ExtensionDocument{Path: path, revision: revision}
	seen := make(map[string]bool)
	for _, id := range p.Extensions {
		if seen[id] {
			return nil, fmt.Errorf("duplicate extension ID %q", id)
		}
		seen[id] = true
		if id == "pen" || assets.BuiltinExtensionSource(id) != nil {
			continue
		}
		entry := EditableExtension{ID: id}
		location := p.ExtensionURLs[id]
		if location == "" {
			entry.Error = fmt.Sprintf("extension %s has no JavaScript source", id)
		} else {
			source, _, err := readExtensionSource(ctx, path, location)
			switch {
			case err != nil:
				entry.Error = err.Error()
			case !utf8.Valid(source):
				entry.Error = fmt.Sprintf("extension %s is not UTF-8 text", id)
			default:
				entry.Source = string(source)
			}
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		doc.Sources = append(doc.Sources, entry)
	}
	return doc, nil
}

func (d *ExtensionDocument) Save(ctx context.Context, edits map[string]string) error {
	revision, err := extensionArchiveHash(ctx, d.Path)
	if err != nil {
		return err
	}
	if revision != d.revision {
		return fmt.Errorf("SB3 changed outside the editor; reopen it before saving")
	}
	sources := make(map[string][]byte, len(edits))
	for id, source := range edits {
		sources[id] = []byte(source)
	}
	if len(sources) == 0 {
		return nil
	}
	if err = rewriteExtensions(ctx, d.Path, d.Path, sources, false, &d.revision); err != nil {
		return err
	}
	for i := range d.Sources {
		if source, ok := edits[d.Sources[i].ID]; ok {
			d.Sources[i].Source = source
			d.Sources[i].Error = ""
		}
	}
	return nil
}

func extensionArchiveHash(ctx context.Context, path string) ([32]byte, error) {
	var digest [32]byte
	file, err := os.Open(path)
	if err != nil {
		return digest, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, &contextReader{ctx: ctx, reader: file}); err != nil {
		return digest, err
	}
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func replaceExtensionArchive(ctx context.Context, staged, output string, revision *[32]byte) error {
	var next [32]byte
	if revision != nil {
		var err error
		next, err = extensionArchiveHash(ctx, staged)
		if err != nil {
			return err
		}
		current, err := extensionArchiveHash(ctx, output)
		if err != nil {
			return err
		}
		if current != *revision {
			return fmt.Errorf("SB3 changed outside the editor; reopen it before saving")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(staged, output); err != nil {
		return err
	}
	if revision != nil {
		*revision = next
	}
	return nil
}
