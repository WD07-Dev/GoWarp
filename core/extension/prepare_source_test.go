package extension

import "testing"

func TestPrepareCallbackInExtensionSource(t *testing.T) {
	source, err := ExtractSource("library.js", []byte(`/*
@gowarp-config { require example.com/helper v1.2.3 }
@gowarp-go {
package extension
import e "gowarp/core/extension"
func New() e.Extension { return e.Extension{ID: "library", Prepare: func(p *e.Project) error { return nil }} }
}
*/`))
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Requirements) != 1 {
		t.Fatal("module requirement lost")
	}
}
