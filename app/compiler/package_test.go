package compiler

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gowarp/app/config"
)

func TestExportStoresCompressedAssets(t *testing.T) {
	dir := t.TempDir()
	var source bytes.Buffer
	writer := zip.NewWriter(&source)
	if err := writer.SetComment("keep archive metadata"); err != nil {
		t.Fatal(err)
	}
	names := []string{"project.json", "music.mp3", "sound.ogg", "image.png", "photo.jpg", "photo.JPEG", "sound.wav", "shape.svg", "extension.js"}
	contents := make(map[string][]byte)
	for _, name := range names {
		data := bytes.Repeat([]byte(name), 100)
		if name == "project.json" {
			data = []byte(`{"targets":[{"isStage":true,"name":"Stage","blocks":{}}]}`)
		}
		contents[name] = data
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Comment: "keep entry metadata"}
		header.SetMode(0644)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "source.sb3")
	if err := os.WriteFile(input, source.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	templatePath := filepath.Join(dir, "player")
	templateData := []byte("runtime template")
	if err := os.WriteFile(templatePath, templateData, 0755); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(templateData)
	tpl := Template{Version: config.Version, OS: "linux", Arch: "amd64", Path: templatePath, SHA256: hex.EncodeToString(hash[:])}
	output := filepath.Join(dir, "game")
	if err := writePackage(context.Background(), tpl, input, output, config.Default(nil)); err != nil {
		t.Fatal(err)
	}
	packaged, err := ReadPackage(output)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(packaged.Project), int64(len(packaged.Project)))
	if err != nil {
		t.Fatal(err)
	}
	if archive.Comment != "keep archive metadata" || len(archive.File) != len(names) {
		t.Fatal("archive metadata or entries changed")
	}
	for i, entry := range archive.File {
		if entry.Name != names[i] || entry.Comment != "keep entry metadata" || entry.Mode().Perm() != 0644 {
			t.Fatalf("entry metadata changed: %+v", entry.FileHeader)
		}
		method := uint16(zip.Deflate)
		if i >= 1 && i <= 5 {
			method = zip.Store
		}
		if entry.Method != method {
			t.Errorf("%s compression = %d, want %d", entry.Name, entry.Method, method)
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || !bytes.Equal(data, contents[entry.Name]) {
			t.Fatalf("%s content changed: %v", entry.Name, err)
		}
	}
	original, err := os.ReadFile(input)
	if err != nil || !bytes.Equal(original, source.Bytes()) {
		t.Fatalf("source archive changed: %v", err)
	}
	if err := os.WriteFile(input, packaged.Project, 0644); err != nil {
		t.Fatal(err)
	}
	again, err := packageArchive(context.Background(), input)
	if err != nil || !bytes.Equal(again, packaged.Project) {
		t.Fatalf("prepared archive was changed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := packageArchive(ctx, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled export: %v", err)
	}
}

