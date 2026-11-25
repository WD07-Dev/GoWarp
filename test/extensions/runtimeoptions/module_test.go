package extension

import (
	e "gowarp/core/extension"
	"testing"
)

func TestCloneLimitMenu(t *testing.T) {
	for _, b := range New().Blocks {
		if b.Opcode != "runtimeoptions_menu_clones" {
			continue
		}
		for _, value := range []string{"300", "Infinity"} {
			result, err := b.Handler(&e.Context{Fields: map[string]string{"clones": value}})
			if err != nil || result.Value != value {
				t.Fatalf("%s: %+v, %v", value, result, err)
			}
		}
		return
	}
	t.Fatal("clone limit menu is not registered")
}
