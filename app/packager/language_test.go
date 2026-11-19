package packager

import (
	"path/filepath"
	"testing"

	"gowarp/app/i18n"
)

func TestLanguagePreferencePreservesProjectText(t *testing.T) {
	previous := i18n.LanguageCode()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	i18n.SetLanguage("en")
	a := renderedApplication(t)
	a.languagePath = filepath.Join(t.TempDir(), "preferences.json")
	for i := range a.form.fields {
		if a.form.fields[i].key == "loading.text" {
			a.form.fields[i].value = "내 게임 loading"
		}
	}
	a.setLanguage("ko")
	if i18n.LanguageCode() != "ko" || a.errorText != "" {
		t.Fatalf("language selection failed: %s", a.errorText)
	}
	s, err := a.form.settings()
	if err != nil || s.Loading.Text != "내 게임 loading" {
		t.Fatalf("language selection modified project text: %+v, %v", s.Loading, err)
	}
	i18n.SetLanguage("en")
	if err := loadLanguage(a.languagePath); err != nil || i18n.LanguageCode() != "ko" {
		t.Fatalf("language was not restored: %s, %v", i18n.LanguageCode(), err)
	}
	if err := loadLanguage(filepath.Join(t.TempDir(), "missing.json")); err != nil || i18n.LanguageCode() != "en" {
		t.Fatalf("fresh editor did not default to English: %s, %v", i18n.LanguageCode(), err)
	}
}
