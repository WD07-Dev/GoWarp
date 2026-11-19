package extension

import (
	"strings"
	"testing"
)

const blockHeader = `/*
@gowarp-config {
	require example.com/library v1.2.3
}
@gowarp-go {
	package extension
	import e "gowarp/core/extension"
	import _ "example.com/library/sub"
	func New() e.Extension {
		// A brace in a comment must not close the wrapper: }
		_ = "} @gowarp-config {"
		return e.Extension{ID: "fresh"}
	}
}
*/`

func TestExtractSourceBlocks(t *testing.T) {
	source, err := ExtractSource("fresh.js", []byte(blockHeader))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(source.Go), "package extension") || strings.Contains(string(source.Go), "require example.com") {
		t.Fatalf("config leaked into Go: %s", source.Go)
	}
	if len(source.Requirements) != 1 || source.Requirements[0].Version != "v1.2.3" {
		t.Fatalf("requirements = %+v", source.Requirements)
	}
	for name, body := range map[string]string{
		"raw string":       "_ = `}\n@gowarp-go {\n{`",
		"rune":             "_ = '}'",
		"escaped quote":    "_ = \"\\\"}\"",
		"nested composite": "_ = map[string]any{\"key\": []int{1, 2}}",
	} {
		t.Run(name, func(t *testing.T) {
			header := strings.Replace(blockHeader, `_ = "} @gowarp-config {"`, body, 1)
			if _, err := ExtractSource("fresh.js", []byte(header)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExtractSourceRejectsInvalidBlocks(t *testing.T) {
	for name, source := range map[string]string{
		"missing open":        strings.Replace(blockHeader, "@gowarp-go {", "@gowarp-go", 1),
		"missing close":       strings.Replace(blockHeader, "\n}\n*/", "\n*/", 1),
		"extra close":         strings.Replace(blockHeader, "\n}\n*/", "\n}\n}\n*/", 1),
		"duplicate go":        blockHeader + blockHeader,
		"duplicate config":    strings.Replace(blockHeader, "@gowarp-go {", "@gowarp-config {}\n@gowarp-go {", 1),
		"unknown config":      strings.Replace(blockHeader, "require example.com", "replace example.com", 1),
		"latest":              strings.Replace(blockHeader, "v1.2.3", "latest", 1),
		"trailing code":       strings.Replace(blockHeader, "\n}\n*/", "\n}\nfunc Other() {}\n*/", 1),
		"unterminated string": strings.Replace(blockHeader, `_ = "} @gowarp-config {"`, `_ = "}`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ExtractSource("fresh.js", []byte(source)); err == nil {
				t.Fatal("accepted invalid header")
			}
		})
	}
}
