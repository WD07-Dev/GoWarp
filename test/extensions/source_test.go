package extensions

import (
	"bytes"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
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
	root, err := filepath.Abs(filepath.Join("..", "..", "extensions", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "*.js"))
	if err != nil || len(files) == 0 {
		t.Fatalf("example files: %v", err)
	}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".js")
		t.Run(name, func(t *testing.T) {
			js, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			source, err := extension.ExtractGo(name, js)
			if err != nil {
				t.Fatal(err)
			}
			source, err = extensionCode(source)
			if err != nil {
				t.Fatal(err)
			}
			moduleName := name
			switch name {
			case "cubesterRichPresence":
				moduleName = "discord"
			case "Deltatime":
				moduleName = "dtbyxeroname"
			}
			path, err := filepath.Abs(filepath.Join(moduleName, "module.go"))
			if err != nil {
				t.Fatal(err)
			}
			module, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			module, err = extensionCode(module)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(source, module) {
				t.Fatal("test module differs from the extension Go comment")
			}
		})
	}
}

// Go block comments cannot be nested inside the JS header comment.
// Compare the parsed code so either comment style remains valid in test modules.
func extensionCode(source []byte) ([]byte, error) {
	positions := token.NewFileSet()
	file, err := parser.ParseFile(positions, "extension.go", source, 0)
	if err != nil {
		return nil, err
	}
	var code bytes.Buffer
	if err := format.Node(&code, positions, file); err != nil {
		return nil, err
	}
	return code.Bytes(), nil
}

func TestBundleRegistrations(t *testing.T) {
	bundle := Bundle()
	ids := make([]string, len(bundle))
	for i, factory := range bundle {
		ids[i] = factory().ID
	}
	if err := extension.Validate(bundle, ids, nil); err != nil {
		t.Fatal(err)
	}
}