func TestPackageArchiveRejectsCorruptMedia(t *testing.T) {
	for _, name := range []string{"music.mp3", "project.json"} {
		t.Run(name, func(t *testing.T) {
			var source bytes.Buffer
			writer := zip.NewWriter(&source)
			entry, err := writer.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write([]byte("media data")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			archive, err := zip.NewReader(bytes.NewReader(source.Bytes()), int64(source.Len()))
			if err != nil {
				t.Fatal(err)
			}
			archive.File[0].CRC32 ^= 1
			var damaged bytes.Buffer
			writer = zip.NewWriter(&damaged)
			if err := writer.Copy(archive.File[0]); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			input := filepath.Join(t.TempDir(), "damaged.sb3")
			if err := os.WriteFile(input, damaged.Bytes(), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := packageArchive(context.Background(), input); !errors.Is(err, zip.ErrChecksum) {
				t.Fatalf("corrupt media was accepted: %v", err)
			}
		})
	}
}

func TestPackageArchiveCompressesManifestLosslessly(t *testing.T) {
	manifest := []byte(`{"targets":[],"description":"` + strings.Repeat("repeated project metadata ", 4096) + `"}`)
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		t.Run(fmt.Sprint(method), func(t *testing.T) {
			var source bytes.Buffer
			writer := zip.NewWriter(&source)
			header := &zip.FileHeader{Name: "project.json", Method: method, Comment: "preserve metadata"}
			header.SetMode(0644)
			entry, err := writer.CreateHeader(header)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write(manifest); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if method == zip.Store {
				archive, err := zip.NewReader(bytes.NewReader(source.Bytes()), int64(source.Len()))
				if err != nil {
					t.Fatal(err)
				}
				archive.File[0].ReaderVersion = 10
				var legacy bytes.Buffer
				writer := zip.NewWriter(&legacy)
				if err := writer.Copy(archive.File[0]); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				source = legacy
			}
			path := filepath.Join(t.TempDir(), "project.sb3")
			if err := os.WriteFile(path, source.Bytes(), 0644); err != nil {
				t.Fatal(err)
			}
			packed, err := packageArchive(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			archive, err := zip.NewReader(bytes.NewReader(packed), int64(len(packed)))
			if err != nil {
				t.Fatal(err)
			}
			file := archive.File[0]
			if file.Method != zip.Deflate || file.ReaderVersion < 20 || file.Comment != header.Comment || file.Mode().Perm() != 0644 {
				t.Fatalf("manifest metadata changed: %+v", file.FileHeader)
			}
			reader, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			actual, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || !bytes.Equal(actual, manifest) {
				t.Fatalf("manifest changed: %v", err)
			}
			if method == zip.Store && len(packed) >= source.Len()/2 {
				t.Fatal("repeated manifest was not compressed")
			}
			if method == zip.Deflate && len(packed) > source.Len() {
				t.Fatal("compression increased the archive size")
			}
			if err := os.WriteFile(path, packed, 0644); err != nil {
				t.Fatal(err)
			}
			again, err := packageArchive(context.Background(), path)
			if err != nil || !bytes.Equal(again, packed) {
				t.Fatalf("compression is not stable: %v", err)
			}
		})
	}
}

func TestPackageArchiveKeepsCompressionWhenExpansionExceedsLimit(t *testing.T) {
	/*
		Declare large entries without allocating their contents. The preflight
		must leave this archive alone instead of attempting to inflate it.
	*/
	var source bytes.Buffer
	writer := zip.NewWriter(&source)
	for i := 0; i < 17; i++ {
		header := &zip.FileHeader{
			Name:               fmt.Sprintf("%d.png", i),
			Method:             zip.Deflate,
			UncompressedSize64: 64 << 20,
			CompressedSize64:   1,
		}
		entry, err := writer.CreateRaw(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte{0}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "large.sb3")
	if err := os.WriteFile(input, source.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	packaged, err := packageArchive(context.Background(), input)
	if err != nil || !bytes.Equal(packaged, source.Bytes()) {
		t.Fatalf("optional optimization changed an oversized archive: %v", err)
	}
}

func TestExportPackageRoundTripAndProtectSource(t *testing.T) {
	dir := t.TempDir()
	templatePath := filepath.Join(dir, "player")
	data := []byte("runtime template")
	if err := os.WriteFile(templatePath, data, 0755); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	tpl := Template{
		Version: config.Version,
		OS:      "linux",
		Arch:    "amd64",
		SHA256:  hex.EncodeToString(hash[:]),
		Path:    templatePath,
	}
	input, err := filepath.Abs("../../test/fixtures/projects/counter.sb3")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "game")
	s := config.Default(nil)
	s.FPS = 1.5
	s.Player.AutoStart = false
	loadingImage := image.NewNRGBA(image.Rect(0, 0, 640, 640))
	if _, err := rand.New(rand.NewSource(1)).Read(loadingImage.Pix); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, loadingImage); err != nil {
		t.Fatal(err)
	}
	s.Loading.Image = encoded.Bytes()
	s.Loading.Text = "Custom loading screen"
	if err := writePackage(context.Background(), tpl, input, out, s); err != nil {
		t.Fatal(err)
	}
	p, err := ReadPackage(out)
	if err != nil {
		t.Fatal(err)
	}
	if p.Settings.FPS != 1.5 || p.Settings.Player.AutoStart || len(p.Project) == 0 {
		t.Fatalf("bad payload: %+v", p.Settings)
	}
	if !bytes.Equal(p.Settings.Loading.Image, s.Loading.Image) || p.Settings.Loading.Text != s.Loading.Text {
		t.Fatal("custom loading screen lost in packaged executable")
	}
	packed, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	footer := packed[len(packed)-22:]
	if string(footer[:6]) != "GOWARP" || binary.LittleEndian.Uint64(footer[6:14]) != uint64(len(p.Project)) {
		t.Fatal("incorrect package signature or project length")
	}
	metaSize := binary.LittleEndian.Uint64(footer[14:])
	if uint64(len(packed)) != uint64(len(data)+len(p.Project)+22)+metaSize {
		t.Fatal("incorrect package metadata length")
	}
	if err := writePackage(context.Background(), tpl, input, input, s); err == nil {
		t.Fatal("overwrote source")
	}
	before, _ := os.ReadFile(out)
	tpl.SHA256 = "bad"
	if err := writePackage(context.Background(), tpl, input, out, s); err == nil {
		t.Fatal("accepted damaged template")
	}
	after, _ := os.ReadFile(out)
	if string(before) != string(after) {
		t.Fatal("failure changed output")
	}
}

func TestPackageWithoutLoadingUsesDefaults(t *testing.T) {
	settings, err := json.Marshal(config.Default(nil))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(settings, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "loading")
	meta, err := json.Marshal(map[string]any{"name": "game.sb3", "settings": fields})
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	data.WriteString("project")
	data.Write(meta)
	data.WriteString("GOWARP")
	if err := binary.Write(&data, binary.LittleEndian, uint64(len("project"))); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(&data, binary.LittleEndian, uint64(len(meta))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "game")
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := ReadPackage(path)
	if err != nil {
		t.Fatal(err)
	}
	defaults := config.DefaultLoadingSettings()
	if p.Settings.Loading.Background != defaults.Background || p.Settings.Loading.Text != defaults.Text {
		t.Fatal("package did not receive loading defaults")
	}
}
