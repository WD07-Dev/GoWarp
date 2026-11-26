package editorbridge

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEditorButtonInBrowser(t *testing.T) {
	var chromium string
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		path, err := exec.LookPath(name)
		if err == nil {
			chromium = path
			break
		}
	}
	if chromium == "" {
		t.Skip("Chrome/Chromium is not installed")
	}

	options := make(chan Options, 1)
	bridge, err := Start(context.Background(), func(ctx context.Context, path string, o Options) (<-chan error, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if string(data) != "test snapshot" {
			t.Error("browser sent the wrong project")
		}
		t.Log("preview requested", o)
		options <- o
		done := make(chan error, 1)
		go func() { <-ctx.Done(); done <- nil }()
		return done, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bridge.Close)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	page, err := template.New("editor").Parse(`<!doctype html>
<meta charset="utf-8">
<title>테스트 게임 - TurboWarp</title>
<div class="menu-bar_main-menu"></div>
<script>
class Events extends EventTarget {
    on(name, handler) { this.addEventListener(name, handler); }
    removeListener(name, handler) { this.removeEventListener(name, handler); }
    emit(name) { this.dispatchEvent(new Event(name)); }
}
const runtime = Object.assign(new Events(), {
    targets: [{id: "stage", isOriginal: true, blocks: {getScripts: () => []}}], stageWidth: 640, stageHeight: 480,
    turboMode: true, interpolationEnabled: true,
    frameLoop: {framerate: 60, running: false},
    extensionManager: {getExtensionURLs: () => ({})},
    toggleScript() {}, greenFlag() {}, stopAll() {}
});
const vm = Object.assign(new Events(), {
    runtime,
    toJSON: () => JSON.stringify({targets: [{isStage: true, name: "Stage", blocks: {}}], extensions: []}),
    serializeAssets: () => [],
    exports: {JSZip: class {
        file() {}
        async generateAsync() { return new Blob(["test snapshot"]); }
    }}
});
window.Scratch = {
    vm, translate: {language: "en"},
    BlockType: {BUTTON: "button", REPORTER: "reporter"},
    extensions: {unsandboxed: true, register() {}}
};
window.addEventListener("error", event => {
    document.body.dataset.testResult = event.message;
});
</script>
<script src="/editor.js"></script>
<script>
(async () => {
    try {
        const run = document.getElementById("gowarp-editor-button");
        run.click();
        const dialog = document.getElementById("gowarp-editor-dialog");
        if (!dialog.open) throw new Error("Run did not open connection settings");
        dialog.querySelector("input").value = {{.}};
        const connect = [...dialog.querySelectorAll("button")].find(button => button.textContent === "Connect");
        await connect.onclick();
        if (dialog.querySelector("pre").textContent !== "Ready") throw new Error(dialog.querySelector("pre").textContent);
        await run.onclick();
        if (dialog.open) throw new Error("Run did not close connection settings");
        document.body.dataset.testResult = "pass";
    } catch (error) {
        document.body.dataset.testResult = String(error);
    } finally {
        window.__gowarpEditor?.dispose();
    }
})();
</script>`)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Log("browser request", r.URL.Path)
		if r.URL.Path == "/editor.js" {
			http.ServeFile(w, r, filepath.Join(root, "extensions", "editor", "gowarp-editor.js"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := page.Execute(w, bridge.URL()); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, chromium, "--headless", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage", "--no-first-run", "--user-data-dir="+t.TempDir(), "--dump-dom", "--timeout=10000", "--virtual-time-budget=12000", server.URL)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), `data-test-result="pass"`) {
		t.Fatalf("editor interaction failed:\n%s", output)
	}
	select {
	case got := <-options:
		if got.Name != "테스트 게임" || got.FPS != 60 || got.Width != 640 || got.Height != 480 || !got.Turbo || !got.Interpolation {
			t.Fatalf("settings changed in transit: %+v", got)
		}
	default:
		t.Fatal("browser never requested a preview")
	}
}
