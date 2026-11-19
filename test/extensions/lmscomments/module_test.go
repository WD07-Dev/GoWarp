package extension

import (
	e "gowarp/core/extension"
	"testing"
)

func TestCommentCEntersSubstack(t *testing.T) {
	for _, b := range New().Blocks {
		if b.Opcode == "lmscomments_commentC" {
			if b.Kind != "branch" {
				t.Fatalf("commentC kind = %q", b.Kind)
			}
			r, err := b.Handler(&e.Context{})
			if err != nil || r.Branch != "SUBSTACK" || r.Again {
				t.Fatalf("%#v %v", r, err)
			}
			return
		}
	}
	t.Fatal("missing commentC")
}
