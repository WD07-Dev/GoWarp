package desktop

import (
	"context"
	"encoding/base64"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"gowarp/app/dialog"
)

// Called by the VM's file worker, never by the window or scheduler thread.
func pickFile(ctx context.Context, encoding string) (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	path, err := dialog.Select(ctx, dialog.Options{Title: "Open a file", Directory: directory})
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if encoding == "text" {
		return strings.ToValidUTF8(strings.TrimPrefix(string(data), "\xef\xbb\xbf"), "\ufffd"), nil
	}
	mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	mediaType, _, _ = strings.Cut(mediaType, ";")
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
