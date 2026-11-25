package extension

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBrowserBoundaryPreview(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is needed for the browser extension test")
	}
	script, err := filepath.Abs("preview_test.js")
	if err != nil {
		t.Fatal(err)
	}
	source, err := filepath.Abs(filepath.Join("..", "..", "..", "extensions", "examples", "gowarpWindows.js"))
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(node, script, source).CombinedOutput(); err != nil {
		t.Fatalf("browser preview: %v\n%s", err, output)
	}
}
