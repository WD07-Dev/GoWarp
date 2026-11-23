package vm

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gowarp/core/extension"
	"gowarp/core/project"
)

/*
Dynamic assets are optional graphics capabilities so headless runtimes need
not allocate a renderer until a project actually requests pixels.
*/
type costumeEditor interface {
	AddCostume(int, project.Costume, []byte) (project.Costume, error)
	DeleteCostume(int, int) error
}

func (c *extensionContext) assetsCall(operation string, args []any) (any, error) {
	r := c.r
	arg := func(i int) string {
		if i < len(args) {
			return extension.String(args[i])
		}
		return ""
	}
	if operation == "assets.snapshot" {
		renderer, ok := r.graphicsHost().(interface {
			Snapshot([]DisplayTarget) ([]byte, error)
		})
		if !ok {
			return nil, fmt.Errorf("stage snapshot requires a graphics renderer")
		}
		data, err := renderer.Snapshot(r.Scene())
		if err != nil {
			return nil, err
		}
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	if c.th == nil || c.th.target == nil {
		return nil, fmt.Errorf("costume operation requires a target")
	}
	t := c.th.target
	editor, ok := r.graphicsHost().(costumeEditor)
	if !ok {
		return nil, fmt.Errorf("dynamic costumes require a graphics renderer")
	}
	switch operation {
	case "assets.addCostume":
		data, format, err := costumeURL(arg(0))
		if err != nil {
			return nil, err
		}
		name := arg(1)
		original := name
		for suffix := 2; ; suffix++ {
			found := false
			for _, costume := range t.source.Costumes {
				if costume.Name == name {
					found = true
					break
				}
			}
			if !found {
				break
			}
			name = fmt.Sprintf("%s%d", original, suffix)
		}
		meta, err := editor.AddCostume(t.sourceIndex, project.Costume{Name: name, DataFormat: format, BitmapResolution: 1}, data)
		if err != nil {
			return nil, err
		}
		t.source.Costumes = append(t.source.Costumes, meta)
		delete(r.costumeNames, t.source)
		setCostume(t, float64(len(t.source.Costumes)-1))
		return nil, nil
	case "assets.deleteCostume":
		index := -1
		for i, costume := range t.source.Costumes {
			if costume.Name == arg(0) {
				index = i
				break
			}
		}
		if index < 0 || len(t.source.Costumes) <= 1 {
			return nil, nil
		}
		if err := editor.DeleteCostume(t.sourceIndex, index); err != nil {
			return nil, err
		}
		t.source.Costumes = append(t.source.Costumes[:index], t.source.Costumes[index+1:]...)
		delete(r.costumeNames, t.source)
		for _, other := range r.targets {
			if other.source != t.source {
				continue
			}
			current := other.state.CurrentCostume
			if current > index || current >= len(t.source.Costumes) {
				current--
			}
			setCostume(other, float64(current))
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported asset operation %q", operation)
}

func costumeURL(source string) ([]byte, string, error) {
	const limit = 32 << 20
	var data []byte
	var mime string
	var err error
	if strings.HasPrefix(source, "data:") {
		header, payload, ok := strings.Cut(source[5:], ",")
		if !ok || len(payload) > limit*2 {
			return nil, "", fmt.Errorf("invalid or excessive costume data URL")
		}
		mime, _, _ = strings.Cut(header, ";")
		if strings.HasSuffix(header, ";base64") {
			data, err = base64.StdEncoding.DecodeString(payload)
		} else {
			var decoded string
			decoded, err = url.PathUnescape(payload)
			data = []byte(decoded)
		}
	} else {
		parsed, parseErr := url.Parse(source)
		if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, "", fmt.Errorf("costume URL must use data, http, or https")
		}
		response, fetchErr := (&http.Client{Timeout: 15 * time.Second}).Get(source)
		if fetchErr != nil {
			return nil, "", fetchErr
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, "", fmt.Errorf("costume download: HTTP %d", response.StatusCode)
		}
		mime, _, _ = strings.Cut(response.Header.Get("Content-Type"), ";")
		data, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
	}
	if err != nil {
		return nil, "", err
	}
	if len(data) > limit {
		return nil, "", fmt.Errorf("costume exceeds 32 MiB")
	}
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/svg+xml":
		return data, "svg", nil
	case "image/png", "image/jpeg", "image/gif", "image/bmp":
		return data, "png", nil
	}
	return nil, "", fmt.Errorf("unsupported costume MIME type %q", mime)
}
