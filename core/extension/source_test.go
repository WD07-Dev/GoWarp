package extension

import (
	"strings"
	"testing"
)

const validGoHeader = `/*
@gowarp-go {
package extension

import "gowarp/core/extension"

func New() extension.Extension {
	return extension.Extension{ID: "fresh"}
}
}
*/`

func TestIgnoredExtensionHeader(t *testing.T) {
	for _, js := range []string{"/* @gowarp-ignore */\nvoid 0;", "// license\n/*\n@gowarp-ignore\n*/\n(function () {})();"} {
		source, err := ExtractSource("editor.js", []byte(js))
		if err != nil {
			t.Fatal(err)
		}
		if len(source.Go) != 0 || len(source.Requirements) != 0 {
			t.Fatal("ignored extension still supplies executable Go")
		}
	}
	for _, js := range []string{
		"const text = '/* @gowarp-ignore */';",
		"void 0; /* @gowarp-ignore */",
		"/* @gowarp-ignore-extra */",
		"/* @gowarp-ignore extra */",
		"/* @gowarp-ignore */" + validGoHeader,
		validGoHeader + "/* @gowarp-ignore */",
	} {
		if _, err := ExtractSource("editor.js", []byte(js)); err == nil {
			t.Fatalf("accepted invalid ignore header: %s", js)
		}
	}
}

func TestExtractGoHeader(t *testing.T) {
	for name, prefix := range map[string]string{
		"plain header":         "",
		"BOM and license text": "\ufeff // license\n/* license */\n",
	} {
		t.Run(name, func(t *testing.T) {
			source, err := ExtractGo("fresh.js", []byte(prefix+validGoHeader+"\n(function(){})()"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(source), "func New()") {
				t.Fatalf("extracted source is missing New():\n%s", source)
			}
		})
	}
}

func TestExtractGoRequiresExactMarker(t *testing.T) {
	for _, marker := range []string{"@gowarp-go extra", "@gowarp-go-example"} {
		t.Run(marker, func(t *testing.T) {
			header := strings.Replace(validGoHeader, "@gowarp-go", marker, 1)
			_, err := ExtractGo("fresh.js", []byte(header))
			if err == nil {
				t.Fatal("accepted a marker with extra text")
			}
		})
	}
}

func TestExtractGoRejectsFakeAndInvalidHeaders(t *testing.T) {
	for name, source := range map[string]string{
		"string":               "const x = `" + validGoHeader + "`;",
		"after statement":      "run();\n" + validGoHeader,
		"line comment":         "// @gowarp-go\nrun();",
		"invalid suffix":       strings.Replace(validGoHeader, "@gowarp-go", "@gowarp-go extra", 1),
		"multiple":             validGoHeader + validGoHeader,
		"syntax":               strings.Replace(validGoHeader, "func New()", "func New(", 1),
		"no factory":           strings.Replace(validGoHeader, "New()", "Other()", 1),
		"wrong factory result": strings.Replace(validGoHeader, "New() extension.Extension", "New() string", 1),
		"generic factory":      strings.Replace(validGoHeader, "New()", "New[T any]()", 1),
		"external import":      strings.Replace(validGoHeader, "gowarp/core/extension", "example.com/custom", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ExtractGo("fresh.js", []byte(source)); err == nil {
				t.Fatal("accepted invalid header")
			}
		})
	}
}

func TestExtractGoRespectsJSLineCommentTerminators(t *testing.T) {
	for name, terminator := range map[string]string{
		"LF":                  "\n",
		"CR":                  "\r",
		"CRLF":                "\r\n",
		"line separator":      "\u2028",
		"paragraph separator": "\u2029",
	} {
		t.Run(name, func(t *testing.T) {
			fake := "// license" + terminator + "const x = `\n" + validGoHeader + "\n`;"
			if _, err := ExtractGo("fake.js", []byte(fake)); err == nil {
				t.Fatal("extracted a header inside executable JavaScript after a line comment")
			}
			real := "// license" + terminator + validGoHeader + "\nvoid 0;"
			if _, err := ExtractGo("real.js", []byte(real)); err != nil {
				t.Fatalf("rejected a header following a terminated line comment: %v", err)
			}
		})
	}
}
