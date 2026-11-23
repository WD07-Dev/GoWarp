// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"strings"
	"unicode"
)

func New() e.Extension {
	return e.Extension{ID: "languageChecker", Blocks: []e.Block{
		{Opcode: "languageChecker_isEnglish", Kind: "boolean", Inputs: []string{"TEXT"}, Handler: func(c *e.Context) (e.Result, error) {
			text := c.String("TEXT")
			if strings.TrimFunc(text, jsWhitespace) == "" {
				return e.Result{Value: false}, nil
			}
			for _, r := range text {
				if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || jsWhitespace(r) || strings.ContainsRune(".,!?'\"-", r)) {
					return e.Result{Value: false}, nil
				}
			}
			return e.Result{Value: true}, nil
		}},
		{Opcode: "languageChecker_isKorean", Kind: "boolean", Inputs: []string{"TEXT"}, Handler: func(c *e.Context) (e.Result, error) {
			for _, r := range c.String("TEXT") {
				if r >= 0x3131 && r <= 0x3163 || r >= 0xac00 && r <= 0xd7a3 {
					return e.Result{Value: true}, nil
				}
			}
			return e.Result{Value: false}, nil
		}},
	}}
}

func jsWhitespace(r rune) bool { return r == '\ufeff' || r != '\u0085' && unicode.IsSpace(r) }
