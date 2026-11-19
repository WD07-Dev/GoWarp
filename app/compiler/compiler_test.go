package compiler

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedRejectsMalformedFooter(t *testing.T) {
	for _, lengths := range [][2]uint64{{1, 0}, {1 << 63, 1}, {1, 1 << 63}, {0, 0}} {
		path := filepath.Join(t.TempDir(), "game")
		footer := make([]byte, 22)
		copy(footer, "GOWARP")
		binary.LittleEndian.PutUint64(footer[6:14], lengths[0])
		binary.LittleEndian.PutUint64(footer[14:22], lengths[1])
		if err := os.WriteFile(path, footer, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadEmbedded(path); err == nil {
			t.Fatalf("accepted corrupt footer %v", lengths)
		}
	}
}

func TestPackReplacesPreviousPayload(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "runtime")
	input := filepath.Join(dir, "test.sb3")
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	for path, data := range map[string]string{
		executable: "executable bytes",
		input:      "project bytes",
		first:      "",
		second:     "",
	} {
		if err := os.WriteFile(path, []byte(data), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := pack(context.Background(), executable, input, first, []string{"--headless", "2"}); err != nil {
		t.Fatal(err)
	}
	if err := pack(context.Background(), first, input, second, []string{"--headless", "2"}); err != nil {
		t.Fatal(err)
	}
	firstInfo, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstInfo.Size() != secondInfo.Size() {
		t.Fatal("repacking retained duplicate payload")
	}
	data, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 22 {
		t.Fatal("missing embedded footer")
	}
	footer := data[len(data)-22:]
	if string(footer[:6]) != "GOWARP" {
		t.Fatalf("unexpected embedded signature: %q", footer[:6])
	}
	projectSize := binary.LittleEndian.Uint64(footer[6:14])
	optionsSize := binary.LittleEndian.Uint64(footer[14:22])
	if projectSize != uint64(len("project bytes")) || optionsSize == 0 ||
		uint64(len(data)) != uint64(len("executable bytes"))+projectSize+optionsSize+22 {
		t.Fatalf("invalid embedded lengths: project=%d options=%d", projectSize, optionsSize)
	}
	embedded, err := ReadEmbedded(second)
	if err != nil {
		t.Fatal(err)
	}
	if embedded.Name != "test.sb3" || string(embedded.Project) != "project bytes" {
		t.Fatalf("bad embedded project: %+v", embedded)
	}
}
