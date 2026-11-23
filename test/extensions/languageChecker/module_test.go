package extension

import (
	e "gowarp/core/extension"
	"testing"
)

func TestCharacterClassification(t *testing.T) {
	tests := []struct {
		text            any
		english, korean bool
	}{
		{"Hello, world!", true, false}, {"?!-'\".", true, false}, {"", false, false}, {" \t\n\ufeff", false, false},
		{"Hello\u00a0world\u2028!", true, false}, {"Hello\u0085world", false, false}, {"A\ufeffB", true, false},
		{"Hello123", false, false}, {"café", false, false}, {"Hello 안녕", false, true}, {"ㄱㅣ", false, true},
		{"\u3130\u3164\ud7a4", false, false}, {"\u1100", false, false}, {123, false, false}, {true, true, false},
	}
	for _, tt := range tests {
		for _, block := range New().Blocks {
			want := tt.english
			if block.Opcode == "languageChecker_isKorean" {
				want = tt.korean
			}
			result, err := block.Handler(&e.Context{Args: map[string]any{"TEXT": tt.text}})
			if err != nil || result.Value != want {
				t.Errorf("%s(%q) = %v, %v; want %v", block.Opcode, tt.text, result.Value, err, want)
			}
		}
	}
}
