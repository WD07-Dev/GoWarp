package extensions

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"testing"

	"gowarp/core/extension"
)

func TestExampleHeaders(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "extensions", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "*.js"))
	if err != nil || len(files) == 0 {
		t.Fatalf("example files: %v", err)
	}
	for _, name := range files {
		t.Run(filepath.Base(name), func(t *testing.T) {
			js, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := extension.ExtractSource(name, js); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExampleCommentsMatchTestModules(t *testing.T) {
	for _, name := range []string{"penP", "text", "runtimeoptions", "jeremygamerTweening"} {
		t.Run(name, func(t *testing.T) {
			path, err := filepath.Abs(filepath.Join("..", "..", "extensions", "examples", name+".js"))
			if err != nil {
				t.Fatal(err)
			}
			js, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			source, err := extension.ExtractGo(name, js)
			if err != nil {
				t.Fatal(err)
			}
			source, err = format.Source(source)
			if err != nil {
				t.Fatal(err)
			}
			path, err = filepath.Abs(filepath.Join(name, "module.go"))
			if err != nil {
				t.Fatal(err)
			}
			module, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			_, module, _ = bytes.Cut(module, []byte("\n"))
			if !bytes.Equal(source, module) {
				t.Fatal("test module differs from the extension Go comment")
			}
		})
	}
}
