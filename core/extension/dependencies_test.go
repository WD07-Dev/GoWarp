package extension

import (
	"strings"
	"testing"
)

func TestExtractGoExternalDependencies(t *testing.T) {
	header := strings.Replace(validGoHeader, "@gowarp-go {", "@gowarp-config {\nrequire example.com/library v1.2.3\n}\n@gowarp-go {", 1)
	header = strings.Replace(header, "import \"gowarp/core/extension\"", "import \"gowarp/core/extension\"\nimport _ \"example.com/library/subpackage\"", 1)
	if _, err := ExtractGo("library.js", []byte(header)); err != nil {
		t.Fatal(err)
	}

	for name, source := range map[string]string{
		"undeclared":       strings.Replace(header, "require example.com/library v1.2.3\n", "", 1),
		"latest":           strings.Replace(header, "v1.2.3", "latest", 1),
		"branch":           strings.Replace(header, "v1.2.3", "main", 1),
		"partial version":  strings.Replace(header, "v1.2.3", "v1.2", 1),
		"prefix collision": strings.Replace(header, "library/subpackage", "library-other", 1),
		"local path":       strings.Replace(header, "example.com/library v1.2.3", "../library v1.2.3", 1),
		"extra argument":   strings.Replace(header, "v1.2.3", "v1.2.3 replace", 1),
		"runtime import":   strings.Replace(header, "example.com/library/subpackage", "gowarp/core/vm", 1),
		"cgo":              strings.Replace(header, "example.com/library/subpackage", "C", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ExtractGo("library.js", []byte(source)); err == nil {
				t.Fatal("accepted invalid dependency")
			}
		})
	}
}

func TestConfigRequirements(t *testing.T) {
	source := "require example.com/library v1.2.3\nrequire example.com/library v1.2.3\nrequire example.com/other/v2 v2.0.0-rc.1"
	requirements, err := parseRequirements("test", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 2 || requirements[0].Path != "example.com/library" || requirements[0].Version != "v1.2.3" {
		t.Fatalf("requirements = %+v", requirements)
	}
	conflict := strings.Replace(source, "library v1.2.3", "library v1.2.4", 1)
	if _, err := parseRequirements("test", conflict); err == nil {
		t.Fatal("accepted conflicting versions")
	}
	for _, version := range []string{"v0.0.0-20260519131909-c4659679e3fa", "v2.1.0+incompatible"} {
		_, err := parseRequirements("test", "require example.com/library "+version)
		if err != nil {
			t.Fatal(err)
		}
	}
}
